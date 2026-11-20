# The built-in example site

"Alex's Place" — the personal site a node offers to create when its
public folder is empty. One person's place on the web: an about page with
a photo gallery, notes, projects, a for-sale page, and an `offline.html`
business card for when the computer is off.

What it quietly demonstrates:

- multi-page navigation and a shared stylesheet over the data channel;
- images (the SVGs in `photos/`);
- that a site's own scripts run on its published address (the gallery
  lightbox on the home page);
- a correct, script-free `offline.html` within the 64 KB contract.

This file itself is never written into a user's folder — only the site
files are, and never over an existing file.
