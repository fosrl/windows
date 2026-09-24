import { useEffect, useState } from "react";
import { StatusService, type StatusView } from "@bindings";
import { Dot, Row, SectionTitle, Tabs } from "../components/controls";
import { report, useEvent } from "../lib";

const empty: StatusView = {
  stateText: "Disconnected",
  color: "gray",
  version: "",
  agent: "",
  orgId: "",
  sites: [],
  json: '{\n  "connected": false\n}',
};

export function StatusTab() {
  const [view, setView] = useState<StatusView>(empty);
  const [inner, setInner] = useState(0);

  useEffect(() => {
    report(StatusService.Current().then(setView));
  }, []);
  useEvent<StatusView>("status:update", setView);

  const sites = view.sites ?? [];

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col p-[9px]">
      <Tabs tabs={["Formatted", "JSON"]} index={inner} onChange={setInner}>
        {inner === 0 ? (
          <div className="min-h-0 flex-1 overflow-auto p-[9px]">
            <div className="flex min-w-[340px] flex-col gap-4">
              <SectionTitle>Connection Status</SectionTitle>
              <div className="flex flex-col gap-2">
                <Row label="Status">
                  <Dot color={view.color} size={15} />
                  <span className="text-text-secondary">{view.stateText}</span>
                </Row>
                {view.version && (
                  <Row label="Version">
                    <span className="text-text-secondary select-text">{view.version}</span>
                  </Row>
                )}
                {view.agent && (
                  <Row label="Agent">
                    <span className="text-text-secondary select-text">{view.agent}</span>
                  </Row>
                )}
                {view.orgId && (
                  <Row label="Organization">
                    <span className="text-text-secondary select-text">{view.orgId}</span>
                  </Row>
                )}
              </div>

              <SectionTitle>Sites</SectionTitle>
              {sites.length === 0 ? (
                <div className="text-text-secondary">No sites connected</div>
              ) : (
                <div className="flex flex-col gap-2">
                  {sites.map((site) => (
                    <Row
                      key={site.id}
                      label={
                        <div className="flex flex-col gap-0.5">
                          <span className="break-words">{site.name}</span>
                          {site.endpoint && (
                            <span className="break-all text-text-secondary select-text">{site.endpoint}</span>
                          )}
                        </div>
                      }
                    >
                      <Dot color={site.color} />
                      <span className="text-text-secondary">{site.status}</span>
                    </Row>
                  ))}
                </div>
              )}
            </div>
          </div>
        ) : (
          <pre className="m-0 min-h-0 flex-1 overflow-auto p-2 font-mono text-[13px] select-text">{view.json}</pre>
        )}
      </Tabs>
    </div>
  );
}
