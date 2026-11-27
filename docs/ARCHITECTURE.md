# Architecture

Three parts, with a deliberately small middle:

- **Link Node** (`cmd/linknode`, this repository) runs on the owner's
  device. It holds the identity and serves exactly one public folder as
  static files.
- **The Link service** (`hub.link.us`, operated by Link) is a control
  plane only: addresses, presence, signaling, STUN and a small shared
  bootstrap page. It carries no page content.
- **The bootstrap shell** is the page a visitor's browser loads at a node
  address, from the service. It connects to the node peer-to-peer and
  renders the site.

## Identity

Each node generates an Ed25519 key pair on first run and keeps it in its
settings folder (`~/Link/Settings/identity.json`). The private key is
never sent anywhere; it leaves the device only when the owner copies the
settings folder or exports the node. A second copy of the key is kept
outside `~/Link`, so deleting the settings folder resets the settings but
not the address. Registration sends the public key; the service assigns
an 8-character ID, issued as digits (`58310427`).

One node runs on one device at a time: the copy that is connected keeps
the node. A copy that connects while another one is connected and
answering is refused and stays offline until it is started again; a
connection that no longer answers (a crash, a dropped network) is
replaced, so a node's own reconnect always succeeds.

## Presence and signaling

While published, the node holds a WebSocket to the service and proves its
identity by signing a challenge. Visitors' browsers reach the service over
a second WebSocket; the service passes the few kilobytes of WebRTC
connection setup (offer, answer) between the two, and that is all it
carries.

## A visit

1. The browser opens `https://<id>.mdoc.us` and receives the bootstrap
   shell from the service.
2. The shell asks the service for the node's registered public key and a
   signaling channel.
3. Offer and answer travel through the service; the node **signs its
   answer** (Ed25519 over a hash binding of both SDPs) and the shell
   verifies the signature against the registered key before connecting,
   so the browser knows it is talking to the real node.
4. WebRTC data channels open directly between browser and node. A service
   worker installed by the shell serves the site's pages, stylesheets and
   images from them, so links, assets and deep links work like a normal
   site.

If the node is off, the shell shows the owner's
[offline page](OFFLINE-PAGE.md), or a plain "isn't available right now"
note. If no direct connection can be made (some corporate and mobile
networks block it), the shell says so plainly. There is no relay, by
design: the visit fails honestly rather than passing content through the
service.

## Origins and scripts

Each site is its own browser origin (`<id>.mdoc.us`), so a site's own
scripts run; only the offline page is script-free, always. The service's
domain is not on the [Public Suffix List](https://publicsuffix.org/), so
sibling sites share one registrable domain: see
[SECURITY.md](../SECURITY.md).

## Serving rules

The node serves static files only, read-only, from one folder. No path
may resolve outside it: `..`, absolute paths and symlinks that leave the
folder are rejected. Dot-prefixed files and folders (`.git`, `.env`, …)
and the generated `AGENTS.md`/`CLAUDE.md` are never served. Everything
else in it is public. No directory listings. Everything arriving from the
network is treated as hostile: bounded reads, timeouts, validated lengths
and types. Data channels the node has no use for are closed.

## Port mapping

To make visits succeed from more networks, the node asks the router for a
UDP port mapping while published (PCP first, then NAT-PMP, with UPnP in
parallel) and **verifies it before using it**: the router's reported
external address must match what STUN sees, and the service sends a few
probe packets so the node can see that unsolicited inbound traffic really
arrives. Only then is the mapped address offered to visitors. The mapping
is renewed while the node runs and deleted when it exits.
`--portmap=false` turns this off.

## Repository layout

```
cmd/linknode/       the command-line node
internal/node/      running a node: registration, presence, WebRTC, publishing, port mapping
internal/site/      the folder confinement and static file rules
internal/protocol/  message shapes shared with the service, with tests
internal/identity/  the Ed25519 identity
internal/portmap/   PCP, NAT-PMP and UPnP clients
web/starter/        the example site (`linknode example`)
deploy/             the systemd unit
docs/               this documentation
```

The node is built to be extended: features register their own data
channel handlers (`WebRTC.HandleChannel`) and the CLI's commands are a
table, which is how the desktop app adds its own on top of this core.
