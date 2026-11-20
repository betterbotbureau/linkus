package node

import (
	"testing"

	"github.com/betterbotbureau/linkus/internal/protocol"
)

func TestNewerVersion(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v1.0", "v1.1", true},
		{"v1.1", "v1.1", false},
		{"v1.1", "v1.0", false},
		{"v1.1", "v1.1.1", true},
		{"v1.9", "v1.10", true}, // numeric, not text, order
		{"v1.10", "v1.9", false},
		{"v1.1.0", "v1.1", false},
		{"v2.0", "v10.0", true},
		{"v1.1-3-gabc1234", "v1.2", true}, // dev build counts as its base
		{"v1.1-3-gabc1234", "v1.1", false},
		{"dev", "v9.9", false}, // unknown versions never nag
		{"v1.1", "", false},
		{"v1.1", "latest", false},
		{"1.1", "v1.2", false},
		{"v1.1.1.1", "v1.2", false},
	}
	for _, tt := range tests {
		if got := NewerVersion(tt.current, tt.latest); got != tt.want {
			t.Errorf("NewerVersion(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestCompareOnlyLinksGitHubAndLinkUs(t *testing.T) {
	u := Compare("v1.0", protocol.ReleaseInfo{Latest: "v1.1", URL: "https://github.com/betterbotbureau/linkus/releases/tag/v1.1"})
	if !u.Available || u.URL == "" {
		t.Fatalf("expected an update with a link: %+v", u)
	}
	u = Compare("v1.0", protocol.ReleaseInfo{Latest: "v1.1", URL: "https://evil.example/download"})
	if u.Available || u.URL != "" {
		t.Fatalf("a non-GitHub link must not be offered: %+v", u)
	}
	u = Compare("v1.0", protocol.ReleaseInfo{Latest: "v1.1", URL: "https://link.us/download"})
	if !u.Available || u.URL != "https://link.us/download" {
		t.Fatalf("expected the link.us download page: %+v", u)
	}
	u = Compare("v1.0", protocol.ReleaseInfo{Latest: "v1.1", URL: "https://link.us.evil.example/download"})
	if u.Available || u.URL != "" {
		t.Fatalf("a look-alike host must not be offered: %+v", u)
	}
}
