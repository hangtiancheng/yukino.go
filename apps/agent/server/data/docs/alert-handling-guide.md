# ServiceOffline

Alert Explanation: a Prometheus target is unreachable, so `up == 0`. Labels: `severity=critical`, `service` (the job name). Common causes: the process was never started, it crashed or panicked, the port is occupied by something else, or a firewall blocks the scrape port.

Resolution:

1. Query `up{job="<service-job>"}`. A value of 0 confirms the target is unreachable.
2. Open http://localhost:9090/targets and read the `lastError` field for the target. "connection refused" means nothing is listening on the port.
3. Verify the process is alive: `lsof -i :<port>` or `ps aux | grep <process-name>`.
4. Search logs from the last 1 hour for the keyword "panic" to find a crash cause.
5. Log topic region: ap-guangzhou; log topic ID: 869830db-a055-4479-963b-3c898d27e755.
6. If the process exited without a panic, look at `process_resident_memory_bytes` and `rate(process_cpu_seconds_total[5m])` just before it disappeared, plus `yukino_go_heap_used_ratio` for an out-of-memory abort.
7. After restarting, `up{job="<job-name>"}` returns to 1 within one scrape interval (15s).

Known targets and their scrape endpoints:

- prometheus: http://localhost:9090/metrics (job=prometheus)
- yukino-agent: http://127.0.0.1:8123/api/metrics (job=yukino-agent)
- milvus: http://localhost:9091/metrics (job=milvus)

# GoGoroutineLeak

Alert Explanation: `go_goroutines > 500 and deriv(go_goroutines[30m]) > 0` for 15 minutes on the Go server (job `yukino-agent-go`). Goroutine count is high and still climbing, which is the Go equivalent of a memory leak. Labels: `severity=warning`, `service`.

Resolution:

1. Confirm the trend rather than a spike: graph `go_goroutines{job="yukino-agent-go"}` over 6 hours. A healthy service returns to a baseline between load peaks.
2. Break down what they are doing: `go_sched_goroutines_waiting_goroutines` versus `_running_` and `_runnable_`. A growing waiting count means goroutines blocked forever on a channel, mutex, or network read with no deadline.
3. Compare with creation rate: `rate(go_sched_goroutines_created_goroutines_total[5m])`. Creation without a matching decline means something spawns per request and never returns.
4. Leaked goroutines retain their stacks, so check `go_memory_classes_heap_stacks_bytes` climbing alongside.
5. Usual causes here: a request context that is never cancelled, an unbuffered channel with no reader, or an eino pipeline whose stream is never drained to completion.

# GoSchedulerLatencyHigh

Alert Explanation: p99 of `go_sched_latencies_seconds` exceeds 50ms for 5 minutes. This is the Go analogue of event loop lag: goroutines are runnable but not getting CPU. Labels: `severity=warning`, `service`.

Resolution:

1. Confirm the percentile: `histogram_quantile(0.99, sum by (le) (rate(go_sched_latencies_seconds_bucket[5m])))`, and compare with p50 to see whether it is a tail or the whole distribution.
2. Check CPU supply against demand: `rate(process_cpu_seconds_total{job="yukino-agent-go"}[5m])` against `go_sched_gomaxprocs_threads`. A rate approaching GOMAXPROCS means the process is genuinely out of CPU.
3. Rule out GC as the consumer with `GoGcCpuPressure`'s expression.
4. Check `go_sched_goroutines_runnable_goroutines`: a large runnable queue with idle CPU points at host-level contention instead, visible as `go_cpu_classes_idle_cpu_seconds_total` staying flat.
5. Look for CPU-bound work on the request path: embedding batches, markdown splitting during indexing, or large JSON encodes.

# GoStopTheWorldHigh

Alert Explanation: p99 of `go_sched_pauses_total_gc_seconds` exceeds 50ms for 5 minutes. Garbage collection halts every goroutine for that long, so all request latency inherits the pause. Labels: `severity=warning`, `service`.

Resolution:

1. Separate the phases: `go_sched_pauses_stopping_gc_seconds` measures how long it took to bring goroutines to a stop, `go_sched_pauses_total_gc_seconds` the whole pause. A large stopping component means goroutines were slow to reach a preemption point, typically a tight non-preemptible loop.
2. Check the live set: `go_gc_heap_live_bytes` and `go_gc_heap_objects_objects`. Pause cost scales with pointer-dense structures more than raw bytes.
3. Check scan volume: `go_gc_scan_heap_bytes` versus `go_gc_scan_stack_bytes` and `go_gc_scan_globals_bytes`.
4. Check cycle frequency: `rate(go_gc_cycles_total_gc_cycles_total[5m])`, and `rate(go_gc_cycles_forced_gc_cycles_total[5m])` for explicit `runtime.GC()` calls that should not be in a hot path.
5. Large caches of pointer-heavy values (conversation memory, retrieved document chunks) are the usual driver; storing them as flat byte slices removes them from the scan set.

