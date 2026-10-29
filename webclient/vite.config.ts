import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  // The API's CORS allow-list is exactly http://localhost:5173.
  server: { port: 5173, strictPort: true, host: "localhost", fs: { allow: [".", "../design/system"] } }, // design-sync.test.ts reads design/system as ?raw
  test: {
    environment: "jsdom",
    globals: true,
    css: true, // so ?raw CSS reads in design-sync.test.ts return real text
    setupFiles: ["./src/test-setup.ts"],
  },
});
