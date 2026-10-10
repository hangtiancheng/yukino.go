import { defineConfig, loadEnv } from "vite";
import tailwindcss from "@tailwindcss/vite";
import { sentryPlugin } from "@yukino.js/sentry/vite";
import { fileURLToPath, URL } from "node:url";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");

  const serverTarget = env.VITE_SERVER_BASE_URL || "http://127.0.0.1:8090";

  const sentryDsn = env.VITE_SENTRY_DSN || "/api/v1/telemetry/log";

  const proxy = {
    "/api": {
      target: serverTarget,
      changeOrigin: true,
    },
  };

  return {
    plugins: [tailwindcss(), sentryPlugin({ dsn: sentryDsn })],
    resolve: {
      alias: {
        "@": fileURLToPath(new URL("./src", import.meta.url)),
      },
    },
    server: {
      port: 5173,
      proxy,
    },
    preview: {
      port: 4173,
      proxy,
    },
  };
});