# GoGcCpuPressure

Alert Explanation: `rate(go_cpu_classes_gc_total_cpu_seconds_total[5m]) / rate(go_cpu_classes_total_cpu_seconds_total[5m]) > 0.25` for 10 minutes. More than a quarter of CPU time goes to garbage collection. Labels: `severity=warning`, `service`.

Resolution:

1. Split the GC cost: `go_cpu_classes_gc_mark_assist_cpu_seconds_total` is the damning one, because assist time is charged to the goroutine doing the allocating, meaning allocation is outrunning the collector.
2. Check allocation churn: `rate(go_gc_heap_allocs_bytes_total[5m])` and `rate(go_gc_heap_allocs_objects_total[5m])`.
3. Check the heap goal: `go_gc_heap_goal_bytes` against `go_gc_heap_live_bytes`. A goal barely above the live set means GOGC or GOMEMLIMIT is forcing continuous collection.
4. Check whether the limiter engaged: `go_gc_limiter_last_enabled_gc_cycle` advancing means the runtime is actively throttling GC to avoid starving the program.
5. Typical fixes: reuse buffers with `sync.Pool`, preallocate slices with a known capacity, and avoid per-request JSON round-trips.

# GoHeapNearLimit

Alert Explanation: `yukino_go_heap_used_ratio > 0.9` for 5 minutes, that is the live heap sitting above 90% of GOMEMLIMIT. The collector will thrash and then the process is OOM-killed. Labels: `severity=critical`, `service`.

Resolution:

1. This metric reads 0 when GOMEMLIMIT is unset, so a firing alert means a limit is configured. Read it with `yukino_go_memory_limit_bytes`.
2. Compare `go_gc_heap_live_bytes` with `go_memory_classes_total_bytes`. The latter includes stacks, metadata and OS-held memory, and it is what the limit actually governs.
3. Check what is not heap objects: `go_memory_classes_heap_stacks_bytes` (goroutine leak), `go_memory_classes_metadata_other_bytes`, and `go_memory_classes_os_stacks_bytes`.
4. Check whether memory is being returned: `go_memory_classes_heap_released_bytes` and `rate(go_cpu_classes_scavenge_total_cpu_seconds_total[5m])`.
5. Either reduce retention (bound the conversation memory LRU, cap retrieved chunk sizes) or raise GOMEMLIMIT. Raising GOGC will not help once the limit dominates.

# GoMutexContention

Alert Explanation: `rate(go_sync_mutex_wait_total_seconds_total[5m]) > 1` for 10 minutes. Goroutines lose more than one second per wall-clock second waiting for mutex handoff, so a shared lock is serialising the service. Labels: `severity=warning`, `service`.

Resolution:

1. A rate above 1 means multiple goroutines are blocked simultaneously; compare against `go_goroutines` to gauge how much of the service is stalled.
2. Correlate with `go_sched_latencies_seconds` p99, since lock convoys show up there too.
3. Audit the shared locks on the request path. In this service the candidates are the conversation memory mutex in `internal/utility/mem` (one process-wide `sync.Mutex` guards the session map and its LRU list) and the `boundedLabel` mutex in the metrics bridge.
4. A global lock held across an I/O call is the classic cause; move Redis, MySQL and model calls outside the critical section.
5. Sharding the map or switching to per-session locks removes the convoy without changing semantics.

# GoThreadGrowth

Alert Explanation: `go_threads > 100` for 15 minutes. The Go runtime only creates OS threads when goroutines block in syscalls or cgo, so a high count means blocking work is escaping the scheduler. Labels: `severity=warning`, `service`.

Resolution:

1. Compare with `go_sched_gomaxprocs_threads`. A thread count many multiples of GOMAXPROCS means threads are parked in syscalls, not running Go code.
2. Check `go_sched_goroutines_not_in_go_goroutines`, which counts goroutines currently outside Go code, i.e. in a syscall or cgo call.
3. Check cgo traffic: `rate(go_cgo_go_to_c_calls_calls_total[5m])`. A cgo call that blocks pins its thread for the whole duration.
4. Check filesystem work, since document loading and indexing use blocking reads: correlate with knowledge-index activity.
5. Threads are never reclaimed once created, so the count is a high-water mark; a flat elevated line after a burst is expected and only sustained growth is a problem.

