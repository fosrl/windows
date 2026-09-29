import { useEffect, useState, type ReactNode } from "react";
import { OnboardingService, type OnboardingState } from "@bindings";
import { Button, cx, openExternal } from "../components/controls";
import { report, urls, useEvent } from "../lib";
import logoLight from "../assets/pangolin_logo_light.png";
import logoDark from "../assets/pangolin_logo_dark.png";
import trayIcon from "../assets/tray_icon.png";

// Pangolin Setup, as in the macOS app's MacOnboardingFlowView: welcome →
// privacy, then "You're All Set" when there is no account yet. The macOS
// system extension and VPN configuration pages have no Windows counterpart.

const pageWelcome = 0;
const pagePrivacy = 1;
const pageAllSet = 2;

/** The first unfinished page, where setup starts each time it opens. */
function firstPage(s: OnboardingState) {
  if (!s.seenWelcome) return pageWelcome;
  if (!s.acknowledgedPrivacy) return pagePrivacy;
  return pageAllSet;
}

export function OnboardingWindow() {
  const [state, setState] = useState<OnboardingState | null>(null);
  const [page, setPage] = useState(pageWelcome);

  const onOpened = (s: OnboardingState) => {
    setState(s);
    setPage(firstPage(s));
  };
  useEffect(() => {
    report(OnboardingService.State().then(onOpened));
  }, []);
  useEvent<OnboardingState>("onboarding:opened", onOpened);

  const seenWelcome = state?.seenWelcome ?? false;
  const acknowledgedPrivacy = state?.acknowledgedPrivacy ?? false;
  const hasNoAccounts = !(state?.hasAccounts ?? false);
  // The closing page only counts as a step while it shows, as on macOS.
  const stepCount = page === pageAllSet ? 3 : 2;

  const close = () => report(OnboardingService.Close());
  const next = () => {
    switch (page) {
      case pageWelcome:
        report(OnboardingService.MarkWelcomeSeen());
        setState((s) => s && { ...s, seenWelcome: true });
        setPage(pagePrivacy);
        break;
      case pagePrivacy:
        report(OnboardingService.MarkPrivacyAcknowledged());
        setState((s) => s && { ...s, acknowledgedPrivacy: true });
        // Privacy is the last setup step here; without an account, say where to log in.
        if (hasNoAccounts) setPage(pageAllSet);
        else close();
        break;
      default:
        close();
    }
  };
  const back = () => setPage((p) => Math.max(pageWelcome, p - 1));
  const showsBack = page > pageWelcome && page < pageAllSet;

  let primary: string;
  switch (page) {
    case pageWelcome:
      primary = "Next";
      break;
    case pagePrivacy:
      primary = !acknowledgedPrivacy ? "I understand" : hasNoAccounts ? "Next" : "Done";
      break;
    default:
      primary = "Done";
  }

  let completed: string | null = null;
  if (page === pageWelcome && seenWelcome) completed = "You've completed this step";
  if (page === pagePrivacy && acknowledgedPrivacy) completed = "You've already confirmed this step";

  // Return runs the primary button; Escape goes back.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Enter" && !(e.target instanceof HTMLButtonElement)) next();
      if (e.key === "Escape" && showsBack) back();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-1.5 px-6 pt-5 pb-4">
        {Array.from({ length: stepCount }, (_, i) => (
          <span
            key={i}
            className={cx("size-[8px] rounded-full", i <= page ? "bg-mac-accent" : "bg-mac-tertiary")}
          />
        ))}
        <span className="flex-1" />
        <span className="text-[10px] text-mac-secondary">
          Step {page + 1} of {stepCount}
        </span>
      </header>

      <main className="flex min-h-0 flex-1 flex-col px-6">
        {page === pageWelcome && <WelcomePage />}
        {page === pagePrivacy && <PrivacyPage />}
        {page === pageAllSet && <AllSetPage />}
      </main>

      <footer className="flex items-center gap-3 px-6 pt-4 pb-6">
        <div className="flex min-w-0 flex-1 items-center gap-2 text-[13px] text-mac-secondary">
          {completed && (
            <>
              <CheckCircle size={16} />
              {completed}
            </>
          )}
        </div>
        {showsBack && <Button onClick={back}>Back</Button>}
        <Button variant="prominent" onClick={next}>
          {primary}
        </Button>
      </footer>
    </div>
  );
}

