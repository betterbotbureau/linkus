package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/betterbotbureau/linkus/internal/protocol"
)

func TestPerAddressVisits(t *testing.T) {
	for total, want := range map[int]int{1: 1, 2: 1, 3: 1, 5: 2, 10: 4, 32: 12, 100: 40, 200: 80} {
		if got := PerAddressVisits(total); got != want {
			t.Errorf("PerAddressVisits(%d) = %d, want %d", total, got, want)
		}
	}
}

func TestAddressKey(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.7":              "203.0.113.7",
		" 203.0.113.7 ":            "203.0.113.7",
		"::ffff:203.0.113.7":       "203.0.113.7",
		"2001:db8:1:2:aaaa::1":     "2001:db8:1:2::/64",
		"2001:db8:1:2:bbbb:cc::99": "2001:db8:1:2::/64", // same home network
		"":                         "",
		"not an ip":                "",
		"203.0.113.7:4000":         "",
	} {
		if got := addressKey(in); got != want {
			t.Errorf("addressKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVisitSlots(t *testing.T) {
	v := newVisitSlots(5) // 2 per address
	for i := 0; i < 2; i++ {
		if ok, _ := v.take("a"); !ok {
			t.Fatalf("take a #%d refused", i+1)
		}
	}
	if ok, why := v.take("a"); ok || why != "address" {
		t.Fatalf("third from one address: %v %q, want refused by address", ok, why)
	}
	for _, addr := range []string{"b", "", ""} {
		if ok, _ := v.take(addr); !ok {
			t.Fatalf("take %q refused below the total", addr)
		}
	}
	if ok, why := v.take("c"); ok || why != "full" {
		t.Fatalf("sixth visit: %v %q, want refused as full", ok, why)
	}
	v.release("a")
	if ok, _ := v.take("a"); !ok {
		t.Fatal("a place freed by one address is usable again")
	}
	// Lowering the limit ends nothing; new visits wait for room.
	v.setMax(2)
	if ok, _ := v.take("d"); ok {
		t.Fatal("visit taken above a lowered limit")
	}
	for _, addr := range []string{"a", "a", "b", "", ""} {
		v.release(addr)
	}
	if v.open != 0 || len(v.byAddr) != 0 {
		t.Fatalf("after releasing all: open %d, byAddr %v", v.open, v.byAddr)
	}
	if total, per := v.limits(); total != 2 || per != 1 {
		t.Fatalf("limits %d/%d, want 2/1", total, per)
	}
}

func TestLoadMaxVisits(t *testing.T) {
	dir := t.TempDir()
	if n := LoadMaxVisits(dir); n != protocol.DefaultMaxVisits {
		t.Fatalf("missing file: %d, want the default", n)
	}
	for _, bad := range []string{"0", "201", "-3", "many", ""} {
		os.WriteFile(filepath.Join(dir, visitLimitFile), []byte(bad), 0o600)
		if n := LoadMaxVisits(dir); n != protocol.DefaultMaxVisits {
			t.Fatalf("file %q: %d, want the default", bad, n)
		}
	}
	if err := SaveMaxVisits(dir, 50); err != nil {
		t.Fatal(err)
	}
	if n := LoadMaxVisits(dir); n != 50 {
		t.Fatalf("saved 50, loaded %d", n)
	}
}
