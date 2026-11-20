# Privacy

Link Node serves your site from your own device. The Link service
(`hub.link.us`, with node addresses on `mdoc.us`) only helps visitors
find your node and set up a direct connection. This page says exactly
what is kept, where, and for how long.

## What the Link service keeps

**About each node** (for as long as the node exists):

- its node ID and public key;
- when it registered and when it was last online;
- the network address it registered from and last connected from, so
  that owners stay accountable for what they publish;
- its offline page, only if the owner uploads one, until the owner
  removes it.

When a node shows no sign of life for a year, all of this is deleted
except its ID and public key, which are kept so that the ID is never
given to anyone else and the same node can come back to it. A public key
alone says nothing about who owns it.

**About visitors:** no accounts, no cookies, no analytics and no visitor
profiles. A visit passes through the service only while the connection is
being set up; the pages themselves travel directly between the visitor
and the node and never pass through the service. Visitors' network
addresses may appear only in the service's ordinary logs, which are used for
rate limiting and abuse handling and are deleted after about 90 days.
A visitor's browser keeps copies of the pages it has seen, in its own
storage only, so they show at once on the next visit; clearing the
browser's site data removes them.

**Abuse reports:** the report, the reported address and the reporter's
network address are kept with the node's record, so reports can be
investigated and abuse of the form itself can be limited.

**Offline pages** are checked for common phishing patterns when uploaded.
On the public service they may also be checked by an AI model at
Cloudflare Workers AI, which receives the page's text for that check only.

## What your node keeps

Everything else stays on your device, in your Link folder: your
private key (never sent anywhere), your settings, and a log of recent
visits to your site — time, the pages requested, and the visitor's
network address. The visit log is capped at a few megabytes and is only
ever read by you.

## What a direct connection means

Because visitors connect straight to your device, **a visitor's browser
learns your device's public network address, and your node learns the
visitor's**, just as with any web server you host yourself. That address
usually shows a visitor your internet provider and rough area, not your
street address. If that matters to you, publish from an always-on device
on a network you are comfortable sharing, or keep your site unpublished.

## Other services

- **Downloads** come from GitHub, which has its own privacy policy.
- **Update checks:** your node asks the Link service about once a day
  whether a newer version exists. The service asks GitHub, not your
  node.

Questions or concerns: for anything sensitive, use the private reporting
described in [SECURITY.md](SECURITY.md); otherwise contact us through
[link.us](https://link.us).
