package protocol

import (
	"strings"
	"time"
)

// Link.us accounts that watch a node (shown on link.us/account).
// A node is added to an account only with the node's own signature, and
// each node can list the accounts that added it and remove any of them.
// Watching is read-only: an account sees a node's status, never its
// content, and cannot act on it.

const watchContext = "linkus-watch-v1:"

// WatchSigningBytes returns the exact bytes a node signs for a watch
// action: "code" (arg: empty), "list" (arg: the node's version and
// edition) or "remove" (arg: the account's email address).
func WatchSigningBytes(action, id, ts, arg string) []byte {
	return []byte(watchContext + action + ":" + id + ":" + ts + ":" + arg)
}

// A node joins an account with a short code it asks the service for and
// shows its owner, who types it on link.us. A code works once, for a
// short time; a node has at most one at a time.
const (
	WatchCodeTTL      = 15 * time.Minute
	WatchCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no 0/O, 1/I/L
	WatchCodeLength   = 8
)

// NormalizeWatchCode reads a code as typed (any case, spaces and dashes
// allowed); "" if it cannot be one.
func NormalizeWatchCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch {
		case r == '-' || r == ' ':
		case strings.ContainsRune(WatchCodeAlphabet, r):
			b.WriteRune(r)
		default:
			return ""
		}
	}
	if b.Len() != WatchCodeLength {
		return ""
	}
	return b.String()
}

// FormatWatchCode shows a code in two groups: ABCD-EFGH.
func FormatWatchCode(c string) string {
	if len(c) != WatchCodeLength {
		return c
	}
	return c[:4] + "-" + c[4:]
}

// WatchRequest is the body of POST /v1/nodes/{id}/accounts: the node asks
// for a code to join an account ("code"), for the accounts watching it
// ("list") or removes one ("remove"). A list also tells the service the
// node's version, shown to those accounts. Every answer lists the accounts.
type WatchRequest struct {
	Action  string `json:"action"`
	Email   string `json:"email,omitempty"`   // remove
	Version string `json:"version,omitempty"` // list
	Edition string `json:"edition,omitempty"` // list: "desktop" or "cli"
	// Children is a manager's "active/paired" count of the nodes it
	// manages (list; empty when it manages none). Counts only.
	Children  string `json:"children,omitempty"`
	Timestamp string `json:"ts"`
	Signature string `json:"signature"`
}

// WatchArg is the signed argument of a request.
func (r WatchRequest) WatchArg() string {
	switch r.Action {
	case "remove":
		return r.Email
	case "code":
		return ""
	}
	return r.Version + "/" + r.Edition + "/" + r.Children
}

// WatchEntry is one account watching a node.
type WatchEntry struct {
	Email string    `json:"email"`
	Added time.Time `json:"added"`
	// Listed: this account lists the node in the link.us directory.
	Listed bool `json:"listed,omitempty"`
}
