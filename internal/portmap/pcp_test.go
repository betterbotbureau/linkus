package portmap

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// fakeGateway answers PCP/NAT-PMP datagrams on loopback. The handler gets
// each request and returns the reply to send (nil for silence).
type fakeGateway struct {
	conn net.PacketConn
	got  chan []byte
}

func startFakeGateway(t *testing.T, handler func(req []byte) []byte) *fakeGateway {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := &fakeGateway{conn: conn, got: make(chan []byte, 16)}
	mappingPort = conn.LocalAddr().(*net.UDPAddr).Port
	t.Cleanup(func() { conn.Close(); mappingPort = 5351 })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			req := append([]byte(nil), buf[:n]...)
			g.got <- req
			if resp := handler(req); resp != nil {
				conn.WriteTo(resp, addr) //nolint:errcheck // test fake
			}
		}
	}()
	return g
}

var gwAddr = netip.MustParseAddr("127.0.0.1")

// pcpOK builds a success response for a PCP MAP request.
func pcpOK(req []byte, extPort uint16, extIP netip.Addr, lifetime uint32) []byte {
	resp := make([]byte, 60)
	resp[0] = 2
	resp[1] = 0x81
	binary.BigEndian.PutUint32(resp[4:8], lifetime)
	copy(resp[24:36], req[24:36]) // nonce
	resp[36] = 17
	copy(resp[40:42], req[40:42]) // internal port
	binary.BigEndian.PutUint16(resp[42:44], extPort)
	ip16 := extIP.As16()
	copy(resp[44:60], ip16[:])
	return resp
}

func TestPCPMapAndDelete(t *testing.T) {
	ext := netip.MustParseAddr("203.0.113.9")
	g := startFakeGateway(t, func(req []byte) []byte {
		if req[0] != 2 || req[1] != 1 {
			t.Errorf("unexpected request % x", req[:2])
			return nil
		}
		return pcpOK(req, binary.BigEndian.Uint16(req[42:44]), ext, 7200)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := pcpOrNATPMP(ctx, gwAddr, 61000)
	if err != nil {
		t.Fatal(err)
	}
	if m.Protocol != "pcp" || m.ExternalIP != ext || m.ExternalPort != 61000 ||
		m.InternalPort != 61000 || m.Lifetime != 2*time.Hour {
		t.Fatalf("mapping = %s", m)
	}

	if err := m.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	<-g.got // the map request
	del := <-g.got
	if lifetime := binary.BigEndian.Uint32(del[4:8]); lifetime != 0 {
		t.Fatalf("delete sent lifetime %d, want 0", lifetime)
	}
	if port := binary.BigEndian.Uint16(del[42:44]); port != 0 {
		t.Fatalf("delete suggested external port %d, want 0", port)
	}
}

func TestPCPRefusalIsAnError(t *testing.T) {
	startFakeGateway(t, func(req []byte) []byte {
		resp := pcpOK(req, 0, netip.IPv4Unspecified(), 0)
		resp[3] = 2 // NOT_AUTHORIZED
		return resp
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := pcpOrNATPMP(ctx, gwAddr, 61000)
	if err == nil || !strings.Contains(err.Error(), "result 2") {
		t.Fatalf("err = %v, want a PCP refusal", err)
	}
}

func TestPCPNonceMismatchIsAnError(t *testing.T) {
	startFakeGateway(t, func(req []byte) []byte {
		resp := pcpOK(req, 61000, netip.MustParseAddr("203.0.113.9"), 7200)
		resp[24] ^= 0xff
		return resp
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pcpOrNATPMP(ctx, gwAddr, 61000); err == nil ||
		!strings.Contains(err.Error(), "nonce") {
		t.Fatalf("err = %v, want a nonce mismatch", err)
	}
}

// natpmpReply answers the two NAT-PMP requests.
func natpmpReply(req []byte, extIP [4]byte, extPort uint16, lifetime uint32) []byte {
	switch {
	case len(req) == 2 && req[1] == 0: // external address
		resp := make([]byte, 12)
		resp[1] = 128
		copy(resp[8:12], extIP[:])
		return resp
	case len(req) == 12 && req[1] == 1: // UDP mapping
		resp := make([]byte, 16)
		resp[1] = 129
		copy(resp[8:10], req[4:6]) // internal port
		binary.BigEndian.PutUint16(resp[10:12], extPort)
		binary.BigEndian.PutUint32(resp[12:16], lifetime)
		return resp
	}
	return nil
}

func TestNATPMPFallbackOnVersionZero(t *testing.T) {
	ext := [4]byte{203, 0, 113, 10}
	startFakeGateway(t, func(req []byte) []byte {
		if req[0] == 2 { // PCP request: answer UNSUPP_VERSION with version 0
			resp := make([]byte, 60)
			resp[1] = 0x81
			resp[3] = 1
			return resp
		}
		return natpmpReply(req, ext, 61000, 3600)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := pcpOrNATPMP(ctx, gwAddr, 61000)
	if err != nil {
		t.Fatal(err)
	}
	if m.Protocol != "nat-pmp" || m.ExternalIP != netip.AddrFrom4(ext) ||
		m.ExternalPort != 61000 || m.Lifetime != time.Hour {
		t.Fatalf("mapping = %s", m)
	}
}

func TestNATPMPFallbackOnSilence(t *testing.T) {
	ext := [4]byte{203, 0, 113, 11}
	startFakeGateway(t, func(req []byte) []byte {
		if req[0] == 2 {
			return nil // a pre-PCP router says nothing to version 2
		}
		return natpmpReply(req, ext, 61234, 3600)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := pcpOrNATPMP(ctx, gwAddr, 61000)
	if err != nil {
		t.Fatal(err)
	}
	if m.Protocol != "nat-pmp" || m.ExternalPort != 61234 {
		t.Fatalf("mapping = %s", m)
	}
}

func TestNATPMPRefusalIsAnError(t *testing.T) {
	startFakeGateway(t, func(req []byte) []byte {
		if req[0] == 2 {
			resp := make([]byte, 60)
			resp[1] = 0x81
			resp[3] = 1
			return resp
		}
		if len(req) == 2 {
			return natpmpReply(req, [4]byte{1, 2, 3, 4}, 0, 0)
		}
		resp := make([]byte, 16)
		resp[1] = 129
		binary.BigEndian.PutUint16(resp[2:4], 2) // refused
		return resp
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pcpOrNATPMP(ctx, gwAddr, 61000); err == nil ||
		!strings.Contains(err.Error(), "NAT-PMP result 2") {
		t.Fatalf("err = %v, want a NAT-PMP refusal", err)
	}
}

func TestTruncatedRepliesAreIgnored(t *testing.T) {
	calls := 0
	startFakeGateway(t, func(req []byte) []byte {
		if req[0] != 2 {
			return nil
		}
		calls++
		if calls == 1 {
			return []byte{2, 0x81, 0} // truncated: must be ignored, retried
		}
		return pcpOK(req, 61000, netip.MustParseAddr("203.0.113.9"), 7200)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := pcpOrNATPMP(ctx, gwAddr, 61000)
	if err != nil {
		t.Fatal(err)
	}
	if m.Protocol != "pcp" {
		t.Fatalf("mapping = %s", m)
	}
}
