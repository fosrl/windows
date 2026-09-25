import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import { TrayMenu } from "./tray/TrayMenu";
import { PreferencesWindow } from "./prefs/PreferencesWindow";
import { LoginWindow } from "./login/LoginWindow";
import { ProgressWindow } from "./progress/ProgressWindow";

// Each native window loads the same page with a different hash route.
const [route, query = ""] = window.location.hash.replace(/^#\/?/, "").split("?");
const params = new URLSearchParams(query);

function App() {
  switch (route) {
    case "tray":
      return <TrayMenu />;
    case "preferences":
      return <PreferencesWindow />;
    case "login":
      return <LoginWindow />;
    case "progress":
      return <ProgressWindow kind={params.get("kind") ?? ""} />;
    default:
      return null;
  }
}

if (route === "tray") document.documentElement.classList.add("tray");
else document.documentElement.classList.add("mac");
if (route === "login") document.documentElement.classList.add("login");

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
