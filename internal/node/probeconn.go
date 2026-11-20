package node

import (
	"bytes"
	"net"
	"sync"

	"github.com/betterbotbureau/linkus/internal/protocol"
)

// probeConn wraps the node's WebRTC UDP socket so the server's
// mapping-verification probe packets can be observed.
// Packets carrying the probe magic never reach ICE — matching an expected
// nonce or not, they are hostile-until-proven input that Pion has no
// business parsing. Everything else passes through untouched.
type probeConn struct {
	net.PacketConn

	mu    sync.Mutex
	nonce string        // the one currently expected nonce, "" for none
	hit   chan struct{} // closed when the expected probe arrives
}

func newProbeConn(c net.PacketConn) *probeConn {
	return &probeConn{PacketConn: c}
}

// Expect arms the wrapper for one probe: the returned channel is closed
// when a probe datagram with this nonce arrives. A new Expect replaces any
// previous one.
func (p *probeConn) Expect(nonce string) <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nonce = nonce
	p.hit = make(chan struct{})
	return p.hit
}

func (p *probeConn) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, addr, err := p.PacketConn.ReadFrom(b)
		if err != nil || !p.swallow(b[:n]) {
			return n, addr, err
		}
	}
}

// swallow reports whether the datagram was a probe packet (and so must not
// reach ICE), closing the hit channel on a nonce match.
func (p *probeConn) swallow(b []byte) bool {
	if !bytes.HasPrefix(b, []byte(protocol.ProbeMagic)) {
		return false
	}
	got := string(b[len(protocol.ProbeMagic):])
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.nonce != "" && got == p.nonce {
		close(p.hit)
		p.nonce = ""
	}
	return true
}
