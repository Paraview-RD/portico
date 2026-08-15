import { useEffect, useState } from "react";

import { trialApi } from "../api/endpoints";
import { BrandLockup } from "../components/brand";
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
 * field.
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
    <div className="flex min-h-dvh flex-col bg-[var(--color-bg-soft)]">
      <main className="mx-auto flex w-full max-w-2xl flex-1 flex-col justify-center gap-8 p-6">
        <BrandLockup
          name="Portico"
          descriptor={t("brand.descriptor")}
          size={48}
        />

        <div className="flex flex-col gap-4">
          <h1 className="text-[length:var(--font-size-xl)] font-semibold text-[var(--color-fg)]">
            {t("landing.title")}
          </h1>
          <p className="text-[length:var(--font-size-lg)] text-[var(--color-fg-muted)]">
            {t("landing.subtitle")}
          </p>
        </div>

        {/* Three things it does, rather than a feature list. Somebody who has
            just arrived is deciding whether this is the kind of thing they
            were looking for, which is a coarser question than which
            protocols it speaks. */}
        <ul className="flex flex-col gap-3">
          {["signIn", "directory", "applications"].map((key) => (
            <li
              key={key}
              className="flex gap-3 text-[var(--color-fg-muted)]"
              // A bullet drawn rather than a list-style, so it lines up with
              // the text it belongs to at every font size.
            >
              <span aria-hidden="true" className="text-[var(--color-primary)]">
                —
              </span>
              <span>{t(`landing.point.${key}` as "landing.point.signIn")}</span>
            </li>
          ))}
        </ul>

        <div className="flex flex-wrap gap-3">
          {trialsOpen && (
            <Button onClick={() => navigate("/trial")}>
              {t("landing.tryIt")}
            </Button>
          )}
          <Button
            variant={trialsOpen ? "secondary" : "primary"}
            onClick={() => navigate("/login")}
          >
            {t("landing.signIn")}
          </Button>
        </div>

        {/* Ordinary links rather than buttons: they leave the application,
            and one of them leaves the site. */}
        <p className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
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
        </p>
      </main>
    </div>
  );
}
