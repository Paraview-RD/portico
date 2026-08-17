import { useEffect, useState } from "react";

import { trialApi } from "../api/endpoints";
import { BrandLockup } from "../components/brand";
import {
  ApplicationsIcon,
  IdentityProvidersIcon,
  UsersIcon,
} from "../components/icons";
import { LanguageMenu } from "../components/menu";
import { Button } from "../components/ui";
import { useT } from "../i18n";
import { useRouter } from "../router";

/**
 * What a stranger sees at the root address, when a deployment asks for it.
 *
 * Off by default and reached only through App's routing, so an ordinary
 * deployment behaves exactly as it did before this file existed: the root
 * address goes to the sign-in form. This is for the other case — a public
 * address somebody opens with no idea what they have found, where a sign-in
 * form asks for something they do not have and the way in is a line of small
 * print underneath it.
 *
 * Not an AuthShell. That frame is a narrow centred card built around a form,
 * and this page's job is the opposite: say what this is before asking for
 * anything. Reusing it would mean fighting a max-width meant for a password
 * field. The brand lockup does sit where AuthShell puts it, top-left, because
 * that position is a property of the page rather than of the screen.
 *
 * Four bands, each answering the next question somebody would ask: what is
 * this, what does it do, does it speak what I already have, and what do I get
 * if I say yes. An earlier version was the first band alone, which looked
 * finished and said almost nothing — a backdrop would have made that a
 * better-looking page with the same amount of content in it.
 *
 * The trial button and the last band appear only where trials are on, and
 * this page does not wait for that answer before rendering — the two calls
 * are independent, and a page that blocked on the second would show nothing
 * while it arrived.
 */

/**
 * The protocols, by the job they do rather than as one list.
 *
 * Names rather than translated phrases: `SAML 2.0` is `SAML 2.0` in every
 * language, and somebody scanning for the one they already run is looking for
 * the string. Only the group headings are translated.
 */
const SPEAKS = [
  { key: "sso", names: ["OAuth 2.1", "OpenID Connect", "SAML 2.0", "CAS"] },
  { key: "directory", names: ["LDAP", "SCIM 2.0"] },
  { key: "events", names: ["Webhooks"] },
] as const;

