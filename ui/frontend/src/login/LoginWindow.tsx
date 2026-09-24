import { useEffect, useState } from "react";
import { LoginService, type LoginView } from "@bindings";
import { Button, ExternalLink, Marquee, TextField } from "../components/controls";
import { report, urls, useEvent } from "../lib";
import wordMark from "../assets/word_mark_black.png";

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

/** The fixed-size login window. It is always light, like the old dialog. */
export function LoginWindow() {
  const [view, setView] = useState<LoginView>(initial);
  const [url, setUrl] = useState("");

  useEffect(() => {
    report(LoginService.State().then(apply));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const apply = (v: LoginView) => {
    // Take the URL from the backend only when the stage changes, so echoes of
    // SetURL calls never overwrite what the user is typing.
    setView((prev) => {
      if (prev.stage !== v.stage || v.stage !== "url") setUrl(v.selfHostedUrl);
      return v;
    });
  };
  useEvent<LoginView>("login:state", apply);

  return (
    <div className="flex h-full flex-col gap-[5px] bg-[#fcfcfc] px-5 py-2.5 text-[#1b1b1b]">
      <div className="flex h-[60px] shrink-0 justify-center">
        <img src={wordMark} alt="Pangolin" className="h-[60px] w-[240px]" draggable={false} />
      </div>

      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-1">
        {view.stage === "hosting" && (
          <>
            <HostingButton onClick={() => report(LoginService.ChooseCloud())}>Pangolin Cloud</HostingButton>
            <HostingButton onClick={() => report(LoginService.ChooseSelfHosted())}>
              Self-hosted or dedicated instance
            </HostingButton>
          </>
        )}

        {view.stage === "url" && (
          <>
            <label htmlFor="server-url" className="text-center">
              Pangolin Server URL
            </label>
            <TextField
              id="server-url"
              autoFocus
              className="w-[300px] bg-white"
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
          </>
        )}

        {view.stage === "code" && (
          <>
            <div className="min-h-[40px] text-center text-[32px] font-bold leading-[40px] select-text">
              {view.code}
            </div>
            <div className="flex justify-center gap-2">
              <Button onClick={() => report(LoginService.CopyCode())}>Copy Code</Button>
              <Button onClick={() => report(LoginService.OpenBrowser())}>Open Browser</Button>
            </div>
            {view.manualUrl && (
              <div className="pt-2.5 text-center text-[11px] text-[#808080] select-text">{view.manualUrl}</div>
            )}
            <div className="w-[300px] pt-1">
              <Marquee />
            </div>
          </>
        )}
      </div>

      {view.stage === "hosting" && (
        <div className="text-center text-[11px] text-[#808080]">
          By continuing, you agree to our <ExternalLink href={urls.terms}>Terms of Service</ExternalLink> and{" "}
          <ExternalLink href={urls.privacy}>Privacy Policy</ExternalLink>.
        </div>
      )}

      <div className="flex shrink-0 justify-end gap-2">
        {view.showBack && (
          <Button disabled={!view.backEnabled} onClick={() => report(LoginService.Back())}>
            Back
          </Button>
        )}
        <Button onClick={() => report(LoginService.Cancel())}>Cancel</Button>
        {view.showLogin && (
          <Button primary disabled={!view.loginEnabled} onClick={() => report(LoginService.Login())}>
            Login
          </Button>
        )}
      </div>
    </div>
  );
}

function HostingButton({ children, onClick }: { children: string; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="h-10 w-[300px] rounded-[4px] border border-[#adadad]/60 bg-white hover:bg-[#f3f3f3] active:bg-[#ebebeb]"
    >
      {children}
    </button>
  );
}
