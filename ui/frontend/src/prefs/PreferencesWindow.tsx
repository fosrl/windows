import { useEffect, useState } from "react";
import { PreferencesService, type PrefsOpened, type Settings } from "@bindings";
import { Tabs } from "../components/controls";
import { report, useEvent } from "../lib";
import { PreferencesTab } from "./PreferencesTab";
import { StatusTab } from "./StatusTab";
import { LogsTab } from "./LogsTab";
import { AboutTab } from "./AboutTab";

const tabNames = ["Preferences", "Status", "Logs", "About"];

export function PreferencesWindow() {
  const [tab, setTab] = useState(0);
  const [settings, setSettings] = useState<Settings | null>(null);
  // Bumped each time the window is shown, so tabs reset like a freshly opened window.
  const [openCount, setOpenCount] = useState(0);

  const onOpened = (o: PrefsOpened) => {
    if (o.tab >= 0 && o.tab < tabNames.length) setTab(o.tab);
    setSettings(o.settings);
    setOpenCount((n) => n + 1);
  };

  useEffect(() => {
    report(PreferencesService.Opened().then(onOpened));
  }, []);
  useEvent<PrefsOpened>("prefs:opened", onOpened);

  return (
    <div className="flex h-full min-w-0 flex-col p-[9px]">
      <Tabs tabs={tabNames} index={tab} onChange={setTab}>
        {/* Tabs stay mounted so the logs view keeps its scroll position and selection. */}
        <div className={tab === 0 ? "flex min-h-0 min-w-0 flex-1 flex-col" : "hidden"}>
          {settings && <PreferencesTab key={openCount} initial={settings} />}
        </div>
        <div className={tab === 1 ? "flex min-h-0 min-w-0 flex-1 flex-col" : "hidden"}>
          <StatusTab key={openCount} />
        </div>
        <div className={tab === 2 ? "flex min-h-0 min-w-0 flex-1 flex-col" : "hidden"}>
          <LogsTab visible={tab === 2} />
        </div>
        <div className={tab === 3 ? "flex min-h-0 min-w-0 flex-1 flex-col" : "hidden"}>
          <AboutTab />
        </div>
      </Tabs>
    </div>
  );
}
