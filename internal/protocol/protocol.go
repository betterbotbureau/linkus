// Package protocol defines the wire types and signing rules shared by the
// node and the Link.us service. Signatures use domain-separation prefixes so
// a signature produced for one purpose can never be replayed for another.
package protocol

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const (
	// registerContext prefixes the bytes signed when registering a key.
	registerContext = "linkus-register-v1:"
	// presenceContext prefixes the bytes signed when answering a presence
	// challenge.
	presenceContext = "linkus-presence-v1:"
	// offlineContext prefixes the bytes signed on offline-homepage uploads.
	offlineContext = "linkus-offline-v1:"

	// MaxBodyBytes caps HTTP request bodies accepted by the service.
	MaxBodyBytes = 4096
	// MaxWSMessageBytes caps WebSocket messages in both directions.
	MaxWSMessageBytes = 4096
	// ChallengeSize is the length in bytes of a presence challenge.
	ChallengeSize = 32
	// MaxOfflineBytes caps the offline homepage: the one
	// piece of user content Link.us stores, hard-capped and static.
	MaxOfflineBytes = 64 * 1024
	// MaxOfflineBodyBytes caps the upload HTTP body: the page in base64
	// plus JSON framing.
	MaxOfflineBodyBytes = MaxOfflineBytes*4/3 + 1024
	// OfflineTimestampSkew bounds how old or new an upload's timestamp may
	// be, limiting replay of captured requests.
	OfflineTimestampSkew = 10 * time.Minute
	// PingInterval is how often each side pings its presence connection.
	PingInterval = 30 * time.Second
	// IdleTimeout is how long the service keeps a presence connection
	// without a pong before closing it.
	IdleTimeout = 90 * time.Second
)

// CloseInUse is the WebSocket close code (application range) the service
// sends to a node that connects while another copy of the same node — the
// same identity, on another computer — is already connected and
// answering. The copy that runs keeps running; the newcomer stays
// offline until it is started again (after the other copy has quit).
const CloseInUse = 4001

// RegisterRequest is the body of POST /v1/nodes. The signature proves
// possession of the private key matching PublicKey.
type RegisterRequest struct {
	PublicKey string `json:"public_key"` // base64 (std encoding) Ed25519 public key
	Signature string `json:"signature"`  // base64 signature over RegisterSigningBytes
	// Challenge and Work answer a RegisterChallengeReply (regwork.go);
	// empty unless the service asked for them.
	Challenge string `json:"challenge,omitempty"`
	Work      string `json:"work,omitempty"`
}

// RegisterResponse is the reply to a successful registration.
type RegisterResponse struct {
	ID string `json:"id"`
}

// ReleaseInfo is the reply to GET /v1/release: the newest published node
// release, so nodes can tell their owners an update exists.
type ReleaseInfo struct {
	Latest string `json:"latest"` // a release tag such as "v1.2"
	URL    string `json:"url"`    // its release page
}

// NodeStatus is the reply to GET /v1/nodes/{id}. PublicKey lets the
// bootstrap shell verify the node's signed answers; it is
// public by construction, since registration binds the ID to the key.
type NodeStatus struct {
	ID        string    `json:"id"`
	Online    bool      `json:"online"`
	LastSeen  time.Time `json:"last_seen"`
	PublicKey string    `json:"public_key,omitempty"` // base64 Ed25519
	// Address is the node's canonical public URL, set by the service when
	// per-node subdomains are configured. Empty means the
	// client should fall back to the path form it can build itself.
	Address string `json:"address,omitempty"`
	// Suspended means the name is frozen for abuse.
	Suspended bool `json:"suspended,omitempty"`
}

// Presence WebSocket messages. The server opens with a challenge, the node
// answers with a signature, the server confirms with ok.
type PresenceMessage struct {
	Type      string `json:"type"`                // "challenge", "auth", "ok"
	Challenge string `json:"challenge,omitempty"` // base64, ChallengeSize bytes
	Signature string `json:"signature,omitempty"` // base64
}

// RegisterSigningBytes returns the exact bytes signed in a registration
// request: the context prefix followed by the raw public key.
func RegisterSigningBytes(pub ed25519.PublicKey) []byte {
	b := make([]byte, 0, len(registerContext)+len(pub))
	b = append(b, registerContext...)
	b = append(b, pub...)
	return b
}

// PresenceSigningBytes returns the exact bytes signed in answer to a
// presence challenge: the context prefix, the raw challenge, then the node
// ID. The challenge has a fixed length, so the concatenation is unambiguous.
func PresenceSigningBytes(challenge []byte, id string) []byte {
	b := make([]byte, 0, len(presenceContext)+len(challenge)+len(id))
	b = append(b, presenceContext...)
	b = append(b, challenge...)
	b = append(b, id...)
	return b
}

// OfflineRequest is the body of PUT and DELETE /v1/nodes/{id}/offline.
// Content carries the page for PUT and is empty for DELETE.
type OfflineRequest struct {
	Content   string `json:"content,omitempty"` // base64, decoded ≤ MaxOfflineBytes
	Timestamp string `json:"ts"`                // RFC 3339
	Signature string `json:"signature"`         // base64 over OfflineSigningBytes
}

// OfflineSigningBytes returns the exact bytes signed for an offline-page
// request: context, method ("PUT" or "DELETE"), node ID, timestamp, and the
// hex SHA-256 of the page content (of nothing, for DELETE), all
// colon-separated. The timestamp bounds replay; the method binds the
// signature to its effect.
func OfflineSigningBytes(method, id, ts string, content []byte) []byte {
	sum := sha256.Sum256(content)
	return []byte(offlineContext + method + ":" + id + ":" + ts + ":" + hex.EncodeToString(sum[:]))
}

// AnswerSigningBytes returns the exact bytes the node signs over a visit
// answer: a domain-separated line, the hex SHA-256 of the visitor's offer
// SDP, and the hex SHA-256 of the node's answer SDP, newline-separated.
// Hashing keeps the message fixed-size; the offer hash binds the signature
// to this one exchange (no replay); the answer hash covers the DTLS
// fingerprint, so a completed handshake proves the peer holds the key.
//
// The bootstrap shell mirrors this construction in JavaScript; the Go test
// vector in protocol_test.go is the shared reference.
func AnswerSigningBytes(offerSDP, answerSDP string) []byte {
	offer := sha256.Sum256([]byte(offerSDP))
	answer := sha256.Sum256([]byte(answerSDP))
	msg := "linkus answer v1\n" + hex.EncodeToString(offer[:]) + "\n" + hex.EncodeToString(answer[:])
	return []byte(msg)
}

// Encode marshals a presence message for the wire.
func (m PresenceMessage) Encode() ([]byte, error) { return json.Marshal(m) }

// DecodePresenceMessage parses a presence message, rejecting oversized input.
func DecodePresenceMessage(data []byte) (PresenceMessage, error) {
	var m PresenceMessage
	if len(data) > MaxWSMessageBytes {
		return m, ErrMessageTooLarge
	}
	err := json.Unmarshal(data, &m)
	return m, err
}

// ErrMessageTooLarge reports a message exceeding MaxWSMessageBytes.
var ErrMessageTooLarge = jsonError("message too large")

type jsonError string

func (e jsonError) Error() string { return string(e) }
