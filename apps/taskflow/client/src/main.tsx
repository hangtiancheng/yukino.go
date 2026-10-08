import { createRoot } from "@yukino.js/lit-jsx";
import { init, enablePlugin } from "@yukino.js/sentry";
import { PerformancePlugin } from "@yukino.js/sentry/plugins";
import "./index.css";
import "@/components/app-shell";

// Report endpoint comes from the environment; empty falls back to the dev mock
// / same-origin proxy path (see vite.config.ts sentryPlugin dsn).
const sentryDsn = import.meta.env.VITE_SENTRY_DSN || "/api/v1/telemetry/log";

init({
  dsn: sentryDsn,
  projectId: "taskflow-web",
  // Declarative instrumentation: only elements carrying yukino-sentry-* are
  // reported (see the view/ev/msg attributes across the pages).
  enableClick: true,
  enableHistory: true,
  enableHashChange: true,
  enableWhiteScreen: true,
  enableHttpPerformance: true,
  rootCssSelectors: ["html", "body", "#app"],
  clickThrottleDelay: 200,
  tracesSampleRate: 1,
});
enablePlugin(new PerformancePlugin());

const container = document.getElementById("app");
if (container) {
  createRoot(container).render(<app-shell />);
}
