import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react, { reactCompilerPreset } from "@vitejs/plugin-react";
import babel from "@rolldown/plugin-babel";
import { defineConfig } from "vite";

// 后端地址：默认 18080（本机 8080 常被 nginx 占用；后端用 SERVER_PORT=18080 启动）。
// 可用 VITE_BACKEND_URL 覆盖，如 VITE_BACKEND_URL=http://localhost:8080 pnpm dev
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
