import { enablePlugin, init, isInitialized } from "@yukino.js/sentry";
import { ExposurePlugin, PerformancePlugin } from "@yukino.js/sentry/plugins";
import { startErrorSeeder } from "./crash/seeder.js";

const MAX_EVENT_BYTES = 50 * 1024;

export function setupSentry() {
  if (isInitialized()) return;
  init({
    dsn: "/api/log",
    projectId: "yukino-agent-fe2",
    beforeSendBatch: (eventList) =>
      eventList.filter(
        (item) => JSON.stringify(item).length <= MAX_EVENT_BYTES,
      ),
  });
  enablePlugin(new PerformancePlugin(), new ExposurePlugin());

  if (import.meta.env.DEV) {
    startErrorSeeder();
  }
}
