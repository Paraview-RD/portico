# Publishing a demo strangers can use

[access-guide.md](access-guide.md#publishing-a-demonstration-anybody-can-open)
covers getting a demonstration onto Render and handing somebody the address.
This page is the rest of it: a domain of your own, mail that arrives in other
people's inboxes, and self-service trials — the difference between a demo you
can show and a demo anyone can walk up to.

The order below is not a preference. Four of the steps produce a deployment
that looks entirely healthy while being broken in a way nothing reports, and
each of those is marked. Read past the step you are on before doing it.

## One domain, two jobs

The address people open and the address mail comes from should be the same
registered domain, for a reason that is not tidiness: a receiving mail server
that has never seen your domain before weighs everything it can find about
it, and a sending domain that also serves a real website over HTTPS looks
like an organization rather than like a name registered this morning to send
one campaign.

Which leaves one decision inside it. Send from a subdomain —
`mail.example.com` — rather than from the root, and point the public address
at another — `demo.example.com`. Sending reputation attaches to the domain
that signs the message, so a subdomain keeps a bad week for the demo's mail
from following the root domain to wherever you use it next. Either works;
this is the one that is harder to regret.

## Choosing a registrar

The prices are all within a few dollars of each other for a `.com`. What
actually differs is how much of yourself you have to hand over, and whether
the DNS is somewhere you want to be at two in the morning.

**Registration data is not optional.** ICANN requires a name, a postal
address, an email address and a phone number for every generic top-level
domain, and no registrar can waive it. What varies is who then sees it:
WHOIS privacy replaces your details in the public record with the
registrar's, and at the registrars below it is free and on by default. So
"as little information as possible" is a question about *verification*, not
about the form.

And that is where the sharpest difference is. **A registrar operating under
Chinese regulation requires real-name verification** — a government ID
uploaded and matched before the domain resolves at all. It is not a fee or a
delay; it is a different category of thing to hand over. If that is what you
are trying to avoid, the decision is made before you compare prices.

Among registrars that ask for an email address and a payment method and
nothing else:

| Registrar | Why it is on this list |
| --- | --- |
| **Cloudflare Registrar** | Sells at cost — no first-year discount and no renewal markup, so the number stays the number. WHOIS privacy always on and free. Its DNS is the same panel, which matters here because you are about to add four records and want them in one place. It will not let you keep DNS elsewhere; that is the trade. |
| **Porkbun** | Comparable pricing, free WHOIS privacy, unusually good DNS for the money, and no upsell path to click through. The right answer if you want the domain and the DNS separable later. |
| **Namecheap** | Larger, longer-established, free WHOIS privacy on most TLDs. Worth it if you already have an account there; not otherwise distinguishable from Porkbun for this. |

Any of the three finishes in about five minutes. Cloudflare is the one this
page assumes, because having the DNS in the same account as the registration
removes the step where the records go to the wrong panel.

### The top-level domain

`.com` is the conservative choice for a domain whose job is to deliver mail
to people who have never heard of you. Newer TLDs are cheaper and read
better, and they also carry a weaker default reputation with some filters,
because a great deal of throwaway sending has happened on them. This is a
heuristic rather than a measurement — filters do not publish their weights —
but the cost of being wrong is a confirmation email that silently does not
arrive, which is the one failure this whole page exists to avoid.

## The steps

### 1. Register the domain

Nothing else can be done first. DNS usually resolves within minutes of
registration; if the registrar is also the DNS host, immediately.

### 2. Verify the domain with your mail provider — before anything else needs it

Add the domain in the provider's dashboard and put the DKIM, SPF and DMARC
records it gives you into DNS. Wait for it to say verified.

**This is the step that fails invisibly.** Until a domain is verified, a
provider will typically still let you send — from its own shared address, to
the address that owns the account. So every test you run on yourself
succeeds, and every message to anybody else is refused or dropped, and the
application logs a successful send either way. A demo verified only against
your own inbox is a demo you have not tested.

Start DMARC at `p=none`. It asks for reports without asking receivers to
reject anything, which is what you want from a domain that has never sent
mail: a policy of `reject` on a misconfigured domain rejects your own
messages.

### 3. Apply the Blueprint

Render → New → Blueprint, pointed at your fork, with `render.yaml` in the
repository root. Change `region` first if Singapore is not the nearest one; a
service cannot be moved afterwards.

Leave the trial signup off. It has a prerequisite that is not in place until
step 6, and turning it on early produces a button that always fails.

For `PORTICO_PUBLIC_URL`, put in the `onrender.com` address for now. Step 5
is where it becomes the real one.

### 4. Point the domain at the service

Render → your service → Settings → Custom Domain → `demo.example.com`. It
gives you a CNAME. Add it at the registrar.

**If the DNS host proxies traffic — Cloudflare's orange cloud — turn it off
for this record.** Behind a proxy, Render cannot complete the challenge that
issues its certificate, and the symptom is a domain that resolves, answers,
and is untrusted by every browser.

### 5. Set `PORTICO_PUBLIC_URL` to the real address

`https://demo.example.com`, then redeploy.

**This one is worth stopping for.** Every link the server writes into an
email is built from this value, as are OpenID Connect redirects and SAML
metadata. Left pointing at the old address, the console works perfectly, the
mail sends successfully, and the only broken thing is the link inside it —
which is the only part of an email a recipient can act on. Nothing reports
this. There is no error; the address is simply the wrong one.

### 6. Give it a way to send mail

On a free Render instance this cannot be SMTP: outbound 25, 465 and 587 are
blocked there, and the symptom is a connection timeout that no relay setting
fixes. Use the HTTP transport:

```
PORTICO_MAIL_TRANSPORT   resend
PORTICO_RESEND_API_KEY   a send-only key
PORTICO_MAIL_FROM        portico@mail.example.com
```

`PORTICO_MAIL_FROM` must be on the domain verified in step 2. A sender on any
other domain is refused by the provider, not by Portico.

On a paid instance SMTP works on 465 and 587 — the `PORTICO_SMTP_*` keys are
listed in `render.yaml` and in
[access-guide.md](access-guide.md#then-if-you-want-self-service-trials).

### 7. Turn on the three switches

```
PORTICO_TRIAL_SIGNUP     true
PORTICO_LANDING_PAGE     true
PORTICO_TENANT_CONSOLE   true
```

In that order relative to step 6, not before it. Trials refuse every request
with `TRIAL_MAIL_UNAVAILABLE` when there is no relay.

The landing page gives the root address something other than a sign-in form,
which is what a stranger needs — a sign-in form asks them for something they
do not have. The tenant console is how you see what has been created without
opening any of it; it is visible only to an administrator of the default
tenant.

### 8. Keep it awake, and know when it dies

Add `DEMO_URL` as an Actions **variable** — not a secret — set to the public
address. Without it `.github/workflows/demo-keepalive.yml` runs and does
nothing, which looks exactly like running.

Then put two dates in a calendar somewhere that is not this repository:

- A free Render instance sleeps after fifteen minutes without traffic. The
  keepalive covers a working day, deliberately: 750 instance hours a month is
  not enough to stay awake around the clock. Outside that window the first
  visitor waits out a cold start.
- **A free Postgres expires thirty days after creation, plus a fourteen-day
  grace period.** Every trial tenant and everything in them goes with it, and
  the Blueprint has to be applied again. Nothing warns you.

### 9. Test it as somebody who is not you

Request a trial using an address you do not control — a colleague's, or a
disposable one. Then read the mail on that side and follow the link to the
end.

This is the only test that means anything, and it is the reason step 2 is
where it is. Sending to yourself passes on a deployment that can reach
nobody else, because the provider treats the account owner as a special
case. Everything up to here can be verified by looking; this one has to be
verified by somebody who is not you.

## What you should see at the end

- The root address explains what this is and offers two ways forward.
- A stranger who asks for a trial gets an email, within a minute, in their
  inbox rather than their spam folder.
- The link in it creates a tenant, and the second email carries credentials
  that sign in.
- `/tenants`, as an administrator of the default tenant, lists what exists.

If the first email does not arrive, the answer is almost always step 2 or
step 5 — the domain was never verified, or the link points somewhere that no
longer exists.
