/**
 * UI primitives.
 *
 * These follow the shadcn/ui convention — plain components owned by the
 * project rather than pulled from a component library, styled entirely
 * through the design tokens in styles/theme.css. They are hand-written
 * rather than generated so the dependency tree stays at zero UI packages,
 * which matters for a project whose selling point is a single small binary.
 *
 * Rule: no hard-coded colors here. Every color comes from a var(--color-*).
 *
 * Sizing note: controls are w-full by design. Passing a conflicting width
 * through className does NOT reliably win — Tailwind resolves conflicts by
 * stylesheet order, not by the order classes appear on the element. Wrap the
 * control in a sized container instead.
 */

import type {
  ButtonHTMLAttributes,
  InputHTMLAttributes,
  ReactElement,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from "react";
import {
  cloneElement,
  isValidElement,
  useEffect,
  useId,
  useState,
} from "react";

import { docsUrl, useLanguage, useT } from "../i18n";
import { ChevronRightIcon, CloseIcon, GuideIcon } from "./icons";

function cx(...classes: (string | false | undefined | null)[]): string {
  return classes.filter(Boolean).join(" ");
}

/* -------------------------------------------------------------- Button */

type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
type ButtonSize = "sm" | "md";

const buttonVariants: Record<ButtonVariant, string> = {
  primary:
    "bg-[var(--color-primary)] text-[var(--color-fg-on-primary)] hover:bg-[var(--color-primary-hover)]",
  secondary:
    "bg-[var(--color-bg)] text-[var(--color-fg)] border border-[var(--color-border-strong)] hover:bg-[var(--color-bg-hover)]",
  ghost:
    "text-[var(--color-fg-muted)] hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-fg)]",
  danger:
    "bg-[var(--color-danger)] text-[var(--color-fg-on-primary)] hover:opacity-90",
};

const buttonSizes: Record<ButtonSize, string> = {
  sm: "h-8 px-3 text-[length:var(--font-size-sm)]",
  md: "h-9 px-4 text-[length:var(--font-size-base)]",
};

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
}

export function Button({
  variant = "primary",
  size = "md",
  className,
  ...props
}: ButtonProps) {
  return (
    <button
      className={cx(
        "inline-flex items-center justify-center gap-1.5 rounded-[var(--radius-sm)]",
        "font-[weight:var(--font-weight-medium)] whitespace-nowrap transition-colors",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--color-primary)]",
        "disabled:opacity-50 disabled:pointer-events-none",
        buttonVariants[variant],
        buttonSizes[size],
        className,
      )}
      {...props}
    />
  );
}

/* --------------------------------------------------------------- Input */

interface FieldProps {
  label: string;
  hint?: string;
  error?: string;
  required?: boolean;
  children: ReactNode;
}

/**
 * Wraps a control with its label, hint, and error text.
 *
 * Two things here exist for assistive technology, both found by the browser
 * suite, which addresses fields the way a screen reader does rather than the
 * way a developer remembers writing them.
 *
 * The required marker is hidden from it: the asterisk restates the control's
 * own `required` attribute, which is already announced, and while it was
 * exposed the field's accessible name was "Password *".
 *
 * The hint and the error sit outside the label and are attached with
 * aria-describedby. A wrapping label contributes everything inside it to the
 * accessible name, so the sign-in field was named "Username, email, or phone
 * Any of the three reaches the same account." — the entire hint, read out as
 * though it were the field's name, every time focus arrived. As a
 * description it is announced after the name and can be skipped, which is
 * what a hint is for.
 */
export function Field({ label, hint, error, required, children }: FieldProps) {
  const id = useId();
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;

  // The error replaces the hint rather than joining it, matching what is
  // rendered: pointing at a description that is not on the page would leave
  // the control describedby nothing.
  const describedBy = error ? errorId : hint ? hintId : undefined;

  // Every control this is used with spreads its props onto a real element,
  // so the attribute lands where it has to. Anything else is passed through
  // untouched rather than silently dropped.
  const control =
    describedBy && isValidElement(children)
      ? cloneElement(
          children as ReactElement<{ "aria-describedby"?: string }>,
          {
            "aria-describedby": describedBy,
          },
        )
      : children;

  return (
    <div className="flex flex-col gap-1.5">
      <label className="flex flex-col gap-1.5">
        <span className="text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]">
          {label}
          {required && (
            <span className="text-[var(--color-danger)]" aria-hidden="true">
              {" "}
              *
            </span>
          )}
        </span>
        {control}
      </label>
      {hint && !error && (
        <span
          id={hintId}
          className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]"
        >
          {hint}
        </span>
      )}
      {error && (
        <span
          id={errorId}
          className="text-[length:var(--font-size-sm)] text-[var(--color-danger)]"
        >
          {error}
        </span>
      )}
    </div>
  );
}

