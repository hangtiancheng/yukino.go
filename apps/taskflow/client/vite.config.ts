import { defineConfig, loadEnv } from "vite";
import tailwindcss from "@tailwindcss/vite";
import { sentryPlugin } from "@yukino.js/sentry/vite";
import { fileURLToPath, URL } from "node:url";

export default defineConfig(({ mode }) => {
  // VITE_* are also exposed to client code; the empty prefix ("") additionally
  // lets us read non-VITE dev-only vars here if needed.
  const env = loadEnv(mode, process.cwd(), "");

  // Server origin for the dev/preview proxy. When VITE_SERVER_BASE_URL is an
  // absolute origin it is used directly; otherwise the client calls same-origin
  // /api and this proxy forwards it to the local server.
  const serverTarget = env.VITE_SERVER_BASE_URL || "http://127.0.0.1:8090";

  // The dev-server mock endpoint must match the runtime dsn (see src/main.tsx).
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
