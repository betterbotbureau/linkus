# Link Node

**Put a device on the internet with one small program: a public address,
a folder served as a website, and no port forwarding.**

Link Node is the open-source core of [Link](https://link.us): a single
binary for Linux, Raspberry Pi, NAS boxes, routers and other always-on or
headless devices. It serves one folder as a website (pages, images,
scripts, small web apps) at a permanent public address like:

```
https://58310427.mdoc.us
```

Visitors connect straight to the device over an encrypted peer-to-peer
connection. The site is never uploaded to a hosting provider and never
relayed through Link's servers: the Link service helps visitors find the
node, and the device serves the site.

This repository is the command-line edition: connectivity and public
sharing, managed over SSH or by scripts and AI agents. The desktop app
(with its settings window, visitor messages and shared folders) is built
on this core and available from [link.us](https://link.us).

## Why

- **Your files stay on your device.** The live site is served from the
  device itself, not copied to a hosting account.
- **No traffic relay.** The Link service only sets up connections; pages
  travel directly between the visitor and the device. The one optional
  exception is a small [offline page](docs/OFFLINE-PAGE.md) shown while
  the device is off.
- **No router setup, usually.** The node connects through typical home
  and office networks by itself, and verifies any router port mapping
  before trusting it.
- **Real websites, nothing running on the device.** The node only hands
  out files; it never runs code itself. Each site is its own browser
  origin, so its pages' JavaScript runs in the visitor's browser:
  galleries, games, maps and small apps work as on any host.
- **Small and dependency-free.** One static binary (no cgo), about 13 MB,
  for amd64, arm64, armv7 and 386; anything that can write files can
  publish.
- **Made for agents too.** The node writes instructions into the folder
  (`AGENTS.md`, `CLAUDE.md`), so a coding agent such as Claude Code or
  Codex knows the address and the rules with no explanation.
- **Local without the service.** The folder, local preview, identity and
  settings keep working when the service is unreachable.

## How it works

1. Run `linknode`. It creates a permanent local identity (an Ed25519 key
   that never leaves the device) and gets a permanent public address.
2. Put files in the public folder (default `~/Link/Public`) and check
   the local preview at `http://localhost:7465/`.
3. Turn publishing on: `linknode publish on`.
4. A visitor opening the address gets a small bootstrap page from the
   service, which verifies the node's signature and then connects
   **directly** to the device. Every byte of the site travels over an
   encrypted WebRTC data channel.

```
your device                                  visitor's browser
~/Link/Public  ── direct encrypted P2P ──▶   https://<id>.mdoc.us
```

When the node is off, visitors see an offline notice (or your offline
page). Because there is no relay, some restrictive networks cannot reach
a node; the page says so honestly instead of failing silently. More in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Quick start

```sh
# Linux, 64-bit ARM (Raspberry Pi OS 64-bit, ARM NAS); see the releases
# page for the other builds
curl -LO https://github.com/betterbotbureau/linkus/releases/latest/download/linknode-linux-arm64
sudo install linknode-linux-arm64 /usr/local/bin/linknode

linknode example        # optional: a ready-made example site to start from
linknode run            # start (Ctrl-C to stop); prints the address
linknode publish on     # in another shell: put the folder online
```

To keep it running, install the [systemd unit](deploy/linknode@.service).
[docs/INSTALL.md](docs/INSTALL.md) has the details: verifying downloads,
every command and setting, running as a service, and building from
source.

## With an AI agent

Coding agents can install Link Node, build the site and publish it:

- point the agent at **https://link.us/agents.md** (step-by-step
  instructions written for agents), or
- add the [`link-node` skill](skills/link-node/SKILL.md) to your agent,
  for example `cp -r skills/link-node ~/.claude/skills/` for Claude Code.

Once installed, the node keeps `AGENTS.md` and `CLAUDE.md` in the public
folder with the site's address and rules, so any agent opened there knows
what to do.

## What the node does, and doesn't

- keeps its identity local: the private key is never sent anywhere; it
  leaves the device only if you copy or export it yourself
- serves exactly one folder, read-only, as static files, and never
  executes anything in it
- hides what you cannot keep out of the folder (dotfiles and the
  generated agent files) and serves everything else you put there
- logs every visit for you: time, outcome, visitor address, paths, bytes
  (`linknode log`)
- accepts no remote commands: the only things it does for visitors are
  connecting and serving files

The Link service assigns the address, tracks whether the node is online,
passes the few kilobytes of connection setup, and serves the bootstrap
page and the optional offline page. It never stores or proxies your site,
and uses no cookies. The service itself is operated by Link and is not
part of this repository.

## Documentation

- [docs/INSTALL.md](docs/INSTALL.md): install, verify, commands, settings, systemd, building
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): how a visit works, identity, origins, port mapping
- [docs/PROTOCOL.md](docs/PROTOCOL.md): registration, presence, signaling, the fetch protocol
- [docs/OFFLINE-PAGE.md](docs/OFFLINE-PAGE.md): the one page that works while the device is off
- [SECURITY.md](SECURITY.md): promises, limitations, reporting vulnerabilities
- [PRIVACY.md](PRIVACY.md): what the service and the node keep
- [CHANGELOG.md](CHANGELOG.md): what changed in each release

## License

[MIT](LICENSE). You are free to modify and redistribute the node. The
public Link service is built for the released node, and a modified node
may not be supported by it.