const controlClasses = cx(
  "h-9 w-full rounded-[var(--radius-sm)] border border-[var(--color-border)]",
  "bg-[var(--color-bg)] px-3 text-[length:var(--font-size-base)] text-[var(--color-fg)]",
  "placeholder:text-[var(--color-fg-muted)]",
  "focus:outline-2 focus:outline-offset-[-1px] focus:outline-[var(--color-primary)]",
  "disabled:opacity-50",
);

export function Input({
  className,
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={cx(controlClasses, className)} {...props} />;
}

export function Select({
  className,
  ...props
}: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={cx(controlClasses, className)} {...props} />;
}

/**
 * A multi-line control, for the two things in this application that are
 * genuinely documents rather than values: a SAML metadata XML file and a
 * list of redirect URIs.
 *
 * It repeats the control styling minus the fixed height rather than reusing
 * controlClasses, because h-9 on a textarea makes it one line tall and no
 * amount of className afterwards reliably overrides it — Tailwind resolves
 * conflicts by stylesheet order, not by the order classes are written.
 */
export function Textarea({
  className,
  ...props
}: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={cx(
        "w-full rounded-[var(--radius-sm)] border border-[var(--color-border)]",
        "bg-[var(--color-bg)] px-3 py-2 text-[length:var(--font-size-sm)] text-[var(--color-fg)]",
        "placeholder:text-[var(--color-fg-muted)]",
        "focus:outline-2 focus:outline-offset-[-1px] focus:outline-[var(--color-primary)]",
        "disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

/**
 * A read-only value with a button that copies it.
 *
 * Used wherever the screen is handing somebody something to paste into
 * another system — an endpoint address, a client secret, a certificate. The
 * value stays selectable so that a browser without clipboard permission is
 * not a dead end.
 */
export function CopyField({
  label,
  value,
  multiline = false,
}: {
  label: string;
  value: string;
  multiline?: boolean;
}) {
  const t = useT();
  const [copied, setCopied] = useState(false);

  // The confirmation clears itself. Without the cleanup a component
  // unmounted inside the two seconds would set state after unmount.
  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 2000);
    return () => window.clearTimeout(timer);
  }, [copied]);

  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
    } catch {
      // Clipboard access can be refused — an insecure origin, or a browser
      // policy. The value is on screen and selectable, so there is nothing
      // to recover from and nothing worth interrupting anyone about.
    }
  }

  return (
    <div className="flex flex-col gap-1">
      <span className="text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)] text-[var(--color-fg)]">
        {label}
      </span>
      <div className="flex items-start gap-2">
        <code
          className={cx(
            "min-w-0 flex-1 rounded-[var(--radius-sm)] border border-[var(--color-border)]",
            "bg-[var(--color-bg-soft)] px-2 py-1.5",
            "text-[length:var(--font-size-xs)] text-[var(--color-fg)]",
            multiline
              ? "block max-h-40 overflow-auto whitespace-pre-wrap break-all"
              : "block truncate",
          )}
          title={multiline ? undefined : value}
        >
          {value}
        </code>
        <Button size="sm" variant="secondary" type="button" onClick={copy}>
          {copied ? t("common.copied") : t("common.copy")}
        </Button>
      </div>
    </div>
  );
}

/* --------------------------------------------------------------- Badge */

type BadgeTone = "neutral" | "success" | "danger" | "warning";

