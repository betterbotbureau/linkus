package portmap

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
)

// PCP (RFC 6887, MAP opcode) and NAT-PMP (RFC 6886). Both talk to the
// default gateway on UDP 5351 with small fixed-format datagrams; NAT-PMP is
// PCP's predecessor and announces itself with version 0, which is the
// fallback signal. Every reply is hostile input: sizes, versions, opcodes,
// nonces and ports are all checked before anything is believed.

// mappingPort is the gateway port PCP and NAT-PMP use; a variable so tests
// can point at a fake gateway on loopback.
var mappingPort = 5351

const (
	protocolUDP  = 17
	pcpVersion   = 2
	pcpOpMap     = 1
	replyTimeout = 1 * time.Second
	attempts     = 3
)

var errTryNATPMP = errors.New("router does not speak PCP version 2")

// pcpOrNATPMP tries PCP and falls back to NAT-PMP.
func pcpOrNATPMP(ctx context.Context, gw netip.Addr, internalPort uint16) (*Mapping, error) {
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	m, err := pcpMap(ctx, gw, nonce, internalPort, internalPort, RequestedLifetime)
	if err == nil {
		return m, nil
	}
	if pmpErr := err; errors.Is(err, errTryNATPMP) || isTimeout(err) {
		m, err = natpmpMap(ctx, gw, internalPort, internalPort, RequestedLifetime)
		if err != nil {
			return nil, fmt.Errorf("pcp: %w; nat-pmp: %w", pmpErr, err)
		}
		return m, nil
	}
	return nil, fmt.Errorf("pcp: %w", err)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout() || errors.Is(err, errNoReply)
}

var errNoReply = errors.New("no reply from the gateway")

// exchange sends req to gw:5351 and returns the first reply of at least
// minResp bytes, retrying a few times.
func exchange(ctx context.Context, gw netip.Addr, req []byte, minResp int) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", net.JoinHostPort(gw.String(), fmt.Sprint(mappingPort)))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	buf := make([]byte, 1100) // the PCP maximum message size
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := conn.Write(req); err != nil {
			return nil, err
		}
		deadline := time.Now().Add(replyTimeout)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		conn.SetReadDeadline(deadline) //nolint:errcheck // deadline on UDP never fails
		n, err := conn.Read(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return nil, err
		}
		if n >= minResp {
			return append([]byte(nil), buf[:n]...), nil
		}
	}
	return nil, errNoReply
}

// --- PCP ------------------------------------------------------------------

// pcpMap requests (or with lifetime 0, deletes) one UDP mapping.
func pcpMap(ctx context.Context, gw netip.Addr, nonce [12]byte, internalPort, suggestPort uint16, lifetime time.Duration) (*Mapping, error) {
	clientIP, err := localIPToward(gw)
	if err != nil {
		return nil, err
	}

	req := make([]byte, 60)
	req[0] = pcpVersion
	req[1] = pcpOpMap
	binary.BigEndian.PutUint32(req[4:8], uint32(lifetime/time.Second))
	ip16 := clientIP.As16()
	copy(req[8:24], ip16[:])
	copy(req[24:36], nonce[:])
	req[36] = protocolUDP
	binary.BigEndian.PutUint16(req[40:42], internalPort)
	binary.BigEndian.PutUint16(req[42:44], suggestPort)
	// Suggested external IP: the "don't care" zero address, in the family
	// of the internal host (v4-mapped zero for IPv4).
	var zero netip.Addr
	if clientIP.Is4() {
		zero = netip.AddrFrom4([4]byte{})
	} else {
		zero = netip.IPv6Unspecified()
	}
	z16 := zero.As16()
	copy(req[44:60], z16[:])

	resp, err := exchange(ctx, gw, req, 60)
	if err != nil {
		return nil, err
	}
	if resp[0] != pcpVersion {
		return nil, errTryNATPMP
	}
	if resp[1] != pcpOpMap|0x80 {
		return nil, fmt.Errorf("unexpected opcode %#x", resp[1])
	}
	if code := resp[3]; code != 0 {
		if code == 1 { // UNSUPP_VERSION
			return nil, errTryNATPMP
		}
		return nil, fmt.Errorf("router refused the mapping (PCP result %d)", code)
	}
	if !bytes.Equal(resp[24:36], nonce[:]) {
		return nil, errors.New("reply nonce mismatch")
	}
	if binary.BigEndian.Uint16(resp[40:42]) != internalPort {
		return nil, errors.New("reply for a different internal port")
	}
	extPort := binary.BigEndian.Uint16(resp[42:44])
	extIP, ok := netip.AddrFromSlice(resp[44:60])
	if !ok {
		return nil, errors.New("unusable external address in reply")
	}
	granted := time.Duration(binary.BigEndian.Uint32(resp[4:8])) * time.Second

	m := &Mapping{
		Protocol:     "pcp",
		ExternalIP:   extIP.Unmap(),
		ExternalPort: extPort,
		InternalPort: internalPort,
		Lifetime:     granted,
	}
	m.renew = func(ctx context.Context) (*Mapping, error) {
		return pcpMap(ctx, gw, nonce, internalPort, extPort, RequestedLifetime)
	}
	m.delete = func(ctx context.Context) error {
		_, err := pcpMap(ctx, gw, nonce, internalPort, 0, 0)
		return err
	}
	return m, nil
}

