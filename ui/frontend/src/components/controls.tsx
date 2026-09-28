import {
  useEffect,
  useRef,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
} from "react";
import { AppService } from "@bindings";
import appIcon from "../assets/app_icon.svg";
import { report } from "../lib";

// Components that follow the macOS app's SwiftUI look: grouped forms,
// switches, bordered buttons and sheets.

export function cx(...classes: (string | false | undefined | null)[]) {
  return classes.filter(Boolean).join(" ");
}

type ButtonVariant = "bordered" | "prominent";

/** SwiftUI-style button: `.bordered` by default, `.borderedProminent` for the default action. */
export function Button({
  children,
  variant = "bordered",
  size = "regular",
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: "small" | "regular" | "large";
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      {...props}
      className={cx(
        "inline-flex shrink-0 items-center justify-center gap-1 rounded-[6px] leading-none whitespace-nowrap",
        "shadow-[0_0.5px_1px_rgb(0_0_0/0.12)] transition-colors disabled:opacity-45",
        size === "small"
          ? "h-[22px] px-2.5 text-[12px]"
          : size === "large"
            ? "h-[30px] min-w-[80px] rounded-[7px] px-4 text-[13px]"
            : "h-[26px] min-w-[68px] px-3 text-[13px]",
        variant === "prominent"
          ? "bg-mac-accent text-white enabled:hover:bg-mac-accent-hover"
          : "border border-mac-control-border bg-mac-control enabled:hover:brightness-[0.97] enabled:active:brightness-[0.93]",
        className,
      )}
    >
      {children}
    </button>
  );
}

/** macOS switch (`.toggleStyle(.switch)`). */
export function Switch({
  checked,
  onChange,
  disabled,
  label,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  label: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cx(
        "relative h-[20px] w-[36px] shrink-0 rounded-full transition-colors duration-150 disabled:opacity-45",
        checked ? "bg-mac-accent" : "bg-mac-switch-off",
      )}
    >
      <span
        className={cx(
          "absolute top-[2px] size-[16px] rounded-full bg-white shadow-[0_1px_2px_rgb(0_0_0/0.3)] transition-[left] duration-150",
          checked ? "left-[18px]" : "left-[2px]",
        )}
      />
    </button>
  );
}

export function TextField({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      type="text"
      spellCheck={false}
      autoComplete="off"
      {...props}
      className={cx(
        "h-[26px] min-w-0 rounded-[6px] border border-mac-control-border bg-mac-control px-2 text-[13px]",
        "shadow-[inset_0_0.5px_1px_rgb(0_0_0/0.06)] select-text placeholder:text-mac-tertiary",
        "focus:outline-[3px] focus:outline-offset-0 focus:outline-mac-accent/45 disabled:opacity-50",
        className,
      )}
    />
  );
}

/** `.pickerStyle(.menu)`: a native select styled as a pop-up button. */
export function Picker({ className, children, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <div className="relative">
      <select
        {...props}
        className={cx(
          "h-[22px] appearance-none rounded-[6px] border border-mac-control-border bg-mac-control py-0 pr-7 pl-2.5 text-[13px]",
          "shadow-[0_0.5px_1px_rgb(0_0_0/0.12)]",
          className,
        )}
      >
        {children}
      </select>
      <span className="pointer-events-none absolute top-1/2 right-1 flex h-[16px] w-[16px] -translate-y-1/2 items-center justify-center rounded-[4px] bg-mac-accent text-white">
        <svg viewBox="0 0 10 10" className="size-[9px]" fill="none" stroke="currentColor" strokeWidth="1.5">
          <path d="M3 4 5 2 7 4M3 6l2 2 2-2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </span>
    </div>
  );
}

/** A `Form` scroll area with `.formStyle(.grouped)` spacing. */
export function Form({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto flex max-w-[640px] flex-col gap-5 px-5 pt-3 pb-6">{children}</div>
    </div>
  );
}

