import { useRef, useState } from "react";
import { PreferencesService, type Settings, type SettingsResult } from "@bindings";
import { Button, Form, LinkRow, Row, Section, Sheet, Switch, TextField, Value } from "../components/controls";
import { report, urls } from "../lib";

const defaultMTU = "1280";

type EditField = "primaryDns" | "secondaryDns" | "mtu";

/** Settings apply as soon as they change, like the macOS app. */
export function PreferencesTab({ initial }: { initial: Settings }) {
  const [form, setForm] = useState<Settings>(initial);
  const [editing, setEditing] = useState<EditField | null>(null);
  const formRef = useRef(form);
  formRef.current = form;
  const disabled = form.disabled;

  /** Saves a change. Resolves to the backend's result so sheets can show validation errors. */
  const apply = async (patch: Partial<Settings>): Promise<SettingsResult> => {
    const next = { ...formRef.current, ...patch };
    setForm(next);
    const result = await PreferencesService.Update(next);
    setForm(result.settings);
    return result;
  };
  const toggle = (patch: Partial<Settings>) => report(apply(patch));

  return (
    <>
      <Form>
        {disabled && (
          <div className="rounded-[10px] border border-mac-group-border bg-mac-group px-3 py-2 text-[12px] text-mac-secondary">
            These settings are managed by your administrator.
          </div>
        )}

        <Section header="General">
          <Row title="Start at Login" description="Starts Pangolin when you sign in to Windows.">
            <Switch
              label="Start at Login"
              checked={form.openAtLogin}
              disabled={disabled}
              // Turning off start at login also turns off connect at start.
              onChange={(v) => toggle({ openAtLogin: v, autoConnect: v ? form.autoConnect : false })}
            />
          </Row>
          <Row
            title="Connect at Start"
            description="Connects the tunnel whenever Pangolin starts. Also opens Pangolin at sign-in."
          >
            <Switch
              label="Connect at Start"
              checked={form.autoConnect}
              disabled={disabled}
              // Connect at start implies start at login.
              onChange={(v) => toggle({ autoConnect: v, openAtLogin: v ? true : form.openAtLogin })}
            />
          </Row>
        </Section>

        <Section header="DNS Settings">
          <Row
            title="Enable Aliases (DNS Override)"
            description="When enabled, the client uses custom DNS servers to resolve internal resources and aliases. This overrides your system’s default DNS settings. Queries that cannot be resolved as a Pangolin resource will be forwarded to your configured Upstream DNS Server."
          >
            <Switch
              label="Enable Aliases (DNS Override)"
              checked={form.dnsOverride}
              disabled={disabled}
              onChange={(v) => toggle({ dnsOverride: v })}
            />
          </Row>
          <Row
            title="DNS Over Tunnel"
            description="When enabled, DNS queries are routed through the tunnel for remote resolution. To ensure queries are tunneled correctly, you must define the DNS server as a Pangolin resource and enter its address as an Upstream DNS Server."
          >
            <Switch
              label="DNS Over Tunnel"
              checked={form.dnsTunnel}
              disabled={disabled}
              onChange={(v) => toggle({ dnsTunnel: v })}
            />
          </Row>
          <Row title="Primary Upstream DNS Server">
            <Value>{form.primaryDns || "System DNS"}</Value>
            <Button size="small" disabled={disabled} onClick={() => setEditing("primaryDns")}>
              Set...
            </Button>
          </Row>
          <Row title="Secondary Upstream DNS Server">
            <Value>{form.secondaryDns || "System DNS"}</Value>
            <Button size="small" disabled={disabled} onClick={() => setEditing("secondaryDns")}>
              Set...
            </Button>
          </Row>
        </Section>

        <Section header="Advanced">
          <Row
            title="Exit Node Takes Precedence Over Resources"
            description="When enabled, routes for individual resources are not added to the system and their aliases are not resolved, so all traffic is sent through the exit node instead of directly to resources. Exit node (gateway) routes are unaffected."
          >
            <Switch
              label="Exit Node Takes Precedence Over Resources"
              checked={form.exitNodeTakesPrecedence}
              disabled={disabled}
              onChange={(v) => toggle({ exitNodeTakesPrecedence: v })}
            />
          </Row>
          <Row title="MTU" description="Your sites must be configured to use the same MTU value.">
            <Value>{form.mtu}</Value>
            <Button size="small" disabled={disabled} onClick={() => setEditing("mtu")}>
              Set...
            </Button>
          </Row>
        </Section>

        <Section header="Help">
          <LinkRow href={urls.configureClient}>See docs for more info on these settings</LinkRow>
        </Section>
      </Form>

      {editing === "mtu" && (
        <ValueSheet
          title="MTU:"
          initial={form.mtu}
          hint="Enter an integer between 576 and 9000 (e.g., 1280)"
          resetLabel="Default"
          resetValue={defaultMTU}
          onClose={() => setEditing(null)}
          onSave={(v) => apply({ mtu: v })}
          field="mtu"
        />
      )}
      {(editing === "primaryDns" || editing === "secondaryDns") && (
        <ValueSheet
          title={editing === "primaryDns" ? "Primary Upstream DNS Server:" : "Secondary Upstream DNS Server:"}
          initial={form[editing]}
          placeholder="System DNS"
          resetLabel="Use System DNS"
          resetValue=""
          resetSaves
          onClose={() => setEditing(null)}
          onSave={(v) => apply({ [editing]: v })}
          field={editing}
        />
      )}
    </>
  );
}

/** The DNS and MTU editing sheets from the macOS app. */
function ValueSheet({
  title,
  initial,
  placeholder,
  hint,
  resetLabel,
  resetValue,
  resetSaves,
  field,
  onSave,
  onClose,
}: {
  title: string;
  initial: string;
  placeholder?: string;
  hint?: string;
  resetLabel: string;
  resetValue: string;
  /** "Use System DNS" saves right away; "Default" only fills the field. */
  resetSaves?: boolean;
  field: EditField;
  onSave: (value: string) => Promise<SettingsResult>;
  onClose: () => void;
}) {
  const [value, setValue] = useState(initial);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const save = async (v: string) => {
    setBusy(true);
    try {
      const result = await onSave(v.trim());
      if (result.field === field && result.error) setError(result.error);
      else onClose();
    } catch (err) {
      console.error(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Sheet
      onCancel={onClose}
      onSubmit={() => save(value)}
      footer={
        <>
          <Button
            onClick={() => {
              setValue(resetValue);
              setError("");
              if (resetSaves) void save(resetValue);
            }}
          >
            {resetLabel}
          </Button>
          <div className="flex-1" />
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="prominent" disabled={busy}>
            Done
          </Button>
        </>
      }
    >
      <label className="text-[13px]" htmlFor="sheet-value">
        {title}
      </label>
      <TextField
        id="sheet-value"
        autoFocus
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          setValue(e.target.value);
          setError("");
        }}
        className="w-full"
      />
      {(error || hint) && (
        <div className={error ? "text-[11px] text-mac-danger" : "text-[11px] text-mac-secondary"}>{error || hint}</div>
      )}
    </Sheet>
  );
}
