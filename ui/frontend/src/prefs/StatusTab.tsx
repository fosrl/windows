import { useEffect, useRef, useState } from "react";
import { Clipboard } from "@wailsio/runtime";
import { StatusService, type StatusSite, type StatusView } from "@bindings";
import { Button, Chevron, Form, Picker, Row, Section, Sheet, StatusDot, Value } from "../components/controls";
import { report, useEvent } from "../lib";

const empty: StatusView = {
  stateText: "Disconnected",
  color: "gray",
  version: "",
  agent: "",
  orgId: "",
  gateway: "Off",
  sites: [],
  json: '{\n  "connected": false\n}',
};

const modeKey = "pangolin.statusDisplayMode";

function savedMode(): "formatted" | "json" {
  try {
    return localStorage.getItem(modeKey) === "json" ? "json" : "formatted";
  } catch {
    return "formatted";
  }
}

export function StatusTab() {
  const [view, setView] = useState<StatusView>(empty);
  const [mode, setMode] = useState(savedMode);
  const [copied, setCopied] = useState(false);
  const [selectedSite, setSelectedSite] = useState<number | null>(null);
  const copiedTimer = useRef<number | undefined>(undefined);

  useEffect(() => {
    report(StatusService.Current().then(setView));
    return () => window.clearTimeout(copiedTimer.current);
  }, []);
  useEvent<StatusView>("status:update", setView);

  const changeMode = (m: "formatted" | "json") => {
    setMode(m);
    try {
      localStorage.setItem(modeKey, m);
    } catch {
      // Remembering the mode is only a convenience.
    }
  };

  const copyJSON = () => {
    report(Clipboard.SetText(view.json));
    setCopied(true);
    window.clearTimeout(copiedTimer.current);
    copiedTimer.current = window.setTimeout(() => setCopied(false), 2000);
  };

  const sites = view.sites ?? [];

  return (
    <Form>
      <Section header="View">
        <Row title="Display Mode">
          <Picker value={mode} onChange={(e) => changeMode(e.target.value as "formatted" | "json")}>
            <option value="formatted">Formatted</option>
            <option value="json">JSON</option>
          </Picker>
        </Row>
      </Section>

      {mode === "json" ? (
        <Section
          header="JSON"
          accessory={
            <Button size="small" onClick={copyJSON}>
              {copied ? "✓ Copied" : "Copy"}
            </Button>
          }
        >
          <pre className="m-0 overflow-x-auto px-2.5 py-2.5 font-mono text-[12px] leading-[17px] select-text">
            {view.json}
          </pre>
        </Section>
      ) : (
        <>
          <Section header="Connection Status">
            {view.agent && (
              <Row title="Agent">
                <Value>{view.agent}</Value>
              </Row>
            )}
            {view.version && (
              <Row title="Version">
                <Value>{view.version}</Value>
              </Row>
            )}
            <Row title="Status">
              <StatusDot color={view.color} />
              <Value>{view.stateText}</Value>
            </Row>
            {view.orgId && (
              <Row title="Organization">
                <Value>{view.orgId}</Value>
              </Row>
            )}
            {view.orgId && (
              <Row title="Exit Node">
                <Value>{view.gateway}</Value>
              </Row>
            )}
          </Section>

          <Section header="Sites">
            {sites.length === 0 ? (
              <Row title={<span className="text-mac-secondary">No sites connected</span>} />
            ) : (
              sites.map((site) => (
                <Row
                  key={site.id}
                  title={site.name}
                  description={site.endpoint || undefined}
                  onClick={() => setSelectedSite(site.id)}
                >
                  <StatusDot color={site.color} />
                  <Value>{site.status}</Value>
                  <Chevron />
                </Row>
              ))
            )}
          </Section>
        </>
      )}

      {selectedSite !== null && (
        <SiteSheet site={sites.find((s) => s.id === selectedSite)} onClose={() => setSelectedSite(null)} />
      )}
    </Form>
  );
}

/** Site details, like the macOS app's SiteStatusSheet. It follows live status updates. */
function SiteSheet({ site, onClose }: { site?: StatusSite; onClose: () => void }) {
  return (
    <Sheet
      divider={false}
      onCancel={onClose}
      onSubmit={onClose}
      footer={
        <>
          <div className="flex-1" />
          <Button type="submit" autoFocus>
            Done
          </Button>
        </>
      }
    >
      <div className="pb-1 text-[13px] font-semibold">{site?.name ?? "Site"}</div>
      {site ? (
        <div className="flex flex-col gap-3.5">
          <Detail label="Site" value={site.name} />
          <Detail label="Status">
            <span className="flex items-center gap-1.5">
              <StatusDot color={site.color} />
              <span className="text-mac-secondary select-text">{site.status}</span>
            </span>
          </Detail>
          <Detail label="Connection" value={site.connection || "—"} />
          <Detail label="Gateway" value={site.connection ? (site.gateway ? "Yes" : "No") : "—"} />
          <Detail label="Endpoint" value={site.endpoint || "—"} />
          <Detail label="Last Seen" value={relativeTime(site.lastSeen)} />
        </div>
      ) : (
        <div className="text-mac-secondary">This site is no longer in the status response.</div>
      )}
    </Sheet>
  );
}

function Detail({ label, value, children }: { label: string; value?: string; children?: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <span>{label}</span>
      {children ?? <span className="text-right break-all text-mac-secondary select-text">{value}</span>}
    </div>
  );
}

/** "12s ago", "5m ago", "3h ago", "2d ago", matching the macOS app. */
function relativeTime(iso: string): string {
  if (!iso) return "—";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return iso;
  const seconds = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}
