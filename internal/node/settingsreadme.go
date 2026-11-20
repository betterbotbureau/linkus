package node

import (
	"os"
	"path/filepath"
)

const settingsReadmeName = "README.txt"

const settingsReadme = `This folder holds Link Node's settings: whether your site is published,
the website folder you chose, an optional linknode.conf, the visit log,
and the node's identity (identity.json).

Deleting this folder resets the settings. Your node keeps its address:
its key is also kept safely elsewhere on this computer, and comes back
when Link Node starts again (the site starts unpublished).

Keep this folder private: identity.json is the key that proves your
address is yours.

To move your node to another computer, copy this folder to
~/Link/settings there (or use "linknode export" and "linknode restore"),
quit Link Node here, then start it there. Only one copy runs a node: a
copy started while another is running stays offline. Stop it from
starting here (Start at login, or its service) if you no longer use this
computer.

To give up this address for good and start over with a new one:
"linknode forget-identity --yes".
`

// EnsureSettingsReadme writes README.txt into the settings folder when it
// is missing, so the folder explains itself. The owner may edit or delete
// it; it is only written again when missing.
func EnsureSettingsReadme(dataDir string) {
	p := filepath.Join(dataDir, settingsReadmeName)
	if _, err := os.Stat(p); err == nil {
		return
	}
	os.WriteFile(p, []byte(settingsReadme), 0o644) //nolint:errcheck // a courtesy file
}
