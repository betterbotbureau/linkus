package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pion/webrtc/v4"

	"github.com/betterbotbureau/linkus/internal/identity"
	"github.com/betterbotbureau/linkus/internal/protocol"
)

// The node's card (protocol.NodeCard): the owner gives a name and a short
// description in the app; the node adds its ID, address, key, what it
// accepts, signs it, and sends it to anyone who opens a linkus-card-v1
// channel. Other nodes fetch it with FetchNodeCard.

const (
	nodeCardFile = "node_card.json"
	// nodeCardSent records the card last sent to Link.us (a hash of its
	// content, its date left out), so it is sent again only when it
	// changes.
	nodeCardSent = "node_card_sent"
	// cardHold is how long a card channel may stay open: the card goes
	// out at once, and the visitor closes.
	cardHold = 30 * time.Second
)

// NodeCards keeps the owner's part of the card and builds the whole one.
type NodeCards struct {
	dataDir string
	id      *identity.Identity
	version string
	// ownerName is the name when the owner set none, while the node is
	// published: the site's title, else "Node <id>". Unpublished, the
	// default is "Node <id>", so a site's title stays private until it
	// goes online. nil: no card until the owner names the node.
	ownerName func() string
	// accepts says who may message the node: "anyone", "contacts" or
	// "nobody" (nil: nobody).
	accepts func() string

	mu   sync.Mutex
	kick chan struct{}
}

// cardSettings is what the owner sets.
type cardSettings struct {
	Name  string `json:"name"`
	About string `json:"about"`
}

// OpenNodeCards makes the card for this node. ownerName and accepts may
// be nil (see NodeCards).
func OpenNodeCards(dataDir string, id *identity.Identity, version string, ownerName, accepts func() string) *NodeCards {
	return &NodeCards{dataDir: dataDir, id: id, version: version, ownerName: ownerName, accepts: accepts, kick: make(chan struct{}, 1)}
}

// DataDir is the folder the card's settings live in.
func (n *NodeCards) DataDir() string { return n.dataDir }

// DefaultName is the name the card has while the owner set none: while
// the node is published, the site's title, else "Node <id>"; while
// unpublished, "Node <id>". "" when there is no default (or no ID yet).
func (n *NodeCards) DefaultName() string {
	if n.ownerName == nil || n.id == nil || n.id.NodeID == "" {
		return ""
	}
	if !LoadPublished(n.dataDir) {
		return "Node " + n.id.NodeID
	}
	return clipRunes(n.ownerName(), protocol.MaxCardNameRunes)
}

// PublishedName is the default name once the node is published.
func (n *NodeCards) PublishedName() string {
	if n.ownerName == nil {
		return ""
	}
	return clipRunes(n.ownerName(), protocol.MaxCardNameRunes)
}

// CardSettings returns what the owner set: the name and the description.
func (n *NodeCards) CardSettings() (name, about string) {
	s := n.settings()
	return s.Name, s.About
}

func (n *NodeCards) settings() cardSettings {
	var s cardSettings
	data, err := os.ReadFile(filepath.Join(n.dataDir, nodeCardFile))
	if err == nil {
		json.Unmarshal(data, &s) //nolint:errcheck // a damaged file reads as empty
	}
	return s
}

