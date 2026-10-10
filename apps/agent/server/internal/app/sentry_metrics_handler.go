package app

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hangtiancheng/yukino.go/libs/yukino_http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/common/expfmt"
)

var (
	sentryRegistry = prometheus.NewRegistry()

	sentryEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_events_total",
			Help: "Events reported by the yukino-sentry browser SDK, by type and status.",
		},
		[]string{"type", "status", "project_id"},
	)

	sentryEventLastSeen = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yukino_sentry_event_last_seen_timestamp_seconds",
			Help: "Unix timestamp of the most recent SDK event of each type.",
		},
		[]string{"type", "project_id"},
	)

	sentryErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_errors_total",
			Help: "Browser errors (Error/React/Vue/OtherFrameworks), counting each event inside a batched group.",
		},
		[]string{"type", "name", "project_id"},
	)

	sentryBatchErrorGroupsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_batch_error_groups_total",
			Help: "Error bursts the SDK collapsed into a single batched report (5+ identical errors within 2s).",
		},
		[]string{"type", "name", "project_id"},
	)

	sentryResourceErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_resource_errors_total",
			Help: "Static resource load failures, by failing element tag (img/script/link).",
		},
		[]string{"tag", "project_id"},
	)

	sentryHTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_http_requests_total",
			Help: "Browser-side XHR/fetch requests. Successes only appear when the SDK runs with enableHttpPerformance.",
		},
		[]string{"method", "status_code", "status", "project_id"},
	)

	sentryHTTPRequestDurationMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "yukino_sentry_http_request_duration_ms",
			Help:    "Browser-side HTTP request duration in milliseconds (XHR/fetch events).",
			Buckets: []float64{50, 100, 300, 500, 1000, 3000, 10000},
		},
		[]string{"method", "status_code", "project_id"},
	)

	sentryWebVitals = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yukino_sentry_web_vitals",
			Help: "Latest web vital value (LCP/FCP/INP/TTFB/FSP in ms, CLS unitless).",
		},
		[]string{"name", "rating", "project_id"},
	)

	sentryWebVitalSamplesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_web_vital_samples_total",
			Help: "Web vital samples by rating, for rate-based degradation alerts.",
		},
		[]string{"name", "rating", "project_id"},
	)

	sentryNavigationTimingMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "yukino_sentry_navigation_timing_ms",
			Help:    "Page navigation timing phases in milliseconds.",
			Buckets: []float64{10, 50, 100, 300, 500, 1000, 2000, 5000, 10000, 30000},
		},
		[]string{"phase", "project_id"},
	)

	sentryResourceEntriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_resource_entries_total",
			Help: "Static resource timing entries, by initiator type and cache hit.",
		},
		[]string{"initiator_type", "from_cache", "project_id"},
	)

	sentryResourceDurationMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "yukino_sentry_resource_duration_ms",
			Help:    "Static resource load duration in milliseconds.",
			Buckets: []float64{10, 50, 100, 300, 500, 1000, 3000, 10000},
		},
		[]string{"initiator_type", "from_cache", "project_id"},
	)

	sentryResourceTransferBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "yukino_sentry_resource_transfer_bytes",
			Help:    "Static resource transfer size in bytes (0 for cache hits).",
			Buckets: []float64{1024, 10240, 51200, 102400, 512000, 1048576, 5242880},
		},
		[]string{"initiator_type", "project_id"},
	)

	sentryLongTasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_long_tasks_total",
			Help: "Main-thread long tasks observed in the browser.",
		},
		[]string{"project_id"},
	)

	sentryLongTaskDurationMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "yukino_sentry_long_task_duration_ms",
			Help:    "Main-thread long task duration in milliseconds.",
			Buckets: []float64{50, 100, 200, 500, 1000, 2000, 5000},
		},
		[]string{"project_id"},
	)

	sentryBrowserMemoryBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yukino_sentry_browser_memory_bytes",
			Help: "Browser tab memory from performance.measureUserAgentSpecificMemory (Chromium only).",
		},
		[]string{"project_id"},
	)

	sentryBrowserMemoryBreakdownBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yukino_sentry_browser_memory_breakdown_bytes",
			Help: "Browser tab memory breakdown by reported allocation type.",
		},
		[]string{"kind", "project_id"},
	)

	sentryClicksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_clicks_total",
			Help: "Declarative click events, by yukino-sentry-ev identifier.",
		},
		[]string{"ev", "project_id"},
	)

	sentryExposuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_exposures_total",
			Help: "Element exposure events completed (element left the viewport after being visible).",
		},
		[]string{"project_id"},
	)

	sentryExposureDurationMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "yukino_sentry_exposure_duration_ms",
			Help:    "Element visible duration in milliseconds.",
			Buckets: []float64{100, 500, 1000, 3000, 10000, 30000, 60000},
		},
		[]string{"project_id"},
	)

	sentryWhiteScreensTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_white_screens_total",
			Help: "White-screen detections reported by the SDK sampler.",
		},
		[]string{"project_id"},
	)

	sentryPageViewsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_page_views_total",
			Help: "Page view events, by lifecycle name (PageLoad/HistoryChange/HashChange/ManualPageView/PageDwell).",
		},
		[]string{"name", "project_id"},
	)

	sentryPageDwellMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "yukino_sentry_page_dwell_ms",
			Help:    "Time spent on a page before navigating away, in milliseconds.",
			Buckets: []float64{1000, 5000, 15000, 30000, 60000, 300000, 900000},
		},
		[]string{"project_id"},
	)

	sentryCustomEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_custom_events_total",
			Help: "Business events reported through traceCustomEvent.",
		},
		[]string{"name", "project_id"},
	)

	sentryPerformanceValue = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "yukino_sentry_performance_value",
			Help: "Latest value of Performance events that are not web vitals (e.g. tracePerformance metrics).",
		},
		[]string{"name", "project_id"},
	)

	sentryReportBatchesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "yukino_sentry_report_batches_total",
			Help: "Report batches received at /api/log, by validation outcome.",
		},
		[]string{"outcome"},
	)

	sentryReportBatchSize = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "yukino_sentry_report_batch_size",
			Help:    "Number of events per accepted report batch.",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100},
		},
	)
)

