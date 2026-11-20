---
name: link-node
description: Put a website online from the owner's own computer, Raspberry Pi, NAS or server with Link Node, with no hosting account, upload or port forwarding. Use when someone wants a homepage, portfolio, small business page or file sharing served from their own machine, or mentions Link Node, link.us or a ~/Link/public folder.
---

# Link Node

You are an AI agent (Claude Code, Codex or similar) and the person you
work for wants a website, a homepage, a portfolio, a page for a small
business, or a way to share files from their own computer or device.
Link Node puts a folder on that machine online at a permanent public
address, served straight from the machine, peer-to-peer. No hosting
account, no upload, no port forwarding, free.

Follow these steps. Ask the owner before anything that installs software
or makes their site public.

## 1. Is Link Node already there?

Look for the folder `~/Link/public` containing `AGENTS.md`. If it is
there, Link Node is installed: read that `AGENTS.md` (it has the site's
own address and rules) and go to step 3.

On Linux and on a Mac, `linknode status` also tells you, if the command
is on the PATH. In the Mac app it is
`"/Applications/Link Node.app/Contents/MacOS/linknode" status`.

## 2. Install it

**Windows, Mac, a Linux PC with a desktop:** the owner downloads the app
from https://link.us/#download and opens it once. Windows may show a warning
the first time; on a Mac, the owner allows it once in Settings:

- Windows: *More info → Run anyway*.
- Mac: *Done*, then *System Settings → Privacy & Security → Open Anyway*.

Explain this to the owner and let them do it; do not work around the
warning yourself. The app then shows its settings window and creates
`~/Link/public`.

**Raspberry Pi, NAS, server, router (no screen):** the open-source
command-line node. With the owner's OK:

```sh
# pick the file for the device: linux-amd64, linux-arm64, linux-armv7,
# linux-386, linux-riscv64, linux-mips, linux-mipsle
curl -LO https://github.com/betterbotbureau/linkus/releases/latest/download/linknode-linux-arm64
sudo install linknode-linux-arm64 /usr/local/bin/linknode
linknode run      # first run: creates the identity and ~/Link/public; Ctrl-C after it prints the address
```

To keep it running, install the systemd unit from
https://github.com/betterbotbureau/linkus/blob/main/deploy/linknode@.service
(`sudo systemctl enable --now linknode@<user>`).

## 3. Build the site

- Write the files into the public folder (`~/Link/public` unless
  `linknode folder` says otherwise). `index.html` is the homepage.
- Plain HTML, CSS, images and JavaScript, as on any static host. There is
  no build step and no server code: nothing in the folder ever runs on
  the machine. Scripts do run in the visitor's browser, so galleries,
  maps and small apps work.
- Everything in the folder is public, except files starting with a dot
  and the generated `AGENTS.md` and `CLAUDE.md`. Never put secrets,
  passwords or private data there.
- `linknode example` writes a complete example site to start from, if the
  folder has no homepage yet.
- Check your work in the local preview: http://localhost:7465/

## 4. Publish

A new node starts **unpublished**. When the owner is happy with the site
and agrees, publish it:

```sh
linknode publish on
```

(or the Publish switch in the app's settings window). `linknode status`
then shows `Published: yes`, the node's state on the Link service, and
the public address, like `https://58310427.mdoc.us/`. Give the owner that
address.

## 5. Good to know

- The site is online while the computer or device runs Link Node. Add
  `offline.html` (one self-contained file, under 64 KB, no scripts) for
  what visitors see when it is off.
- Some strict networks cannot reach a home computer directly. Link never
  relays traffic, so those visitors see a clear message instead.
- `linknode public` lists exactly what visitors can fetch; `linknode log`
  shows recent visits.
- In the desktop app, the owner can add a chat button (visitors message
  them) and share private folders with chosen email addresses; the
  `AGENTS.md` in the folder explains both.
- To take the site offline again: `linknode publish off`.

More: https://link.us · https://link.us/llms.txt ·
https://github.com/betterbotbureau/linkus
