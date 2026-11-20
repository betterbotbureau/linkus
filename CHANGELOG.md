# Changelog

What changed in each release of the command-line node. Downloads are on
the [releases page](https://github.com/betterbotbureau/linkus/releases).

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
