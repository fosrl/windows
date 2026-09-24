import { useState, type ReactNode } from "react";
import { PreferencesService, type Settings } from "@bindings";
import { Button, Checkbox, ExternalLink, Row, Secondary, SectionTitle, TextField } from "../components/controls";
import { report, urls } from "../lib";

export function PreferencesTab({ initial }: { initial: Settings }) {
  const [form, setForm] = useState<Settings>(initial);
  const [saving, setSaving] = useState(false);
  const disabled = form.disabled;
  const set = (patch: Partial<Settings>) => setForm((f) => ({ ...f, ...patch }));

  const save = () => {
    setSaving(true);
    report(
      PreferencesService.Save(form)
        .then(setForm)
        .finally(() => setSaving(false)),
    );
  };

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col p-[9px]">
      <div className="min-h-0 flex-1 overflow-auto pr-1">
        <fieldset disabled={disabled} className="flex min-w-[400px] flex-col gap-4 disabled:opacity-60">
          <SectionTitle>General</SectionTitle>

          <Setting
            label="Start at Login"
            description="Starts Pangolin when you sign in to Windows."
          >
            <Checkbox
              label="Start at Login"
              checked={form.openAtLogin}
              disabled={disabled}
              // Turning off start at login also turns off connect at start.
              onChange={(v) => set({ openAtLogin: v, autoConnect: v ? form.autoConnect : false })}
            />
          </Setting>

          <Setting
            label="Connect at Start"
            description={"Connects the tunnel whenever Pangolin starts.\nAlso opens Pangolin at sign-in."}
          >
            <Checkbox
              label="Connect at Start"
              checked={form.autoConnect}
              disabled={disabled}
              // Connect at start implies start at login.
              onChange={(v) => set({ autoConnect: v, openAtLogin: v ? true : form.openAtLogin })}
            />
          </Setting>

          <SectionTitle>DNS Settings</SectionTitle>

          <Setting
            label="Enable Aliases (DNS Override)"
            description={
              "When enabled, the client uses custom DNS servers to resolve internal\nresources and aliases. This overrides your system’s default DNS settings.\nQueries that cannot be resolved as a Pangolin resource will be forwarded\nto your configured Upstream DNS Server."
            }
          >
            <Checkbox
              label="Enable Aliases (DNS Override)"
              checked={form.dnsOverride}
              disabled={disabled}
              onChange={(v) => set({ dnsOverride: v })}
            />
          </Setting>

          <Setting
            label="DNS Over Tunnel"
            description={
              "When enabled, DNS queries are routed through the tunnel for\nremote resolution. To ensure queries are tunneled correctly,\nyou must define the DNS server as a Pangolin resource and\nenter its address as an Upstream DNS Server."
            }
          >
            <Checkbox
              label="DNS Over Tunnel"
              checked={form.dnsTunnel}
              disabled={disabled}
              onChange={(v) => set({ dnsTunnel: v })}
            />
          </Setting>

          <Row label="Primary Upstream DNS Server">
            <TextField
              className="w-[150px]"
              placeholder="Default: system DNS"
              value={form.primaryDns}
              onChange={(e) => set({ primaryDns: e.target.value })}
            />
          </Row>
          <Row label="Secondary Upstream DNS Server">
            <TextField
              className="w-[150px]"
              placeholder="Default: system DNS"
              value={form.secondaryDns}
              onChange={(e) => set({ secondaryDns: e.target.value })}
            />
          </Row>

          <SectionTitle>Advanced</SectionTitle>

          <Row label="MTU">
            <TextField className="w-[150px]" value={form.mtu} onChange={(e) => set({ mtu: e.target.value })} />
          </Row>
          <Secondary>Your sites must be configured to use the same MTU value.</Secondary>
        </fieldset>

        <div className="mt-6">
          Tip: <ExternalLink href={urls.configureClient}>See the docs for more information on these settings</ExternalLink>
        </div>
      </div>

      <div className="flex justify-end pt-[9px]">
        <Button accessKey="s" onClick={save} disabled={disabled || saving}>
          Save
        </Button>
      </div>
    </div>
  );
}

function Setting({ label, description, children }: { label: string; description: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <Row label={label}>{children}</Row>
      <Secondary className="whitespace-pre">{description}</Secondary>
    </div>
  );
}
