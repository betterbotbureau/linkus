# Protocol

This is an overview; [`internal/protocol`](../internal/protocol/) is the
source of truth, and every message shape lives there with tests. All
signatures are Ed25519 with the node's identity key, over
domain-separated messages (each context string is versioned, e.g.
`linkus-register-v1`).

## Node IDs

A node's address is its ID under the names domain: `58310427.mdoc.us`.
An ID is 8 characters from the 33-character alphabet
`0-9` and `a-z` without `i`, `l` and `o`, which read like `1` and `0`, as
on number plates. IDs are issued as digits; every part of the system
accepts letters, so the service can start issuing them when the 90
million numeric IDs grow scarce (33⁸ is about 1.4 trillion), with no
longer addresses and nothing to update. **An activated node address is
never assigned to another node** (see below).

## Service API

| Endpoint | Purpose |
| --- | --- |
| `POST /v1/nodes` | Register a public key, get a node ID. Signed. Repeating it for a registered key returns the same ID and confirms the node still exists. New IDs are rate-limited per network (per IPv4 address; per IPv6 /64 and /48). When the service is busy, a `429` reply may carry a challenge the node answers before sending the registration again. |
| `GET /v1/nodes/{id}` | Node status: online/suspended, last seen, public key. Readable from any website (`Access-Control-Allow-Origin: *`, no credentials). |
| `GET /v1/node/connect` | WebSocket the node holds while running (presence). The service issues a challenge; the node signs it. If another copy of the node is already connected and answers a ping, the newcomer is closed with code 4001 ("in use") and stays offline; a connection that does not answer is replaced. |
| `GET /v1/visit` | WebSocket a visitor's shell uses for WebRTC signaling with one node. Accepted from any origin. |
| `PUT/DELETE /v1/nodes/{id}/offline` | Upload or remove the offline homepage. Signed over method + ID + timestamp + content, ±10 min clock skew, 64 KB cap, rate-limited. |
| `GET /v1/nodes/{id}/offline` | Serve the stored offline page under a hard no-script CSP. |
| `GET /v1/release` | The latest published node release (tag and release page), which the service checks hourly. Nodes ask at startup and daily, to tell their owner about updates. |

Every issued ID is in one of two states:

- **Reserved** — issued, but its node has never been online. The node
  keeps it by confirming it, at startup and about once a day. A reserved
  ID left unconfirmed for the service's reclaim period (a year by
  default) returns to the pool; a node returning after that adopts the
  new ID the service gives it.
- **Activated** — its node has been online at least once, so its
  address may have been shared. An activated ID is never assigned to
  another node. If its node shows no sign of life (neither online nor
  confirming) for the retire period (a year by default), the ID is
  **retired**: its record is removed, keeping only the number and the
  node's public key. The number is never issued to another node; the
  same node, proving its key by registering again, gets it back.

## Connection setup

Offer and answer SDPs travel through the service as opaque relay
messages. The node signs `"linkus answer v1\n" + hex(SHA-256(offer)) +
"\n" + hex(SHA-256(answer))`; the shell verifies that signature against
the public key registered for the node's ID **before** completing the
connection, binding the WebRTC session to the node identity. Browsers
use their own Web Crypto Ed25519 where it exists; older ones use a
plain-JavaScript verifier bundled with the shell, so every visit is
verified and no current WebRTC browser is turned away.

Signal message kinds include `offer`, `answer`, `offline`, `busy` and
`suspended` (the shell shows each state honestly), plus `probe` /
`probe-sent` used for port-mapping verification: the service sends a few
UDP probe packets (`linkus-probe-v1` magic) so a node can confirm that
unsolicited inbound traffic actually arrives at its mapped port before
advertising it.

A node serves a limited number of visits at once, set by its owner
(32 by default, 1 to 200), and one visitor address may hold 40 percent
of them (an IPv6 /64 counts as one address). The service passes the
visitor's address to the node with each offer (`from`) so an offer
beyond either limit is answered with `busy` before any connection is
made. A site visit that asks for nothing for two minutes is closed to
free its place; the shell reconnects when the page next needs a file.

## Fetching files

Site content moves over WebRTC data channels with the `linkus-fetch-v1`
protocol: the shell's service worker sends `get`/`head` requests for
paths, the node answers with a header message, the file's bytes, and
`done` (or `error`). Channels per visit are capped; every read is
bounded and every input validated — the node treats everything from the
network as hostile. Channels with other `linkus-` labels belong to
features this node does not have and are closed.

## What the service never sees

Site files never pass through the service: signaling carries only
connection setup, and the fetch protocol runs end-to-end between browser
and node over the encrypted WebRTC channel. The single, bounded
exception is the optional [offline homepage](OFFLINE-PAGE.md), which the
owner explicitly uploads.
