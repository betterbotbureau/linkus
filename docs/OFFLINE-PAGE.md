# The offline homepage

`offline.html` in the public folder is the one page visitors can see
while your computer is off or unreachable. The node mirrors it to the
Link service (signed, capped at **64 KB**), and the service shows it on
its own, with nothing added on top, in place of the plain "isn't
available right now" note. Remove the file and visitors get the plain
note again.

It is the single bounded exception to "no user content through the
service" — which is why its rules are strict.

## What works

- Inline styles (`<style>` and `style=` attributes).
- Images embedded as `data:` URIs — they count toward the 64 KB cap.
- Images loaded from `https:` URLs — they don't count toward the cap,
  but then your offline page depends on that host being up.
- Links, on a click: `target="_blank"` opens a new tab and
  `target="_top"` replaces the offline page; `mailto:` and `tel:` work
  as usual. A link without a `target` opens inside the frame the page is
  shown in, so give links to other sites one of the two.

## What never works

Scripts, external stylesheets, web fonts and forms. The service delivers
the page under a hard Content-Security-Policy (`sandbox`, lifted only for
links on a click, + `default-src 'none'`), so these fail even if the
file contains them — whatever the rest of your site is allowed to do.

So the file must be **self-contained**: one HTML file with inline styles
and embedded or `https:` images. Think of it as a business card: who you
are, how to reach you, when the site is usually up.

## Mechanics

The running node watches the file and mirrors changes within seconds.
Uploads are signed by the node's identity key with a timestamp (±10
minutes of clock skew allowed) and rate-limited. Stored pages are checked
for common phishing shapes and may be reviewed by an AI model — flags go
to the service operator, nothing is removed automatically.

The example site (`linknode example`, or "Create the example site" in
the desktop app) includes a matching `offline.html` to start from.
