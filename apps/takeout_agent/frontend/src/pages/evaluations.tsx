import {
  ChevronDown,
  ChevronRight,
  ClipboardCheck,
  FlaskConical,
  Gauge,
  LoaderCircle,
  Play,
  Tags,
} from "lucide-react";
import { Fragment, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { toast } from "sonner";

import { DateTimePicker } from "@/components/shared/date-time-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useEvaluate } from "@/hooks/queries";
import { ApiError } from "@/lib/api";
import {
  defaultRange,
  formatDateTime,
  formatScore,
  safePrettyJson,
  shortId,
} from "@/lib/format";
import type { EvaluationReport, TraceEvaluationResult } from "@/lib/schemas";

// Canonical display order for evaluation metrics. The Go backend marshals
// metricAverages as a JSON object, so its keys arrive alphabetically; this
// restores the stable order produced by the rule pipeline.
const METRIC_ORDER = [
  "intentAccuracy",
  "slotAccuracy",
  "clarifyNecessityAccuracy",
  "tokenCost",
  "tokenCostScore",
  "latencyMs",
  "latencyScore",
  "fallbackRate",
  "fallbackScore",
  "safetyCompliance",
  "hallucinationControl",
  "multiTurnConsistency",
  "explanationQuality",
  "naturalness",
] as const;

const METRIC_LABELS: Record<string, string> = {
  intentAccuracy: "Intent accuracy",
  slotAccuracy: "Slot accuracy",
  clarifyNecessityAccuracy: "Clarify accuracy",
  tokenCost: "Token cost",
  tokenCostScore: "Token cost score",
  latencyMs: "Latency",
  latencyScore: "Latency score",
  fallbackRate: "Fallback rate",
  fallbackScore: "Fallback score",
  safetyCompliance: "Safety compliance",
  hallucinationControl: "Hallucination control",
  multiTurnConsistency: "Multi-turn consistency",
  explanationQuality: "Explanation quality",
  naturalness: "Naturalness",
};

// Metrics that are raw observations rather than 0-1 quality ratios. They are
// shown as an integer with a unit instead of percentage score coloring.
const RAW_METRIC_UNITS: Record<string, string> = {
  tokenCost: "tokens",
  latencyMs: "ms",
};

function metricLabel(metric: string): string {
  return METRIC_LABELS[metric] ?? metric;
}

// orderedMetrics sorts metric keys into the canonical order, appending any
// unknown keys alphabetically so future metrics still surface.
function orderedMetrics(
  metrics: Record<string, number | null | undefined> | undefined,
): [string, number | null | undefined][] {
  const source = metrics ?? {};
  const present = new Set(Object.keys(source));
  const known = METRIC_ORDER.filter((key) => present.has(key));
  const unknown = [...present]
    .filter((key) => !(METRIC_ORDER as readonly string[]).includes(key))
    .sort();
  return [...known, ...unknown].map(
    (key) => [key, source[key]] as [string, number | null | undefined],
  );
}

function ScoreBadge({ value }: { value?: number | null }) {
  if (value === null || value === undefined) {
    return <span className="text-muted-foreground">-</span>;
  }
  const tone =
    value >= 80
      ? "text-primary"
      : value >= 60
        ? "text-foreground"
        : "text-destructive";
  return (
    <span className={`font-semibold tabular-nums ${tone}`}>
      {formatScore(value)}
    </span>
  );
}

// MetricValue renders a single metric average. Raw observations keep their
// unit; 0-1 ratios become percentages. fallbackRate is a rate where lower is
// better, so it stays neutral instead of borrowing the higher-is-better tone.
function MetricValue({
  metric,
  value,
}: {
  metric: string;
  value?: number | null;
}) {
  if (value === null || value === undefined) {
    return <span className="text-muted-foreground">-</span>;
  }
  const unit = RAW_METRIC_UNITS[metric];
  if (unit) {
    return (
      <span className="text-foreground font-semibold tabular-nums">
        {Math.round(value).toLocaleString()}
        <span className="text-muted-foreground ml-1 text-xs font-normal">
          {unit}
        </span>
      </span>
    );
  }
  const percent = value * 100;
  if (metric === "fallbackRate") {
    return (
      <span className="text-foreground font-semibold tabular-nums">
        {percent.toFixed(0)}%
      </span>
    );
  }
  return <ScoreBadge value={percent} />;
}

function SummaryCard({
  icon: Icon,
  label,
  value,
  hint,
}: {
  icon: typeof Gauge;
  label: string;
  value: React.ReactNode;
  hint?: string;
}) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardDescription className="flex items-center gap-1.5">
          <Icon className="text-primary size-3.5" />
          {label}
        </CardDescription>
        <CardTitle className="text-3xl tabular-nums">{value}</CardTitle>
        {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
      </CardHeader>
    </Card>
  );
}

