package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/pion/stun/v4"

	"github.com/betterbotbureau/linkus/internal/portmap"
	"github.com/betterbotbureau/linkus/internal/protocol"
)

// Port mapping orchestration: obtain a router mapping
// for the WebRTC port, verify it — router IP against STUN, then a server
// probe for real inbound — and only then advertise it as an extra ICE
// candidate. Best effort throughout: every failure is a log line and a
// retry later, never a broken node.

const (
	mapAttemptTimeout = 30 * time.Second
	mapRetryDelay     = 30 * time.Minute
	probeSendRetries  = 15 // the presence connection may still be dialing
	probeSendGap      = 2 * time.Second
	probeWait         = 3 * time.Second
)

// RunPortMapping keeps one verified mapping alive until ctx ends, deleting
// it on shutdown. Run it on its own goroutine.
func RunPortMapping(ctx context.Context, w *WebRTC, stunServers []string, log *slog.Logger) {
	for ctx.Err() == nil {
		m := obtainVerified(ctx, w, stunServers, log)
		if m == nil {
			if !sleepCtx(ctx, mapRetryDelay) {
				return
			}
			continue
		}
		renewLoop(ctx, w, m, stunServers, log)
		w.SetExternalIP("")
		if ctx.Err() == nil && !sleepCtx(ctx, mapRetryDelay) {
			return
		}
	}
}

// obtainVerified maps, verifies and advertises once; nil means try later.
func obtainVerified(ctx context.Context, w *WebRTC, stunServers []string, log *slog.Logger) *portmap.Mapping {
	mctx, cancel := context.WithTimeout(ctx, mapAttemptTimeout)
	m, err := portmap.Map(mctx, uint16(w.Port()), log)
	cancel()
	if err != nil {
		log.Info("no port mapping available", "reason", err)
		return nil
	}
	log.Info("port mapping obtained", "mapping", m.String())
	if m.ExternalPort != uint16(w.Port()) {
		// Advertising needs the external port to equal the local one
		//; log and release.
		log.Info("router granted a different external port; not advertising",
			"wanted", w.Port(), "granted", m.ExternalPort)
		deleteMapping(m, log)
		return nil
	}
	if !verifyAndAdvertise(ctx, w, m, stunServers, log) {
		deleteMapping(m, log)
		return nil
	}
	return m
}

// renewLoop renews at half the lifetime until something changes or fails.
func renewLoop(ctx context.Context, w *WebRTC, m *portmap.Mapping, stunServers []string, log *slog.Logger) {
	for {
		wait := max(min(m.Lifetime/2, time.Hour), time.Minute)
		if !sleepCtx(ctx, wait) {
			deleteMapping(m, log)
			return
		}
		rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		m2, err := m.Renew(rctx)
		cancel()
		if err != nil {
			log.Info("port mapping renewal failed", "reason", err)
			return
		}
		if m2.ExternalIP != m.ExternalIP || m2.ExternalPort != m.ExternalPort {
			log.Info("port mapping moved on renewal, re-verifying", "mapping", m2.String())
			if m2.ExternalPort != uint16(w.Port()) || !verifyAndAdvertise(ctx, w, m2, stunServers, log) {
				deleteMapping(m2, log)
				return
			}
		}
		m = m2
	}
}

// verifyAndAdvertise runs the two verification steps from the plan and, on
// success, advertises the external IP.
func verifyAndAdvertise(ctx context.Context, w *WebRTC, m *portmap.Mapping, stunServers []string, log *slog.Logger) bool {
	stunIP, err := stunPublicIP(ctx, stunServers)
	if err != nil {
		log.Info("cannot verify mapping without STUN", "reason", err)
		return false
	}
	if stunIP != m.ExternalIP {
		log.Info("mapping not advertised: another NAT upstream",
			"router_reports", m.ExternalIP.String(), "stun_sees", stunIP.String())
		return false
	}

	var nb [8]byte
	if _, err := rand.Read(nb[:]); err != nil {
		return false
	}
	nonce := hex.EncodeToString(nb[:])
	hit := w.ExpectProbe(nonce)
	addr := netip.AddrPortFrom(m.ExternalIP, m.ExternalPort).String()

	// The presence connection may still be coming up; retry the request.
	sent := false
	for i := 0; i < probeSendRetries && ctx.Err() == nil; i++ {
		if err := w.SendSignal(protocol.SignalMessage{
			V:     protocol.SignalVersion,
			Type:  protocol.SignalProbe,
			Addr:  addr,
			Nonce: nonce,
		}); err == nil {
			sent = true
			break
		}
		if !sleepCtx(ctx, probeSendGap) {
			return false
		}
	}
	if !sent {
		log.Info("mapping not verified: could not reach the server for a probe")
		return false
	}

	wait := probeWait + time.Duration(protocol.ProbePackets)*protocol.ProbePacketSpacing
	select {
	case <-hit:
		w.SetExternalIP(m.ExternalIP.String())
		log.Info("port mapping verified and advertised", "addr", addr, "protocol", m.Protocol)
		return true
	case <-time.After(wait):
		log.Info("mapping not verified: probe packets did not arrive (inbound blocked)", "addr", addr)
		return false
	case <-ctx.Done():
		return false
	}
}

// stunPublicIP asks the first answering STUN server for our public IP, from
// an ephemeral socket (the IP is what matters; the WebRTC port is checked
// by the probe instead).
func stunPublicIP(ctx context.Context, servers []string) (netip.Addr, error) {
	var errs []error
	for _, s := range servers {
		ip, err := stunQuery(ctx, s)
		if err == nil {
			return ip, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", s, err))
	}
	if len(errs) == 0 {
		errs = append(errs, errors.New("no STUN servers configured"))
	}
	return netip.Addr{}, errors.Join(errs...)
}

func stunQuery(ctx context.Context, server string) (netip.Addr, error) {
	host := strings.TrimPrefix(server, "stun:")
	if !strings.Contains(host, ":") {
		host += ":3478"
	}
	u, err := url.Parse("stun://" + host) // validates the shape
	if err != nil || u.Port() == "" {
		return netip.Addr{}, fmt.Errorf("unusable STUN server %q", server)
	}
	var d net.Dialer
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := d.DialContext(qctx, "udp4", host)
	if err != nil {
		return netip.Addr{}, err
	}
	defer conn.Close()

	req := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	if _, err := conn.Write(req.Raw); err != nil {
		return netip.Addr{}, err
	}
	deadline, _ := qctx.Deadline()
	conn.SetReadDeadline(deadline) //nolint:errcheck // deadline on UDP never fails
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return netip.Addr{}, err
	}
	resp := &stun.Message{Raw: buf[:n]}
	if err := resp.Decode(); err != nil {
		return netip.Addr{}, err
	}
	var xa stun.XORMappedAddress
	if err := xa.GetFrom(resp); err != nil {
		return netip.Addr{}, err
	}
	ip, ok := netip.AddrFromSlice(xa.IP)
	if !ok {
		return netip.Addr{}, errors.New("unusable address from STUN")
	}
	return ip.Unmap(), nil
}

func deleteMapping(m *portmap.Mapping, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Delete(ctx); err != nil {
		log.Info("could not delete port mapping", "reason", err)
	}
}

// sleepCtx sleeps for d, reporting false when ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