# SentryReportPipelineRejecting

Alert Explanation: `rate(yukino_sentry_report_batches_total{outcome="invalid"}[10m]) > 0` for 10 minutes. `POST /api/log` is returning 400 for browser report batches, so telemetry is being dropped at the door. Labels: `severity=warning`, `service=yukino-sentry`.

Resolution:

1. Compare the two outcomes: `sum by (outcome) (rate(yukino_sentry_report_batches_total[10m]))`. If `accepted` is 0, nothing is getting through at all.
2. The two rejection paths in `app/api/log/route.ts` are a `JSON.parse` failure and a `reportBatchSchema.safeParse` failure. The schema requires an array of objects each carrying a string `type`.
3. Confirm the SDK contract still holds: `@yukino.js/sentry` posts a JSON array of `IReportData` via `sendBeacon` (Content-Type text/plain) or `fetch` with `keepalive`.
4. Check whether an unrelated client is posting to the dsn path, which would explain invalid batches alongside healthy accepted ones.
5. Reproduce locally with `npx tsx scripts/metrics-smoke.ts`, which feeds one synthetic report per event type through the same schema.

# SentryTelemetryStalled

Alert Explanation: `time() - max by (project_id) (yukino_sentry_event_last_seen_timestamp_seconds) > 1800`. The newest browser event for a project is more than 30 minutes old. Either nobody is using the app or the reporting path is broken. Labels: `severity=warning`, `service=yukino-sentry`.

Resolution:

1. Check which event types stopped: `time() - yukino_sentry_event_last_seen_timestamp_seconds`. PV events arrive on every page load, so a stale PV timestamp with fresh Performance events is contradictory and indicates partial loss.
2. Confirm the target is still up: `up{job="yukino-agent"}`.
3. Confirm the endpoint answers: `GET /api/metrics` should return the exposition, and `POST /api/log` should return `{ "message": "OK" }`.
4. Check `yukino_sentry_report_batches_total{outcome="invalid"}` for silent rejection.
5. In the browser, the SDK drops oversized events client-side. `components/sentry-provider.tsx` filters events above 50KB to protect the 64KB fetch-keepalive body limit, so a single huge event will not wedge the queue but will never arrive either.
6. Remember counters reset when the Node process restarts; a fresh process legitimately has no events until the first report.

# ClientReactCrash

Alert Explanation: `increase(yukino_sentry_errors_total{type="React"}[5m]) > 0`. A React render-phase error was caught by `ReactErrorBoundary` and reported as an `EventType.React` event, which means users saw a fallback instead of the page. Labels: `severity=critical`, `service=yukino-sentry`, plus `name` (the error constructor name) and `project_id`.

Resolution:

1. Identify the error: `sum by (name, project_id) (increase(yukino_sentry_errors_total{type="React"}[1h]))`.
2. React render errors are invisible to the global `window "error"` listener; only an error boundary observes them. The reported payload `extra` carries `{ error, stack, context }` where `context` is React's `ErrorInfo` with the component stack.
3. Read the component stack from the raw report to locate the failing component. In development, the Vite and webpack dev-server plugins resolve stack frames back to original sources and write them to `logs/sentry_*.jsonl` under a `sourcemap.frames` field.
4. Note that `crash/index.tsx` (`RandomCrash`) intentionally throws roughly every 20 seconds with 4% probability to seed this event type. Errors named "Seeded React render crash: probe component exploded" are that probe, not a real defect.
5. Check for a simultaneous `ClientErrorSpike` or `ClientWhiteScreen`, which would mean the crash is taking the whole page down rather than one subtree.

# ClientErrorSpike

Alert Explanation: `sum by (project_id) (rate(yukino_sentry_errors_total[5m])) > 0.2` for 5 minutes, that is more than 12 browser errors per minute aggregated across the `Error`, `React`, `Vue` and `OtherFrameworks` event types. Labels: `severity=warning`, `service=yukino-sentry`.

Resolution:

