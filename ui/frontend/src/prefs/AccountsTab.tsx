import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { AccountsService, type AccountRow, type AccountsView, type LoginRequest } from "@bindings";
import { Alert, Button, Form, Section, Spinner, cx } from "../components/controls";
import { report, useEvent } from "../lib";
import appIcon from "../assets/app_icon.svg";
import { AddAccountSheet } from "./AddAccountSheet";

// Preferences > Accounts, as in the macOS app's AccountsContentView: a welcome
// screen until there is an account, then the list of accounts with Switch,
// Log In… and Remove… on each row.

type SheetRequest = { key: number; hostname: string };

export function AccountsTab({
  visible,
  openCount,
  login,
  headerAccessory,
}: {
  visible: boolean;
  /** Bumped each time the window is shown. */
  openCount: number;
  /** A request from the tray to show the add-account sheet. */
  login: LoginRequest | null;
  /** Where the toolbar's Add Account button goes. */
  headerAccessory: HTMLElement | null;
}) {
  const [view, setView] = useState<AccountsView>({ accounts: [], busyUserId: "", tunnelStarting: false });
  const [sheet, setSheet] = useState<SheetRequest | null>(null);
  const [pendingRemoval, setPendingRemoval] = useState<AccountRow | null>(null);
  const sheetKey = useRef(0);

  useEffect(() => {
    report(AccountsService.State().then(setView));
  }, [openCount]);
  useEvent<AccountsView>("accounts:state", setView);

  const showLogin = useCallback((hostname: string) => {
    sheetKey.current += 1;
    setSheet({ key: sheetKey.current, hostname });
  }, []);
  // Requests from the tray: Log In…, Add Account… and logging in again.
  const handledLogin = useRef(0);
  useEffect(() => {
    if (!login || login.id === handledLogin.current) return;
    handledLogin.current = login.id;
    setPendingRemoval(null);
    showLogin(login.hostname);
  }, [login, showLogin]);

  const accounts = view.accounts ?? [];
  const busy = view.busyUserId !== "";

  return (
    <>
      {accounts.length === 0 ? (
        <WelcomeScreen active={visible && !sheet} onLogIn={() => showLogin("")} />
      ) : (
        <Form>
          <Section header="Accounts">
            {accounts.map((a) => (
              <Row
                key={a.userId}
                account={a}
                busy={view.busyUserId === a.userId}
                anyBusy={busy}
                tunnelStarting={view.tunnelStarting}
                onLogIn={() => showLogin(a.hostname)}
                onSwitch={() => report(AccountsService.Switch(a.userId))}
                onRemove={() => setPendingRemoval(a)}
              />
            ))}
          </Section>
          <div className="-mt-3 flex justify-end">
            <Button onClick={() => showLogin("")}>Add Account…</Button>
          </div>
        </Form>
      )}

      {/* The toolbar button only shows once there are accounts; the welcome screen has its own. */}
      {accounts.length > 0 &&
        visible &&
        headerAccessory &&
        createPortal(
          <button
            type="button"
            title="Add Account"
            aria-label="Add Account"
            onClick={() => showLogin("")}
            className="flex size-[28px] items-center justify-center rounded-[6px] text-mac-secondary hover:bg-mac-fill active:bg-mac-separator"
          >
            <PlusIcon />
          </button>,
          headerAccessory,
        )}

      {sheet && visible && (
        <AddAccountSheet key={sheet.key} renewHostname={sheet.hostname} onClose={() => setSheet(null)} />
      )}
      {pendingRemoval && visible && (
        <Alert
          title={`Remove ${pendingRemoval.displayName || "Account"}?`}
          message={`You'll be logged out of ${pendingRemoval.host} on this computer. You can add the account again at any time.`}
          confirmLabel="Remove"
          destructive
          onCancel={() => setPendingRemoval(null)}
          onConfirm={() => {
            report(AccountsService.Remove(pendingRemoval.userId));
            setPendingRemoval(null);
          }}
        />
      )}
    </>
  );
}

function WelcomeScreen({ active, onLogIn }: { active: boolean; onLogIn: () => void }) {
  // Enter logs in, like the macOS button's `.keyboardShortcut(.defaultAction)`.
  useEffect(() => {
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Enter" && !(e.target instanceof HTMLButtonElement)) onLogIn();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, onLogIn]);

  return (
    <div className="flex min-h-0 flex-1 flex-col items-center p-8 text-center">
      <div className="flex-1" />
      <img src={appIcon} alt="" className="size-[96px]" />
      <h2 className="mt-4 text-[22px] font-semibold">Log In to Pangolin</h2>
      <p className="mt-1.5 max-w-[340px] text-[13px] text-mac-secondary">
        Connect to your organization's private resources by logging in to Pangolin Cloud or your own Pangolin server.
      </p>
      <Button variant="prominent" size="large" className="mt-6 min-w-[120px]" onClick={onLogIn}>
        Log In…
      </Button>
      <div className="flex-[2]" />
    </div>
  );
}

function Row({
  account,
  busy,
  anyBusy,
  tunnelStarting,
  onLogIn,
  onSwitch,
  onRemove,
}: {
  account: AccountRow;
  busy: boolean;
  anyBusy: boolean;
  tunnelStarting: boolean;
  onLogIn: () => void;
  onSwitch: () => void;
  onRemove: () => void;
}) {
  let status;
  if (busy) {
    status = <Spinner size={14} />;
  } else if (account.locked) {
    status = (
      <Button size="small" onClick={onLogIn}>
        Log In…
      </Button>
    );
  } else if (account.active) {
    status = (
      <span title="Current account" aria-label="Current account" className="text-mac-secondary">
        <CheckIcon />
      </span>
    );
  } else {
    status = (
      <Button size="small" disabled={anyBusy || tunnelStarting} onClick={onSwitch}>
        Switch
      </Button>
    );
  }

  return (
    <div className="flex min-h-[44px] items-center gap-2.5 px-2.5 py-[6px]">
      <PersonIcon />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-[13px]">{account.displayName}</span>
        <span className={cx("truncate text-[11px]", account.locked ? "text-mac-warning" : "text-mac-secondary")}>
          {account.subtitle}
        </span>
      </div>
      <span className="w-2" />
      {status}
      <Button size="small" disabled={anyBusy} onClick={onRemove}>
        Remove…
      </Button>
    </div>
  );
}

/** `person.crop.circle.fill` at 26pt. */
function PersonIcon() {
  return (
    <svg viewBox="0 0 26 26" className="size-[26px] shrink-0 text-mac-secondary" fill="currentColor" aria-hidden>
      <path
        fillRule="evenodd"
        d="M13 0a13 13 0 1 1 0 26 13 13 0 0 1 0-26Zm0 5.2a4.4 4.4 0 1 0 0 8.8 4.4 4.4 0 0 0 0-8.8ZM5.3 20.4a9.9 9.9 0 0 0 15.4 0c-1.2-2.4-4.3-3.9-7.7-3.9s-6.5 1.5-7.7 3.9Z"
      />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg viewBox="0 0 12 12" className="size-[12px]" fill="none" stroke="currentColor" strokeWidth="1.9" aria-hidden>
      <path d="M2.2 6.4l2.5 2.5 5.1-5.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function PlusIcon() {
  return (
    <svg viewBox="0 0 14 14" className="size-[14px]" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
      <path d="M7 2v10M2 7h10" strokeLinecap="round" />
    </svg>
  );
}
