// Package portmap asks the local router for a UDP port mapping, so visits
// can hole-punch against far more networks. PCP
// (RFC 6887) and NAT-PMP (RFC 6886) are implemented here — both are small
// fixed-format UDP exchanges with the default gateway — and UPnP IGD goes
// through goupnp. Everything is best effort: a mapping is a hint for ICE,
// never a requirement, and it must be verified before it is advertised.
package portmap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"time"

	"github.com/jackpal/gateway"
)

// RequestedLifetime is the mapping lifetime asked of the router. The node
// renews at half of whatever is granted.
const RequestedLifetime = 2 * time.Hour

// Mapping is one established router mapping for a UDP port.
type Mapping struct {
	Protocol     string // "pcp", "nat-pmp" or "upnp"
	ExternalIP   netip.Addr
	ExternalPort uint16
	InternalPort uint16
	Lifetime     time.Duration

	renew  func(ctx context.Context) (*Mapping, error)
	delete func(ctx context.Context) error
}

// Renew re-requests the mapping and returns the fresh result (the router
// may move the external address or port).
func (m *Mapping) Renew(ctx context.Context) (*Mapping, error) { return m.renew(ctx) }

// Delete releases the mapping at the router.
func (m *Mapping) Delete(ctx context.Context) error { return m.delete(ctx) }

func (m *Mapping) String() string {
	return fmt.Sprintf("%s %s:%d for :%d (%s)",
		m.Protocol, m.ExternalIP, m.ExternalPort, m.InternalPort, m.Lifetime)
}

// Map asks the default gateway for a UDP mapping of internalPort, trying
// PCP first, NAT-PMP second (same socket; a version-0 reply is the fallback
// signal) and UPnP in parallel. The first success wins.
func Map(ctx context.Context, internalPort uint16, log *slog.Logger) (*Mapping, error) {
	gw, err := gateway.DiscoverGateway()
	if err != nil {
		return nil, fmt.Errorf("no default gateway: %w", err)
	}
	gwAddr, ok := netip.AddrFromSlice(gw)
	if !ok {
		return nil, fmt.Errorf("unusable gateway address %v", gw)
	}
	gwAddr = gwAddr.Unmap()
	return mapVia(ctx, gwAddr, internalPort, log)
}

// mapVia races the PCP/NAT-PMP path against UPnP for the given gateway.
// Split from Map so tests can inject a fake gateway.
func mapVia(ctx context.Context, gw netip.Addr, internalPort uint16, log *slog.Logger) (*Mapping, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		m   *Mapping
		err error
	}
	results := make(chan result, 2)
	go func() {
		m, err := pcpOrNATPMP(ctx, gw, internalPort)
		results <- result{m, err}
	}()
	go func() {
		m, err := upnpMap(ctx, gw, internalPort, log)
		results <- result{m, err}
	}()

	var errs []error
	for i := 0; i < 2; i++ {
		select {
		case r := <-results:
			if r.err == nil {
				cancel() // abandon the slower path
				return r.m, nil
			}
			errs = append(errs, r.err)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, errors.Join(errs...)
}

// localIPToward returns the local address the OS would use to reach addr.
func localIPToward(addr netip.Addr) (netip.Addr, error) {
	c, err := net.Dial("udp", net.JoinHostPort(addr.String(), "5351"))
	if err != nil {
		return netip.Addr{}, err
	}
	defer c.Close()
	local, ok := netip.AddrFromSlice(c.LocalAddr().(*net.UDPAddr).IP)
	if !ok {
		return netip.Addr{}, errors.New("unusable local address")
	}
	return local.Unmap(), nil
}