1. Break the spike down by type and name: `sum by (type, name) (rate(yukino_sentry_errors_total[5m]))`. For code errors the `name` label holds the script URL; for framework errors it holds the error constructor name.
2. Check `yukino_sentry_batch_error_groups_total`. The SDK collapses 5 or more identical errors within a 2 second window into one batched report, and `yukino_sentry_errors_total` already counts every event inside the group, so a high group count means a tight repeating loop.
3. Remember the SDK deduplicates code errors by a composite key of type, message, filename, line and column unless `repeatCodeError` is enabled. A sustained rate therefore means genuinely distinct errors or errors with unknown source filenames, which bypass deduplication.
4. Correlate with a deploy. Errors named after hashed chunk URLs usually mean clients are running a stale bundle against a new server.
5. Check `yukino_sentry_resource_errors_total` at the same time; failed chunk loads and runtime errors often share a root cause.

# ClientWhiteScreen

Alert Explanation: `increase(yukino_sentry_white_screens_total[10m]) > 0`. The SDK sampler found the viewport still resolving to root elements after the page finished loading, so users are looking at a blank page. Labels: `severity=critical`, `service=yukino-sentry`.

Resolution:

1. Understand the detector before trusting it: the SDK samples 18 points (9 on the horizontal centre line, 9 on the vertical centre line) with `document.elementFromPoint`, and declares a white screen when all 18 resolve to a configured root selector or to nothing. It samples every 1000ms up to 10 times after `load`.
2. Confirm `rootCssSelectors` matches the app. The default is `["html", "body", "#app", "#root"]`; an app that mounts into a different root element under a wrapper can be misdetected.
3. If the app renders a skeleton, `hasSkeleton` must be enabled, otherwise the skeleton counts as content and real white screens are missed.
4. Check `yukino_sentry_errors_total` and `yukino_sentry_resource_errors_total` in the same window. A white screen is almost always downstream of a failed bundle load or a top-level render crash.
5. Check `yukino_sentry_web_vitals{name="FCP"}` and `{name="FSP"}`; a genuine white screen has no first contentful paint.

# ClientResourceLoadFailure

Alert Explanation: `increase(yukino_sentry_resource_errors_total[10m]) > 5` for 5 minutes. Static resources are failing to load. The `tag` label is the failing element type (`img`, `script`, `link`). Labels: `severity=warning`, `service=yukino-sentry`.

Resolution:

1. Break down by element type: `sum by (tag, project_id) (increase(yukino_sentry_resource_errors_total[1h]))`. Failing `script` or `link` elements are far more serious than `img`.
2. The reported payload carries `src` (for `img` and `script`) or `href` (for `link`) with the failing URL, and the message reads `Failed to load <tag>: <url>`. Read the raw report to get the URL, since it is deliberately not a metric label.
3. Correlate with a deploy: stale asset hashes after a release are the most common cause, and they resolve themselves as clients reload.
4. Rule out a CDN or origin problem by requesting the failing URL directly.
5. Check `yukino_sentry_resource_entries_total{from_cache="false"}` and `yukino_sentry_resource_duration_ms` for elevated latency on the same initiator type, which suggests the origin is degraded rather than the asset being gone.

# HighInterfaceFailureRate

Alert Explanation: more than 10% of browser XHR/fetch requests returned 4xx or 5xx over 5 minutes, computed as the ratio of `yukino_sentry_http_requests_total{status="Error"}` to all requests. Labels: `severity=warning`, `service=yukino-sentry`.

Resolution:

1. Break down by status code: `sum by (status_code, method) (rate(yukino_sentry_http_requests_total{status="Error"}[5m]))`. The SDK classifies 1xx, 2xx and 3xx as OK and everything else as Error, and a `status_code` of 0 means the request never completed (network failure or CORS rejection).
2. Note the denominator caveat: by default the SDK reports only failed requests. Successful requests appear only when `enableHttpPerformance` is on, in which case they arrive as `Performance` events named `HTTP <METHOD>` and are folded into the same counter with `status="OK"`. If the app has that option off, this ratio is close to 1 whenever any error occurs, so read the absolute error rate as well.
3. Search logs from the last 1 hour using the interface name and the keyword "response".
4. Check request latency: `histogram_quantile(0.99, sum by (le) (rate(yukino_sentry_http_request_duration_ms_bucket[5m])))` to find endpoints that are timing out. Buckets: 50, 100, 300, 500, 1000, 3000, 10000 ms.
5. Isolate server errors: `histogram_quantile(0.95, sum by (le) (rate(yukino_sentry_http_request_duration_ms_bucket{status_code=~"5.."}[5m])))`.
6. If a downstream dependency is suspected, verify its target health with `up{job="<downstream-job>"}` and check server-side event loop lag.

