import { defineConfig, type ProxyOptions } from "vite";
import tailwindcss from "@tailwindcss/vite";

const apiProxy: Record<string, ProxyOptions> = {
  "/api": {
    target: "http://localhost:8123",
    changeOrigin: true,
  },
};

export default defineConfig({
  plugins: [tailwindcss()],
  server: { proxy: apiProxy },
  preview: { proxy: apiProxy },
});
