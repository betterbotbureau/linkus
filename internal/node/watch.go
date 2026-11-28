package node

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/betterbotbureau/linkus/internal/identity"
	"github.com/betterbotbureau/linkus/internal/protocol"
)

// Accounts are the link.us accounts that see this node (on
// link.us/account). The node joins one only when its owner shows a code
// here (asked from the service with the node's signature; one use, 15
// minutes) and types it on link.us. The node lists every account that
// added it and can remove any of them. An account only looks: it sees
// the node's status, never its content, and cannot act on it.
type Accounts struct {
	server   string
	id       *identity.Identity
	version  string
	log      *slog.Logger
	children func() (active, total int) // a manager's managed nodes; may be nil

	wake chan struct{} // a code was shown: watch for the new account

	mu    sync.Mutex
	list  []protocol.WatchEntry
	at    time.Time // when list was fetched
	err   string
	known bool
}

// NewAccounts prepares the accounts list for this node.
func NewAccounts(server string, id *identity.Identity, version string, log *slog.Logger) *Accounts {
	return &Accounts{server: strings.TrimSuffix(server, "/"), id: id, version: version, log: log.With("part", "accounts"),
		wake: make(chan struct{}, 1)}
}

// AccountsState is what the settings page shows.
type AccountsState struct {
	Accounts []protocol.WatchEntry `json:"accounts"`
	Known    bool                  `json:"known"` // fetched at least once
	At       time.Time             `json:"at"`
	Error    string                `json:"error,omitempty"`
	NodeID   string                `json:"node_id"`
}

func (a *Accounts) State() AccountsState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AccountsState{Accounts: append([]protocol.WatchEntry{}, a.list...), Known: a.known, At: a.at, Error: a.err, NodeID: a.id.NodeID}
}

// Code asks the service for a code that adds this node to the account
// it is typed into, replacing any earlier code.
func (a *Accounts) Code(ctx context.Context) (string, time.Time, error) {
	out, err := a.call(ctx, protocol.WatchRequest{Action: "code"})
	if err != nil {
		return "", time.Time{}, err
	}
	if protocol.NormalizeWatchCode(out.Code) == "" {
		return "", time.Time{}, errors.New("link.us sent no code")
	}
	a.log.Info("showing a code to add this node to a link.us account")
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return out.Code, out.Expires, nil
}

// SetChildren lets a manager report how many of the nodes it manages are
// active (counts only).
func (a *Accounts) SetChildren(f func() (active, total int)) {
	a.mu.Lock()
	a.children = f
	a.mu.Unlock()
}

// updateEdition is the edition this program's releases belong to.
func updateEdition() string {
	if ReleaseEdition == "" {
		return "desktop"
	}
	return "cli"
}

// Refresh asks the service which accounts watch this node; it is also
// this node's check-in: its version, and a manager's child counts, shown
// to those accounts, and that it is running even when not published.
func (a *Accounts) Refresh(ctx context.Context) error {
	req := protocol.WatchRequest{Action: "list", Version: a.version, Edition: updateEdition()}
	a.mu.Lock()
	f := a.children
	a.mu.Unlock()
	if f != nil {
		if active, total := f(); total > 0 {
			req.Children = fmt.Sprintf("%d/%d", active, total)
		}
	}
	_, err := a.call(ctx, req)
	return err
}

// Remove takes an account off this node's watchers.
func (a *Accounts) Remove(ctx context.Context, email string) error {
	_, err := a.call(ctx, protocol.WatchRequest{Action: "remove", Email: email})
	if err == nil {
		a.log.Info("removed a link.us account from this node", "email", email)
	}
	return err
}

type watchReply struct {
	Accounts []protocol.WatchEntry `json:"accounts"`
	Code     string                `json:"code"`
	Expires  time.Time             `json:"expires"`
}

func (a *Accounts) call(ctx context.Context, req protocol.WatchRequest) (watchReply, error) {
	var out watchReply
	if a.id.NodeID == "" {
		return out, errors.New("this node has no address yet; wait until it is online")
	}
	req.Timestamp = time.Now().UTC().Format(time.RFC3339)
	req.Signature = base64.StdEncoding.EncodeToString(a.id.Sign(protocol.WatchSigningBytes(req.Action, a.id.NodeID, req.Timestamp, req.WatchArg())))
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, a.server+"/v1/nodes/"+a.id.NodeID+"/accounts", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	hr.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(hr)
	if err != nil {
		a.setErr("Link.us could not be reached")
		return out, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		a.setErr(msg)
		return out, fmt.Errorf("link.us: %s", msg)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	if len(out.Accounts) > 1000 {
		out.Accounts = out.Accounts[:1000]
	}
	a.mu.Lock()
	a.list, a.at, a.err, a.known = out.Accounts, time.Now(), "", true
	a.mu.Unlock()
	return out, nil
}

func (a *Accounts) setErr(msg string) {
	a.mu.Lock()
	a.err = msg
	a.mu.Unlock()
}

// CheckIn is how often a node that any account sees checks in (jittered,
// so many nodes do not arrive together). A node no account sees checks in
// rarely:
// at start, every six hours (an account may have added it through a code
// shown by another program, such as `linknode accounts code`), and every
// minute while a code it showed is valid.
const (
	CheckIn     = 15 * time.Minute
	checkInIdle = 6 * time.Hour
)

// Run checks in until ctx ends.
func (a *Accounts) Run(ctx context.Context) {
	var watchUntil time.Time // a code is out: look every minute until then
	next := time.Now().Add(time.Minute)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
			watchUntil = time.Now().Add(protocol.WatchCodeTTL)
			next = time.Now().Add(time.Minute)
			continue
		case <-t.C:
		}
		if time.Now().Before(next) {
			continue
		}
		err := a.Refresh(ctx)
		switch {
		case time.Now().Before(watchUntil):
			next = time.Now().Add(time.Minute)
		case err != nil:
			a.log.Debug("accounts refresh", "error", err)
			next = time.Now().Add(30 * time.Minute) // e.g. not registered yet
		case len(a.State().Accounts) > 0:
			// 12 to 18 minutes.
			next = time.Now().Add(CheckIn - 3*time.Minute + time.Duration(rand.Int64N(int64(6*time.Minute))))
		default:
			next = time.Now().Add(checkInIdle)
		}
	}
}
