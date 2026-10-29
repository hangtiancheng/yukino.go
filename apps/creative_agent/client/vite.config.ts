import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react, { reactCompilerPreset } from "@vitejs/plugin-react";
import babel from "@rolldown/plugin-babel";
import { defineConfig } from "vite";

// Backend address: defaults to 18080 (local port 8080 is often taken by nginx; the backend runs with SERVER_PORT=18080).
// Override with VITE_BACKEND_URL, e.g. VITE_BACKEND_URL=http://localhost:8080 pnpm dev
const backend = process.env.VITE_BACKEND_URL ?? "http://localhost:18080";

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    babel({ presets: [reactCompilerPreset()] }),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": backend,
      "/aigc": backend,
    },
  },
});