const noLimit = uint64(math.MaxInt64)

func init() {
	sentryRegistry.MustRegister(
		collectors.NewGoCollector(
			collectors.WithGoCollectorRuntimeMetrics(
				collectors.MetricsGC,
				collectors.MetricsMemory,
				collectors.MetricsScheduler,
				collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile(`^/cpu/classes/.*`)},
				collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile(`^/sync/.*`)},
				collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile(`^/cgo/.*`)},
			),
		),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewBuildInfoCollector(),

		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "yukino_go_memory_limit_bytes",
				Help: "GOMEMLIMIT soft memory limit in bytes; 0 when no limit is configured.",
			},
			func() float64 {
				limit, ok := readRuntimeUint64("/gc/gomemlimit:bytes")
				if !ok || limit == noLimit {
					return 0
				}
				return float64(limit)
			},
		),

		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "yukino_go_heap_used_ratio",
				Help: "Live heap divided by GOMEMLIMIT; 0 when GOMEMLIMIT is unset. 1.0 means an imminent OOM.",
			},
			func() float64 {
				limit, okLimit := readRuntimeUint64("/gc/gomemlimit:bytes")
				live, okLive := readRuntimeUint64("/gc/heap/live:bytes")
				if !okLimit || !okLive || limit == noLimit || limit == 0 {
					return 0
				}
				return float64(live) / float64(limit)
			},
		),

		sentryEventsTotal,
		sentryEventLastSeen,
		sentryErrorsTotal,
		sentryBatchErrorGroupsTotal,
		sentryResourceErrorsTotal,
		sentryHTTPRequestsTotal,
		sentryHTTPRequestDurationMs,
		sentryWebVitals,
		sentryWebVitalSamplesTotal,
		sentryNavigationTimingMs,
		sentryResourceEntriesTotal,
		sentryResourceDurationMs,
		sentryResourceTransferBytes,
		sentryLongTasksTotal,
		sentryLongTaskDurationMs,
		sentryBrowserMemoryBytes,
		sentryBrowserMemoryBreakdownBytes,
		sentryClicksTotal,
		sentryExposuresTotal,
		sentryExposureDurationMs,
		sentryWhiteScreensTotal,
		sentryPageViewsTotal,
		sentryPageDwellMs,
		sentryCustomEventsTotal,
		sentryPerformanceValue,
		sentryReportBatchesTotal,
		sentryReportBatchSize,
	)
}

