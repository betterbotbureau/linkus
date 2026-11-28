# Installing and running Link Node

## Download

Get the file for your device from the
[releases page](https://github.com/betterbotbureau/linkus/releases). It
is one static binary; there is nothing else to install.

| Device | File |
| --- | --- |
| Linux PC, server, mini PC (64-bit) | `linknode-linux-amd64` |
| Raspberry Pi (64-bit OS), ARM NAS, ARM boards | `linknode-linux-arm64` |
| Raspberry Pi (32-bit OS, Pi 2 and later), ARMv7 boards | `linknode-linux-armv7` |
| Older 32-bit x86 | `linknode-linux-386` |
| Routers and boards with MIPS (little / big endian) | `linknode-linux-mipsle` / `linknode-linux-mips` |
| RISC-V boards (64-bit) | `linknode-linux-riscv64` |
| Windows (command line) | `linknode-windows-amd64.exe` |
| macOS (command line) | `linknode-darwin-arm64` (Apple silicon) / `linknode-darwin-amd64` (Intel) |

Another CPU or OS that Go supports? [Build it yourself](#building-from-source).

## Verifying a download

Every release carries `sha256sums.txt`; compare your download's SHA-256
against it. Releases also carry GitHub build attestations, which prove a
binary was built by this repository's release workflow from the tagged
source:

```sh
gh attestation verify linknode-linux-arm64 --repo betterbotbureau/linkus
```

Windows may warn the first time a downloaded binary starts; on a Mac, a binary downloaded in a browser has to be allowed once in System Settings. Verify the checksum or the attestation, or
build from source.

## First run

```sh
sudo install linknode-linux-arm64 /usr/local/bin/linknode
linknode run
```

On first run the node generates its Ed25519 identity in the settings
folder (`~/Link/Settings`), creates the public folder (`~/Link/Public`)
with a placeholder homepage, registers to get its ID, and serves a local
preview at `http://localhost:7465/`, on the device only. It keeps running
and serving locally even when the Link service is unreachable.

A new node starts **unpublished**: the folder is served only on the local
preview until you run `linknode publish on`. The switch is a file in the
settings folder, polled by the running node, so it takes effect within
seconds, and scripts or agents can flip it too.

## Commands

```sh
linknode                        # run (the same as `linknode run`)
linknode status                 # ID, folders, config file, publish state, version, server view
linknode publish on|off         # put the folder online, or take it offline
linknode folder [path]          # show the public folder, or choose a new one (applies at next start)
linknode forget-identity --yes  # give up this node's address for good
linknode example                # write the example site into a folder with no homepage yet
linknode public                 # list exactly what visitors can fetch, and what is hidden
linknode log -n 50              # the last 50 visits: time, outcome, address, duration, paths, bytes
linknode card                   # this node's card; `linknode card help` for its name and description
linknode accounts               # link.us accounts that see this node; `code` adds one, `remove` takes one off
linknode export backup.tar.gz   # identity + state + public folder (contains the PRIVATE KEY)
linknode restore backup.tar.gz  # bring a node back, same ID (--force to replace an identity)
linknode version
```

There is no automatic updating. When a newer release exists, the node
says so in the `linknode run` output and on the `Version` line of
`linknode status`. Replace the binary and restart; the identity and
settings carry over.

## Card, link.us account and Explore

A node describes itself with a card: a name and a sentence or two,
signed with its key, that other nodes and agents read. It is public, at
the node's address under `/.well-known/link-card.json`. Until you name
it, the card says "Node" and the node's ID, and once the node is
published, the site's title (so an unpublished site's title stays
private). Give it your own name and description with:

```sh
linknode card name "Garden sensors"
linknode card about "Soil and air readings from a back garden in Leeds."
```

A running node sends the card to Link.us within a minute. To see the
node in your link.us account, run `linknode accounts code` and type the
code on link.us/account under Add a node. From there the account can
list the node in Explore (link.us/explore), where people and agents find
it. `linknode accounts` shows which accounts see the node, and which of
them lists it; `linknode accounts remove <email>` takes one off, and
with it that account's listing.

When the node is offline, Link.us shows the day it was last online,
never the time.

## Running as a service (systemd)

[`deploy/linknode@.service`](../deploy/linknode@.service) runs the node
as an existing login user:

```sh
sudo cp linknode@.service /etc/systemd/system/
sudo systemctl enable --now linknode@pi      # "pi" = the login user
linknode status
linknode publish on
```

The service uses that user's `~/Link/Settings`, including the optional
`linknode.conf`, so the same user's SSH commands act on the running node
with no extra flags. Publishing applies within seconds; a new folder or
config file applies after `sudo systemctl restart linknode@pi`.

On systems without systemd (OpenWrt, BusyBox), start `linknode run` from
the system's own init or service manager with the settings described
below.

## Where Link Node keeps its data

```
~/Link/
  Public/     the website (dotfiles and agent instructions are never served)
  Settings/   the node's settings and identity
```

`Settings` holds whether the site is published, the website folder you
chose, an optional `linknode.conf`, the visit log, and `identity.json`:
the key that proves the address is yours. Keep it private, and never put
it inside the website folder (the node refuses a public folder that
contains it).

A second copy of the key is kept outside `~/Link` (a private
`LinkNodeKeys` file in the per-user application folder, or Windows
Credential Manager), so deleting `Settings` resets the node but keeps its
address.

- **Moving to another device:** copy `Settings` to `~/Link/Settings`
  there, or use `linknode export` here and `linknode restore` there. Stop
  the node here first: one node runs on one device at a time, and a copy
  started while another one is running stays offline.
- **Starting over with a new address,** giving up the old one for good:
  stop the node, then `linknode forget-identity --yes`.
- **Another location:** `--data-dir` (or `LINKNODE_DATA_DIR`); move the
  folder's contents with it, or the node starts with a new identity.

Once a node has been online, its address is **activated** and never given
to anyone else. After a year with no sign of life it is retired: still
never given to anyone else, and yours again when the same identity comes
back. An address whose node has never been online is only **reserved**:
starting the node keeps it, and after a year without that it may be
reissued.

## Settings: flags, environment and config file

| Flag | Environment | Default |
| --- | --- | --- |
| `--config` | `LINKNODE_CONFIG` | `linknode.conf` in the settings folder, if present |
| `--server` | `LINKNODE_SERVER` | `https://hub.link.us` |
| `--data-dir` | `LINKNODE_DATA_DIR` | `~/Link/Settings` |
| `--public-dir` | `LINKNODE_PUBLIC_DIR` | `~/Link/Public` |
| `--preview-addr` | `LINKNODE_PREVIEW_ADDR` | `127.0.0.1:7465` |
| `--stun` | `LINKNODE_STUN` | derived from `--server` |
| `--portmap` | `LINKNODE_PORTMAP` | `true` |

The config file is plain text, one `key = value` per line, using the
flag names without the dashes. `#` starts a comment, `~/` means the home
directory, and values may be quoted:

```ini
# ~/Link/Settings/linknode.conf
public-dir = /srv/site
portmap = true
```

Flags win over environment variables, which win over the config file. A
misspelled or repeated setting stops the node with the line number. A
file passed with `--config` may also set `data-dir`; the one inside the
settings folder cannot move the settings folder.

## Building from source

Requires Go 1.24.2 or newer. No cgo and nothing else to install, so
cross-compiling is one command:

```sh
go test ./...
go build ./cmd/linknode
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" ./cmd/linknode
GOOS=linux GOARCH=mipsle GOMIPS=softfloat go build -trimpath -ldflags "-s -w" ./cmd/linknode
```

`go tool dist list` shows every target Go supports. The public Link
service is built for this repository's released node; a modified node may
not be supported by it.
