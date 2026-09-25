// Fake backend for previewing the UI in a plain browser (`npm run mock`).
import type {
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

const params = new URLSearchParams(window.location.search);
const ok = <T,>(v: T) => Promise.resolve(v);
const log = (...a: unknown[]) => console.log("[mock]", ...a);

const it = (id: string, label: string, enabled = true, checked = false) => ({ id, kind: "item", label, enabled, checked });
const sep = { id: "", kind: "separator", label: "", enabled: false, checked: false };
const hdr = (label: string) => ({ id: "", kind: "header", label, enabled: false, checked: false });

const menu: MenuState = {
  items: [
    it("", "Status: Connected", false),
    it("connect", "Disconnect", true, true),
    sep,
    hdr("Account"),
    {
      id: "accounts", kind: "submenu", label: "milo@pangolin.net", enabled: true, checked: false,
      items: [hdr("Available Accounts"), sep, it("account:1", "milo@pangolin.net", true, true), it("account:2", "ops@example.com"), sep, it("account.add", "Add Account"), it("account.logout", "Logout")],
    },
    hdr("Organization"),
    {
      id: "orgs", kind: "submenu", label: "Fossorial", enabled: true, checked: false,
      items: [hdr("2 Organizations"), sep, it("org:a", "Fossorial", true, true), it("org:b", "Home Lab")],
    },
    sep,
    it("preferences", "Preferences"),
    {
      id: "more", kind: "submenu", label: "More", enabled: true, checked: false,
      items: [hdr("Support"), it("more.howItWorks", "How Pangolin Works"), it("more.docs", "Documentation"), sep, it("", "© 2026 Fossorial, Inc.", false), it("more.terms", "Terms of Service"), it("more.privacy", "Privacy Policy"), sep, it("", "Version: 0.9.0", false), it("more.checkUpdates", "Check for Updates"), it("more.installCLI", "Install Pangolin CLI")],
    },
    sep,
    it("", "Community Edition. Consider supporting.", false),
    sep,
    it("quit", "Quit"),
  ],
};

const settings: Settings = {
  openAtLogin: true, autoConnect: false, dnsOverride: true, dnsTunnel: false,
  primaryDns: "", secondaryDns: "", mtu: "1280", disabled: params.has("disabled"),
};

const status: StatusView = {
  stateText: "Connected", color: "green", version: "1.9.0", agent: "Pangolin Windows", orgId: "fossorial",
  sites: [
    { id: -1, name: "Pangolin Server", endpoint: "203.0.113.10:51820", status: "Connected", color: "green", connection: "", lastSeen: new Date(Date.now() - 4000).toISOString() },
    { id: 3, name: "Home Lab", endpoint: "198.51.100.7:51820", status: "Connected", color: "green", connection: "Direct", lastSeen: new Date(Date.now() - 90000).toISOString() },
    { id: 7, name: "Office", endpoint: "", status: "Connecting", color: "yellow", connection: "Relay", lastSeen: "" },
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

const stage = params.get("stage") ?? "hosting";
const login: LoginView = {
  stage, selfHostedUrl: stage === "url" ? "pangolin.example.com" : "",
  code: stage === "code" ? "A B C D - E F G H" : "",
  manualUrl: stage === "code" ? "https://app.pangolin.net/auth/login/device" : "",
  showBack: stage !== "hosting", backEnabled: stage !== "code", showLogin: stage === "url", loginEnabled: stage === "url",
};

const layout: TrayLayout = { submenuSide: "left", anchor: "bottom", panelWidth: 260, padding: 8 };

export const MenuService = {
  State: () => ok(menu), Layout: () => ok(layout),
  Invoke: (id: string) => ok(log("invoke", id)), Resize: (h: number) => ok(log("resize", h)), Hide: () => ok(log("hide")),
};
export const PreferencesService = {
  Opened: () => ok<PrefsOpened>({ tab: Number(params.get("tab") ?? 0), settings }),
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
  State: () => ok(login), ChooseCloud: () => ok(log("cloud")), ChooseSelfHosted: () => ok(log("self")),
  SetURL: (u: string) => ok(log("url", u)), Login: () => ok(log("login")), Back: () => ok(log("back")),
  Cancel: () => ok(log("cancel")), CopyCode: () => ok(log("copy")), OpenBrowser: () => ok(log("browser")),
};
export const AppService = {
  Info: () => ok<AppInfo>({ version: "0.9.0", year: 2026 }),
  OpenURL: (u: string) => ok(log("open", u)),
  ProgressText: () => ok("Downloading update (12.4 / 38.0 MB)…"),
};