func readRuntimeUint64(name string) (uint64, bool) {
	sample := []metrics.Sample{{Name: name}}
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindUint64 {
		return 0, false
	}
	return sample[0].Value.Uint64(), true
}

var webVitalNames = map[string]struct{}{
	"LCP": {}, "FCP": {}, "CLS": {}, "INP": {}, "TTFB": {}, "FSP": {},
}

var navigationPhases = []string{
	"paintTime",
	"domInteractive",
	"domContentLoaded",
	"loadEvent",
	"firstByte",
	"dnsLookup",
	"tcpConnection",
	"tlsHandshake",
	"timeToFirstByte",
	"contentTransfer",
	"domProcessing",
	"resourceLoad",
	"redirect",
	"unloadTime",
}

const maxLabelValues = 50

var labelValues = struct {
	sync.Mutex
	seen map[string]map[string]struct{}
}{seen: make(map[string]map[string]struct{})}

func boundedLabel(key, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	labelValues.Lock()
	defer labelValues.Unlock()
	seen, ok := labelValues.seen[key]
	if !ok {
		seen = make(map[string]struct{})
		labelValues.seen[key] = seen
	}
	if _, ok := seen[trimmed]; ok {
		return trimmed
	}
	if len(seen) >= maxLabelValues {
		return "other"
	}
	seen[trimmed] = struct{}{}
	return trimmed
}

type sentryReportItem struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	ProjectID string          `json:"projectId"`
	Payload   json.RawMessage `json:"payload"`
}

type sentryHTTPPayload struct {
	Method      string   `json:"method"`
	StatusCode  *float64 `json:"statusCode"`
	ElapsedTime *float64 `json:"elapsedTime"`
}

type sentryBatchErrorPayload struct {
	BatchError       bool     `json:"batchError"`
	BatchErrorLength *float64 `json:"batchErrorLength"`
}

type sentryResourceTiming struct {
	InitiatorType string   `json:"initiatorType"`
	Duration      *float64 `json:"duration"`
	TransferSize  *float64 `json:"transferSize"`
	FromCache     bool     `json:"fromCache"`
}

type sentryMemoryBreakdown struct {
	Bytes float64  `json:"bytes"`
	Types []string `json:"types"`
}

type sentryPerformancePayload struct {
	Value        *float64               `json:"value"`
	Rating       string                 `json:"rating"`
	Extra        json.RawMessage        `json:"extra"`
	ResourceList []sentryResourceTiming `json:"resourceList"`
	LongTasks    []struct {
		Duration *float64 `json:"duration"`
	} `json:"longTasks"`
	Memory *struct {
		Bytes     *float64                `json:"bytes"`
		Breakdown []sentryMemoryBreakdown `json:"breakdown"`
	} `json:"memory"`
}

type sentryExtraCarrier struct {
	Extra json.RawMessage `json:"extra"`
}

type sentryClickExtra struct {
	Ev  string `json:"ev"`
	Msg string `json:"msg"`
}

type sentryDurationExtra struct {
	Duration *float64 `json:"duration"`
}

type sentryResourceExtra struct {
	Resource sentryResourceTiming `json:"resource"`
}