// Each tone is a matched set — soft background, its own border, and a text
// colour dark enough to stay legible on it. Deriving them from one hue with
// an opacity produces washed-out text at small sizes, which is exactly where
// a badge lives.
const badgeTones: Record<BadgeTone, string> = {
  neutral:
    "bg-[var(--color-bg-soft)] border-[var(--color-border)] text-[var(--color-fg-muted)]",
  success:
    "bg-[var(--color-success-bg)] border-[var(--color-success-border)] text-[var(--color-success-text)]",
  danger:
    "bg-[var(--color-danger-bg)] border-[var(--color-danger-border)] text-[var(--color-danger-text)]",
  warning:
    "bg-[var(--color-warning-bg)] border-[var(--color-warning-border)] text-[var(--color-warning-text)]",
};

export function Badge({
  tone = "neutral",
  children,
}: {
  tone?: BadgeTone;
  children: ReactNode;
}) {
  return (
    <span
      className={cx(
        "inline-flex items-center rounded-full border px-2 py-0.5",
        "text-[length:var(--font-size-xs)] font-[weight:var(--font-weight-medium)]",
        badgeTones[tone],
      )}
    >
      {children}
    </span>
  );
}

/* --------------------------------------------------------------- Modal */

interface ModalProps {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}

/**
 * A modal dialog whose chrome stays put.
 *
 * The layout is deliberate and was once wrong: `overflow-y-auto` sat on the
 * dialog itself, so a form taller than the viewport scrolled its own title
 * off the top and its own Save button off the bottom. What the person filling
 * in a long registration form could see was a stack of fields with no
 * indication of what they were registering and no way to submit it.
 *
 * So the dialog is a column with a fixed height budget, and only the middle
 * of it scrolls. The header and footer are `shrink-0` rather than merely
 * outside the scrolling region: without that, flex would compress them to fit
 * rather than letting the middle take the overflow, and the title would be
 * squashed instead of pinned.
 *
 * The close button belongs to that pinned header for the same reason. A way
 * out that is only reachable before anyone has scrolled is not a way out.
 */
export function Modal({ open, title, onClose, children, footer }: ModalProps) {
  const t = useT();

  // Escape closes the dialog, which users expect and which also gives
  // keyboard users a way out without reaching for the mouse.
  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
      role="presentation"
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={cx(
          "flex w-full max-w-lg flex-col rounded-[var(--radius-lg)]",
          "bg-[var(--color-bg)] shadow-[var(--shadow-md)] max-h-[90vh]",
        )}
        onClick={(event) => event.stopPropagation()}
      >
        <header className="flex shrink-0 items-start justify-between gap-3 border-b border-[var(--color-border)] px-5 py-4">
          <h2 className="text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
            {title}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label={t("common.close")}
            className={cx(
              "-mr-1 -mt-1 inline-flex h-8 w-8 shrink-0 items-center justify-center",
              "rounded-[var(--radius-sm)] text-[var(--color-fg-muted)]",
              "hover:bg-[var(--color-bg-hover)] hover:text-[var(--color-fg)]",
              "focus-visible:outline-2 focus-visible:outline-offset-2",
              "focus-visible:outline-[var(--color-primary)]",
            )}
          >
            <CloseIcon size={18} />
          </button>
        </header>
        <div className="overflow-y-auto px-5 py-4">{children}</div>
        {footer && (
          <footer className="flex shrink-0 justify-end gap-2 border-t border-[var(--color-border)] px-5 py-4">
            {footer}
          </footer>
        )}
      </div>
    </div>
  );
}

/** A confirmation dialog for actions that are awkward to undo. */
export function ConfirmDialog({
  open,
  title,
  message,
  destructive,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  message: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  return (
    <Modal
      open={open}
      title={title}
      onClose={onCancel}
      footer={
        <>
          <Button variant="secondary" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button
            variant={destructive ? "danger" : "primary"}
            onClick={onConfirm}
          >
            {t("common.confirm")}
          </Button>
        </>
      }
    >
      <p className="text-[var(--color-fg)]">{message}</p>
    </Modal>
  );
}

/* --------------------------------------------------------------- Table */

export function Table({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-x-auto rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-bg)]">
      <table className="w-full border-collapse text-[length:var(--font-size-base)]">
        {children}
      </table>
    </div>
  );
}

