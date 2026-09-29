import { useEffect, useState, type ReactNode } from "react";
import { UpdateService, type UpdateInfo } from "@bindings";
import { Button, ProgressBar, Spinner } from "../components/controls";
import { DialogMessage as Message, DialogWindow } from "../components/dialog";
import { report, useEvent } from "../lib";

const close = () => report(UpdateService.Close());
const mb = (bytes: number) => (bytes / (1024 * 1024)).toFixed(1);

/**
 * Shows a manual update check and its result, offers an available update, then
 * shows its download and install, all in the same window.
 */
export function UpdateWindow() {
  const [info, setInfo] = useState<UpdateInfo | null>(null);

  useEffect(() => {
    report(UpdateService.State().then(setInfo));
  }, []);
  useEvent<UpdateInfo>("update:state", setInfo);

  if (!info) return null;
  const install = () => report(UpdateService.Install());
  const check = () => report(UpdateService.Check());
  const ok = (
    <Button variant="prominent" onClick={close} autoFocus>
      OK
    </Button>
  );

  let title: string;
  let body: ReactNode;
  let buttons: ReactNode;
  switch (info.phase) {
    case "checking":
      title = "Checking for updates…";
      body = (
        <div className="mt-2">
          <Spinner />
        </div>
      );
      buttons = <Button onClick={close}>Cancel</Button>;
      break;
    case "upToDate":
      title = "You’re up to date";
      body = <Message>Pangolin {info.currentVersion} is the latest version.</Message>;
      buttons = ok;
      break;
    case "disabled":
      title = "Updates are disabled";
      body = <Message>Updates are disabled for unofficial builds of Pangolin.</Message>;
      buttons = ok;
      break;
    case "checkFailed":
      title = "Couldn’t check for updates";
      body = <Message>{info.error || "An unknown error occurred."}</Message>;
      buttons = (
        <>
          <Button onClick={close}>Close</Button>
          <Button variant="prominent" onClick={check} autoFocus>
            Try Again
          </Button>
        </>
      );
      break;
    case "downloading": {
      const known = info.bytesTotal > 0;
      title = info.version ? `Updating to Pangolin ${info.version}` : "Updating Pangolin";
      body = (
        <div className="mt-3 flex flex-col gap-1.5">
          <ProgressBar value={known ? info.bytesDownloaded / info.bytesTotal : undefined} />
          <div className="flex justify-between gap-2 text-[11px] text-mac-secondary">
            <span className="truncate">{info.activity || "Working…"}</span>
            {known && (
              <span className="shrink-0 tabular-nums">
                {mb(info.bytesDownloaded)} of {mb(info.bytesTotal)} MB
              </span>
            )}
          </div>
        </div>
      );
      buttons = <Button onClick={close}>Hide</Button>;
      break;
    }
    case "error":
      title = "The update couldn’t be installed";
      body = <Message>{info.error || "An unknown error occurred."}</Message>;
      buttons = (
        <>
          <Button onClick={close}>Close</Button>
          <Button variant="prominent" onClick={install} autoFocus>
            Try Again
          </Button>
        </>
      );
      break;
    case "complete":
      title = "Installing update…";
      body = <Message>Pangolin will restart automatically when the installation finishes.</Message>;
      buttons = <Button onClick={close}>Close</Button>;
      break;
    default:
      title = "A new version of Pangolin is available";
      body = (
        <Message>
          {info.version
            ? `Pangolin ${info.version} is available — you have ${info.currentVersion}.`
            : `You have ${info.currentVersion}.`}{" "}
          Would you like to install it now? Your connection will be interrupted briefly while the update installs.
        </Message>
      );
      buttons = (
        <>
          <Button onClick={close}>Later</Button>
          <Button variant="prominent" onClick={install} autoFocus>
            Install Update
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
