package node

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/betterbotbureau/linkus/internal/identity"
	"github.com/betterbotbureau/linkus/internal/protocol"
)

func TestAccounts(t *testing.T) {
	id, _, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id.NodeID = "12345678"
	var got []protocol.WatchRequest
	accounts := []protocol.WatchEntry{{Email: "ops@example.com", Added: time.Now().UTC().Truncate(time.Second)}}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/nodes/12345678/accounts" {
			http.NotFound(w, r)
			return
		}
		var req protocol.WatchRequest
		json.NewDecoder(r.Body).Decode(&req)
		sig, _ := base64.StdEncoding.DecodeString(req.Signature)
		if !identity.Verify(id.PublicKey, protocol.WatchSigningBytes(req.Action, "12345678", req.Timestamp, req.WatchArg()), sig) {
			http.Error(w, `{"error":"bad signature"}`, http.StatusUnauthorized)
			return
		}
		got = append(got, req)
		if req.Action == "remove" {
			accounts = nil
		}
		out := map[string]any{"accounts": accounts}
		if req.Action == "code" {
			out["code"], out["expires"] = "K7QX-M2PA", time.Now().Add(15*time.Minute)
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer hub.Close()
	a := NewAccounts(hub.URL+"/", id, "v1.15", slog.New(slog.NewTextHandler(io.Discard, nil)))

	a.SetChildren(func() (int, int) { return 37, 40 })
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got[0].Children != "37/40" {
		t.Fatalf("children: %q", got[0].Children)
	}
	if st := a.State(); !st.Known || len(st.Accounts) != 1 || st.Accounts[0].Email != "ops@example.com" {
		t.Fatalf("state: %+v", st)
	}
	if got[0].Version != "v1.15" || got[0].Edition == "" {
		t.Fatalf("list request: %+v", got[0])
	}
	if err := a.Remove(context.Background(), "ops@example.com"); err != nil || len(a.State().Accounts) != 0 {
		t.Fatalf("remove: %v %+v", err, a.State())
	}

	// A code for adding this node, asked with the node's signature.
	code, exp, err := a.Code(context.Background())
	if err != nil || code != "K7QX-M2PA" || time.Until(exp) < 14*time.Minute || got[len(got)-1].Action != "code" {
		t.Fatalf("code: %q %v %v", code, exp, err)
	}

	// A refusal shows on the page.
	id.NodeID = "87654321"
	if err := a.Refresh(context.Background()); err == nil || a.State().Error == "" {
		t.Fatalf("refusal: %v %+v", err, a.State())
	}
	id.NodeID = ""
	if _, _, err := a.Code(context.Background()); err == nil {
		t.Fatal("a code before the node has an address")
	}
}
