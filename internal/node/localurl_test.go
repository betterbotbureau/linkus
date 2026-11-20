package node

import (
	"testing"
)

func TestLocalURL(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"127.0.0.1:7465", "http://localhost:7465/"},
		{"[::1]:7465", "http://localhost:7465/"},
		{"localhost:7466", "http://localhost:7466/"},
		{"192.168.1.5:7465", "http://192.168.1.5:7465/"}, // never loopback-renamed
		{"no-port", "http://no-port/"},
	} {
		if got := LocalURL(tt.in); got != tt.want {
			t.Errorf("LocalURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