/** A grouped form section: a header above a rounded box of rows. */
export function Section({
  header,
  accessory,
  children,
}: {
  header?: ReactNode;
  accessory?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-1.5">
      {(header || accessory) && (
        <div className="flex min-h-[20px] items-end justify-between px-2.5">
          <h2 className="text-[13px] font-semibold">{header}</h2>
          {accessory}
        </div>
      )}
      <div className="overflow-hidden rounded-[10px] border border-mac-group-border bg-mac-group">
        <div className="flex flex-col [&>*+*]:relative [&>*+*]:before:pointer-events-none [&>*+*]:before:absolute [&>*+*]:before:inset-x-2.5 [&>*+*]:before:top-0 [&>*+*]:before:h-px [&>*+*]:before:bg-mac-separator">
          {children}
        </div>
      </div>
    </section>
  );
}

/** A form row: title (and optional description) on the left, accessory on the right. */
export function Row({
  title,
  description,
  children,
  onClick,
}: {
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  onClick?: () => void;
}) {
  return (
    <div
      onClick={onClick}
      className={cx(
        "flex min-h-[38px] items-center gap-4 px-2.5 py-2",
        onClick && "cursor-default hover:bg-mac-fill active:bg-mac-separator",
      )}
    >
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="break-words">{title}</div>
        {description && <div className="text-[11px] leading-[14px] text-mac-secondary">{description}</div>}
      </div>
      {children !== undefined && <div className="flex shrink-0 items-center gap-2">{children}</div>}
    </div>
  );
}

/** Secondary-colored value text shown at the trailing edge of a row. */
export function Value({ children }: { children: ReactNode }) {
  return <span className="max-w-[260px] truncate text-mac-secondary select-text">{children}</span>;
}

export function openExternal(href: string) {
  report(AppService.OpenURL(href));
}

/** A `Link` row: accent title with the arrow.up.forward glyph. */
export function LinkRow({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        openExternal(href);
      }}
      className="flex min-h-[38px] items-center gap-4 px-2.5 py-2 text-mac-accent"
    >
      <span className="flex-1">{children}</span>
      <ArrowUpForward />
    </a>
  );
}

/** An inline link that opens in the default browser. */
export function ExternalLink({ href, children, className }: { href: string; children: ReactNode; className?: string }) {
  return (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        openExternal(href);
      }}
      className={cx("text-mac-accent hover:underline", className)}
    >
      {children}
    </a>
  );
}

