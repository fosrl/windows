import { useEffect, useState, type ReactNode } from "react";
import { CLIService, type CLIInfo } from "@bindings";
import { Button, ProgressBar } from "../components/controls";
import { DialogMessage, DialogWindow } from "../components/dialog";
import { report, useEvent } from "../lib";

const close = () => report(CLIService.Close());

/** Confirms the Pangolin CLI install, then shows its progress and result in the same window. */
export function CLIWindow() {
  const [info, setInfo] = useState<CLIInfo | null>(null);

  useEffect(() => {
    report(CLIService.State().then(setInfo));
  }, []);
  useEvent<CLIInfo>("cli:state", setInfo);

  if (!info) return null;
  const install = () => report(CLIService.Install());

  let title: string;
  let body: ReactNode;
  let buttons: ReactNode;
  switch (info.phase) {
    case "installing":
      title = "Installing Pangolin CLI…";
      body = (
        <div className="mt-3 flex flex-col gap-1.5">
          <ProgressBar />
          <div className="text-[11px] text-mac-secondary">Downloading the installer, then running setup.</div>
        </div>
      );
      buttons = <Button onClick={close}>Hide</Button>;
      break;
    case "done":
      title = "Pangolin CLI installed";
      body = (
        <DialogMessage>
          The CLI was added to your PATH. Open a new terminal to use the <code className="font-mono text-[12px] text-mac-label">pangolin</code>{" "}
          command.
        </DialogMessage>
      );
      buttons = (
        <Button variant="prominent" onClick={close} autoFocus>
          Done
        </Button>
      );
      break;
    case "error":
      title = "The CLI couldn’t be installed";
      body = <DialogMessage>{info.error || "An unknown error occurred."}</DialogMessage>;
      buttons = (
        <>
          <Button onClick={close}>Close</Button>
          <Button variant="prominent" onClick={install} autoFocus>
            Try Again
          </Button>
        </>
      );
      break;
    default:
      title = "Install the Pangolin CLI?";
      body = (
        <DialogMessage>
          This downloads the latest Pangolin CLI installer and runs it. The CLI lets you manage your connection with
          the <code className="font-mono text-[12px] text-mac-label">pangolin</code> command in a terminal.
        </DialogMessage>
      );
      buttons = (
        <>
          <Button onClick={close}>Cancel</Button>
          <Button variant="prominent" onClick={install} autoFocus>
            Install
          </Button>
        </>
      );
  }

  return (
    <DialogWindow title={title} buttons={buttons} onEscape={close}>
      {body}
    </DialogWindow>
  );
}
