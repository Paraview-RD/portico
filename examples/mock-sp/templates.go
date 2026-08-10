package main

import "html/template"

// html/template rather than text/template, and not because these pages take
// dangerous input — they barely take input at all. It is so that the places
// that do, the claims and attributes rendered at the end of a sign-in,
// escape what an identity provider said about somebody rather than trusting
// it. A display name is attacker-controlled in exactly the deployments this
// tool is used to check.
var templates = template.Must(template.New("mock-sp").Parse(pages))

const pages = `
{{define "head"}}<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>mock-sp</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 16px/1.55 system-ui, -apple-system, "Segoe UI", sans-serif;
         margin: 0; padding: 3rem 1.5rem; }
  main { max-width: 46rem; margin: 0 auto; }
  h1 { font-size: 1.5rem; margin: 0 0 .25rem; }
  h2 { font-size: 1.05rem; margin: 2.5rem 0 .75rem; }
  p.lede { color: #6b7280; margin: 0 0 2rem; }
  a.card, div.card { display: block; padding: 1rem 1.25rem; margin-bottom: .75rem;
           border: 1px solid #d1d5db; border-radius: .5rem;
           text-decoration: none; color: inherit; }
  a.card:hover { border-color: #6b7280; }
  a.card strong, div.card strong { display: block; }
  a.card span, div.card span { color: #6b7280; font-size: .9rem; }
  div.card { opacity: .7; }
  div.card code { font-size: .82rem; word-break: break-all; }
  table { border-collapse: collapse; width: 100%; font-size: .92rem; }
  th, td { text-align: left; padding: .45rem .75rem .45rem 0;
           border-bottom: 1px solid #e5e7eb; vertical-align: top; }
  th { width: 12rem; font-weight: 600; color: #6b7280; }
  td { font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
       word-break: break-all; }
  pre { background: #f3f4f6; padding: 1rem; border-radius: .5rem;
        overflow-x: auto; font-size: .85rem; }
  @media (prefers-color-scheme: dark) { pre { background: #1f2937; } }
  footer { margin-top: 3rem; color: #6b7280; font-size: .9rem; }
  .badge { display: inline-block; font-size: .78rem; padding: .1rem .5rem;
           border-radius: .75rem; background: #e5e7eb; color: #374151;
           margin-left: .5rem; vertical-align: middle; }
</style>
</head>
<body><main>
{{end}}

{{define "foot"}}
</main></body></html>
{{end}}

{{define "home"}}{{template "head"}}
<h1>mock-sp</h1>
<p class="lede">A relying party you can click. Pick a protocol and sign in
through Portico; the page at the end shows what came back.</p>

{{range .}}
  {{if .Ready}}
<a class="card" href="{{.Path}}">
  <strong>{{.Name}} →</strong>
  <span>{{.Blurb}}</span>
</a>
  {{else}}
<div class="card">
  <strong>{{.Name}} — unavailable</strong>
  <span><code>{{.Err}}</code></span>
</div>
  {{end}}
{{end}}

<footer>This is a demonstration, not a library. Nothing is stored and there
is no session: signing in twice starts over both times.</footer>
{{template "foot"}}{{end}}

{{define "broken"}}{{template "head"}}
<h1>{{.Name}} could not be set up</h1>
<p class="lede">The other protocols are unaffected — each one starts on its
own, so a misconfiguration in this one does not take them down.</p>
<pre>{{.Err}}</pre>
<footer><a href="/">Back</a></footer>
{{template "foot"}}{{end}}

{{define "signedin"}}{{template "head"}}
<h1>Signed in <span class="badge">OpenID Connect</span></h1>
<p class="lede">Portico issued these. The ID token is what it asserted at the
moment of sign-in, signed and verified against the discovered key set; the
userinfo response is what it says about the same person now.</p>

<table>
  <tr><th>Issuer</th><td>{{.Issuer}}</td></tr>
  <tr><th>Subject</th><td>{{.Subject}}</td></tr>
  <tr><th>Audience</th><td>{{.Audience}}</td></tr>
  <tr><th>ID token expires</th><td>{{.Expiration}}</td></tr>
  <tr><th>Access token</th><td>{{.AccessToken}}</td></tr>
  <tr><th>Access token expires</th><td>{{.TokenExpiry}}</td></tr>
  <tr><th>Refresh token</th><td>{{.RefreshToken}}</td></tr>
</table>

<h2>ID token claims</h2>
<pre>{{.IDTokenJSON}}</pre>

<h2>Userinfo</h2>
<pre>{{.UserInfoJSON}}</pre>

<footer><a href="/">Start over</a> — and note that it will not ask again:
the session is Portico's, and this program never had one.</footer>
{{template "foot"}}{{end}}

{{define "samlsignedin"}}{{template "head"}}
<h1>Signed in <span class="badge">SAML 2.0</span></h1>
<p class="lede">The browser posted this assertion back from Portico. Its
signature was checked against the certificate in Portico's metadata, and its
InResponseTo against a request this process actually sent — an assertion
nobody asked for is the one attack the profile has to stop.</p>

<table>
  <tr><th>Issuer</th><td>{{.Issuer}}</td></tr>
  <tr><th>NameID</th><td>{{.NameID}}</td></tr>
  <tr><th>NameID format</th><td>{{.Format}}</td></tr>
  <tr><th>Valid until</th><td>{{.NotOnOrAfter}}</td></tr>
  <tr><th>Encrypted</th><td>{{if .Encrypted}}yes — this service provider
      publishes an encryption key, so Portico encrypted it{{else}}no — this
      service provider publishes no encryption key{{end}}</td></tr>
</table>

<p class="lede" style="margin-top:2rem">The NameID is the account id, not the
username: an administrator can change a username, and a service provider
keyed on one would make a second local record for the same person the day it
changed. The username is below, as <code>uid</code>.</p>

<h2>Attributes</h2>
<table>
{{range .Attributes}}
  <tr><th>{{.Name}}</th><td>{{range .Values}}{{.}}<br>{{end}}</td></tr>
{{else}}
  <tr><td colspan="2">The assertion carried no attribute statements.</td></tr>
{{end}}
</table>

<footer><a href="/">Start over</a> — there is no single logout, by design:
Portico's metadata says so rather than advertising an endpoint that would
half work.</footer>
{{template "foot"}}{{end}}

{{define "samlerror"}}{{template "head"}}
<h1>The assertion was refused</h1>
<p class="lede">Failed while {{.Stage}}.</p>
<pre>{{.Detail}}</pre>
<footer><a href="/">Back</a></footer>
{{template "foot"}}{{end}}

{{define "cassignedin"}}{{template "head"}}
<h1>Signed in <span class="badge">CAS 3.0</span></h1>
<p class="lede">The browser carried only the ticket below. Everything else on
this page came from a second request, made by this server straight to
Portico — which is what makes a CAS ticket worth nothing to anybody who
intercepts it.</p>

<table>
  <tr><th>User</th><td>{{.User}}</td></tr>
  <tr><th>Ticket</th><td>{{.Ticket}}</td></tr>
</table>

<p class="lede" style="margin-top:2rem">That ticket is already spent: it is
good for one validation and one minute. Reloading this page will fail, and
that is the protocol working.</p>

<h2>Attributes</h2>
<table>
{{range .Attributes}}
  <tr><th>{{.Name}}</th><td>{{.Value}}</td></tr>
{{else}}
  <tr><td colspan="2">The response carried no attributes. CAS 2.0 has none —
  this page validates against the CAS 3.0 endpoint precisely so there are.</td></tr>
{{end}}
</table>

<footer><a href="/">Start over</a></footer>
{{template "foot"}}{{end}}

{{define "caserror"}}{{template "head"}}
<h1>The ticket was refused</h1>
<p class="lede">Failed while {{.Stage}}.</p>
<pre>{{.Detail}}</pre>
<footer><a href="/">Back</a></footer>
{{template "foot"}}{{end}}

{{define "oidcerror"}}{{template "head"}}
<h1>The sign-in was refused</h1>
<p class="lede">Portico returned an error instead of an authorization code.</p>
<table>
  <tr><th>error</th><td>{{.Type}}</td></tr>
  <tr><th>error_description</th><td>{{.Description}}</td></tr>
</table>
<footer><a href="/">Back</a></footer>
{{template "foot"}}{{end}}

{{define "notfound"}}{{template "head"}}
<h1>Nothing here</h1>
<p class="lede">{{.}} is not one of this program's pages.</p>
<footer><a href="/">Back</a></footer>
{{template "foot"}}{{end}}
`
