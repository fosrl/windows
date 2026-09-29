import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

const path = (p: string) => fileURLToPath(new URL(p, import.meta.url));

// `vite --mode mock` swaps the Wails runtime and bindings for fakes so the UI
// can be previewed in a plain browser.
export default defineConfig(({ mode }) => ({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias:
      mode === "mock"
        ? {
            "@bindings": path("./src/mocks/bindings.ts"),
            "@wailsio/runtime": path("./src/mocks/runtime.ts"),
          }
        : {
            "@bindings": path("./src/bindings/github.com/fosrl/windows/ui"),
          },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsInlineLimit: 0,
  },
}));