export function Th({ children }: { children: ReactNode }) {
  return (
    <th
      className={cx(
        "border-b border-[var(--color-border)] bg-[var(--color-bg-hover)] px-4 py-2.5 text-left",
        "text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)] text-[var(--color-fg-muted)]",
      )}
    >
      {children}
    </th>
  );
}

export function Td({
  children,
  className,
  colSpan,
}: {
  children: ReactNode;
  className?: string;
  /** For a cell that spans the table, such as an expanded detail panel. */
  colSpan?: number;
}) {
  return (
    <td
      colSpan={colSpan}
      className={cx(
        "border-b border-[var(--color-border)] px-4 py-2.5 text-[var(--color-fg)]",
        className,
      )}
    >
      {children}
    </td>
  );
}

/** Row shown in place of a table body when there is nothing to list. */
export function EmptyRow({ colSpan }: { colSpan: number }) {
  return <MessageRow colSpan={colSpan} messageKey="common.empty" />;
}

/**
 * Row shown while the list is still being fetched.
 *
 * A separate component from EmptyRow rather than a flag on it, because the
 * two say opposite things and a boolean that flips what a component means is
 * how a caller ends up passing the wrong one.
 *
 * It exists because three screens had no loading state at all: their rows
 * came from a `null` initial value, so `rows?.length === 0` was false and
 * `rows?.map` was undefined, and the body rendered as nothing — identical to
 * "there is nothing here". A reader cannot tell a slow query from an empty
 * tenant, and the two call for opposite reactions.
 */
export function LoadingRow({ colSpan }: { colSpan: number }) {
  return <MessageRow colSpan={colSpan} messageKey="common.loading" />;
}

function MessageRow({
  colSpan,
  messageKey,
}: {
  colSpan: number;
  messageKey: "common.empty" | "common.loading";
}) {
  const t = useT();
  return (
    <tr>
      {/* colSpan matters: without it the cell occupies one column and the
          message sits under the first heading rather than under the table.
          Three screens were writing this row by hand and none of them
          passed it. */}
      <td
        colSpan={colSpan}
        className="px-4 py-10 text-center text-[var(--color-fg-muted)]"
      >
        {t(messageKey)}
      </td>
    </tr>
  );
}

/* ---------------------------------------------------------- Pagination */

export function Pagination({
  page,
  pageSize,
  total,
  onChange,
}: {
  page: number;
  pageSize: number;
  total: number;
  onChange: (page: number) => void;
}) {
  const t = useT();
  const lastPage = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div className="flex items-center justify-between gap-4 pt-3">
      <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
        {t("common.totalItems", total)}
      </span>
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          variant="secondary"
          disabled={page <= 1}
          onClick={() => onChange(page - 1)}
        >
          {t("common.previous")}
        </Button>
        <span className="text-[length:var(--font-size-sm)] text-[var(--color-fg-muted)]">
          {t("common.pageOf", page, lastPage)}
        </span>
        <Button
          size="sm"
          variant="secondary"
          disabled={page >= lastPage}
          onClick={() => onChange(page + 1)}
        >
          {t("common.next")}
        </Button>
      </div>
    </div>
  );
}

/* -------------------------------------------------------------- Alerts */

export function Alert({
  tone,
  children,
}: {
  // warning is for "this worked, and there is something you must do now" —
  // a one-time token being the case that needed it. Saying that in the
  // danger colour would read as a failure, which is the opposite.
  tone: "danger" | "success" | "warning";
  children: ReactNode;
}) {
  const tones = {
    danger:
      "bg-[var(--color-danger-bg)] border-[var(--color-danger-border)] text-[var(--color-danger-text)]",
    success:
      "bg-[var(--color-success-bg)] border-[var(--color-success-border)] text-[var(--color-success-text)]",
    warning:
      "bg-[var(--color-warning-bg)] border-[var(--color-warning-border)] text-[var(--color-warning-text)]",
  };
  return (
    <div
      role="alert"
      className={cx(
        "rounded-[var(--radius-sm)] border px-3 py-2",
        "text-[length:var(--font-size-sm)] leading-[var(--leading-normal)]",
        tones[tone],
      )}
    >
      {children}
    </div>
  );
}

/* ----------------------------------------------------------------- Card */