// Set saves the owner's name and description for the card.
func (n *NodeCards) Set(name, about string) error {
	name = strings.Join(strings.Fields(name), " ")
	about = strings.TrimSpace(about)
	if utf8.RuneCountInString(name) > protocol.MaxCardNameRunes {
		return fmt.Errorf("the name is too long (up to %d characters)", protocol.MaxCardNameRunes)
	}
	if utf8.RuneCountInString(about) > protocol.MaxCardAboutRunes {
		return fmt.Errorf("the description is too long (up to %d characters)", protocol.MaxCardAboutRunes)
	}
	if strings.ContainsFunc(name+about, func(r rune) bool { return r < ' ' && r != '\n' }) {
		return errors.New("the text holds control characters")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	s := n.settings()
	s.Name, s.About = name, about
	return n.save(s)
}

// save writes the settings and sends the card again; n.mu is held.
func (n *NodeCards) save(s cardSettings) error {
	data, _ := json.MarshalIndent(s, "", "  ")
	tmp := filepath.Join(n.dataDir, nodeCardFile+".tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(n.dataDir, nodeCardFile)); err != nil {
		return err
	}
	select { // send the new card to Link.us now
	case n.kick <- struct{}{}:
	default:
	}
	return nil
}

// cardHash is the card's content without its date and signature, which
// change on every build.
func cardHash(c protocol.NodeCard) string {
	c.Updated, c.Signature = time.Time{}, ""
	sum := sha256.Sum256(c.SigningBytes())
	return hex.EncodeToString(sum[:])
}

// Run keeps Link.us's copy of the card current, so agents that only speak
// HTTPS can read it at the node's address even while it is offline: the
// card is sent when it differs from the last one sent (checked every
// minute, and at once after the owner saves), and retried after a
// failure.
func (n *NodeCards) Run(ctx context.Context, server string, log *slog.Logger) {
	server = strings.TrimSuffix(server, "/")
	wait := time.Duration(0)
	select { // a first check at start, not a minute later
	case n.kick <- struct{}{}:
	default:
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.kick:
		case <-time.After(max(wait, time.Minute)):
		}
		if n.id.NodeID == "" {
			continue // not registered yet
		}
		card := n.Card()
		if card.Name == "" {
			continue // no card until the owner names the node
		}
		h := cardHash(card)
		sentPath := filepath.Join(n.dataDir, nodeCardSent)
		if old, _ := os.ReadFile(sentPath); strings.TrimSpace(string(old)) == h+" "+n.id.NodeID {
			wait = 0
			continue
		}
		if err := putCard(ctx, server, card); err != nil {
			log.Info("card not sent to Link.us yet", "error", err)
			wait = min(max(2*wait, 5*time.Minute), time.Hour)
			continue
		}
		os.WriteFile(sentPath, []byte(h+" "+n.id.NodeID+"\n"), 0o600) //nolint:errcheck // sent again next time
		log.Info("card sent to Link.us")
		wait = 0
	}
}

func putCard(ctx context.Context, server string, card protocol.NodeCard) error {
	data, err := json.Marshal(card)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, server+"/v1/nodes/"+card.ID+"/card", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("Link.us answered %s: %s", res.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// Card builds and signs the current card.
func (n *NodeCards) Card() protocol.NodeCard {
	s := n.settings()
	name := s.Name
	if name == "" {
		name = n.DefaultName()
	}
	from := "nobody"
	if n.accepts != nil {
		from = n.accepts()
	}
	c := protocol.NodeCard{
		V: 1, Kind: protocol.NodeCardKind,
		ID: n.id.NodeID, Name: name, Description: s.About,
		URL:       SavedAddress(n.dataDir),
		PublicKey: n.id.PublicKeyBase64(),
		Accepts:   protocol.CardAccepts{NodeMessages: from},
		Updated:   time.Now().UTC().Truncate(time.Second),
		Skills:    []protocol.CardSkill{},
	}
	if from != "nobody" {
		who := "Any Link Node"
		if from == "contacts" {
			who = "This node's contacts"
		}
		c.Skills = append(c.Skills, protocol.CardSkill{
			ID: "message", Name: "Message",
			Description: who + " can send this node's owner, or their AI agent, a message with files.",
			InputModes:  []string{"text/plain", "image/*", "application/pdf", "application/zip"},
		})
	}
	c.Signature = base64.StdEncoding.EncodeToString(n.id.Sign(c.SigningBytes()))
	return c
}

// SetNodeCard serves the card to anyone who opens a linkus-card-v1
// channel.
func (w *WebRTC) SetNodeCard(n *NodeCards) {
	w.HandleChannel(protocol.NodeCardProtocol, func(v *Visit, dc *webrtc.DataChannel) {
		v.stats.request("(card)")
		dc.OnOpen(func() {
			c := n.Card()
			data, err := json.Marshal(c)
			if err == nil && c.Name != "" { // no name, no card: the visitor sees the channel close
				dc.SendText(string(data)) //nolint:errcheck // the visitor retries
			}
			time.AfterFunc(cardHold, func() { dc.Close() })
		})
	})
}

func clipRunes(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

var titleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// SiteTitle is the <title> of the public folder's index.html, the name the
// chat widget shows visitors ("Message Alex's Place"), or "" when there is
// none. Only the first 64 KB are read.
func SiteTitle(publicDir string) string {
	f, err := os.Open(filepath.Join(publicDir, "index.html"))
	if err != nil {
		return ""
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, 64<<10))
	m := titleRE.FindSubmatch(data)
	if m == nil {
		return ""
	}
	t := strings.Join(strings.Fields(html.UnescapeString(string(m[1]))), " ")
	if t == "Your Link Node is running" { // the placeholder page
		return ""
	}
	return clipRunes(t, 60)
}

// OwnerNamer returns a cached SiteTitle lookup with a fallback, for the
// chat's "Message …" line.
func OwnerNamer(publicDir func() string, fallback func() string) func() string {
	var mu sync.Mutex
	var name string
	var at time.Time
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) > 30*time.Second {
			name, at = SiteTitle(publicDir()), time.Now()
		}
		if name != "" {
			return name
		}
		return fallback()
	}
}
