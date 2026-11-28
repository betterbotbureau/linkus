package node

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/betterbotbureau/linkus/internal/identity"
	"github.com/betterbotbureau/linkus/internal/protocol"
)

// The command-line node's card: none until the owner names the node, then
// signed, sent to Link.us, taking no messages.
func TestNodeCardCommandLine(t *testing.T) {
	dir := t.TempDir()
	id, _, err := identity.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	id.NodeID = "12345678"
	var mu sync.Mutex
	var sent [][]byte
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/nodes/12345678/card" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sent = append(sent, b)
		mu.Unlock()
	}))
	defer hub.Close()
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(sent) }

	cards := OpenNodeCards(dir, id, "v1.16", nil, nil)
	if c := cards.Card(); c.Name != "" {
		t.Fatalf("a card without a name: %+v", c)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cards.Run(ctx, hub.URL, slog.New(slog.NewTextHandler(io.Discard, nil)))
	time.Sleep(200 * time.Millisecond)
	if count() != 0 {
		t.Fatal("a card sent before the owner named the node")
	}

	if err := cards.Set("  Garden   sensors ", "Soil readings from the back garden."); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for count() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if count() == 0 {
		t.Fatal("card not sent after naming the node")
	}
	mu.Lock()
	body := sent[0]
	mu.Unlock()
	c, err := protocol.DecodeNodeCard(body, "12345678", id.PublicKey)
	if err != nil {
		t.Fatalf("sent card: %v", err)
	}
	if c.Name != "Garden sensors" || c.Accepts.NodeMessages != "nobody" || len(c.Skills) != 0 {
		t.Fatalf("card: %+v", c)
	}

	// A later edit is read from the settings file, so a change made by
	// another program (`linknode card`) counts.
	if err := cards.Set("Garden sensors", "Soil and air."); err != nil {
		t.Fatal(err)
	}
	if name, about := OpenNodeCards(dir, id, "v1.16", nil, nil).CardSettings(); name != "Garden sensors" || about != "Soil and air." {
		t.Fatalf("settings: %q %q", name, about)
	}
	for _, bad := range []string{strings.Repeat("x", protocol.MaxCardNameRunes+1), "a\x01b"} {
		if err := cards.Set(bad, ""); err == nil {
			t.Fatalf("name %q accepted", bad)
		}
	}
}

// The default card: "Node <id>" while unpublished, so a site's title stays
// private; once published, the site's title, else "Node <id>". The
// owner's own name counts at any time.
func TestNodeCardDefaultName(t *testing.T) {
	dataDir, publicDir := t.TempDir(), t.TempDir()
	id, _, err := identity.LoadOrCreate(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	id.NodeID = "12345678"
	title := func() string { return SiteTitle(publicDir) }
	namer := func() string {
		if s := title(); s != "" {
			return s
		}
		return "Node " + id.NodeID
	}
	cards := OpenNodeCards(dataDir, id, "v1.16", namer, nil)

	SavePublished(dataDir, false) // as a new node starts
	os.WriteFile(filepath.Join(publicDir, "index.html"), []byte("<title>Secret draft</title>"), 0o644)
	if c := cards.Card(); c.Name != "Node 12345678" {
		t.Fatalf("unpublished: card %q (the title must stay private)", c.Name)
	}
	os.Remove(filepath.Join(publicDir, "index.html"))
	if err := SavePublished(dataDir, true); err != nil {
		t.Fatal(err)
	}
	if c := cards.Card(); c.Name != "Node 12345678" {
		t.Fatalf("published, no title: %q", c.Name)
	}
	os.WriteFile(filepath.Join(publicDir, "index.html"), []byte("<title>July &amp; West</title>"), 0o644)
	if c := cards.Card(); c.Name != "July & West" || c.Description != "" {
		t.Fatalf("published, titled: %+v", c)
	}
	// The placeholder page's title is not a name.
	os.WriteFile(filepath.Join(publicDir, "index.html"), []byte("<title>Your Link Node is running</title>"), 0o644)
	if c := cards.Card(); c.Name != "Node 12345678" {
		t.Fatalf("placeholder page: %q", c.Name)
	}
	// Unpublished again: back to the ID; the owner's own name counts.
	SavePublished(dataDir, false)
	if c := cards.Card(); c.Name != "Node 12345678" {
		t.Fatalf("unpublished again: %q", c.Name)
	}
	if err := cards.Set("Garden sensors", ""); err != nil {
		t.Fatal(err)
	}
	if c := cards.Card(); c.Name != "Garden sensors" {
		t.Fatalf("owner's name while unpublished: %q", c.Name)
	}
}