/**
 * A white surface on the page background.
 *
 * Tables bring their own; everything else that is a block of content — a
 * form, a summary, a panel — goes in one of these, so no screen ends up with
 * controls floating directly on the page.
 */
export function Card({
  title,
  children,
  className,
}: {
  title?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section
      className={cx(
        "rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-bg)] p-5",
        className,
      )}
    >
      {title && (
        <h2 className="mb-4 text-[length:var(--font-size-base)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
          {title}
        </h2>
      )}
      {children}
    </section>
  );
}

/* ---------------------------------------------------------- Page shell */

export function PageHeader({
  title,
  subtitle,
  actions,
}: {
  title: string;
  subtitle?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
      <div>
        <h1 className="text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
          {title}
        </h1>
        {subtitle && (
          <p className="mt-0.5 text-[var(--color-fg-muted)]">{subtitle}</p>
        )}
      </div>
      {actions && <div className="flex gap-2">{actions}</div>}
    </div>
  );
}

/* ----------------------------------------------------------- Guide panel */

/**
 * The three or four sentences that say what a screen is for.
 *
 * Every screen in this console already has a one-line subtitle, and that line
 * answers "what is this" without answering "when would I need it" or "what do
 * I have to have ready first". Somebody landing on Provisioning for the first
 * time can read "push accounts to downstream systems" and still not know that
 * it is the opposite direction from Directories, which is the thing they
 * actually needed to know.
 *
 * **Length is capped by convention, not by the component.** The manual is
 * compiled into the same binary and `DocsLink` already routes to it in the
 * reader's language, so anything beyond a short paragraph belongs there and
 * not in the translation bundles. Two copies of the same explanation, one in
 * `zh-CN.ts` and one in `docs/*.zh.md`, is a guarantee that they will
 * disagree; which one is wrong is then anybody's guess.
 *
 * Open on arrival and collapsible, remembered per panel. An explanation that
 * has to be found is not read by the person who needed it, and one that
 * cannot be dismissed becomes furniture for the administrator who reads it
 * every day for a year.
 */
export function GuidePanel({
  id,
  title,
  children,
  docsPage,
}: {
  /** Distinguishes this panel's collapsed state from every other panel's. */
  id: string;
  title: string;
  children: ReactNode;
  docsPage?: string;
}) {
  const { language, t } = useLanguage();
  const storageKey = `portico.guide.${id}`;

  // Read during initialization rather than in an effect: an effect would
  // render the panel open and then shut it, which flashes the explanation at
  // the reader who already dismissed it.
  const [open, setOpen] = useState(() => {
    try {
      return localStorage.getItem(storageKey) !== "collapsed";
    } catch {
      // Storage can be unavailable — a locked-down browser, private mode in
      // some versions. Defaulting to open is the harmless direction.
      return true;
    }
  });

  const toggle = () => {
    const next = !open;
    setOpen(next);
    try {
      localStorage.setItem(storageKey, next ? "expanded" : "collapsed");
    } catch {
      // The panel still works for this visit; only the memory is lost.
    }
  };

  const bodyId = `${storageKey}.body`;

  return (
    <section
      className={cx(
        "mb-5 rounded-[var(--radius-md)] border border-[var(--color-border)]",
        "bg-[var(--color-bg-soft)]",
      )}
    >
      <button
        type="button"
        onClick={toggle}
        aria-expanded={open}
        aria-controls={bodyId}
        className={cx(
          "flex w-full items-center gap-2 px-4 py-3 text-left",
          "text-[length:var(--font-size-sm)] font-[weight:var(--font-weight-medium)]",
          "text-[var(--color-fg)] hover:text-[var(--color-primary)]",
        )}
      >
        <GuideIcon
          size={16}
          className="shrink-0 text-[var(--color-fg-muted)]"
        />
        <span className="flex-1">{title}</span>
        <ChevronRightIcon
          size={16}
          className={cx(
            "shrink-0 text-[var(--color-fg-muted)] transition-transform",
            open && "rotate-90",
          )}
        />
      </button>
      {open && (
        <div
          id={bodyId}
          className={cx(
            "px-4 pb-3 text-[length:var(--font-size-sm)]",
            "leading-[var(--leading-normal)] text-[var(--color-fg-muted)]",
          )}
        >
          {children}
          {docsPage && (
            <p className="mt-2">
              <a
                href={docsUrl(language, docsPage)}
                target="_blank"
                rel="noreferrer"
                className="text-[var(--color-primary)] hover:underline"
              >
                {t("common.docs")}
              </a>
            </p>
          )}
        </div>
      )}
    </section>
  );
}

