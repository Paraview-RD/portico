# Application icons

Served from this deployment, at `/icons/<name>.svg`, and referenced by an
application's `logoUri`.

They are here rather than pointed at somebody's CDN for two reasons. A portal
that fetches logos from third parties reports every visitor's address and
referring page to those hosts, which is a small privacy leak repeated on every
sign-in. And an air-gapped or offline deployment would show six broken images
where its applications should be.

Nothing in the product depends on these files. An application with no
`logoUri` gets a tile carrying the first character of its name, which needs no
network and cannot break. These exist so the demo tenant looks like a real
one, and as a worked example of the path form.

Drop your own in beside them and set `logoUri` to `/icons/<file>.svg`. They
are rendered through `<img>`, which does not execute script inside an SVG —
that is what makes accepting a whole SVG document here safe, so do not change
the portal to inline them.
