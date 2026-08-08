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

import { useT } from "../i18n";

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

export function Modal({ open, title, onClose, children, footer }: ModalProps) {
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
          "w-full max-w-lg rounded-[var(--radius-lg)] bg-[var(--color-bg)]",
          "shadow-[var(--shadow-md)] max-h-[90vh] overflow-y-auto",
        )}
        onClick={(event) => event.stopPropagation()}
      >
        <header className="border-b border-[var(--color-border)] px-5 py-4">
          <h2 className="text-[length:var(--font-size-lg)] font-[weight:var(--font-weight-bold)] text-[var(--color-fg)]">
            {title}
          </h2>
        </header>
        <div className="px-5 py-4">{children}</div>
        {footer && (
          <footer className="flex justify-end gap-2 border-t border-[var(--color-border)] px-5 py-4">
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
  const t = useT();
  return (
    <tr>
      <td
        colSpan={colSpan}
        className="px-4 py-10 text-center text-[var(--color-fg-muted)]"
      >
        {t("common.empty")}
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
  tone: "danger" | "success";
  children: ReactNode;
}) {
  const tones = {
    danger:
      "bg-[var(--color-danger-bg)] border-[var(--color-danger-border)] text-[var(--color-danger-text)]",
    success:
      "bg-[var(--color-success-bg)] border-[var(--color-success-border)] text-[var(--color-success-text)]",
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
