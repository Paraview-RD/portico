# mock-sp: click your way through single sign-on

A relying party — service provider, CAS client — you can drive with a
browser, for **demonstrating and accepting** Portico's single sign-on. One
card per protocol: OpenID Connect, SAML 2.0, CAS 3.0.

What it proves is not what the tests prove. `internal/server/federation_test.go`
and `saml_test.go` prove the protocols to a test runner; this program proves
them **to somebody in the room** — a redirect, a sign-in screen, and a page
laying out what came back.

**It is not a library and it is not production code.** It stores nothing
except a SAML private key; there is no session after signing in, so clicking
a second time walks the whole thing again. What is worth copying out of it is
**which library calls happen, in what order** — not the program around them.

---

## Before you start

- Portico running, with `PORTICO_PUBLIC_URL` set to **the address the browser
  actually uses**
- The `portico` command line available, with `PORTICO_DB_DSN` set — registering
  an application needs it, the same as starting the server does
- Port 8413 free (`--addr` changes it)

The commands below assume a default deployment: Portico at
`http://localhost:8410`, the default tenant. Substitute your own addresses.

---

## 1. Register the OIDC client

```bash
portico client register --id mock-sp --name "Mock SP" --public \
  --redirect-uri http://localhost:8413/oidc/callback
```

`--public` means it is a public client, gets no secret, and is authenticated
by PKCE — **Portico implements OAuth 2.1, which requires PKCE of every
client**, confidential ones included.

## 2. Start it once

```bash
go run ./examples/mock-sp
```

At start-up it does three things and then **prints the other two registration
commands for you**:

1. runs OIDC discovery against Portico,
2. fetches Portico's SAML metadata,
3. generates its own SAML key, certificate and metadata document in
   `.mock-sp/`.

Open <http://localhost:8413> now and **the OpenID Connect card already
works**.

## 3. Register the SAML service provider and the CAS service

Run the two commands the previous step printed (it prints absolute paths;
relative ones work just as well):

```bash
portico sp register --metadata .mock-sp/saml-metadata.xml --name "Mock SP"
portico cas register --url http://localhost:8413/cas/ --name "Mock SP"
```

The CAS one can be run at any time — it registers a **URL prefix**, which is
known in advance. The SAML one has to wait for that first start-up, because
it needs the metadata document the program has just generated.

## 4. Click all three cards

**No restart is needed.** Registration happens entirely on Portico's side and
mock-sp does not cache it — go back to <http://localhost:8413> and all three
cards work.

(There is one case that does need a restart: **it never reached Portico at
start-up at all**. The home page says so, in as many words, rather than
leaving you to guess.)

---

## What each card is demonstrating

**OpenID Connect** — authorization code with PKCE. The final page puts the
**ID token's claims** and the **userinfo response** side by side, because
those are the pair integrators most often conflate: the ID token is what
Portico asserted, signed, **at the moment of signing in**; userinfo is what it
says about the same person **now**. An application built without that
distinction either never sees a changed name or calls userinfo on every
request.

**SAML 2.0** — the assertion comes back as a browser POST. The page tells you
**the assertion was encrypted**, because this service provider publishes an
encryption key in its metadata and Portico encrypts whenever one is
published. The signature, the time window, the audience, and whether
`InResponseTo` matches a request **this process actually sent** are all
verified by `crewjam/saml` in the single `ParseResponse` call; the program
re-checks none of it itself. **Hand-writing any one of those is the classic
route to a SAML integration that eventually accepts a forged assertion.**

**CAS 3.0** — the browser carries back nothing but an opaque ticket.
Everything else on the page comes from **a second request this server makes
itself**, which is why an intercepted CAS ticket is worth so little. After
reading the ticket, **refresh the page**: it is refused, because a service
ticket validates once and lives a minute. **That failure is the protocol
working.**

---

## Troubleshooting: three traps that look like something else

**1. The redirect URI is matched character for character.**
The one registered, the one the program sends, and the one the browser
actually visits have to be the same string. `localhost` and `127.0.0.1` are
the same host and **not the same string**; the same goes for the port. A
mismatch reports `invalid_request`, which reads as the sign-in itself having
failed.

**2. `PORTICO_PUBLIC_URL` has to be the address the browser uses.**
It is the OIDC issuer identifier, the discovery document is built from it, and
a relying party checks that what came back matches what it asked for.
Pointing at a server whose public URL says something else fails **during
discovery, at mock-sp's start-up** — not when you click.

**3. A requested scope has to be one the client was registered for.**
Both default to `openid profile email`. For `offline_access` — that is, for a
refresh token — it has to be in the registration and in `--scope`.

Two more belong to SAML and CAS:

**4. `sp register --metadata` refuses a cleartext `http` URL, so give it a
file.** That document states where assertions are delivered, and **anybody on
the path could redirect them elsewhere**. The program also serves the document
at `/saml/metadata`, but register from the file in `.mock-sp/`.

**5. Changing `--state-dir` means registering the service provider again.**
Portico encrypts the assertion with **the encryption key published in the
registered metadata**. A different directory is a different key, and the
symptom is a **decryption failure** rather than anything that explains itself.

---

## Stopping it

```bash
pkill -f '/mock-sp'
```

Do not kill the `go run` process id. **`go run` compiles to a cache and
executes a child**, so the PID you have is the compiler's wrapper — kill it
and the server is still listening. To start and stop it from a script, use
`go build -o <somewhere>/mock-sp ./examples/mock-sp` and run the artefact,
which is what `hack/walk-the-flow.sh` does.

---

## More than one tenant

Add the tenant path to the issuer, and register all three in that tenant:

```bash
portico client register --tenant acme --id mock-sp --name "Mock SP" --public \
  --redirect-uri http://localhost:8413/oidc/callback

go run ./examples/mock-sp --issuer http://localhost:8410/t/acme
# then step 3 again for sp and cas, with --tenant acme on both
```

The sign-in screen carries the tenant automatically, and the issuer on the
final page reads `.../t/acme`.

---

## Odds and ends

**The three protocols initialize independently.** Whichever one fails **says
why on the home page** while the other two carry on — a misconfigured SAML
certificate should not be able to take the OpenID Connect demonstration down
with it.

**It binds `127.0.0.1` by default.** These pages render an access token and
everything an assertion states about somebody, over cleartext HTTP. On
conference wifi, the default should not be something the whole room can reach.

**`.mock-sp/` is in `.gitignore`.** There is a private key in there — a
demonstration private key is still a private key in a repository.

**It is not part of a deployment.** Running it changes nothing in
[docs/integrations.md](../../docs/integrations.md), because it makes no
connection Portico would not make anyway.

**It works behind a proxy.** With a resident proxy such as Clash, `http_proxy`
is inherited by child processes, but Go does not proxy `localhost` and
loopback addresses by default — so mock-sp's outbound requests (discovery,
metadata, CAS ticket validation) are unaffected. Verifying a local address by
hand with `curl` does need `--noproxy '*'`.

The protocols themselves are described in
[docs/federation.md](../../docs/federation.md).
