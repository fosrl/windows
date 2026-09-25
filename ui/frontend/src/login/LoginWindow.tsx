import { useEffect, useState } from "react";
import { LoginService, type LoginView } from "@bindings";
import { Button, ExternalLink, Spinner, TextField, cx } from "../components/controls";
import { report, urls, useEvent } from "../lib";
import wordMarkBlack from "../assets/word_mark_black.png";
import wordMarkWhite from "../assets/word_mark_white.png";

const initial: LoginView = {
  stage: "hosting",
  selfHostedUrl: "",
  code: "",
  manualUrl: "",
  showBack: false,
  backEnabled: true,
  showLogin: false,
  loginEnabled: false,
};

/** The fixed-size login window, laid out like the macOS app's LoginView. */
export function LoginWindow() {
  const [view, setView] = useState<LoginView>(initial);
  const [url, setUrl] = useState("");

  const apply = (v: LoginView) => {
    // Take the URL from the backend only when the stage changes, so echoes of
    // SetURL calls never overwrite what the user is typing.
    setView((prev) => {
      if (prev.stage !== v.stage || v.stage !== "url") setUrl(v.selfHostedUrl);
      return v;
    });
  };
  useEffect(() => {
    report(LoginService.State().then(apply));
  }, []);
  useEvent<LoginView>("login:state", apply);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") report(LoginService.Cancel());
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="relative flex h-full flex-col p-3">
      <div className="flex h-[60px] shrink-0 justify-center pb-[15px] box-content">
        <picture>
          <source srcSet={wordMarkWhite} media="(prefers-color-scheme: dark)" />
          <img src={wordMarkBlack} alt="Pangolin" className="h-[60px]" draggable={false} />
        </picture>
      </div>

      <div className="flex min-h-0 flex-1 flex-col items-center justify-center px-2">
        {view.stage === "hosting" && <HostingSelection />}
        {view.stage === "url" && (
          <div className="flex w-full flex-col items-center gap-3">
            <label htmlFor="server-url" className="font-semibold">
              Pangolin Server URL
            </label>
            <TextField
              id="server-url"
              autoFocus
              className="w-full"
              placeholder="https://your-server.com"
              value={url}
              onChange={(e) => {
                setUrl(e.target.value);
                report(LoginService.SetURL(e.target.value));
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" && view.loginEnabled) report(LoginService.Login());
              }}
            />
          </div>
        )}
        {view.stage === "code" && <DeviceCode view={view} />}
        {view.stage === "success" && <Success />}
      </div>

      {view.stage !== "success" && (
        <div className="flex shrink-0 flex-col gap-2">
          {view.stage === "hosting" && (
            <div className="text-center text-[10px] text-mac-secondary">
              By continuing, you agree to our{" "}
              <ExternalLink href={urls.terms} className="text-mac-label hover:underline">
                Terms of Service
              </ExternalLink>{" "}
              and{" "}
              <ExternalLink href={urls.privacy} className="text-mac-label hover:underline">
                Privacy Policy.
              </ExternalLink>
            </div>
          )}
          <div className="flex justify-end gap-2">
            {view.showBack && (
              <Button disabled={!view.backEnabled} onClick={() => report(LoginService.Back())}>
                Back
              </Button>
            )}
            <Button onClick={() => report(LoginService.Cancel())}>Cancel</Button>
            {view.showLogin && (
              <Button variant="prominent" disabled={!view.loginEnabled} onClick={() => report(LoginService.Login())}>
                Log in
              </Button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function HostingSelection() {
  return (
    <div className="flex w-full flex-col gap-2">
      <HostingCard title="Pangolin Cloud" subtitle="app.pangolin.net" onClick={() => report(LoginService.ChooseCloud())} />
      <HostingCard
        title="Self-hosted or dedicated instance"
        subtitle="Enter your custom hostname"
        onClick={() => report(LoginService.ChooseSelfHosted())}
      />
    </div>
  );
}

function HostingCard({ title, subtitle, onClick }: { title: string; subtitle: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cx(
        "flex w-full flex-col items-center gap-1 rounded-[8px] bg-mac-fill p-4 text-center",
        "outline-2 -outline-offset-2 outline-transparent transition-[outline-color] hover:outline-mac-accent",
      )}
    >
      <span className="text-[13px] font-semibold">{title}</span>
      <span className="text-[10px] text-mac-secondary">{subtitle}</span>
    </button>
  );
}

function DeviceCode({ view }: { view: LoginView }) {
  // The backend spaces the characters out ("A B C D - E F G H"); draw one box each.
  const chars = view.code.split(" ").filter(Boolean);
  return (
    <div className="flex flex-col items-center gap-3">
      <div className="flex min-h-[50px] gap-1.5 select-text">
        {chars.map((c, i) => (
          <span
            key={i}
            className={cx(
              "flex h-[50px] w-[40px] items-center justify-center rounded-[8px] font-mono text-[24px] font-bold",
              c !== "-" && "bg-mac-fill",
            )}
          >
            {c}
          </span>
        ))}
      </div>
      <div className="flex gap-2">
        <Button onClick={() => report(LoginService.CopyCode())}>Copy Code</Button>
        <Button onClick={() => report(LoginService.OpenBrowser())}>Open Browser</Button>
      </div>
      {view.manualUrl && (
        <div className="px-4 text-center text-[11px] text-mac-secondary select-text">{view.manualUrl}</div>
      )}
      <Spinner size={14} />
    </div>
  );
}

function Success() {
  return (
    <div className="flex flex-col items-center gap-4">
      <svg viewBox="0 0 64 64" className="size-[64px]" aria-hidden>
        <circle cx="32" cy="32" r="30" fill="#34c759" />
        <path d="M19 33l9 9 17-19" fill="none" stroke="white" strokeWidth="5.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <div className="text-[20px] font-bold">Authentication Successful</div>
      <div className="text-mac-secondary">You have been successfully logged in.</div>
    </div>
  );
}