func decodePayload(raw json.RawMessage, target any) bool {
	if len(raw) == 0 {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}

func readExtra(raw json.RawMessage) json.RawMessage {
	var carrier sentryExtraCarrier
	if !decodePayload(raw, &carrier) {
		return nil
	}
	return carrier.Extra
}

func recordSentryReportBatch(items []sentryReportItem) {
	sentryReportBatchesTotal.WithLabelValues("accepted").Inc()
	sentryReportBatchSize.Observe(float64(len(items)))
	for _, item := range items {
		recordSentryReportItem(item)
	}
}

func recordSentryReportItem(item sentryReportItem) {
	projectID := item.ProjectID
	if projectID == "" {
		projectID = "unknown"
	}
	status := item.Status
	if status == "" {
		status = "unknown"
	}

	sentryEventsTotal.WithLabelValues(item.Type, status, projectID).Inc()
	sentryEventLastSeen.WithLabelValues(item.Type, projectID).
		Set(float64(time.Now().UnixNano()) / float64(time.Second))

	switch item.Type {
	case "XMLHttpRequest", "fetch":
		recordSentryHTTPEvent(item, projectID, status)
	case "Error", "React", "Vue", "OtherFrameworks":
		recordSentryErrorEvent(item, projectID)
	case "Resource":
		sentryResourceErrorsTotal.
			WithLabelValues(boundedLabel("resource_tag", item.Name), projectID).Inc()
	case "Performance":
		recordSentryPerformanceEvent(item, projectID)
	case "Click":
		recordSentryClickEvent(item, projectID)
	case "Exposure":
		recordSentryExposureEvent(item, projectID)
	case "WhiteScreen":
		sentryWhiteScreensTotal.WithLabelValues(projectID).Inc()
	case "PV":
		recordSentryPageViewEvent(item, projectID)
	case "Custom":
		sentryCustomEventsTotal.
			WithLabelValues(boundedLabel("custom_event", item.Name), projectID).Inc()
	}
}

func recordSentryHTTPEvent(item sentryReportItem, projectID, status string) {
	var payload sentryHTTPPayload
	decodePayload(item.Payload, &payload)

	method := payload.Method
	if method == "" {
		method = "unknown"
	}
	statusCode := "0"
	if payload.StatusCode != nil {
		statusCode = strconv.Itoa(int(*payload.StatusCode))
	}

	sentryHTTPRequestsTotal.WithLabelValues(method, statusCode, status, projectID).Inc()
	if payload.ElapsedTime != nil {
		sentryHTTPRequestDurationMs.
			WithLabelValues(method, statusCode, projectID).
			Observe(*payload.ElapsedTime)
	}
}

func recordSentryErrorEvent(item sentryReportItem, projectID string) {
	name := boundedLabel("error_name", item.Name)

	var batch sentryBatchErrorPayload
	if decodePayload(item.Payload, &batch) && batch.BatchError && batch.BatchErrorLength != nil {
		sentryBatchErrorGroupsTotal.WithLabelValues(item.Type, name, projectID).Inc()
		sentryErrorsTotal.WithLabelValues(item.Type, name, projectID).Add(*batch.BatchErrorLength)
		return
	}
	sentryErrorsTotal.WithLabelValues(item.Type, name, projectID).Inc()
}

func recordSentryPerformanceEvent(item sentryReportItem, projectID string) {
	var payload sentryPerformancePayload
	decodePayload(item.Payload, &payload)

	if _, ok := webVitalNames[item.Name]; ok {
		recordSentryWebVital(item.Name, payload, projectID)
		return
	}

	switch item.Name {
	case "NavigationTiming":
		recordSentryNavigationTiming(payload.Extra, projectID)
		return
	case "ResourceTiming":
		var extra sentryResourceExtra
		if decodePayload(payload.Extra, &extra) {
			recordSentryResourceEntry(extra.Resource, projectID)
		}
		return
	case "ResourceList":
		for _, resource := range payload.ResourceList {
			recordSentryResourceEntry(resource, projectID)
		}
		return
	case "LongTask":
		for _, task := range payload.LongTasks {
			sentryLongTasksTotal.WithLabelValues(projectID).Inc()
			if task.Duration != nil {
				sentryLongTaskDurationMs.WithLabelValues(projectID).Observe(*task.Duration)
			}
		}
		return
	case "Memory":
		recordSentryBrowserMemory(payload, projectID)
		return
	}

	if strings.HasPrefix(item.Name, "HTTP ") {
		recordSentryHTTPPerformance(item.Name, payload, projectID)
		return
	}

	if payload.Value != nil {
		sentryPerformanceValue.
			WithLabelValues(boundedLabel("performance_name", item.Name), projectID).
			Set(*payload.Value)
	}
}

func recordSentryWebVital(name string, payload sentryPerformancePayload, projectID string) {
	if payload.Value == nil {
		return
	}
	rating := payload.Rating
	if rating == "" {
		rating = "none"
	}
	sentryWebVitals.WithLabelValues(name, rating, projectID).Set(*payload.Value)
	sentryWebVitalSamplesTotal.WithLabelValues(name, rating, projectID).Inc()
}

func recordSentryNavigationTiming(extra json.RawMessage, projectID string) {
	var values map[string]any
	if !decodePayload(extra, &values) {
		return
	}
	for _, phase := range navigationPhases {
		if value, ok := values[phase].(float64); ok {
			sentryNavigationTimingMs.WithLabelValues(phase, projectID).Observe(value)
		}
	}
}

func recordSentryResourceEntry(resource sentryResourceTiming, projectID string) {
	initiatorType := boundedLabel("initiator_type", resource.InitiatorType)
	fromCache := strconv.FormatBool(resource.FromCache)

	sentryResourceEntriesTotal.WithLabelValues(initiatorType, fromCache, projectID).Inc()
	if resource.Duration != nil {
		sentryResourceDurationMs.
			WithLabelValues(initiatorType, fromCache, projectID).
			Observe(*resource.Duration)
	}
	if resource.TransferSize != nil {
		sentryResourceTransferBytes.
			WithLabelValues(initiatorType, projectID).
			Observe(*resource.TransferSize)
	}
}

func recordSentryBrowserMemory(payload sentryPerformancePayload, projectID string) {
	if payload.Memory == nil {
		return
	}
	if payload.Memory.Bytes != nil {
		sentryBrowserMemoryBytes.WithLabelValues(projectID).Set(*payload.Memory.Bytes)
	}
	totals := make(map[string]float64)
	for _, entry := range payload.Memory.Breakdown {
		totals[boundedLabel("browser_memory_kind", strings.Join(entry.Types, "+"))] += entry.Bytes
	}
	for kind, value := range totals {
		sentryBrowserMemoryBreakdownBytes.WithLabelValues(kind, projectID).Set(value)
	}
}

func recordSentryHTTPPerformance(name string, payload sentryPerformancePayload, projectID string) {
	method := strings.TrimPrefix(name, "HTTP ")
	statusCode := "0"

	var extra sentryHTTPPayload
	if decodePayload(payload.Extra, &extra) {
		if extra.Method != "" {
			method = extra.Method
		}
		if extra.StatusCode != nil {
			statusCode = strconv.Itoa(int(*extra.StatusCode))
		}
	}

	sentryHTTPRequestsTotal.WithLabelValues(method, statusCode, "OK", projectID).Inc()
	if payload.Value != nil {
		sentryHTTPRequestDurationMs.
			WithLabelValues(method, statusCode, projectID).
			Observe(*payload.Value)
	}
}

func recordSentryClickEvent(item sentryReportItem, projectID string) {
	ev := item.Name
	var extra sentryClickExtra
	if decodePayload(readExtra(item.Payload), &extra) && extra.Ev != "" {
		ev = extra.Ev
	}
	sentryClicksTotal.WithLabelValues(boundedLabel("click_ev", ev), projectID).Inc()
}

func recordSentryExposureEvent(item sentryReportItem, projectID string) {
	sentryExposuresTotal.WithLabelValues(projectID).Inc()

	var extra sentryDurationExtra
	if decodePayload(readExtra(item.Payload), &extra) && extra.Duration != nil {
		sentryExposureDurationMs.WithLabelValues(projectID).Observe(*extra.Duration)
	}
}

func recordSentryPageViewEvent(item sentryReportItem, projectID string) {
	sentryPageViewsTotal.
		WithLabelValues(boundedLabel("page_view_name", item.Name), projectID).Inc()
	if item.Name != "PageDwell" {
		return
	}
	var extra sentryDurationExtra
	if decodePayload(readExtra(item.Payload), &extra) && extra.Duration != nil {
		sentryPageDwellMs.WithLabelValues(projectID).Observe(*extra.Duration)
	}
}

func (a *App) handleSentryLog(ctx *yukino_http.Context, next func()) {
	var items []sentryReportItem
	if err := ctx.BindJSON(&items); err != nil {
		sentryReportBatchesTotal.WithLabelValues("invalid").Inc()
		ctx.Throw(http.StatusBadRequest, "invalid report batch")
		return
	}
	recordSentryReportBatch(items)
	ctx.JSON(map[string]any{
		"message": "OK",
		"data":    map[string]any{"received": len(items)},
	})
}

func (a *App) handleMetrics(ctx *yukino_http.Context, next func()) {
	families, err := sentryRegistry.Gather()
	if err != nil {
		ctx.Throw(http.StatusInternalServerError, err.Error())
		return
	}
	format := expfmt.NewFormat(expfmt.TypeTextPlain)
	var buf bytes.Buffer
	encoder := expfmt.NewEncoder(&buf, format)
	for _, family := range families {
		if err := encoder.Encode(family); err != nil {
			ctx.Throw(http.StatusInternalServerError, err.Error())
			return
		}
	}
	ctx.Set("Content-Type", string(format))
	ctx.Data(buf.Bytes())
}
