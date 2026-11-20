// Package identity manages the node's Ed25519 key pair and server-assigned
// ID. The private key is written once with owner-only permissions and never
// leaves the machine.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the identity file inside the data directory.
const FileName = "identity.json"

// Identity is the node's key pair plus the numeric ID assigned by the
// server at registration. NodeID is empty until registration succeeds.
type Identity struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
	NodeID     string

	path string
}

type identityFile struct {
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	NodeID     string `json:"node_id,omitempty"`
}

// LoadOrCreate loads the identity from dir, generating and saving a new key
// pair on first run. It reports whether a new identity was created.
func LoadOrCreate(dir string) (*Identity, bool, error) {
	path := filepath.Join(dir, FileName)
	id, err := load(path)
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, false, fmt.Errorf("generate key: %w", err)
	}
	id = &Identity{PublicKey: pub, PrivateKey: priv, path: path}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, err
	}
	if err := id.save(); err != nil {
		return nil, false, err
	}
	return id, true, nil
}

// Restore writes an identity file in dir for an existing private key — a
// key recovered from a backup. The node ID is left empty: registering the
// same key again returns the ID it already has.
func Restore(dir string, priv ed25519.PrivateKey) (*Identity, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid private key")
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("invalid private key")
	}
	id := &Identity{PublicKey: pub, PrivateKey: priv, path: filepath.Join(dir, FileName)}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := id.save(); err != nil {
		return nil, err
	}
	return id, nil
}

// Load loads an existing identity from dir without creating one; the error
// wraps os.ErrNotExist when none is stored.
func Load(dir string) (*Identity, error) {
	return load(filepath.Join(dir, FileName))
}

func load(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f identityFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	pub, err := base64.StdEncoding.DecodeString(f.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%s: invalid public key", path)
	}
	priv, err := base64.StdEncoding.DecodeString(f.PrivateKey)
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%s: invalid private key", path)
	}
	return &Identity{
		PublicKey:  ed25519.PublicKey(pub),
		PrivateKey: ed25519.PrivateKey(priv),
		NodeID:     f.NodeID,
		path:       path,
	}, nil
}

// SetNodeID stores the server-assigned ID next to the key.
func (id *Identity) SetNodeID(nodeID string) error {
	id.NodeID = nodeID
	return id.save()
}

// save writes the identity atomically with owner-only permissions. On
// Windows the mode bits are advisory and the user-profile ACL applies.
func (id *Identity) save() error {
	f := identityFile{
		PublicKey:  base64.StdEncoding.EncodeToString(id.PublicKey),
		PrivateKey: base64.StdEncoding.EncodeToString(id.PrivateKey),
		NodeID:     id.NodeID,
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := id.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, id.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Sign signs msg with the node's private key.
func (id *Identity) Sign(msg []byte) []byte {
	return ed25519.Sign(id.PrivateKey, msg)
}

// PublicKeyBase64 returns the public key as sent in registration requests.
func (id *Identity) PublicKeyBase64() string {
	return base64.StdEncoding.EncodeToString(id.PublicKey)
}

// Verify checks sig over msg against pub.
func Verify(pub ed25519.PublicKey, msg, sig []byte) bool {
	return len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, msg, sig)
}
