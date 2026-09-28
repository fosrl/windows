import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { LoginService, type LoginView } from "@bindings";
import { Button, Radio, Sheet, Spinner, TextField, cx, openExternal } from "../components/controls";
import { report, urls, useEvent } from "../lib";

// The add-account sheet, as in the macOS app's AddAccountSheet: choose a
// server, approve the code in the browser, done. Renewing an account skips the
// server choice.

const cloudHostname = "https://app.pangolin.net";
// Steps cross-fade with a blur and the sheet eases to its new height, like the
// macOS sheet's `.smooth(duration: 0.3)`.
const stepMs = 300;
// Closest to the macOS app's SF Mono on Windows 11, then older fallbacks.
const codeFont = '"Cascadia Mono", "Cascadia Code", Consolas, ui-monospace, monospace';

type Server = "cloud" | "selfHosted";

/** Trims, adds https:// when there is no scheme, and strips surrounding slashes. */
function normalizeServerURL(text: string): string | null {
  const trimmed = text.trim();
  if (!trimmed) return null;
  const withScheme = trimmed.includes("://") ? trimmed : `https://${trimmed}`;
  const url = withScheme.replace(/^\/+|\/+$/g, "");
  return url || null;
}

export function AddAccountSheet({ renewHostname, onClose }: { renewHostname: string; onClose: () => void }) {
  const [view, setView] = useState<LoginView | null>(null);
  const [server, setServer] = useState<Server>("cloud");
  const [serverURL, setServerURL] = useState("");
  const [copied, setCopied] = useState(false);
  const session = useRef(0);

  useEffect(() => {
    let closed = false;
    report(
      LoginService.Open(renewHostname).then((v) => {
        session.current = v.session;
        if (closed) {
          report(LoginService.Close(v.session));
          return;
        }
        setView(v);
        if (v.selfHosted) setServer("selfHosted");
        if (v.serverUrl) setServerURL(v.serverUrl);
      }),
    );
    // Closing the sheet cancels a login that hasn't finished.
    return () => {
      closed = true;
      if (session.current) report(LoginService.Close(session.current));
    };
  }, [renewHostname]);
  useEvent<LoginView>("login:state", (v) => {
    if (v.session === session.current) setView(v);
  });

  // Until the backend answers, a renewal shows the spinner, never the server choice.
  const step = view?.step ?? (renewHostname ? "loading" : "server");
  const fixedHost = view?.fixedHost ?? !!renewHostname;
  const renewing = view?.renewing ?? !!renewHostname;
  const host = view?.host ?? "the server";
  const normalizedURL = normalizeServerURL(serverURL);
  const canContinue = server === "cloud" || normalizedURL !== null;

  // The sheet closes itself shortly after logging in.
  useEffect(() => {
    if (step !== "success") return;
    const t = window.setTimeout(onClose, 1000);
    return () => window.clearTimeout(t);
  }, [step, onClose]);

  const continueLogin = () => {
    if (!canContinue) return;
    report(LoginService.Start(server === "cloud" ? cloudHostname : normalizedURL!));
  };
  const copyCode = () => {
    report(LoginService.CopyCode());
    setCopied(true);
  };
  useEffect(() => {
    if (!copied) return;
    const t = window.setTimeout(() => setCopied(false), 1500);
    return () => window.clearTimeout(t);
  }, [copied]);

  let content: ReactNode;
  switch (step) {
    case "server":
      content = (
        <div className="flex flex-col gap-4">
          <Header title="Add Account" subtitle="Log in to Pangolin Cloud or to your own Pangolin server." />
          <div role="radiogroup" aria-label="Server" className="flex flex-col gap-1">
            <Radio
              checked={server === "cloud"}
              onSelect={() => setServer("cloud")}
              title="Pangolin Cloud"
              detail="app.pangolin.net"
            />
            <Radio
              checked={server === "selfHosted"}
              onSelect={() => setServer("selfHosted")}
              title="Self-hosted or dedicated"
              detail="Your organization's Pangolin server"
            />
          </div>
          {server === "selfHosted" && (
            <div className="pl-5">
              <TextField
                aria-label="Server URL"
                autoFocus
                className="w-full"
                placeholder="https://pangolin.example.com"
                value={serverURL}
                onChange={(e) => setServerURL(e.target.value)}
              />
            </div>
          )}
          {view?.error && (
            <div className="flex items-start gap-1.5 text-[12px] text-mac-danger">
              <WarningIcon />
              <span className="min-w-0 break-words">{view.error}</span>
            </div>
          )}
          <p className="text-[11px] text-mac-secondary">
            By continuing, you agree to the <LegalLink href={urls.terms}>Terms of Service</LegalLink> and{" "}
            <LegalLink href={urls.privacy}>Privacy Policy</LegalLink>
          </p>
        </div>
      );
      break;
    case "starting":
      content = (
        <div className="flex flex-col gap-4">
          <Header title="Add Account" subtitle={`Contacting ${host}…`} />
          <div className="flex justify-center py-3">
            <Spinner />
          </div>
        </div>
      );
      break;
    case "loading":
      content = (
        <div className="flex min-h-[200px] items-center justify-center">
          <Spinner size={32} />
        </div>
      );
      break;
    case "code":
      content = (
        <div className="flex flex-col gap-4">
          <Header
            title={renewing ? "Log In Again" : "Approve in Your Browser"}
            subtitle={`Your browser opened ${host}. Check that it shows this code, then approve the login.`}
          />
          <div role="img" aria-label={`Code ${view?.code ?? ""}`} className="flex justify-center gap-1.5">
            {[...(view?.code ?? "")].map((c, i) => (
              <span
                key={i}
                aria-hidden
                style={{ fontFamily: codeFont }}
                className={cx(
                  "flex h-[44px] items-center justify-center text-[22px] font-semibold",
                  c === "-" ? "w-[14px]" : "w-[34px] rounded-[6px] border border-mac-separator bg-mac-control",
                )}
              >
                {c}
              </span>
            ))}
          </div>
          <div className="flex justify-center gap-3">
            <Button onClick={copyCode}>{copied ? "Copied" : "Copy Code"}</Button>
            <Button onClick={() => report(LoginService.OpenBrowser())}>Open Browser</Button>
          </div>
          <div className="flex items-center justify-center gap-2 text-[12px] text-mac-secondary">
            <Spinner size={12} />
            Waiting for approval…
          </div>
        </div>
      );
      break;
    case "success":
      content = (
        <div className="flex flex-col items-center gap-2 py-3 text-center">
          <SuccessIcon />
          <div className="text-[13px] font-bold">You're Logged In</div>
          {view?.email && <div className="text-[13px] text-mac-secondary">{view.email}</div>}
        </div>
      );
      break;
  }

  const showBack = step !== "server" && step !== "success" && !fixedHost;
  return (
    <Sheet
      width={440}
      footerPadding={16}
      onCancel={onClose}
      onSubmit={() => {
        if (step === "success") onClose();
        else if (step === "server") continueLogin();
      }}
      footer={
        <>
          {showBack && (
            <Button size="large" onClick={() => report(LoginService.Back())}>
              Back
            </Button>
          )}
          <span className="flex-1" />
          {step === "success" ? (
            <Button size="large" variant="prominent" type="submit">
              Done
            </Button>
          ) : (
            <>
              <Button size="large" onClick={onClose}>
                Cancel
              </Button>
              {step === "server" && (
                <Button size="large" variant="prominent" type="submit" disabled={!canContinue}>
                  Continue
                </Button>
              )}
            </>
          )}
        </>
      }
    >
      <AnimatedHeight>
        <div key={step} className="mac-step-in">
          {content}
        </div>
      </AnimatedHeight>
    </Sheet>
  );
}