/* ------------------------------------------------------------ App icon */

/**
 * The picture on an application's tile, with a fallback that always works.
 *
 * Two shapes, in order of preference. A registered logo is rendered as an
 * image; anything else — no logo, or a logo that fails to load — becomes a
 * lettered tile in a colour derived from the name.
 *
 * The fallback is not a placeholder for a missing feature. Most deployments
 * will never upload a logo for an internal tool, and a wall of identical grey
 * squares would be worse than no icons at all. A coloured initial is what
 * makes a portal scannable: people find an application by its shape and
 * colour long before they finish reading its name.
 *
 * **Rendered through `<img>`, and that matters.** A logo may be an SVG, and
 * an SVG is a document that can carry script. A browser does not run that
 * script when the file is loaded as an image, which is the entire reason
 * accepting a whole SVG here is safe. Inlining one instead — to recolour it
 * with CSS, say — would turn every registered logo into stored cross-site
 * scripting on everybody's home screen.
 */
export function AppIcon({
  name,
  src,
  size = 40,
}: {
  name: string;
  src?: string;
  size?: number;
}) {
  const [failed, setFailed] = useState(false);

  const dimensions = { width: size, height: size };

  if (src && !failed) {
    return (
      <img
        src={src}
        // Decorative: the application's name is always beside it, and a
        // screen reader announcing it twice is worse than not at all.
        alt=""
        width={size}
        height={size}
        loading="lazy"
        // An external logo would otherwise tell its host the address of the
        // page every visitor opened it from.
        referrerPolicy="no-referrer"
        onError={() => setFailed(true)}
        className="shrink-0 rounded-[var(--radius-sm)] object-cover"
        style={dimensions}
      />
    );
  }

  // The first character, which for a Chinese name is a whole word and for a
  // Latin one an initial — both of which are what somebody expects on a tile.
  const initial = [...name.trim()][0] ?? "?";

  return (
    <span
      aria-hidden="true"
      className="flex shrink-0 items-center justify-center rounded-[var(--radius-sm)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg-on-primary)]"
      style={{
        ...dimensions,
        background: `var(--color-tile-${tileColour(name)})`,
        fontSize: Math.round(size * 0.45),
      }}
    >
      {initial}
    </span>
  );
}

/**
 * Which of the six tile colours a name gets, 1-based.
 *
 * Any stable function of the name would do. What it must not be is random or
 * index-based: an application that changes colour when the list is re-sorted,
 * or between two visits, is worse than one with no colour at all, because
 * people navigate by it.
 */
function tileColour(name: string): number {
  let hash = 0;
  for (const character of name) {
    hash = (hash * 31 + character.codePointAt(0)!) % 100000;
  }
  return (hash % 6) + 1;
}

/**
 * A link to the page of the manual that explains this screen.
 *
 * Contextual rather than one entry in the sidebar, because that is the whole
 * advantage of shipping the manual inside the product: the answer is one
 * click from the question rather than a search away. It opens in a new tab —
 * somebody halfway through configuring a directory should not lose the form
 * to read about it.
 *
 * The reader's own language, and the manual falls back to English with a
 * notice on pages that have not been translated yet.
 */
export function DocsLink({ page }: { page: string }) {
  const t = useT();
  const { language } = useLanguage();

  return (
    <a
      href={docsUrl(language, page)}
      target="_blank"
      rel="noreferrer"
      className={cx(
        "inline-flex h-8 items-center rounded-[var(--radius-sm)] px-3",
        "border border-[var(--color-border-strong)] bg-[var(--color-bg)]",
        "text-[length:var(--font-size-sm)] text-[var(--color-fg)]",
        "hover:bg-[var(--color-bg-hover)]",
      )}
    >
      {t("common.docs")}
    </a>
  );
}
