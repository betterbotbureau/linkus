package protocol

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

// TestAnswerSigningBytesVector pins the exact construction. The bootstrap
// shell mirrors it in JavaScript (web/bootstrap/shell.js, verifyAnswer);
// any change here breaks deployed shells, so the vector is fixed.
func TestAnswerSigningBytesVector(t *testing.T) {
	got := string(AnswerSigningBytes("offer-sdp", "answer-sdp"))
	want := "linkus answer v1\n" +
		"7ac1b6c67de0af4c3f45160e17fd710af1f19e930a8d9cded639832ccd21038e\n" +
		"be5945a44bb6894459f1a68a673072b35253e2a912174e0c0c0e54b7bcffa1a1"
	if got != want {
		t.Fatalf("signing bytes:\n got %q\nwant %q", got, want)
	}
}

func TestAnswerSigningBytesBindsBothSides(t *testing.T) {
	base := AnswerSigningBytes("o", "a")
	if string(AnswerSigningBytes("o2", "a")) == string(base) {
		t.Fatal("offer not bound")
	}
	if string(AnswerSigningBytes("o", "a2")) == string(base) {
		t.Fatal("answer not bound")
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, base)
	if !ed25519.Verify(pub, base, sig) {
		t.Fatal("round trip failed")
	}
	if ed25519.Verify(pub, AnswerSigningBytes("o", "tampered"), sig) {
		t.Fatal("signature verified against a tampered answer")
	}
}

func TestValidProbeNonce(t *testing.T) {
	ok := strings.Repeat("a1", 8)
	if !ValidProbeNonce(ok) {
		t.Fatalf("%q rejected", ok)
	}
	for _, bad := range []string{"", "a1", strings.Repeat("A1", 8), strings.Repeat("g1", 8), ok + "0"} {
		if ValidProbeNonce(bad) {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestValidNodeID(t *testing.T) {
	for _, tt := range []struct {
		id   string
		want bool
	}{
		{"58310427", true},
		{"10000000", true},
		{"4k7x2mq9", true}, // letters, as issued if numbers ever run out
		{"zzzzzzzz", true},
		{"00000000", true},
		{"5831042", false},   // too short
		{"583104270", false}, // too long
		{"4K7X2MQ9", false},  // uppercase: addresses are lowercase
		{"4k7x2mi9", false},  // i reads like 1
		{"4k7x2ml9", false},  // l reads like 1
		{"4k7x2mo9", false},  // o reads like 0
		{"u5831042", true},   // u is an ID character like any other
		{"5831-427", false},
		{"", false},
	} {
		if got := ValidNodeID(tt.id); got != tt.want {
			t.Errorf("ValidNodeID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
	if len(NodeIDAlphabet) != 33 {
		t.Errorf("alphabet has %d characters, want 33", len(NodeIDAlphabet))
	}
}