function Header({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className="flex flex-col gap-1">
      <div className="text-[13px] font-bold">{title}</div>
      <div className="text-[13px] text-mac-secondary">{subtitle}</div>
    </div>
  );
}

function LegalLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        openExternal(href);
      }}
      className="underline"
    >
      {children}
    </a>
  );
}

/** Eases to the height of its content as it changes. */
function AnimatedHeight({ children }: { children: ReactNode }) {
  const inner = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | undefined>(undefined);
  const [animate, setAnimate] = useState(false);
  useLayoutEffect(() => {
    const el = inner.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setHeight(el.offsetHeight));
    ro.observe(el);
    setHeight(el.offsetHeight);
    // The first size is set at once; later changes animate.
    const t = requestAnimationFrame(() => setAnimate(true));
    return () => {
      ro.disconnect();
      cancelAnimationFrame(t);
    };
  }, []);
  return (
    <div
      style={{
        height,
        overflow: "hidden",
        transition: animate ? `height ${stepMs}ms cubic-bezier(0.32, 0.72, 0, 1)` : undefined,
      }}
    >
      <div ref={inner}>{children}</div>
    </div>
  );
}

function WarningIcon() {
  return (
    <svg viewBox="0 0 14 13" className="mt-[2px] h-[12px] w-[13px] shrink-0" aria-hidden>
      <path
        fill="currentColor"
        d="M5.7 1.2a1.5 1.5 0 0 1 2.6 0l5 8.8A1.5 1.5 0 0 1 12 12.2H2A1.5 1.5 0 0 1 .7 10zM6.3 4.3v3.4h1.4V4.3zm0 4.6v1.4h1.4V8.9z"
      />
    </svg>
  );
}

/** `checkmark.circle.fill` at 44pt, in green. */
function SuccessIcon() {
  return (
    <svg viewBox="0 0 44 44" className="size-[44px]" aria-hidden>
      <circle cx="22" cy="22" r="22" fill="#34c759" />
      <path
        d="M13 22.5l6 6 12-13"
        fill="none"
        stroke="white"
        strokeWidth="3.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
