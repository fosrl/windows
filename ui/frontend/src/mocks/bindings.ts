// Fake backend for previewing the UI in a plain browser (`npm run mock`).
import type {
  AccountsView,
  AppInfo,
  LogsSnapshot,
  LoginView,
  MenuState,
  PrefsOpened,
  Settings,
  StatusView,
  TrayLayout,
} from "../bindings/github.com/fosrl/windows/ui/models";

export type * from "../bindings/github.com/fosrl/windows/ui/models";
import { Events } from "./runtime";

const params = new URLSearchParams(window.location.search);
const ok = <T,>(v: T) => Promise.resolve(v);
const log = (...a: unknown[]) => console.log("[mock]", ...a);

// Menu rows as the backend sends them. `?menu=` picks a state to preview:
// connected (default), disconnected, locked, signedOut or loading.
type Row = MenuState["items"] extends (infer T)[] | null ? T : never;
const row = (r: Partial<Row>): Row => ({
  id: "", kind: "item", label: "", enabled: true, checked: false, checkable: false,
  loading: false, dot: "", icon: "", inset: false, value: "", ...r,
});
const it = (id: string, label: string, enabled = true) => row({ id, label, enabled });
const check = (id: string, label: string, checked = false) => row({ id, label, checkable: true, checked });
const sep = row({ kind: "separator", enabled: false });
const hdr = (label: string, inset = false) => row({ kind: "header", label, enabled: false, inset });
const lbl = (label: string, icon = "") => row({ kind: "label", label, icon, enabled: false });
const det = (label: string, value: string, dot = "") => row({ kind: "detail", label, value, dot, enabled: false });

const site = (id: number, name: string, dot: string, status: string, connection: string, endpoint: string, lastSeen: string) =>
  row({
    id: `site:${id}`, kind: "submenu", label: name, dot,
    items: [hdr(name), det("Status", status, dot), det("Connection", connection), det("Endpoint", endpoint), det("Last Seen", lastSeen)],
  });

let menuState = new URLSearchParams(window.location.search).get("menu") ?? "connected";

function buildMenu(): MenuState {
const connected = menuState === "connected";
const locked = menuState === "locked";

const tunnel: Row[] = [
  row({
    id: "sites", kind: "status", label: connected ? "Connected" : locked ? "Account Locked" : "Disconnected",
    dot: connected ? "green" : "gray",
    items: connected
      ? [
          it("sites.openStatus", "Open Status…"), sep, hdr("3 Sites"),
          site(-1, "Pangolin Server", "green", "Connected", "—", "203.0.113.10:51820", "4s ago"),
          site(3, "Home Lab", "green", "Connected", "Direct", "198.51.100.7:51820", "1m ago"),
          site(7, "Office", "yellow", "Connecting", "Relay", "—", "—"),
        ]
      : null,
  }),
  row({ id: "connect", kind: "toggle", label: connected ? "Disconnect" : "Connect", checked: connected, enabled: !locked }),
  ...(locked ? [lbl("Your session expired. Log in again to connect.", "lock"), it("reauth", "Log In…")] : []),
  ...(menuState === "disconnected" ? [lbl("Could not reach the server.", "warning")] : []),
  sep,
];

return {
  loading: menuState === "loading",
  items: [
    ...(menuState === "signedOut" ? [it("login", "Log In…")] : [
      ...tunnel,
      hdr("Account"),
      row({
        id: "accounts", kind: "submenu", label: "milo@pangolin.net",
        items: [hdr("Available Accounts", true), check("account:1", "milo@pangolin.net", true), check("account:2", "ops@example.com"), sep, it("account.add", "Add Account…"), it("account.logout", "Log Out")],
      }),
      hdr("Organization"),
      row({
        id: "orgs", kind: "submenu", label: "Fossorial",
        items: [hdr("2 Organizations", true), check("org:a", "Fossorial", true), check("org:b", "Home Lab")],
      }),
      hdr("Exit Node"),
      row({
        id: "exitnodes", kind: "submenu", label: "None",
        items: [hdr("Route All Traffic Through", true), check("exitnode.none", "None", true), check("exitnode:7", "Office")],
      }),
    ]),
    sep,
    it("preferences", "Preferences…"),
    row({
      id: "more", kind: "submenu", label: "More",
      items: [hdr("Support"), it("more.howItWorks", "How Pangolin Works"), it("more.docs", "Documentation"), sep, hdr("© 2026 Fossorial, Inc."), it("more.terms", "Terms of Service"), it("more.privacy", "Privacy Policy"), sep, hdr("Version 0.9.0"), it("more.checkUpdates", "Check for Updates…"), it("more.installCLI", "Install Pangolin CLI…")],
    }),
    sep,
    it("quit", "Quit Pangolin"),
    sep,
    lbl("Community Edition. Consider supporting."),
  ],
};
}

// Clicks change the preview so the menu's transitions can be seen: the switch
// connects and disconnects, and switching accounts or orgs shows the spinner.
function mockInvoke(id: string) {
  log("invoke", id);
  const emit = () => Events.Emit("menu:state", buildMenu());
  if (id === "connect") {
    menuState = menuState === "connected" ? "disconnected" : "connected";
    emit();
  } else if (id.startsWith("account:") || id.startsWith("org:")) {
    const was = menuState;
    menuState = "loading";
    emit();
    setTimeout(() => {
      menuState = was;
      emit();
    }, 1200);
  }
  return ok(undefined);
}

const settings: Settings = {
  openAtLogin: true, autoConnect: false, dnsOverride: true, dnsTunnel: false,
  primaryDns: "", secondaryDns: "", mtu: "1280", exitNodeTakesPrecedence: false,
  disabled: params.has("disabled"),
};