export function LandingPage() {
  const t = useT();
  const { navigate } = useRouter();
  const [trialsOpen, setTrialsOpen] = useState(false);

  useEffect(() => {
    const cancel = new AbortController();
    void trialApi
      .trialStatus(cancel.signal)
      .then((status) => setTrialsOpen(status.enabled))
      .catch(() => setTrialsOpen(false));
    return () => cancel.abort();
  }, []);

  return (
    // `isolate` is load-bearing, not a tidy-up. The backdrop below sits at a
    // negative z-index, and `relative` alone leaves this element at
    // z-index:auto — which is not a stacking context, so the backdrop escapes
    // to the root one and paints *behind* this element's own background,
    // where it is invisible. isolation:isolate makes this the context the
    // negative layer belongs to: above the page colour, below the text.
    <div className="relative isolate flex min-h-dvh flex-col bg-[var(--color-bg-soft)] p-4">
      {/* The backdrop. Absolute inside the page rather than fixed to the
          viewport: fixed keeps the glow glued to the window, so it follows
          you down the page and stops reading as light falling on the top of
          it. Absolute spans the document, which puts the two washes over the
          heading where they belong and leaves the grid running the whole way
          down.

          Behind everything at a negative z, and pointer-events-none, so
          nothing here can take a click. aria-hidden and empty: it carries no
          information, and a screen reader announcing "image" for a gradient
          is worse than silence. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 -z-10"
        style={{
          backgroundImage: `
            radial-gradient(60rem 40rem at 15% -10%, var(--landing-wash-1), transparent 70%),
            radial-gradient(50rem 35rem at 90% 5%, var(--landing-wash-2), transparent 70%)`,
        }}
      >
        {/* The dots are on their own layer so they can be faded out down the
            page without taking the washes with them. A mask rather than a
            gradient overlay: an overlay has to be the page's own colour to
            hide anything, which makes it one more place the background is
            written down, and it would sit over the washes too. */}
        <div
          className="absolute inset-0"
          style={{
            backgroundImage:
              "radial-gradient(var(--landing-dot) 1px, transparent 1px)",
            backgroundSize: "var(--landing-dot-gap) var(--landing-dot-gap)",
            maskImage: "linear-gradient(to bottom, #000 0, transparent 60%)",
            WebkitMaskImage:
              "linear-gradient(to bottom, #000 0, transparent 60%)",
          }}
        />
      </div>

      {/* The language menu belongs here more than anywhere: this is the one
          page whose whole audience arrived without an account, so nothing has
          told the server what language they read. Until now the only one was
          inside the signed-in shell, which is a strange place for it — the
          people who most need to change it are the ones who cannot reach it. */}
      <header className="flex shrink-0 items-start justify-between gap-4">
        <BrandLockup
          name="Portico"
          descriptor={t("brand.descriptor")}
          size={40}
        />
        <LanguageMenu />
      </header>

      <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col gap-16 py-16">
        {/* --- What is this ------------------------------------------- */}
        <section className="flex flex-col items-center gap-6 text-center">
          {/* The measure is narrower than the bands below it on purpose: a
              heading and a paragraph are read, and a line of text that runs
              the full width of a desktop is read badly. */}
          <h1 className="max-w-3xl text-[length:var(--font-size-display)] leading-[var(--leading-display)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
            {t("landing.title")}
          </h1>
          <p className="max-w-2xl text-[length:var(--font-size-lead)] leading-[var(--leading-normal)] text-[var(--color-fg-muted)]">
            {t("landing.subtitle")}
          </p>

          <div className="mt-2 flex flex-wrap justify-center gap-3">
            {trialsOpen && (
              <Button size="lg" onClick={() => navigate("/trial")}>
                {t("landing.tryIt")}
              </Button>
            )}
            <Button
              size="lg"
              variant={trialsOpen ? "secondary" : "primary"}
              onClick={() => navigate("/login")}
            >
              {t("landing.signIn")}
            </Button>
          </div>

          {/* Said here rather than left for somebody to find out: a visitor
              about to put their own address into a form is entitled to know
              that what they get is a demonstration, and how long it lasts.
              Shown only where trials are on, because that is exactly the
              deployment the sentence is true of — this flag is what turns a
              private console into a public one.

              Not an Alert. That component is role="alert", for something that
              just happened and has to interrupt; this is a standing fact
              about the page, and announcing it as an alert would talk over
              whatever a screen reader was already saying.

              Left-aligned inside a centred box: three lines of centred prose
              give every line a different starting point. */}
          {trialsOpen && (
            // The primary-soft pairing rather than the info tones. Both look
            // the same on a white page; only this one is redefined for the
            // dark theme. The status tones — info, success, warning, danger —
            // have no dark values at all, so a notice built from them is a
            // pale rectangle in the middle of a dark page. Nothing sets
            // data-theme today, so that is latent rather than broken; it costs
            // nothing to not add to it.
            <p className="max-w-2xl rounded-[var(--radius-md)] border border-[var(--color-primary-soft-hover)] bg-[var(--color-primary-soft)] px-4 py-3 text-left text-[length:var(--font-size-sm)] leading-[var(--leading-normal)] text-[var(--color-fg)]">
              {t("landing.demoNotice")}
            </p>
          )}
        </section>

        {/* --- What does it do --------------------------------------- */}
        <section>
          <ul className="grid gap-4 sm:grid-cols-3">
            {(
              [
                ["signIn", IdentityProvidersIcon],
                ["directory", UsersIcon],
                ["applications", ApplicationsIcon],
              ] as const
            ).map(([key, Icon]) => (
              <li
                key={key}
                className="flex flex-col gap-3 rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-bg)] p-5 shadow-[var(--shadow-sm)]"
              >
                <span className="flex h-9 w-9 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-primary-soft)] text-[var(--color-primary)]">
                  <Icon size={20} />
                </span>
                <h2 className="font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
                  {t(`landing.heading.${key}` as "landing.heading.signIn")}
                </h2>
                <p className="leading-[var(--leading-normal)] text-[var(--color-fg-muted)]">
                  {t(`landing.point.${key}` as "landing.point.signIn")}
                </p>
              </li>
            ))}
          </ul>
        </section>

        {/* --- Does it speak what I already have --------------------- */}
        <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-bg)] p-6 shadow-[var(--shadow-sm)]">
          <h2 className="text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
            {t("landing.speaks.title")}
          </h2>
          <p className="mt-2 leading-[var(--leading-normal)] text-[var(--color-fg-muted)]">
            {t("landing.speaks.subtitle")}
          </p>
          <dl className="mt-5 grid gap-5 sm:grid-cols-3">
            {SPEAKS.map(({ key, names }) => (
              <div key={key}>
                <dt className="text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg-muted)]">
                  {t(`landing.speaks.${key}` as "landing.speaks.sso")}
                </dt>
                <dd className="mt-2 flex flex-wrap gap-2">
                  {names.map((name) => (
                    <span
                      key={name}
                      className="rounded-[var(--radius-sm)] border border-[var(--color-border)] bg-[var(--color-bg-soft)] px-2 py-1 text-[length:var(--font-size-sm)] text-[var(--color-fg)]"
                    >
                      {name}
                    </span>
                  ))}
                </dd>
              </div>
            ))}
          </dl>
        </section>

        {/* --- What do I get if I say yes ----------------------------
            Only where there is a trial to get. This band is the answer to
            the button above it, and without trials there is no button. */}
        {trialsOpen && (
          <section>
            <h2 className="text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
              {t("landing.trial.title")}
            </h2>
            <ul className="mt-4 grid gap-4 sm:grid-cols-3">
              {(["own", "filled", "apps"] as const).map((key) => (
                <li
                  key={key}
                  className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-bg)] p-5 leading-[var(--leading-normal)] text-[var(--color-fg-muted)] shadow-[var(--shadow-sm)]"
                >
                  {t(`landing.trial.${key}` as "landing.trial.own")}
                </li>
              ))}
            </ul>
          </section>
        )}
      </main>

      {/* Ordinary links rather than buttons: they leave the application, and
          one of them leaves the site. */}
      <footer className="shrink-0 py-4 text-center text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        <a className="underline hover:no-underline" href="/docs/">
          {t("landing.docs")}
        </a>
        {" · "}
        <a
          className="underline hover:no-underline"
          href="https://github.com/Paraview-RD/portico"
          rel="noreferrer"
          target="_blank"
        >
          {t("landing.source")}
        </a>
      </footer>
    </div>
  );
}
