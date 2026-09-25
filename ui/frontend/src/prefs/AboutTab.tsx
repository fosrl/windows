import { useEffect, useState } from "react";
import { AppService, type AppInfo } from "@bindings";
import { Form, LinkRow, Row, Section, Value } from "../components/controls";
import { report, urls } from "../lib";

export function AboutTab() {
  const [info, setInfo] = useState<AppInfo>({ version: "", year: new Date().getFullYear() });
  useEffect(() => {
    report(AppService.Info().then(setInfo));
  }, []);

  return (
    <Form>
      <Section header="Application">
        <Row title="Version">
          <Value>{info.version}</Value>
        </Row>
        <Row title="Copyright">
          <Value>© {info.year} Fossorial, Inc.</Value>
        </Row>
      </Section>

      <Section header="Resources">
        <LinkRow href={urls.docs}>Documentation</LinkRow>
        <LinkRow href={urls.howItWorks}>How Pangolin Works</LinkRow>
      </Section>

      <Section header="Legal">
        <LinkRow href={urls.terms}>Terms of Service</LinkRow>
        <LinkRow href={urls.privacy}>Privacy Policy</LinkRow>
      </Section>
    </Form>
  );
}
