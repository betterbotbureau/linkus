package protocol

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// A node's card: a small, signed description of a Link Node that other
// nodes and agents read before they write to it — who it says it is, what
// it is about, and what it accepts. The node sends it on a data channel
// labelled linkus-card-v1 (one text frame, then the visitor closes), so it
// needs no file in the public folder and no reserved path there.
//
// The field names follow the A2A agent card where the meaning is the same
// (name, description, url, skills), so agents that know those read it
// easily; it is not an A2A card, because a node does not speak A2A.
//
// The node signs the card with its own key, so a copy kept elsewhere (a
// contact list, later a directory) can still be checked against the key
// the Link service lists for the ID.
const (
	NodeCardProtocol = "linkus-card-v1"
	NodeCardKind     = "linkus-node-card"
	MaxNodeCardBytes = 8 << 10

	MaxCardNameRunes  = 80
	MaxCardAboutRunes = 200
)

// NodeCard is the card as it travels.
type NodeCard struct {
	V           int         `json:"v"`
	Kind        string      `json:"kind"`
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	URL         string      `json:"url,omitempty"`
	PublicKey   string      `json:"public_key"`
	Skills      []CardSkill `json:"skills"`
	Accepts     CardAccepts `json:"accepts"`
	Software    string      `json:"software,omitempty"`
	Updated     time.Time   `json:"updated"`
	Signature   string      `json:"signature,omitempty"`
}

// CardSkill is something the node offers, as an A2A skill describes one.
type CardSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	InputModes  []string `json:"input_modes,omitempty"`
}

// CardAccepts says who may message the node: "anyone", "contacts" or
// "nobody".
type CardAccepts struct {
	NodeMessages string `json:"node_messages"`
}

// SigningBytes is what the node signs: the card without its signature.
func (c NodeCard) SigningBytes() []byte {
	c.Signature = ""
	data, _ := json.Marshal(c)
	return append([]byte("linkus node card v1\n"), data...)
}

var errBadCard = errors.New("not a valid node card")

// DecodeNodeCard parses a card and checks its shape and its signature
// against pub (the key the Link service lists for the ID it claims).
func DecodeNodeCard(data []byte, wantID string, pub ed25519.PublicKey) (NodeCard, error) {
	var c NodeCard
	if len(data) > MaxNodeCardBytes || !utf8.Valid(data) {
		return c, errBadCard
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, errBadCard
	}
	if c.V != 1 || c.Kind != NodeCardKind || c.ID != wantID || !ValidNodeID(c.ID) ||
		utf8.RuneCountInString(c.Name) > MaxCardNameRunes ||
		utf8.RuneCountInString(c.Description) > MaxCardAboutRunes || len(c.Skills) > 20 {
		return c, errBadCard
	}
	if strings.ContainsFunc(c.Name+c.Description, func(r rune) bool { return r < ' ' && r != '\n' }) {
		return c, errBadCard
	}
	key, err := base64.StdEncoding.DecodeString(c.PublicKey)
	sig, err2 := base64.StdEncoding.DecodeString(c.Signature)
	if err != nil || err2 != nil || !ed25519.PublicKey(key).Equal(pub) || !ed25519.Verify(pub, c.SigningBytes(), sig) {
		return c, errors.New("the card's signature does not match the node's key")
	}
	return c, nil
}