# ClientHttpLatencyHigh

Alert Explanation: the 95th percentile of browser-observed HTTP duration has exceeded 3000ms for 10 minutes. Labels: `severity=warning`, `service=yukino-sentry`.

Resolution:

1. Find the slow shape: `histogram_quantile(0.95, sum by (le, method, status_code) (rate(yukino_sentry_http_request_duration_ms_bucket[10m])))`.
2. This duration is measured in the browser, so it includes network time. Compare with server-side signals (`go_sched_latencies_seconds`, `rate(process_cpu_seconds_total[5m])`) to decide whether the server or the network is slow.
3. Check `yukino_sentry_web_vitals{name="TTFB"}`. High TTFB alongside high request latency points at the server; normal TTFB points at a specific slow endpoint.
4. Remember the endpoint URL is not a label, by design. Read `payload.api` from the raw reports in `logs/sentry_*.jsonl` to identify the endpoint.
5. Requests to the dsn path itself are excluded from reporting, so this never measures the telemetry pipeline.

# WebVitalsDegradation

Alert Explanation: more than 25% of samples for a given web vital landed in the `poor` band over 15 minutes, computed from `yukino_sentry_web_vital_samples_total`. Labels: `severity=warning`, `service=yukino-sentry`, plus `name` and `project_id`.

Resolution:

1. See the current values: `yukino_sentry_web_vitals{rating="poor"}` holds the latest value per vital, and `yukino_sentry_web_vitals{name="LCP"}` tracks one metric over time. Use the samples counter, not the gauge, for anything rate-based.
2. Rating thresholds follow web-vitals conventions: LCP good < 2500ms and needs-improvement < 4000ms; FCP good < 1800ms; CLS good < 0.1; INP good < 200ms; TTFB good < 800ms.
3. Units differ per metric in the same gauge: LCP, FCP, INP, TTFB and FSP are milliseconds, CLS is a unitless layout-shift score. Never aggregate across `name`.
4. High TTFB usually means server slowness; check event loop lag and downstream response times.
5. High LCP or FCP with normal TTFB suggests a client rendering problem: oversized bundle, unoptimized images, or blocking scripts. Cross-check `yukino_sentry_long_tasks_total` and `yukino_sentry_navigation_timing_ms{phase="domProcessing"}`.
6. FSP (First Screen Paint) is a custom SDK metric computed with a `MutationObserver`, not a web-vitals library metric, so it has no rating and appears with `rating="none"`.
7. Check `rate(yukino_sentry_errors_total[5m])` for a simultaneous spike in client errors.

# ClientNavigationSlow

Alert Explanation: the 95th percentile of the `loadEvent` navigation phase has exceeded 5000ms for 15 minutes. Labels: `severity=warning`, `service=yukino-sentry`.

Resolution:

1. Locate the slow phase: `histogram_quantile(0.95, sum by (le, phase) (rate(yukino_sentry_navigation_timing_ms_bucket[15m])))`.
2. Phases are measured from `fetchStart` unless noted, and come from `PerformanceNavigationTiming`: `redirect`, `unloadTime`, `dnsLookup`, `tcpConnection`, `tlsHandshake`, `timeToFirstByte`, `firstByte`, `contentTransfer`, `domProcessing`, `domInteractive`, `domContentLoaded`, `loadEvent`, `resourceLoad`, `paintTime`.
3. Attribute the time: high `dnsLookup`, `tcpConnection` or `tlsHandshake` is network setup; high `timeToFirstByte` is the server; high `domProcessing` is parsing and script execution; high `resourceLoad` is subresources after DOMContentLoaded.
4. If `resourceLoad` dominates, drill into `yukino_sentry_resource_duration_ms` by `initiator_type` and check `yukino_sentry_resource_transfer_bytes` for oversized assets.
5. If `domProcessing` dominates, check `yukino_sentry_long_tasks_total` for main-thread blocking during startup.
6. Correlate with `yukino_sentry_web_vitals{name="LCP"}`, since a slow load almost always degrades LCP too.

# ClientLongTaskPressure

Alert Explanation: `sum by (project_id) (rate(yukino_sentry_long_tasks_total[10m])) > 1` for 10 minutes, that is more than one main-thread long task per second. Long tasks block input handling, so users perceive the UI as frozen. Labels: `severity=warning`, `service=yukino-sentry`.

Resolution:

1. Check severity, not just count: `histogram_quantile(0.95, sum by (le) (rate(yukino_sentry_long_task_duration_ms_bucket[10m])))`. The browser reports any task over 50ms; tasks over 500ms are what users notice.
2. Confirm the user impact through `yukino_sentry_web_vitals{name="INP"}`, which measures interaction responsiveness directly.
3. Long tasks are collected by a `PerformanceObserver` on the `longtask` entry type, which is Chromium-only, so absence of data does not mean absence of blocking.
4. Common causes in this app: large markdown and Shiki highlighting passes during streaming, and synchronous work in the message processor.
5. Cross-check `yukino_sentry_browser_memory_bytes`; heavy retained memory makes browser GC pauses show up as long tasks.

# ClientMemoryHigh

Alert Explanation: `yukino_sentry_browser_memory_bytes > 1.5e9` for 10 minutes. A browser tab holds more than 1.5GB, which risks a renderer out-of-memory kill on lower-memory devices. Labels: `severity=warning`, `service=yukino-sentry`.

Resolution:

1. Break down the allocation: `yukino_sentry_browser_memory_breakdown_bytes` carries a `kind` label built from the reported allocation types (for example `JavaScript`, `DOM`).
2. This metric comes from `performance.measureUserAgentSpecificMemory()`, which is Chromium-only and is sampled once per page load by the PerformancePlugin, so it is a snapshot rather than a trend.
3. Growth in the `JavaScript` kind points at retained application state: chat histories in localStorage-backed React state, or accumulated stream chunks.
4. Growth in the `DOM` kind points at unbounded node creation, for example an ever-growing message list without virtualization.
5. Note that `reactStrictMode` is off in this app, so development double-mount never runs; verify component cleanup manually when auditing retention.

# Reconciliation Discrepancy with Downstream

Alert Explanation: a reconciliation discrepancy with downstream may be caused by data synchronization anomalies or calculation errors.

Resolution:

1. Search logs from the last 1 hour using the keywords "error" and "reconciliation".
2. Analyze the log content to determine the cause of the reconciliation discrepancy.

# Service Region Mismatch with Resource Region

Problem Explanation: In billing data processing, we found that some services used incorrect MQ queues, causing resource events to be delivered to the wrong region, resulting in a mismatch between the resource region and the billing service region.

Resolution:

1. Search logs from the last 1 hour using the keyword "region mismatch".
2. Based on the log content, aggregate the callers and incorrect region names.

# Service Error Codes and Common Causes

- 12000000001: Invalid API call parameters (e.g., type mismatch)
- 12000000002: Database update failed (database issue, recommend checking logs)
- 12000000003: Downstream API error (downstream API returned an error)
- 12000000004: Instance not found (upstream passed an incorrect instance ID)

# Prometheus Infrastructure Reference

One Prometheus instance scrapes the agent server. Homebrew config: `/opt/homebrew/etc/prometheus.yml`, alert rules `/opt/homebrew/etc/prometheus.rules.yml`.

The bridge lives in `internal/app/sentry_metrics_handler.go`, job `yukino-agent`, `http://127.0.0.1:8123/api/metrics`. It exposes the `yukino_sentry_*` metric families fed by the browser SDK, plus the Go runtime metrics `yukino_go_*` and `go_*`.

`prometheus.rules.yml` exists in two places that must stay in sync: the repository copy (which docker-compose mounts) and the Homebrew path that the running instance actually loads.

Scrape interval and rule evaluation interval: 15s for all jobs.

Metrics pipeline: yukino-sentry browser SDK -> POST /api/log (batch JSON array of IReportData) -> bridge -> GET /api/metrics -> Prometheus scrape.

Verification:

- Go: `go test ./internal/app/` covers the event matrix, the per-event values, and malformed payloads.
- Rules: `promtool check rules prometheus.rules.yml`.

Prometheus is started without `--web.enable-lifecycle`, so rule changes need `brew services restart prometheus` rather than a POST to `/-/reload`.

# Metric Reference: Event Coverage

Every yukino-sentry report type except ScreenRecord has a dedicated metric. `History` and `Event hashchange` never reach the reporter as their own events; they become breadcrumbs and PV events. `Event unhandledrejection` is re-dispatched as an `Error` event. All browser metrics carry a `project_id` label.

- `yukino_sentry_events_total` (Counter; type, status, project_id): every event, including ScreenRecord.
- `yukino_sentry_event_last_seen_timestamp_seconds` (Gauge; type, project_id): freshness per event type.
- `yukino_sentry_report_batches_total` (Counter; outcome=accepted|invalid) and `yukino_sentry_report_batch_size` (Histogram): ingest health at /api/log.