// --- NAT-PMP ---------------------------------------------------------------

// natpmpMap requests (or with lifetime 0, deletes) one UDP mapping, asking
// for the external address first.
func natpmpMap(ctx context.Context, gw netip.Addr, internalPort, suggestPort uint16, lifetime time.Duration) (*Mapping, error) {
	// External address request: version 0, opcode 0.
	resp, err := exchange(ctx, gw, []byte{0, 0}, 12)
	if err != nil {
		return nil, err
	}
	if resp[0] != 0 || resp[1] != 128 {
		return nil, fmt.Errorf("unexpected address reply %#x/%#x", resp[0], resp[1])
	}
	if code := binary.BigEndian.Uint16(resp[2:4]); code != 0 {
		return nil, fmt.Errorf("router refused the address request (NAT-PMP result %d)", code)
	}
	extIP := netip.AddrFrom4([4]byte(resp[8:12]))

	req := make([]byte, 12)
	req[1] = 1 // UDP mapping
	binary.BigEndian.PutUint16(req[4:6], internalPort)
	binary.BigEndian.PutUint16(req[6:8], suggestPort)
	binary.BigEndian.PutUint32(req[8:12], uint32(lifetime/time.Second))

	resp, err = exchange(ctx, gw, req, 16)
	if err != nil {
		return nil, err
	}
	if resp[0] != 0 || resp[1] != 129 {
		return nil, fmt.Errorf("unexpected mapping reply %#x/%#x", resp[0], resp[1])
	}
	if code := binary.BigEndian.Uint16(resp[2:4]); code != 0 {
		return nil, fmt.Errorf("router refused the mapping (NAT-PMP result %d)", code)
	}
	if binary.BigEndian.Uint16(resp[8:10]) != internalPort {
		return nil, errors.New("reply for a different internal port")
	}
	extPort := binary.BigEndian.Uint16(resp[10:12])
	granted := time.Duration(binary.BigEndian.Uint32(resp[12:16])) * time.Second

	m := &Mapping{
		Protocol:     "nat-pmp",
		ExternalIP:   extIP,
		ExternalPort: extPort,
		InternalPort: internalPort,
		Lifetime:     granted,
	}
	m.renew = func(ctx context.Context) (*Mapping, error) {
		return natpmpMap(ctx, gw, internalPort, extPort, RequestedLifetime)
	}
	m.delete = func(ctx context.Context) error {
		req := make([]byte, 12)
		req[1] = 1
		binary.BigEndian.PutUint16(req[4:6], internalPort)
		// Delete: suggested external port and lifetime both zero.
		_, err := exchange(ctx, gw, req, 16)
		return err
	}
	return m, nil
}
