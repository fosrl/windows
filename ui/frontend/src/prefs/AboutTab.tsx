import { useEffect, useState } from "react";
import { AppService, type AppInfo } from "@bindings";
import { ExternalLink, Row, SectionTitle } from "../components/controls";
import { report, urls } from "../lib";

export function AboutTab() {
  const [info, setInfo] = useState<AppInfo>({ version: "", year: new Date().getFullYear() });
  useEffect(() => {
    report(AppService.Info().then(setInfo));
  }, []);

  return (
    <div className="min-h-0 flex-1 overflow-auto p-[9px]">
      <div className="flex flex-col gap-4">
        <SectionTitle>Application</SectionTitle>
        <div className="flex flex-col gap-2">
          <Row label="Version">
            <span className="text-text-secondary select-text">{info.version}</span>
          </Row>
          <Row label="Copyright">
            <span className="text-text-secondary">© {info.year} Fossorial, Inc.</span>
          </Row>
        </div>

        <SectionTitle>Resources</SectionTitle>
        <div>
          <ExternalLink href={urls.docs}>Documentation</ExternalLink>
        </div>
        <div>
          <ExternalLink href={urls.howItWorks}>How Pangolin Works</ExternalLink>
        </div>

        <SectionTitle>Legal</SectionTitle>
        <div>
          <ExternalLink href={urls.terms}>Terms of Service</ExternalLink>
        </div>
        <div>
          <ExternalLink href={urls.privacy}>Privacy Policy</ExternalLink>
        </div>
      </div>
    </div>
  );
}
