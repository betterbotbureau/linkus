# Changelog

What changed in each release of the command-line node. Downloads are on
the [releases page](https://github.com/betterbotbureau/linkus/releases).

## v1.16

- **A card for your node.** Its name and a short description, signed
  with its key, which other nodes and agents read; it is public at the
  node's address (`/.well-known/link-card.json`). Until you name it, the
  card says "Node" and the node's ID, and once the node is published,
  the site's title (an unpublished site's title stays private).
  `linknode card name …` and `linknode card about …` give your own.
- **Your node in your link.us account, and in Explore.** `linknode
  accounts code` shows a code that adds the node to a link.us account;
  that account sees whether the node is running and its version, and can
  list it in Explore (link.us/explore). `linknode accounts` shows the
  accounts and which one lists it; `linknode accounts remove` takes one
  off.
- **Last seen: the day only.** Link.us now shows the day an offline
  node was last online, never the time, in Explore and in its public
  lookup.

## v1.15

- **Folder names start with a capital letter:** `~/Link/Public` and
  `~/Link/Settings`. The first time v1.15 starts, it renames the old
  `public` and `settings` folders, so your site, settings and address
  stay as they are. Folders you chose yourself (flag, environment or
  config file) are not moved, and nothing is moved while another copy
  of the node is running.

## v1.14

The first release of the open-source command-line node: the core of
Link, for Linux, Raspberry Pi, NAS boxes, routers and other always-on or
headless devices.

- One static binary per platform: Linux amd64, arm64, armv7, 386, mips,
  mipsle and riscv64, plus Windows and macOS for the command line.
- A permanent public address (`https://<id>.mdoc.us`) for one folder,
  served as a website (pages, scripts, small web apps; nothing runs on
  the device itself) directly from the device over encrypted
  WebRTC, with no relay and usually no router setup (verified PCP,
  NAT-PMP or UPnP port mapping when the router offers it).
- Managed with plain commands (`status`, `publish on|off`, `folder`,
  `public`, `log`, `export`/`restore`), environment variables or a
  config file, and a systemd unit for running as a service.
- An optional offline page, shown by the Link service while the device
  is off, and an example site to start from (`linknode example`).
