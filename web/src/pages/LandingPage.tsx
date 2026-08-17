import { useEffect, useState } from "react";

import { trialApi } from "../api/endpoints";
import { BrandLockup } from "../components/brand";
import {
  ApplicationsIcon,
  IdentityProvidersIcon,
  UsersIcon,
} from "../components/icons";
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
 * Centred rather than a left-hand column. The first version was one column
 * pinned left at the console's type scale, which on a wide display left two
 * thirds of the page empty and every line the same size as every other. The
 * fix is not more furniture: it is a heading at a display size, a measure
 * that stops before the line gets long, and the three things it does as three
 * surfaces instead of three dashes.
 *
 * The trial button appears only where trials are on, and this page does not
 * wait for that answer before rendering — the two calls are independent, and
 * a page that blocked on the second would show nothing while it arrived.
 */
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
    <div className="flex min-h-dvh flex-col bg-[var(--color-bg-soft)] p-4">
      <header className="shrink-0">
        <BrandLockup
          name="Portico"
          descriptor={t("brand.descriptor")}
          size={40}
        />
      </header>

      <main className="mx-auto flex w-full max-w-5xl flex-1 flex-col justify-center gap-12 py-12">
        <div className="flex flex-col items-center gap-6 text-center">
          {/* The measure is narrower than the section below it on purpose: a
              heading and a paragraph are read, and a line of text that runs
              the full width of a desktop is read badly. The cards can be
              wider because each holds one short sentence. */}
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
              whatever a screen reader was already saying. */}
          {trialsOpen && (
            // Left-aligned inside a centred box. Three lines of centred prose
            // give every line a different starting point, and the eye has to
            // find each one.
            <p className="max-w-2xl rounded-[var(--radius-md)] border border-[var(--color-info-border)] bg-[var(--color-info-bg)] px-4 py-3 text-left text-[length:var(--font-size-sm)] leading-[var(--leading-normal)] text-[var(--color-info-text)]">
              {t("landing.demoNotice")}
            </p>
          )}
        </div>

        {/* Three things it does, rather than a feature list. Somebody who has
            just arrived is deciding whether this is the kind of thing they
            were looking for, which is a coarser question than which protocols
            it speaks — so the protocols are inside the sentences rather than
            being the headings. */}
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
