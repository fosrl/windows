import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from "react";
import { AppService } from "@bindings";
import { report } from "../lib";

function cx(...classes: (string | false | undefined)[]) {
  return classes.filter(Boolean).join(" ");
}

/**
 * Push button. `accessKey` works like the old "&Save" accelerators: Alt+key
 * clicks the button, and the matching letter is underlined.
 */
export function Button({
  children,
  primary,
  className,
  accessKey,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { primary?: boolean; children: string }) {
  return (
    <button
      {...props}
      accessKey={accessKey}
      className={cx(
        "h-7 min-w-[75px] rounded-[4px] border px-3 text-[12px] leading-none",
        "disabled:cursor-default disabled:opacity-50",
        primary
          ? "border-accent bg-accent text-accent-text enabled:hover:bg-accent-hover"
          : "border-border-strong/60 bg-control enabled:hover:bg-control-hover enabled:active:bg-control-pressed",
        className,
      )}
    >
      <AccessKeyLabel text={children} accessKey={accessKey} />
    </button>
  );
}

function AccessKeyLabel({ text, accessKey }: { text: string; accessKey?: string }) {
  if (!accessKey) return <>{text}</>;
  const i = text.toLowerCase().indexOf(accessKey.toLowerCase());
  if (i < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, i)}
      <u>{text[i]}</u>
      {text.slice(i + 1)}
    </>
  );
}

export function Checkbox({
  checked,
  onChange,
  disabled,
  label,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
  label?: string;
}) {
  return (
    <input
      type="checkbox"
      aria-label={label}
      checked={checked}
      disabled={disabled}
      onChange={(e) => onChange(e.target.checked)}
      className="size-[13px] accent-accent"
    />
  );
}

export function TextField({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      type="text"
      spellCheck={false}
      {...props}
      className={cx(
        "h-[23px] min-w-0 rounded-[3px] border border-border-strong/70 bg-surface px-1.5 text-[12px]",
        "placeholder:text-text-secondary focus:border-accent focus:outline-none disabled:opacity-60",
        "select-text",
        className,
      )}
    />
  );
}

/** A link that opens in the default browser. */
export function ExternalLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        report(AppService.OpenURL(href));
      }}
      className="text-link hover:underline"
    >
      {children}
    </a>
  );
}

export function SectionTitle({ children }: { children: ReactNode }) {
  return <div className="text-[13px] font-bold">{children}</div>;
}

export function Secondary({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cx("whitespace-pre-line text-text-secondary", className)}>{children}</div>;
}

/** A form row: fixed 200px label column, then the control. */
export function Row({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <div className="flex items-center gap-3">
      <div className="w-[200px] shrink-0">{label}</div>
      <div className="flex min-w-0 flex-1 items-center gap-1.5">{children}</div>
    </div>
  );
}

export function Dot({ color, size = 12 }: { color: string; size?: number }) {
  const colors: Record<string, string> = {
    green: "var(--color-dot-green)",
    gray: "var(--color-dot-gray)",
    yellow: "var(--color-dot-yellow)",
  };
  return (
    <span
      aria-hidden
      className="inline-block shrink-0 rounded-full"
      style={{ width: size - 4, height: size - 4, background: colors[color] ?? colors.gray, margin: 2 }}
    />
  );
}

/** Indeterminate progress bar, like the Win32 marquee progress bar. */
export function Marquee() {
  return (
    <div className="relative h-[15px] w-full overflow-hidden rounded-[2px] border border-border-strong/60 bg-surface">
      <div className="absolute inset-y-0 w-1/3 animate-[marquee_1.6s_linear_infinite] bg-[#06b025]" />
      <style>{`@keyframes marquee { from { left: -33% } to { left: 100% } }`}</style>
    </div>
  );
}

/** Win32-style tab strip with a bordered page below it. */
export function Tabs({
  tabs,
  index,
  onChange,
  children,
  className,
}: {
  tabs: string[];
  index: number;
  onChange: (index: number) => void;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cx("flex min-h-0 min-w-0 flex-1 flex-col", className)}>
      <div role="tablist" className="flex gap-0.5 border-b border-border">
        {tabs.map((t, i) => (
          <button
            key={t}
            role="tab"
            aria-selected={i === index}
            onClick={() => onChange(i)}
            className={cx(
              "-mb-px rounded-t-[3px] border px-3 py-1",
              i === index
                ? "border-border border-b-surface bg-surface"
                : "border-transparent text-text-secondary hover:bg-control-hover",
            )}
          >
            {t}
          </button>
        ))}
      </div>
      <div role="tabpanel" className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden border border-t-0 border-border bg-surface">
        {children}
      </div>
    </div>
  );
}
