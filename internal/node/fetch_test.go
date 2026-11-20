package node

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/betterbotbureau/linkus/internal/protocol"
	"github.com/betterbotbureau/linkus/internal/site"
)

// fakeMsg is one message recorded by fakeChannel, in send order.
type fakeMsg struct {
	text bool
	data []byte
}

// fakeChannel implements fetchDataChannel in memory.
type fakeChannel struct {
	mu     sync.Mutex
	msgs   []fakeMsg
	closed bool
}

func (c *fakeChannel) Send(b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, fakeMsg{text: false, data: append([]byte(nil), b...)})
	return nil
}

func (c *fakeChannel) SendText(s string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.msgs = append(c.msgs, fakeMsg{text: true, data: []byte(s)})
	return nil
}

func (c *fakeChannel) BufferedAmount() uint64               { return 0 }
func (c *fakeChannel) SetBufferedAmountLowThreshold(uint64) {}
func (c *fakeChannel) OnBufferedAmountLow(func())           {}

func (c *fakeChannel) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *fakeChannel) snapshot() (msgs []fakeMsg, closed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]fakeMsg(nil), c.msgs...), c.closed
}

// waitMsgs polls until the channel holds at least n messages.
func (c *fakeChannel) waitMsgs(t *testing.T, n int) []fakeMsg {
	t.Helper()
	var msgs []fakeMsg
	waitFor(t, 5*time.Second, func() bool {
		msgs, _ = c.snapshot()
		return len(msgs) >= n
	}, "fetch responses")
	return msgs
}

func (c *fakeChannel) waitClosed(t *testing.T) {
	t.Helper()
	waitFor(t, 5*time.Second, func() bool {
		_, closed := c.snapshot()
		return closed
	}, "channel close")
}

// newTestFetchServer builds a site from files and a fetch server on a fake
// channel.
func newTestFetchServer(t *testing.T, files map[string][]byte) (*fetchServer, *fakeChannel) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err := site.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ch := &fakeChannel{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f := newFetchServer(ctx, ch, st, &visitStats{}, testLogger())
	return f, ch
}

func get(t *testing.T, f *fetchServer, path string) {
	t.Helper()
	data, err := protocol.FetchRequest{V: 1, Type: protocol.FetchGet, Path: path}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	f.handleMessage(true, data)
}

func decodeText(t *testing.T, m fakeMsg) map[string]any {
	t.Helper()
	if !m.text {
		t.Fatalf("expected a text message, got %d binary bytes", len(m.data))
	}
	var v map[string]any
	if err := json.Unmarshal(m.data, &v); err != nil {
		t.Fatalf("malformed response %q: %v", m.data, err)
	}
	return v
}

func TestFetchServesFile(t *testing.T) {
	content := []byte("<!doctype html><p>hello from the node</p>")
	f, ch := newTestFetchServer(t, map[string][]byte{"index.html": content})

	get(t, f, "/index.html")
	msgs := ch.waitMsgs(t, 3) // head, one chunk, done

	head := decodeText(t, msgs[0])
	if head["v"] != float64(1) || head["type"] != "head" || head["status"] != float64(200) {
		t.Fatalf("head = %v", head)
	}
	if head["path"] != "/index.html" || head["size"] != float64(len(content)) {
		t.Fatalf("head path/size = %v", head)
	}
	if ct, _ := head["content_type"].(string); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content_type = %q", ct)
	}
	if msgs[1].text || !bytes.Equal(msgs[1].data, content) {
		t.Fatalf("chunk mismatch")
	}
	if done := decodeText(t, msgs[2]); done["type"] != "done" {
		t.Fatalf("expected done, got %v", done)
	}
	if _, closed := ch.snapshot(); closed {
		t.Fatal("channel closed after a successful fetch")
	}
}

func TestFetchChunking(t *testing.T) {
	content := make([]byte, 2*protocol.FetchChunkSize+7000) // 3 chunks
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	f, ch := newTestFetchServer(t, map[string][]byte{"big.bin": content})

	get(t, f, "/big.bin")
	msgs := ch.waitMsgs(t, 5) // head, 3 chunks, done

	head := decodeText(t, msgs[0])
	if head["size"] != float64(len(content)) {
		t.Fatalf("head size = %v, want %d", head["size"], len(content))
	}
	var got []byte
	for _, m := range msgs[1:4] {
		if m.text {
			t.Fatalf("expected binary chunk, got %q", m.data)
		}
		if len(m.data) > protocol.FetchChunkSize {
			t.Fatalf("chunk of %d bytes exceeds the %d cap", len(m.data), protocol.FetchChunkSize)
		}
		got = append(got, m.data...)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("reassembled bytes differ from the file")
	}
	if done := decodeText(t, msgs[4]); done["type"] != "done" {
		t.Fatalf("expected done, got %v", done)
	}
}

func TestFetchEmptyFile(t *testing.T) {
	f, ch := newTestFetchServer(t, map[string][]byte{"empty.txt": {}})
	get(t, f, "/empty.txt")
	msgs := ch.waitMsgs(t, 2) // head, done — no chunks
	head := decodeText(t, msgs[0])
	if head["size"] != float64(0) {
		t.Fatalf("head size = %v, want 0", head["size"])
	}
	if done := decodeText(t, msgs[1]); done["type"] != "done" {
		t.Fatalf("expected done, got %v", done)
	}
}

func TestFetchUnsafePathsAre404(t *testing.T) {
	f, ch := newTestFetchServer(t, map[string][]byte{
		"index.html": []byte("x"),
		".hidden":    []byte("secret"),
		"CLAUDE.md":  []byte("instructions"),
	})

	paths := []string{"/missing.html", "/../identity.json", "/.hidden", "/CLAUDE.md", "x", ""}
	for i, p := range paths {
		get(t, f, p)
		msgs := ch.waitMsgs(t, i+1)
		m := decodeText(t, msgs[i])
		if m["type"] != "error" || m["status"] != float64(404) {
			t.Fatalf("path %q: got %v, want error 404", p, m)
		}
	}

	// The channel stays open and still serves safe requests.
	get(t, f, "/index.html")
	msgs := ch.waitMsgs(t, len(paths)+3)
	if head := decodeText(t, msgs[len(paths)]); head["type"] != "head" {
		t.Fatalf("channel unusable after 404s: %v", head)
	}
}

func TestFetchBadRequestsCloseChannel(t *testing.T) {
	tests := []struct {
		name string
		text bool
		data string
	}{
		{"binary request", false, `{"v":1,"type":"get","path":"/index.html"}`},
		{"malformed JSON", true, `{"v":1,`},
		{"unknown type", true, `{"v":1,"type":"post","path":"/index.html"}`},
		{"wrong version", true, `{"v":2,"type":"get","path":"/index.html"}`},
		{"oversized", true, `{"v":1,"type":"get","path":"/` + strings.Repeat("a", protocol.MaxFetchRequestBytes) + `"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ch := newTestFetchServer(t, map[string][]byte{"index.html": []byte("x")})
			f.handleMessage(tt.text, []byte(tt.data))
			ch.waitClosed(t)
			if msgs, _ := ch.snapshot(); len(msgs) != 0 {
				t.Fatalf("got %d responses to a bad request, want none", len(msgs))
			}
		})
	}
}