function ArrowUpForward() {
  return (
    <svg viewBox="0 0 12 12" className="size-[11px] text-mac-secondary" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path d="M3.5 8.5 8.5 3.5M4.5 3.5h4v4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function Chevron() {
  return (
    <svg viewBox="0 0 8 12" className="h-[11px] w-[7px] text-mac-tertiary" fill="none" stroke="currentColor" strokeWidth="1.8">
      <path d="M2 2l4 4-4 4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

const dotColors: Record<string, string> = {
  green: "#34c759",
  gray: "#8e8e93",
  yellow: "#ffcc00",
  orange: "#ff9500",
};

/** 8pt status circle, as in the macOS status view. */
export function StatusDot({ color }: { color: string }) {
  return (
    <span
      aria-hidden
      className="inline-block size-[8px] shrink-0 rounded-full"
      style={{ background: dotColors[color] ?? dotColors.gray }}
    />
  );
}

/** Small indeterminate spinner (`ProgressView()`). */
export function Spinner({ size = 16 }: { size?: number }) {
  return (
    <svg
      viewBox="0 0 16 16"
      width={size}
      height={size}
      className="text-mac-secondary"
      style={{ animation: "mac-spin 0.9s steps(8) infinite" }}
      aria-label="Loading"
    >
      {Array.from({ length: 8 }, (_, i) => (
        <rect
          key={i}
          x="7.25"
          y="1"
          width="1.5"
          height="4"
          rx="0.75"
          fill="currentColor"
          opacity={0.25 + (i / 8) * 0.75}
          transform={`rotate(${i * 45} 8 8)`}
        />
      ))}
    </svg>
  );
}

/** Indeterminate linear progress bar. */
export function ProgressBar() {
  return (
    <div className="relative h-[6px] w-full overflow-hidden rounded-full bg-mac-fill">
      <div
        className="absolute inset-y-0 w-[35%] rounded-full bg-mac-accent"
        style={{ animation: "mac-marquee 1.4s ease-in-out infinite" }}
      />
    </div>
  );
}

/**
 * A modal sheet over the window, like SwiftUI's `.sheet`. Escape cancels and
 * Enter runs the default action.
 */
export function Sheet({
  children,
  footer,
  onCancel,
  onSubmit,
  divider = true,
  width = 400,
  footerPadding = 20,
}: {
  children: ReactNode;
  footer: ReactNode;
  onCancel: () => void;
  onSubmit: () => void;
  /** Draw a separator above the footer, as the DNS and MTU sheets do. */
  divider?: boolean;
  width?: number;
  footerPadding?: number;
}) {
  const ref = useRef<HTMLFormElement>(null);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCancel();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onCancel]);

  return (
    <div className="fixed inset-0 z-40 flex items-start justify-center bg-black/15 pt-[60px]">
      <form
        ref={ref}
        onSubmit={(e) => {
          e.preventDefault();
          onSubmit();
        }}
        className="max-w-[calc(100%-32px)] overflow-hidden rounded-[10px] border border-mac-group-border bg-mac-sheet shadow-[0_10px_40px_rgb(0_0_0/0.25)]"
        style={{ width, animation: "mac-sheet-in 0.16s ease-out" }}
      >
        <div className="flex flex-col gap-3 p-5">{children}</div>
        <div
          className={cx("flex items-center gap-3", divider ? "border-t border-mac-separator" : "!pt-0")}
          style={{ padding: footerPadding }}
        >
          {footer}
        </div>
      </form>
    </div>
  );
}

/**
 * A macOS alert (`.alert`): the app icon, a bold title, a message and a row of
 * buttons, centered over the window. Escape cancels.
 */
export function Alert({
  title,
  message,
  confirmLabel,
  destructive,
  onConfirm,
  onCancel,
}: {
  title: string;
  message: string;
  confirmLabel: string;
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCancel();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onCancel]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/15">
      <div
        role="alertdialog"
        aria-label={title}
        className="flex w-[260px] flex-col items-center rounded-[12px] border border-mac-group-border bg-mac-sheet p-4 pt-5 text-center shadow-[0_10px_40px_rgb(0_0_0/0.3)]"
        style={{ animation: "mac-sheet-in 0.16s ease-out" }}
      >
        <img src={appIcon} alt="" className="size-[64px]" />
        <div className="mt-2.5 text-[13px] font-bold break-words">{title}</div>
        <div className="mt-1.5 text-[11px] leading-[14px] break-words">{message}</div>
        <div className="mt-4 flex w-full gap-2">
          <Button className="flex-1" onClick={onCancel} autoFocus>
            Cancel
          </Button>
          <Button className={cx("flex-1", destructive && "text-mac-danger")} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}

/** A macOS radio button with a title and optional detail line. */
export function Radio({
  checked,
  onSelect,
  title,
  detail,
}: {
  checked: boolean;
  onSelect: () => void;
  title: ReactNode;
  detail?: ReactNode;
}) {
  return (
    <label className="flex cursor-default items-start gap-2 py-0.5">
      <input type="radio" checked={checked} onChange={onSelect} className="peer sr-only" />
      <span
        aria-hidden
        className={cx(
          "mt-[2px] flex size-[14px] shrink-0 items-center justify-center rounded-full border transition-colors",
          "peer-focus-visible:outline-3 peer-focus-visible:outline-[color-mix(in_srgb,var(--color-mac-accent)_45%,transparent)]",
          checked
            ? "border-mac-accent bg-mac-accent"
            : "border-mac-control-border bg-mac-control shadow-[inset_0_0.5px_1px_rgb(0_0_0/0.08)]",
        )}
      >
        {checked && <span className="size-[6px] rounded-full bg-white" />}
      </span>
      <span className="flex flex-col">
        <span className="text-[13px]">{title}</span>
        {detail && <span className="text-[11px] text-mac-secondary">{detail}</span>}
      </span>
    </label>
  );
}