function Title({ children }: { children: ReactNode }) {
  return <h1 className="text-center text-[17px] font-semibold">{children}</h1>;
}

function Body({ children }: { children: ReactNode }) {
  return <div className="px-4 text-center text-[13px] text-mac-secondary">{children}</div>;
}

function Link({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a
      href={href}
      onClick={(e) => {
        e.preventDefault();
        openExternal(href);
      }}
      className="text-mac-accent hover:underline"
    >
      {children}
    </a>
  );
}

function WelcomePage() {
  return (
    <div className="flex flex-1 flex-col items-center">
      <div className="min-h-6 flex-1" />
      <picture className="mb-6">
        <source srcSet={logoDark} media="(prefers-color-scheme: dark)" />
        <img src={logoLight} alt="Pangolin" className="h-[60px]" />
      </picture>
      <div className="mb-2">
        <Title>Welcome to Pangolin</Title>
      </div>
      <Body>
        Pangolin securely connects your devices to your private networks, so you can safely access internal apps and
        resources from anywhere.
      </Body>
      <div className="flex-1" />
      <div className="flex gap-1 pt-2 text-[10px]">
        <span className="text-mac-secondary">New to Pangolin?</span>
        <Link href={urls.howItWorks}>Learn more.</Link>
      </div>
      <div className="h-4 shrink-0" />
    </div>
  );
}

function PrivacyPage() {
  return (
    <div className="flex flex-1 flex-col items-center">
      <div className="min-h-6 flex-1" />
      <ShieldIcon />
      <div className="mb-2">
        <Title>Privacy</Title>
      </div>
      <Body>
        <div className="flex flex-col gap-2">
          <p>
            We collect your email, device name and model, OS version, and IP address. This enables us to connect your
            device to your network securely.
          </p>
          <p>Your traffic is end-to-end encrypted and is never readable by us or anyone outside your network.</p>
          <p>
            If you're using a self-hosted Pangolin server, all data remains on your server and is never sent to our
            servers.
          </p>
        </div>
      </Body>
      <div className="flex-1" />
      <p className="px-4 pt-2 text-center text-[10px] text-mac-secondary">
        By continuing, you agree to our <Link href={urls.terms}>Terms of Service</Link> and{" "}
        <Link href={urls.privacy}>Privacy Policy</Link>.
      </p>
      <div className="h-4 shrink-0" />
    </div>
  );
}

function AllSetPage() {
  return (
    <div className="flex flex-1 flex-col items-center gap-4 pt-2">
      <div className="min-h-6 flex-1" />
      <div className="pb-2">
        <CheckCircle size={52} />
      </div>
      <Title>You're All Set</Title>
      <Body>Setup is complete. You can now log in to connect to your network.</Body>
      <div className="mx-6 mt-4 flex w-[calc(100%-48px)] flex-col items-center gap-3 rounded-[10px] border border-mac-tertiary px-6 py-5">
        <img src={trayIcon} alt="" className="h-[44px]" />
        <div className="text-center text-[13px] font-bold">Look for the Pangolin icon in the system tray to log in.</div>
      </div>
      <div className="flex-1" />
    </div>
  );
}

/** `checkmark.circle.fill` in green. */
function CheckCircle({ size }: { size: number }) {
  return (
    <svg viewBox="0 0 20 20" width={size} height={size} className="shrink-0" aria-hidden>
      <circle cx="10" cy="10" r="10" fill="#34c759" />
      <path d="M5.8 10.3l2.8 2.8 5.6-6.1" fill="none" stroke="white" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

/** `shield.lefthalf.filled` at 44pt in the accent color. */
function ShieldIcon() {
  return (
    <svg viewBox="0 0 40 46" className="mb-6 h-[46px] w-[40px] text-mac-accent" aria-hidden>
      <path
        d="M20 2.5 4 8.3v12.5c0 10.2 6.6 18.7 16 22.7 9.4-4 16-12.5 16-22.7V8.3L20 2.5Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinejoin="round"
      />
      <path d="M20 2.5 4 8.3v12.5c0 10.2 6.6 18.7 16 22.7V2.5Z" fill="currentColor" />
    </svg>
  );
}
