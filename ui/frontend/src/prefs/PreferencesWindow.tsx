import { useEffect, useState, type ReactNode } from "react";
import { PreferencesService, type LoginRequest, type PrefsOpened, type Settings } from "@bindings";
import { cx } from "../components/controls";
import { report, useEvent } from "../lib";
import { PreferencesTab } from "./PreferencesTab";
import { AccountsTab } from "./AccountsTab";
import { StatusTab } from "./StatusTab";
import { LogsTab } from "./LogsTab";
import { AboutTab } from "./AboutTab";

// Tab indexes are shared with the backend (prefsTab* in ui/windows.go).
const sections: { name: string; icon: ReactNode }[] = [
  { name: "Preferences", icon: <GearIcon /> },
  { name: "Accounts", icon: <AccountsIcon /> },
  { name: "Status", icon: <StatusIcon /> },
  { name: "Logs", icon: <LogsIcon /> },
  { name: "About", icon: <InfoIcon /> },
];

/** Sidebar + detail layout, like the macOS app's NavigationSplitView. */
export function PreferencesWindow() {
  const [tab, setTab] = useState(0);
  const [settings, setSettings] = useState<Settings | null>(null);
  // Bumped each time the window is shown, so sections reset like a freshly opened window.
  const [openCount, setOpenCount] = useState(0);
  const [login, setLogin] = useState<LoginRequest | null>(null);
  // Tabs can put a toolbar control at the trailing end of the header.
  const [headerAccessory, setHeaderAccessory] = useState<HTMLElement | null>(null);

  const onOpened = (o: PrefsOpened) => {
    if (o.tab >= 0 && o.tab < sections.length) setTab(o.tab);
    setSettings(o.settings);
    if (o.login) setLogin(o.login);
    setOpenCount((n) => n + 1);
  };

  useEffect(() => {
    report(PreferencesService.Opened().then(onOpened));
  }, []);
  useEvent<PrefsOpened>("prefs:opened", onOpened);

  return (
    <div className="flex h-full min-w-0">
      <nav
        aria-label="Preferences sections"
        className="flex w-[192px] shrink-0 flex-col gap-0.5 border-r border-mac-separator bg-mac-sidebar p-2.5 pt-3"
      >
        {sections.map((s, i) => (
          <button
            key={s.name}
            type="button"
            aria-current={i === tab ? "page" : undefined}
            onClick={() => setTab(i)}
            className={cx(
              "flex h-[28px] items-center gap-2 rounded-[6px] px-2 text-left",
              i === tab ? "bg-mac-accent text-white" : "hover:bg-mac-fill",
            )}
          >
            <span className={cx("flex size-[18px] items-center justify-center", i !== tab && "text-mac-accent")}>
              {s.icon}
            </span>
            {s.name}
          </button>
        ))}
      </nav>

      <main className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-[44px] shrink-0 items-center justify-between pr-3 pl-5">
          <span className="text-[15px] font-semibold">{sections[tab].name}</span>
          <div ref={setHeaderAccessory} className="flex items-center" />
        </header>
        {/* Sections stay mounted so the logs view keeps its scroll position and selection. */}
        <Pane visible={tab === 0}>{settings && <PreferencesTab key={openCount} initial={settings} />}</Pane>
        <Pane visible={tab === 1}>
          <AccountsTab visible={tab === 1} openCount={openCount} login={login} headerAccessory={headerAccessory} />
        </Pane>
        <Pane visible={tab === 2}>
          <StatusTab key={openCount} />
        </Pane>
        <Pane visible={tab === 3}>
          <LogsTab visible={tab === 3} />
        </Pane>
        <Pane visible={tab === 4}>
          <AboutTab />
        </Pane>
      </main>
    </div>
  );
}

function Pane({ visible, children }: { visible: boolean; children: ReactNode }) {
  return <div className={visible ? "flex min-h-0 min-w-0 flex-1 flex-col" : "hidden"}>{children}</div>;
}

// Sidebar glyphs standing in for the SF Symbols the macOS app uses.

function GearIcon() {
  return (
    <svg viewBox="0 0 20 20" className="size-[16px]" fill="currentColor">
      <path
        fillRule="evenodd"
        d="M11.1 1.5a1 1 0 0 1 1 .8l.3 1.6c.5.2 1 .5 1.4.8l1.5-.6a1 1 0 0 1 1.2.4l1.1 1.9a1 1 0 0 1-.2 1.3l-1.3 1c.1.5.1 1.1 0 1.6l1.3 1a1 1 0 0 1 .2 1.3l-1.1 1.9a1 1 0 0 1-1.2.4l-1.5-.6c-.4.3-.9.6-1.4.8l-.3 1.6a1 1 0 0 1-1 .8H8.9a1 1 0 0 1-1-.8l-.3-1.6c-.5-.2-1-.5-1.4-.8l-1.5.6a1 1 0 0 1-1.2-.4l-1.1-1.9a1 1 0 0 1 .2-1.3l1.3-1a5.6 5.6 0 0 1 0-1.6l-1.3-1a1 1 0 0 1-.2-1.3l1.1-1.9a1 1 0 0 1 1.2-.4l1.5.6c.4-.3.9-.6 1.4-.8l.3-1.6a1 1 0 0 1 1-.8h2.2ZM10 7a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z"
      />
    </svg>
  );
}

/** `person.crop.circle.fill`. */
function AccountsIcon() {
  return (
    <svg viewBox="0 0 20 20" className="size-[16px]" fill="currentColor">
      <path
        fillRule="evenodd"
        d="M10 1.5a8.5 8.5 0 1 1 0 17 8.5 8.5 0 0 1 0-17Zm0 3.4a2.9 2.9 0 1 0 0 5.8 2.9 2.9 0 0 0 0-5.8ZM5 15.3a6.5 6.5 0 0 0 10 0c-.8-1.6-2.8-2.6-5-2.6s-4.2 1-5 2.6Z"
      />
    </svg>
  );
}

function StatusIcon() {
  return (
    <svg viewBox="0 0 20 20" className="size-[16px]" fill="currentColor">
      <rect x="5" y="1.5" width="10" height="6.5" rx="2" />
      <rect x="5" y="12" width="10" height="6.5" rx="2" />
      <rect x="9.25" y="8" width="1.5" height="4" />
    </svg>
  );
}

function LogsIcon() {
  return (
    <svg viewBox="0 0 20 20" className="size-[16px]" fill="currentColor">
      <path
        fillRule="evenodd"
        d="M5 1.5h6.6L16 5.9V16.5A2 2 0 0 1 14 18.5H6a2 2 0 0 1-2-2v-13a2 2 0 0 1 2-2Zm1.5 8h7v1.3h-7V9.5Zm0 3h7v1.3h-7v-1.3Zm0-6h3.5v1.3H6.5V6.5Z"
      />
    </svg>
  );
}

function InfoIcon() {
  return (
    <svg viewBox="0 0 20 20" className="size-[16px]" fill="currentColor">
      <path
        fillRule="evenodd"
        d="M10 1.5a8.5 8.5 0 1 1 0 17 8.5 8.5 0 0 1 0-17ZM9 9h2v5.5H9V9Zm1-4a1.25 1.25 0 1 1 0 2.5A1.25 1.25 0 0 1 10 5Z"
      />
    </svg>
  );
}
