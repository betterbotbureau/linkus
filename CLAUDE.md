# Link Node (command-line edition)

## What this is

Link Node is a single-file program for a Linux device, Raspberry Pi, NAS,
router or any computer. It gives the owner a public address and one
public folder; whatever is in the folder is served from the device to
visitors, peer-to-peer. The Link service (`hub.link.us`, node addresses
`<id>.mdoc.us`) is a control plane only: identity, presence, signaling,
STUN and a small shared bootstrap page. It is operated by Link and is
not in this repository.

## Rules that are not negotiable

1. **No user content through Link's servers.** No relay, no proxy, no
   cache of user pages. When a peer-to-peer connection cannot be made,
   the visit fails with a clear explanation. The one exception is the
   owner's offline page: small, size-capped, static, no scripts.
2. **The node serves static files only.** It never executes anything
   from the public folder and accepts no remote commands.
3. **One public folder, strictly confined.** No path may resolve outside
   it: reject `..`, absolute paths and symlinks that leave the folder.
   Never serve dotfiles or the generated `AGENTS.md`/`CLAUDE.md`. No
   directory listings.
4. **The node works without the service.** The local preview and local
   files keep working when the service is unreachable.
5. **One self-contained binary per platform.** Pure Go, no cgo; it must
   build with `CGO_ENABLED=0` for every release target.
6. **Few dependencies.** Standard library first; a new module needs a
   reason in the commit message.
7. **Treat everything from the network as hostile.** Bound every read,
   set timeouts, validate lengths and types.
8. **Node addresses come from the service.** Never derive a node address
   from the service's host.

## Technology

- Go 1.24 or newer (`os.Root`). WebRTC: Pion.
- Port mapping: PCP, NAT-PMP, UPnP (`internal/portmap`), verified before
  use.
- Identity: an Ed25519 key pair generated on the node; the private key
  never leaves the device.

## Repository layout

```
cmd/linknode/       the command-line node
internal/node/      running a node: registration, presence, WebRTC, publishing, port mapping
internal/site/      folder confinement and static file rules
internal/protocol/  message shapes shared with the service
internal/identity/  the Ed25519 identity
internal/portmap/   router port mapping
web/starter/        the example site
deploy/             the systemd unit
```

## Conventions

- `gofmt` and `go vet` clean. Table-driven tests next to the code.
- Every network-facing parser and the static file server get tests for
  malformed and malicious input.
- Small commits with plain messages that say what changed and why.
- Before finishing a change: `go test ./...`, and cross-compile with
  `CGO_ENABLED=0` for linux (amd64, arm64, arm), windows and darwin.
