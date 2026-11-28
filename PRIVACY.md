# Privacy

Link Node serves websites from people's own devices. The Link service
(`hub.link.us`, with node addresses on `mdoc.us`) only helps visitors
find a node and set up a direct connection. This page says exactly what
is kept, where, and for how long: first for visitors of a site, then for
people who run a node.

## If you visit a site

A site on an `mdoc.us` address is run by its owner, on their own device,
not by Link. When you open it, your browser connects straight to that
device.

**What the Link service keeps about you:** no accounts, no cookies, no
analytics and no profile of you. Your visit passes through the service
only while the connection is being set up; the pages themselves travel
directly between your browser and the node and never pass through the
service. Your network address may appear only in the service's ordinary
logs, which are used for rate limiting and abuse handling and are
deleted after about 90 days.

**What the site's owner sees:** because you connect straight to the
owner's device, it learns your public network address, as with any
website, and its visit log records the time, the pages you requested and
that address.

**What your browser keeps:** copies of the pages it has seen, in its own
storage only, so they show at once on your next visit; clearing the
browser's site data removes them.

## If you run a node

**What the Link service keeps about your node** (for as long as it
exists):

- its node ID and public key;
- when it registered and when it was last online;
- the network address it registered from and last connected from, so
  that site owners stay accountable for what they publish;
- its offline page, only if you upload one, until you remove it.

When a node shows no sign of life for a year, all of this is deleted
except its ID and public key, which are kept so that the ID is never
given to anyone else and you can come back to it. A public key alone
says nothing about who owns it.

**Offline pages** are checked for common phishing patterns when uploaded.
On the public service they may also be checked by an AI model at
Cloudflare Workers AI, which receives the page's text for that check only.

**What stays on your device:** everything else, in your Link folder: your
private key (never sent anywhere), your settings, and a log of recent
visits to your site — time, the pages requested, and the visitor's
network address. The visit log is capped at a few megabytes and is only
ever read by you.

**What a direct connection means for you:** because visitors connect
straight to your device, **a visitor's browser learns your device's
public network address, and your node learns the visitor's**, just as
with any web server you host yourself. That address usually shows a
visitor your internet provider and rough area, not your street address.
If that matters to you, publish from an always-on device on a network
you are comfortable sharing, or keep your site unpublished.

## If you report a site

Your report, the reported address and your network address are kept
with the node's record, so reports can be investigated and abuse of the
form itself can be limited.

## Other services

- **Downloads** come from GitHub, which has its own privacy policy.
- **Update checks:** your node asks the Link service about once a day
  whether a newer version exists. The service asks GitHub, not your
  node.

Questions or concerns: for anything sensitive, use the private reporting
described in [SECURITY.md](SECURITY.md); otherwise contact us through
[link.us](https://link.us).
