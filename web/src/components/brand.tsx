/**
 * The brand mark: an arch on two piers, standing on the ground line.
 *
 * A portico is the covered entrance a building presents to the street — the
 * threshold you pass through, and the face it shows before you are inside.
 * For something that stands in front of every other system and decides who
 * gets in, that is a more honest image than a padlock, which says "locked"
 * when the job is "who may pass".
 *
 * Drawn on a 32×32 grid in `currentColor`, so one file serves the white-on-
 * blue tile in the shell, a flat monochrome rendering, and the favicon.
 *
 * The geometry is tuned for 16px rather than for a presentation slide. The
 * piers were narrowed and the opening raised from a first version whose
 * counter closed up at tab-icon size — which is the size the mark is
 * actually seen at most of the time.
 */

import { useEffect, useState } from "react";

interface MarkProps {
  size?: number;
  className?: string;
}

/** BrandMark renders the glyph alone, in `currentColor`. */
export function BrandMark({ size = 20, className }: MarkProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      aria-hidden="true"
      className={className}
    >
      <path
        d="M8 26V13.5a8 8 0 0 1 16 0V26"
        stroke="currentColor"
        strokeWidth={2.6}
        strokeLinecap="round"
      />
      <path
        d="M4 26h24"
        stroke="currentColor"
        strokeWidth={2.6}
        strokeLinecap="round"
      />
    </svg>
  );
}

/**
 * BrandTile is the mark on its blue rounded square — the form it takes in
 * the sidebar and above the sign-in card.
 */
export function BrandTile({ size = 32 }: { size?: number }) {
  return (
    <span
      className="inline-flex shrink-0 items-center justify-center rounded-[var(--radius-md)] text-[var(--color-fg-on-primary)]"
      style={{
        width: size,
        height: size,
        background:
          "linear-gradient(135deg, var(--color-primary) 0%, var(--color-primary-active) 100%)",
      }}
    >
      <BrandMark size={Math.round(size * 0.62)} />
    </span>
  );
}

/**
 * BrandLockup is the tile plus the wordmark: what appears at the top of the
 * sidebar and on the signed-out screens.
 *
 * The descriptor under the name is set in small caps with wide tracking so it
 * reads as a label rather than a second line of the name.
 */
export function BrandLockup({
  name,
  descriptor,
  size = 32,
  logoSrc,
}: {
  name: string;
  descriptor?: string;
  size?: number;
  /**
   * A branding override for the mark, in place of the vector portico glyph.
   * Absent on every screen except the four unauthenticated ones, which are
   * the only place a deployment's own logo is meant to appear — see
   * AuthShell. Falls back to the glyph on a load error, the same as
   * AppIcon, rather than leaving a broken-image icon in the corner of every
   * sign-in attempt.
   */
  logoSrc?: string;
}) {
  const [logoFailed, setLogoFailed] = useState(false);
  // Reset when the address itself changes, not just once per mount. The
  // live branding preview re-renders this component on every keystroke
  // with a new logoSrc — without this, typing a broken URL first and then
  // correcting it would leave the fallback glyph showing forever, telling
  // whoever is editing the field their working logo is still broken.
  useEffect(() => {
    setLogoFailed(false);
  }, [logoSrc]);

  return (
    <div className="flex items-center gap-2.5">
      {logoSrc && !logoFailed ? (
        <img
          src={logoSrc}
          // Decorative: the product name renders right beside it.
          alt=""
          width={size}
          height={size}
          loading="lazy"
          // An external logo would otherwise tell its host the address of
          // every sign-in page it appeared on.
          referrerPolicy="no-referrer"
          onError={() => setLogoFailed(true)}
          className="shrink-0 object-contain"
          style={{ width: size, height: size }}
        />
      ) : (
        <BrandTile size={size} />
      )}
      <div className="min-w-0 leading-tight">
        <div className="truncate text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
          {name}
        </div>
        {descriptor && (
          <div className="truncate text-[length:var(--font-size-xs)] tracking-[0.08em] text-[var(--color-fg-subtle)] uppercase">
            {descriptor}
          </div>
        )}
      </div>
    </div>
  );
}
