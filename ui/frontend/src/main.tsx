import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import { TrayMenu } from "./tray/TrayMenu";
import { PreferencesWindow } from "./prefs/PreferencesWindow";
import { OnboardingWindow } from "./onboarding/OnboardingWindow";
import { UpdateWindow } from "./update/UpdateWindow";
import { CLIWindow } from "./cli/CLIWindow";

// Each native window loads the same page with a different hash route.
const [route] = window.location.hash.replace(/^#\/?/, "").split("?");

function App() {
  switch (route) {
    case "tray":
      return <TrayMenu />;
    case "preferences":
      return <PreferencesWindow />;
    case "onboarding":
      return <OnboardingWindow />;
    case "cli":
      return <CLIWindow />;
    case "update":
      return <UpdateWindow />;
    default:
      return null;
  }
}

if (route === "tray") document.documentElement.classList.add("tray");
else document.documentElement.classList.add("mac");
if (route === "onboarding") document.documentElement.classList.add("onboarding");

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