function TraceDetailRow({ result }: { result: TraceEvaluationResult }) {
  const metrics = orderedMetrics(result.metrics);
  return (
    <TableRow className="hover:bg-transparent">
      <TableCell colSpan={7} className="bg-muted/20 p-0">
        <div className="space-y-4 p-4">
          <div className="space-y-2">
            <p className="text-muted-foreground text-xs font-medium">Metrics</p>
            {metrics.length === 0 ? (
              <p className="text-muted-foreground text-xs">
                No metrics recorded for this trace.
              </p>
            ) : (
              <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                {metrics.map(([metric, value]) => (
                  <div
                    key={metric}
                    className="bg-background flex items-center justify-between rounded-md border px-2.5 py-1.5"
                  >
                    <span className="text-muted-foreground text-xs">
                      {metricLabel(metric)}
                    </span>
                    <MetricValue metric={metric} value={value} />
                  </div>
                ))}
              </div>
            )}
          </div>
          <div className="space-y-2">
            <p className="text-muted-foreground text-xs font-medium">Detail</p>
            <pre className="bg-muted/60 max-h-64 overflow-auto rounded-md p-3 text-xs leading-relaxed">
              {safePrettyJson(result.detail) || "No detail recorded."}
            </pre>
          </div>
        </div>
      </TableCell>
    </TableRow>
  );
}

