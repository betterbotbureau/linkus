# Security

Link Node runs on people's own devices and accepts connections from
strangers, so security reports matter to us more than most projects.

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's security
advisories: open the repository's **Security** tab and choose **Report a
vulnerability**. Do not open a public issue for anything exploitable.

We will acknowledge reports as fast as we can. This is a small project,
but a node vulnerability affects real people's devices, so fixes for
anything serious take priority over everything else.

## Reporting abusive content

A site served through Link is not hosted by us: content travels
peer-to-peer from the owner's device. To report phishing, scams or other
abuse on a `*.mdoc.us` address, use the form at
**[link.us/report](https://link.us/report)**. Confirmed abuse can get the
address suspended.

## What the node promises

- It serves exactly one folder, read-only, and never executes anything
  in it. Paths cannot escape the folder; dotfiles (`.git`, `.env`, …) and
  the generated agent instruction files are never served. Everything else
  in it is public by definition.
- The private key is never sent anywhere. It leaves the device only when
  the owner copies or exports it, to move the node.
- It accepts no remote commands. Visitors can connect and fetch files;
  nothing else.
- Everything arriving from the network is treated as hostile: bounded
  reads, timeouts, validated lengths and types.

If you find the code breaking any of these promises, that is exactly the
kind of report we want.

## Known limitation: shared registrable domain

Each site runs on its own subdomain (`<id>.mdoc.us`), which is its own
browser origin, but the service's domain is not on the
[Public Suffix List](https://publicsuffix.org/), so all node subdomains
share one registrable domain. A site's scripts can therefore set
parent-domain cookies that sibling sites can read, and same-site
assumptions do not separate nodes from each other.

- The Link service itself uses **no cookies** and keeps no
  authentication on the shared domain, so this cannot be used against
  the service.
- One site could plant cookies visible to another. Treat cookies on a
  node site as untrusted, and do not build anything on a node that relies
  on cookies or same-site boundaries for security.
