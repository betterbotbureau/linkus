package protocol

// Registration work: when many new nodes register at once, the service
// may answer a new registration with a challenge instead of an ID. The
// node then finds a Work value whose hash, together with the challenge
// and its own public key, starts with Difficulty zero bits, and sends the
// registration again with both. Checking an answer costs the service one
// hash; finding one costs the node about 2^Difficulty hashes. An answer
// is tied to the node's key, so it cannot be reused for another node.

import (
	"context"
	"crypto/sha256"
	"errors"
	"math/bits"
	"strconv"
)

// RegisterChallengeReply is the body of a 429 reply to POST /v1/nodes.
// Challenge is empty when the service refuses outright (try again later).
type RegisterChallengeReply struct {
	Error      string `json:"error"`
	Challenge  string `json:"challenge,omitempty"`
	Difficulty int    `json:"difficulty,omitempty"` // leading zero bits
}

const (
	// MaxRegisterDifficulty is the most work a node does for a challenge;
	// a harder one is refused (it would take too long on a small device).
	MaxRegisterDifficulty = 28
	// MaxRegisterChallenge bounds a challenge's length.
	MaxRegisterChallenge = 128
	// MaxRegisterWork bounds the length of a Work value.
	MaxRegisterWork = 20
)

// RegisterWorkHash is the hash a Work value is judged by.
func RegisterWorkHash(challenge string, publicKey []byte, work string) [32]byte {
	h := sha256.New()
	h.Write([]byte("link register work\x00"))
	h.Write([]byte(challenge))
	h.Write([]byte{0})
	h.Write(publicKey)
	h.Write([]byte{0})
	h.Write([]byte(work))
	var sum [32]byte
	h.Sum(sum[:0])
	return sum
}

// RegisterWorkMeets reports whether sum starts with at least difficulty
// zero bits.
func RegisterWorkMeets(sum [32]byte, difficulty int) bool {
	if difficulty <= 0 {
		return true
	}
	if difficulty > 256 {
		return false
	}
	zeros := 0
	for _, b := range sum {
		if b != 0 {
			zeros += bits.LeadingZeros8(b)
			break
		}
		zeros += 8
	}
	return zeros >= difficulty
}

// SolveRegisterWork finds a Work value for the challenge. It stops when
// ctx ends.
func SolveRegisterWork(ctx context.Context, challenge string, publicKey []byte, difficulty int) (string, error) {
	if difficulty < 0 || difficulty > MaxRegisterDifficulty {
		return "", errors.New("the service asks for more registration work than this node does")
	}
	if challenge == "" || len(challenge) > MaxRegisterChallenge {
		return "", errors.New("invalid registration challenge")
	}
	buf := make([]byte, 0, MaxRegisterWork)
	for n := uint64(0); ; n++ {
		if n&0xffff == 0 && ctx.Err() != nil {
			return "", ctx.Err()
		}
		w := string(strconv.AppendUint(buf[:0], n, 10))
		if RegisterWorkMeets(RegisterWorkHash(challenge, publicKey, w), difficulty) {
			return w, nil
		}
	}
}