const status: StatusView = {
  stateText: "Connected", color: "green", version: "1.9.0", agent: "Pangolin Windows", orgId: "fossorial", gateway: "Active",
  sites: [
    { id: -1, name: "Pangolin Server", endpoint: "203.0.113.10:51820", status: "Connected", color: "green", connection: "", lastSeen: new Date(Date.now() - 4000).toISOString(), gateway: false },
    { id: 3, name: "Home Lab", endpoint: "198.51.100.7:51820", status: "Connected", color: "green", connection: "Direct", lastSeen: new Date(Date.now() - 90000).toISOString(), gateway: true },
    { id: 7, name: "Office", endpoint: "", status: "Connecting", color: "yellow", connection: "Relay", lastSeen: "", gateway: false },
  ],
  json: JSON.stringify({ connected: true, registered: true, version: "1.9.0" }, null, 2),
};

const levels = ["INFO", "INFO", "DEBUG", "WARN", "ERROR"];
const logs: LogsSnapshot = {
  entries: Array.from({ length: 400 }, (_, i) => ({
    seq: i + 1,
    stamp: `2026-09-24 10:${String(Math.floor(i / 60)).padStart(2, "0")}:${String(i % 60).padStart(2, "0")}.000`,
    level: levels[i % levels.length],
    line: `Tunnel state changed: message ${i + 1} with some extra detail to show truncation of long lines in the table`,
  })),
};

// Accounts tab: `?accounts=none` shows the welcome screen.
const accounts: AccountsView = {
  busyUserId: "", tunnelStarting: false,
  accounts: params.get("accounts") === "none" ? [] : [
    { userId: "1", displayName: "milo@pangolin.net", host: "app.pangolin.net", hostname: "https://app.pangolin.net", subtitle: "app.pangolin.net · Fossorial", active: true, locked: false },
    { userId: "2", displayName: "ops@example.com", host: "pangolin.example.com", hostname: "https://pangolin.example.com", subtitle: "pangolin.example.com", active: false, locked: false },
    { userId: "3", displayName: "lab@home.arpa", host: "pangolin.home.arpa", hostname: "https://pangolin.home.arpa", subtitle: "pangolin.home.arpa · Login required", active: false, locked: true },
  ],
};

// Login sheet: `?step=` previews a step; otherwise Continue walks through them.
const loginView = (step: string, extra: Partial<LoginView> = {}): LoginView => ({
  session: 1, step, host: "app.pangolin.net", code: "ABCD-EFGH", email: "milo@pangolin.net", error: "",
  renewing: false, fixedHost: false, selfHosted: false, serverUrl: "", ...extra,
});
function mockLoginStart(hostname: string) {
  log("login start", hostname);
  const host = new URL(hostname).host;
  const emit = (step: string) => Events.Emit("login:state", loginView(step, { host }));
  emit("starting");
  setTimeout(() => emit("code"), 900);
  setTimeout(() => emit("success"), 3500);
  return ok(undefined);
}

const layout: TrayLayout = { submenuSide: "right", anchor: "top", panelWidth: 310, padding: 8 };

export const MenuService = {
  State: () => ok(buildMenu()), Layout: () => ok(layout),
  Invoke: mockInvoke, Resize: (h: number) => ok(log("resize", h)), Hide: () => ok(log("hide")),
  SetSitesVisible: (v: boolean) => ok(log("sites visible", v)), HideReady: () => ok(log("hide ready")), OpenReady: (h: number) => ok(log("open ready", h)),
};
export const PreferencesService = {
  Opened: () => ok<PrefsOpened>({
    tab: Number(params.get("tab") ?? 0), settings,
    login: params.has("step") ? { id: 1, hostname: params.get("renew") ?? "" } : null,
  }),
  Update: (f: Settings) => {
    Object.assign(settings, f);
    if (!/^\d+$/.test(f.mtu) || +f.mtu < 576 || +f.mtu > 9000)
      return ok({ settings: { ...settings, mtu: "1280" }, field: "mtu", error: "Enter an integer between 576 and 9000 (e.g., 1280)" });
    return ok({ settings: { ...settings }, field: "", error: "" });
  },
};
export const StatusService = { Current: () => ok(status) };
export const LogsService = {
  Snapshot: () => ok(logs), Clear: () => ok(log("clear")), Copy: (s: number[]) => ok(log("copy", s)), Export: () => ok(log("export")),
};
export const LoginService = {
  State: () => ok(loginView(params.get("step") ?? "server")),
  Open: (renew: string) => ok(loginView(params.get("step") ?? (renew ? "loading" : "server"), { renewing: !!renew, fixedHost: !!renew })),
  Start: mockLoginStart, Back: () => ok(log("back")), Close: (id: number) => ok(log("close", id)),
  CopyCode: () => ok(log("copy")), OpenBrowser: () => ok(log("browser")),
};
// Setup: `?onboarding=welcome|privacy|done` picks the first unfinished page.
const onboardingStage = params.get("onboarding") ?? "welcome";
export const OnboardingService = {
  State: () => ok({
    seenWelcome: onboardingStage !== "welcome", acknowledgedPrivacy: onboardingStage === "done",
    hasAccounts: params.get("accounts") !== "none", opened: 1,
  }),
  MarkWelcomeSeen: () => ok(log("welcome seen")), MarkPrivacyAcknowledged: () => ok(log("privacy acknowledged")),
  Close: () => ok(log("close setup")),
};
export const AccountsService = {
  State: () => ok(accounts), Switch: (id: string) => ok(log("switch", id)), Remove: (id: string) => ok(log("remove", id)),
};
export const AppService = {
  Info: () => ok<AppInfo>({ version: "0.9.0", year: 2026 }),
  OpenURL: (u: string) => ok(log("open", u)),
  ProgressText: () => ok("Downloading update (12.4 / 38.0 MB)…"),
};