Type values seen in `yukino_sentry_events_total`: XMLHttpRequest, fetch, Error, React, Vue, OtherFrameworks, Resource, Performance, Click, Exposure, WhiteScreen, PV, Custom, ScreenRecord.

# Metric Reference: Errors and Resources

- `yukino_sentry_errors_total` (Counter; type, name, project_id): Error, React, Vue and OtherFrameworks events. A batched group increments this by `batchErrorLength`, so the counter reflects true error volume rather than report volume. For code errors `name` is the script URL; for framework errors it is the error constructor name.
- `yukino_sentry_batch_error_groups_total` (Counter; type, name, project_id): bursts the SDK collapsed into one report (5 or more identical errors within 2s).
- `yukino_sentry_resource_errors_total` (Counter; tag, project_id): static resource load failures by element tag (img, script, link).
- `yukino_sentry_white_screens_total` (Counter; project_id): white-screen detections.

High-cardinality browser strings (error names, click ids, custom event names, initiator types, page view names) are capped at 50 distinct values per label; anything beyond that is recorded as `other`, and empty values as `unknown`.

# Metric Reference: HTTP and Performance

- `yukino_sentry_http_requests_total` (Counter; method, status_code, status, project_id): XHR/fetch requests. Successes only appear when the SDK runs with `enableHttpPerformance`.
- `yukino_sentry_http_request_duration_ms` (Histogram; method, status_code, project_id): buckets 50, 100, 300, 500, 1000, 3000, 10000.
- `yukino_sentry_web_vitals` (Gauge; name, rating, project_id): latest value. Names LCP, FCP, CLS, INP, TTFB, FSP. Ratings good, needs-improvement, poor, and none for FSP.
- `yukino_sentry_web_vital_samples_total` (Counter; name, rating, project_id): use this for rate-based alerts, not the gauge.
- `yukino_sentry_navigation_timing_ms` (Histogram; phase, project_id): 14 navigation phases.
- `yukino_sentry_resource_entries_total`, `yukino_sentry_resource_duration_ms` (initiator_type, from_cache, project_id) and `yukino_sentry_resource_transfer_bytes` (initiator_type, project_id): static resource timing, fed by both single ResourceTiming events and the initial ResourceList.
- `yukino_sentry_long_tasks_total` and `yukino_sentry_long_task_duration_ms` (project_id): main-thread long tasks.
- `yukino_sentry_browser_memory_bytes` and `yukino_sentry_browser_memory_breakdown_bytes` (kind, project_id): Chromium tab memory.
- `yukino_sentry_performance_value` (Gauge; name, project_id): Performance events that are not web vitals, such as `tracePerformance` metrics.

# Metric Reference: Interaction and Navigation

- `yukino_sentry_clicks_total` (Counter; ev, project_id): declarative clicks, keyed by the resolved `yukino-sentry-ev` identifier. Plain clicks without a tracking attribute are never reported.
- `yukino_sentry_exposures_total` and `yukino_sentry_exposure_duration_ms` (project_id): element exposure, emitted when an observed element leaves the viewport after being visible.
- `yukino_sentry_page_views_total` (Counter; name, project_id): name is PageLoad, HistoryChange, HashChange, ManualPageView or PageDwell.
- `yukino_sentry_page_dwell_ms` (Histogram; project_id): dwell time before navigating away. The SDK discards dwells of 100ms or less.
- `yukino_sentry_custom_events_total` (Counter; name, project_id): `traceCustomEvent` business events.

# Metric Reference: Go Runtime

Defined in `internal/app/sentry_metrics_handler.go` for the Go server (job `yukino-agent-go`). The bridge opts the GoCollector into `runtime/metrics` for the GC, memory, scheduler, CPU-class, sync and cgo families; `/godebug/*` is excluded as 50-odd always-zero series.

Derived gauges the collectors do not provide:

- `yukino_go_memory_limit_bytes`: GOMEMLIMIT, or 0 when no limit is configured.
- `yukino_go_heap_used_ratio`: live heap divided by GOMEMLIMIT, or 0 when unset. The primary OOM early warning.

Key runtime families:

- Scheduler: `go_sched_latencies_seconds` (histogram), `go_sched_pauses_total_gc_seconds` and `go_sched_pauses_stopping_gc_seconds`, `go_sched_goroutines_goroutines` with `_running_`/`_runnable_`/`_waiting_`/`_not_in_go_` variants, `go_sched_goroutines_created_goroutines_total`, `go_sched_gomaxprocs_threads`, `go_sched_threads_total_threads`.
- GC: `go_gc_pauses_seconds`, `go_gc_heap_live_bytes`, `go_gc_heap_goal_bytes`, `go_gc_heap_objects_objects`, `go_gc_heap_allocs_bytes_total`, `go_gc_heap_frees_bytes_total`, `go_gc_cycles_total_gc_cycles_total`, `go_gc_cycles_forced_gc_cycles_total`, `go_gc_scan_heap_bytes`, `go_gc_scan_stack_bytes`, `go_gc_limiter_last_enabled_gc_cycle`, `go_gc_gogc_percent`, `go_gc_gomemlimit_bytes`.
- Memory classes: `go_memory_classes_total_bytes`, `go_memory_classes_heap_objects_bytes`, `_heap_free_`, `_heap_released_`, `_heap_unused_`, `_heap_stacks_`, `_os_stacks_`, `_metadata_mspan_inuse_`, `_metadata_mcache_inuse_`, `_metadata_other_`, `_profiling_buckets_`, `_other_`.
- CPU classes: `go_cpu_classes_total_cpu_seconds_total`, `_gc_total_`, `_gc_mark_assist_`, `_gc_mark_dedicated_`, `_gc_pause_`, `_scavenge_total_`, `_idle_`, `_user_`. The GC share of CPU is `rate(go_cpu_classes_gc_total_cpu_seconds_total[5m]) / rate(go_cpu_classes_total_cpu_seconds_total[5m])`.
- Contention and cgo: `go_sync_mutex_wait_total_seconds_total`, `go_cgo_go_to_c_calls_calls_total`.
- Also present from the standard collectors: `go_goroutines`, `go_threads`, `go_gc_duration_seconds`, the `go_memstats_*` family, and the `process_*` family.

A leak shows up as `go_goroutines` plus `go_memory_classes_heap_stacks_bytes` growing together.

# Useful PromQL Queries for Triage

- All targets status: `up`
- Target down duration: `time() - timestamp(up == 0)`
- Server memory: `process_resident_memory_bytes`
- Go goroutines and growth: `go_goroutines` and `deriv(go_goroutines[30m])`
- Go heap growth projection: `predict_linear(yukino_go_heap_used_ratio[30m], 3600)`
- Go leak fingerprint: `go_goroutines` rising together with `go_memory_classes_heap_stacks_bytes`
- Go scheduler latency p99: `histogram_quantile(0.99, sum by (le) (rate(go_sched_latencies_seconds_bucket[5m])))`
- Go GC CPU share: `rate(go_cpu_classes_gc_total_cpu_seconds_total[5m]) / rate(go_cpu_classes_total_cpu_seconds_total[5m])`
- Go stop-the-world p99: `histogram_quantile(0.99, sum by (le) (rate(go_sched_pauses_total_gc_seconds_bucket[5m])))`
- Go heap against its limit: `yukino_go_heap_used_ratio` and `yukino_go_memory_limit_bytes`
- Go mutex contention: `rate(go_sync_mutex_wait_total_seconds_total[5m])`
- Go threads versus parallelism: `go_threads / go_sched_gomaxprocs_threads`
- CPU saturation: `rate(process_cpu_seconds_total[5m])`
- Browser error rate by type: `sum by (type) (rate(yukino_sentry_errors_total[5m]))`
- React crashes in the last hour: `increase(yukino_sentry_errors_total{type="React"}[1h])`
- Browser HTTP error ratio: `sum(rate(yukino_sentry_http_requests_total{status="Error"}[5m])) / sum(rate(yukino_sentry_http_requests_total[5m]))`
- P99 browser request latency: `histogram_quantile(0.99, sum by (le) (rate(yukino_sentry_http_request_duration_ms_bucket[5m])))`
- Poor web vital share: `sum by (name) (rate(yukino_sentry_web_vital_samples_total{rating="poor"}[15m])) / sum by (name) (rate(yukino_sentry_web_vital_samples_total[15m]))`
- Slowest navigation phase: `topk(3, histogram_quantile(0.95, sum by (le, phase) (rate(yukino_sentry_navigation_timing_ms_bucket[15m]))))`
- Telemetry freshness: `time() - yukino_sentry_event_last_seen_timestamp_seconds`
- Events by type breakdown: `sum by (type) (rate(yukino_sentry_events_total[5m]))`
