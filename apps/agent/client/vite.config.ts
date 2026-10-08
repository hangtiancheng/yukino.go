import { defineConfig, type ProxyOptions } from "vite";
import tailwindcss from "@tailwindcss/vite";

// The app talks to the Go server through same-origin /api paths, so both the
// dev server and `vite preview` forward them to the server.
const apiProxy: Record<string, ProxyOptions> = {
  "/api": {
    target: "http://localhost:8123",
    changeOrigin: true,
  },
};

// https://vite.dev/config/
export default defineConfig({
  plugins: [tailwindcss()],
  server: { proxy: apiProxy },
  preview: { proxy: apiProxy },
});
