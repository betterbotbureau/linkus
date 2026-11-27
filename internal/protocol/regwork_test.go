package protocol

import (
	"context"
	"testing"
	"time"
)

func TestRegisterWorkMeets(t *testing.T) {
	var sum [32]byte
	sum[0], sum[1] = 0x00, 0x1f // 11 zero bits
	for _, c := range []struct {
		bits int
		want bool
	}{{0, true}, {-1, true}, {8, true}, {11, true}, {12, false}, {257, false}} {
		if got := RegisterWorkMeets(sum, c.bits); got != c.want {
			t.Errorf("%d bits: %v, want %v", c.bits, got, c.want)
		}
	}
}

func TestSolveRegisterWork(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	w, err := SolveRegisterWork(context.Background(), "c", key, 12)
	if err != nil || !RegisterWorkMeets(RegisterWorkHash("c", key, w), 12) {
		t.Fatalf("%q %v", w, err)
	}
	for _, c := range []struct {
		ch   string
		bits int
	}{{"c", MaxRegisterDifficulty + 1}, {"c", -1}, {"", 4}, {string(make([]byte, MaxRegisterChallenge+1)), 4}} {
		if _, err := SolveRegisterWork(context.Background(), c.ch, key, c.bits); err == nil {
			t.Errorf("%d bits, challenge of %d bytes: no error", c.bits, len(c.ch))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := SolveRegisterWork(ctx, "c", key, MaxRegisterDifficulty); err == nil {
		t.Fatal("ran past its context")
	}
}
