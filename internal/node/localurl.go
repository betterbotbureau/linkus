package node

import (
	"net"
	"net/netip"
)

// LocalURL is how a loopback listen address is shown to people: with
// "localhost" instead of 127.0.0.1 or ::1, which mean nothing to most
// owners. The node still listens on the literal loopback address; browsers
// treat localhost as this computer, never as a network name, so the
// settings page accepts both names.
func LocalURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr + "/"
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.IsLoopback() {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}