export default function EvaluationsPage() {
  const evaluate = useEvaluate();
  const initialRange = useMemo(() => defaultRange(), []);
  const [startAt, setStartAt] = useState(initialRange.startAt);
  const [endAt, setEndAt] = useState(initialRange.endAt);
  const [includeLlmJudge, setIncludeLlmJudge] = useState(false);
  const [limit, setLimit] = useState("200");
  const [report, setReport] = useState<EvaluationReport | null>(null);
  const [expandedTrace, setExpandedTrace] = useState<string | null>(null);

  const run = () => {
    evaluate.mutate(
      { startAt, endAt, includeLlmJudge, limit: Number(limit) || 200 },
      {
        onSuccess: (data) => {
          setReport(data);
          setExpandedTrace(null);
          if (data.totalTraces === 0) {
            toast.info("No traces found in this time window.");
          } else {
            toast.success(`Evaluated ${data.totalTraces} traces.`);
          }
        },
        onError: (error) =>
          toast.error(
            error instanceof ApiError ? error.message : "Evaluation failed.",
          ),
      },
    );
  };

  const metricEntries = report ? orderedMetrics(report.metricAverages) : [];

  return (
    <div className="mx-auto max-w-6xl space-y-4">
      <Card>
        <CardContent className="flex flex-wrap items-end gap-3 py-4">
          <div className="space-y-1.5">
            <Label className="text-muted-foreground text-xs">Start</Label>
            <DateTimePicker
              value={startAt}
              onChange={setStartAt}
              className="w-52"
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-muted-foreground text-xs">End</Label>
            <DateTimePicker
              value={endAt}
              onChange={setEndAt}
              className="w-52"
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-muted-foreground text-xs">Limit</Label>
            <Select
              value={limit}
              onValueChange={(value) => {
                if (value !== null) setLimit(value);
              }}
            >
              <SelectTrigger className="h-9 w-24">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {["50", "100", "200", "500"].map((value) => (
                  <SelectItem key={value} value={value}>
                    {value}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex h-9 items-center gap-2">
            <Switch
              id="llm-judge"
              checked={includeLlmJudge}
              onCheckedChange={setIncludeLlmJudge}
            />
            <Label htmlFor="llm-judge" className="text-sm font-normal">
              Include LLM judge
            </Label>
          </div>
          <Button
            className="ml-auto"
            onClick={run}
            disabled={evaluate.isPending}
          >
            {evaluate.isPending ? (
              <LoaderCircle className="animate-spin" />
            ) : (
              <Play />
            )}
            {evaluate.isPending ? "Evaluating…" : "Run evaluation"}
          </Button>
        </CardContent>
      </Card>

      {!report && !evaluate.isPending && (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-16 text-center">
            <span className="bg-primary/10 text-primary flex size-12 items-center justify-center rounded-2xl">
              <FlaskConical className="size-6" />
            </span>
            <div>
              <p className="font-medium">Run the evaluation pipeline</p>
              <p className="text-muted-foreground mx-auto mt-1 max-w-md text-sm">
                Scores every trace in the window with rule metrics (intent
                accuracy, slot recall, clarify correctness) and, optionally, an
                LLM judge. Labeled traces produce the richest signal.
              </p>
            </div>
          </CardContent>
        </Card>
      )}

      {evaluate.isPending && (
        <Card>
          <CardContent className="text-muted-foreground flex items-center justify-center gap-2 py-16 text-sm">
            <LoaderCircle className="size-4 animate-spin" />
            Scoring traces, this may take a moment…
          </CardContent>
        </Card>
      )}

      {report && (
        <>
          <div className="grid gap-4 sm:grid-cols-3">
            <SummaryCard
              icon={Gauge}
              label="Average score"
              value={<ScoreBadge value={report.avgScore} />}
              hint="0-100, blended across available signals"
            />
            <SummaryCard
              icon={ClipboardCheck}
              label="Total traces"
              value={report.totalTraces}
              hint={`${formatDateTime(report.startAt)} → ${formatDateTime(report.endAt)}`}
            />
            <SummaryCard
              icon={Tags}
              label="Labeled traces"
              value={report.labeledTraces}
              hint="Ground truth available for rule scoring"
            />
          </div>

          {metricEntries.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Metric averages</CardTitle>
                <CardDescription>
                  Mean of each rule metric across evaluated traces.
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                  {metricEntries.map(([metric, value]) => (
                    <div
                      key={metric}
                      className="bg-muted/30 flex items-center justify-between rounded-lg border px-3 py-2.5"
                    >
                      <span className="text-muted-foreground text-sm">
                        {metricLabel(metric)}
                      </span>
                      <MetricValue metric={metric} value={value} />
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          )}

          <Card>
            <CardHeader>
              <CardTitle className="text-base">Per-trace results</CardTitle>
              <CardDescription>
                {report.traceResults.length} traces scored in this run.
              </CardDescription>
            </CardHeader>
            <CardContent>
              {report.traceResults.length === 0 ? (
                <p className="text-muted-foreground py-8 text-center text-sm">
                  No trace results in this run.
                </p>
              ) : (
                <div className="overflow-x-auto">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-8" />
                        <TableHead>Trace</TableHead>
                        <TableHead>Created</TableHead>
                        <TableHead className="text-right">Score</TableHead>
                        <TableHead className="text-right">Rule</TableHead>
                        <TableHead className="text-right">LLM judge</TableHead>
                        <TableHead className="text-right">Feedback</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {report.traceResults.map((result) => (
                        <Fragment key={result.traceId}>
                          <TableRow>
                            <TableCell>
                              <Button
                                variant="ghost"
                                size="icon"
                                className="size-6"
                                aria-label={
                                  expandedTrace === result.traceId
                                    ? "Hide details"
                                    : "Show details"
                                }
                                onClick={() =>
                                  setExpandedTrace((prev) =>
                                    prev === result.traceId
                                      ? null
                                      : result.traceId,
                                  )
                                }
                              >
                                {expandedTrace === result.traceId ? (
                                  <ChevronDown className="size-4" />
                                ) : (
                                  <ChevronRight className="size-4" />
                                )}
                              </Button>
                            </TableCell>
                            <TableCell>
                              <Link
                                to={`/traces?traceId=${encodeURIComponent(result.traceId)}`}
                                className="text-primary font-mono text-xs underline-offset-2 hover:underline"
                              >
                                {shortId(result.traceId, 14)}
                              </Link>
                            </TableCell>
                            <TableCell className="text-muted-foreground text-xs">
                              {formatDateTime(result.createdAt)}
                            </TableCell>
                            <TableCell className="text-right">
                              <ScoreBadge value={result.score} />
                            </TableCell>
                            <TableCell className="text-muted-foreground text-right tabular-nums">
                              {formatScore(result.ruleScore)}
                            </TableCell>
                            <TableCell className="text-muted-foreground text-right tabular-nums">
                              {formatScore(result.llmJudgeScore)}
                            </TableCell>
                            <TableCell className="text-muted-foreground text-right tabular-nums">
                              {formatScore(result.userFeedbackScore)}
                            </TableCell>
                          </TableRow>
                          {expandedTrace === result.traceId && (
                            <TraceDetailRow result={result} />
                          )}
                        </Fragment>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </CardContent>
          </Card>

          <p className="text-muted-foreground flex items-center gap-1.5 text-xs">
            <Badge variant="outline" className="font-normal">
              Tip
            </Badge>
            Label traces first to unlock intent accuracy and slot recall
            metrics.
          </p>
        </>
      )}
    </div>
  );
}
