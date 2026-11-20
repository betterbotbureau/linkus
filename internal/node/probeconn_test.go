package node

import (
	"net"
	"testing"
	"time"

	"github.com/betterbotbureau/linkus/internal/protocol"
)

func TestProbeConnSwallowsProbesAndPassesTraffic(t *testing.T) {
	inner, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	pc := newProbeConn(inner)

	sender, err := net.Dial("udp", inner.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()

	nonce := "0123456789abcdef"
	hit := pc.Expect(nonce)

	// A probe with the wrong nonce, the right probe, and ordinary traffic.
	for _, payload := range []string{
		protocol.ProbeMagic + "ffffffffffffffff",
		protocol.ProbeMagic + nonce,
		"ordinary ICE-ish datagram",
	} {
		if _, err := sender.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}

	// ReadFrom must deliver only the ordinary datagram.
	buf := make([]byte, 256)
	inner.(*net.UDPConn).SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck // test
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "ordinary ICE-ish datagram" {
		t.Fatalf("ReadFrom delivered %q", buf[:n])
	}

	select {
	case <-hit:
	case <-time.After(5 * time.Second):
		t.Fatal("matching probe not detected")
	}
}
