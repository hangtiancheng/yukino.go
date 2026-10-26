import { useVirtualizer } from "@tanstack/react-virtual";
import {
  Activity,
  AlertTriangle,
  Braces,
  ChevronDown,
  ChevronRight,
  Clock,
  RefreshCw,
  Tag,
  Timer,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import {
  useLabelTrace,
  useSessionTraces,
  useTraceDetail,
  useTraces,
} from "@/hooks/queries";
import { ApiError } from "@/lib/api";
import {
  defaultRange,
  formatDateTime,
  safePrettyJson,
  shortId,
} from "@/lib/format";
import { INTENTS } from "@/lib/format";
import type { TraceRow } from "@/lib/schemas";
import { cn } from "@/lib/utils";

interface TraceEvent {
  stepOrder: number;
  eventType: string;
  phase: string;
  agentName?: string;
  modelName?: string;
  inputPayload?: string;
  outputPayload?: string;
  latencyMs?: number | null;
  inputTokens?: number | null;
  outputTokens?: number | null;
  totalTokens?: number | null;
  errorMessage?: string;
  createdAt: string;
}

interface TraceDocument {
  traceId: string;
  sessionId: string;
  userId: number;
  status: string;
  durationMs: number;
  events: TraceEvent[];
}

const CLARIFY_ACTIONS = ["ASK", "READY"] as const;

function parseTraceDocument(traceJson: string): TraceDocument | null {
  try {
    const parsed = JSON.parse(traceJson) as TraceDocument;
    if (!parsed || !Array.isArray(parsed.events)) return null;
    return parsed;
  } catch {
    return null;
  }
}

function StatusBadge({ status }: { status: string }) {
  const failed =
    status.toUpperCase().includes("FAIL") ||
    status.toUpperCase().includes("ERROR");
  return (
    <Badge
      variant={
        failed ? "destructive" : status === "SUCCESS" ? "default" : "secondary"
      }
    >
      {status}
    </Badge>
  );
}

function EventCard({ event }: { event: TraceEvent }) {
  const [expanded, setExpanded] = useState(false);
  const hasPayload = Boolean(event.inputPayload || event.outputPayload);

  return (
    <div className="bg-card rounded-lg border p-3 text-sm shadow-sm">
      <button
        type="button"
        className="flex w-full flex-wrap items-center gap-2 text-left"
        onClick={() => hasPayload && setExpanded((prev) => !prev)}
      >
        {hasPayload ? (
          expanded ? (
            <ChevronDown className="text-muted-foreground size-4 shrink-0" />
          ) : (
            <ChevronRight className="text-muted-foreground size-4 shrink-0" />
          )
        ) : (
          <span className="size-4 shrink-0" />
        )}
        <span className="text-muted-foreground font-mono text-xs">
          #{event.stepOrder}
        </span>
        <Badge variant="outline" className="font-normal">
          {event.eventType}
        </Badge>
        {event.phase && (
          <Badge variant="secondary" className="font-normal">
            {event.phase}
          </Badge>
        )}
        {event.agentName && (
          <span className="text-muted-foreground text-xs">
            agent: {event.agentName}
          </span>
        )}
        {event.modelName && (
          <span className="text-muted-foreground text-xs">
            model: {event.modelName}
          </span>
        )}
        <span className="text-muted-foreground ml-auto flex items-center gap-3 text-xs">
          {typeof event.latencyMs === "number" && (
            <span className="inline-flex items-center gap-1">
              <Timer className="size-3" />
              {event.latencyMs} ms
            </span>
          )}
          {typeof event.totalTokens === "number" && (
            <span className="inline-flex items-center gap-1">
              <Braces className="size-3" />
              {event.totalTokens} tok
            </span>
          )}
        </span>
      </button>

      {event.errorMessage && (
        <p className="bg-destructive/10 text-destructive mt-2 flex items-start gap-1.5 rounded-md px-2.5 py-1.5 text-xs">
          <AlertTriangle className="mt-0.5 size-3.5 shrink-0" />
          {event.errorMessage}
        </p>
      )}

      {expanded && (
        <div className="mt-3 grid gap-3 lg:grid-cols-2">
          {event.inputPayload && (
            <div className="min-w-0 space-y-1">
              <p className="text-muted-foreground text-xs font-medium">Input</p>
              <pre className="bg-muted/60 max-h-64 overflow-auto rounded-md p-2.5 text-xs leading-relaxed">
                {safePrettyJson(event.inputPayload)}
              </pre>
            </div>
          )}
          {event.outputPayload && (
            <div className="min-w-0 space-y-1">
              <p className="text-muted-foreground text-xs font-medium">
                Output
              </p>
              <pre className="bg-muted/60 max-h-64 overflow-auto rounded-md p-2.5 text-xs leading-relaxed">
                {safePrettyJson(event.outputPayload)}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function EventList({ events }: { events: TraceEvent[] }) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  const virtualizer = useVirtualizer({
    count: events.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 64,
    overscan: 6,
    measureElement: (element) => element.getBoundingClientRect().height,
  });

  return (
    <div
      ref={parentRef}
      className="bg-muted/30 h-130 overflow-y-auto rounded-lg border"
    >
      <div className="relative" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((virtualRow) => {
          const event = events[virtualRow.index];
          return (
            <div
              key={virtualRow.key}
              data-index={virtualRow.index}
              ref={virtualizer.measureElement}
              className="absolute inset-x-0 top-0 px-3 py-1.5"
              style={{ transform: `translateY(${virtualRow.start}px)` }}
            >
              <EventCard event={event} />
            </div>
          );
        })}
      </div>
    </div>
  );
}

function LabelForm({ trace }: { trace: TraceRow }) {
  const labelTrace = useLabelTrace();
  const [expectedIntent, setExpectedIntent] = useState<string>(
    trace.expectedIntent ?? "",
  );
  const [expectedClarifyAction, setExpectedClarifyAction] = useState<string>(
    trace.expectedClarifyAction ?? "",
  );
  const [expectedSlotsJson, setExpectedSlotsJson] = useState<string>(() =>
    trace.expectedSlots ? safePrettyJson(trace.expectedSlots) : "",
  );
  const [labelNote, setLabelNote] = useState<string>(trace.labelNote ?? "");

  useEffect(() => {
    setExpectedIntent(trace.expectedIntent ?? "");
    setExpectedClarifyAction(trace.expectedClarifyAction ?? "");
    setExpectedSlotsJson(
      trace.expectedSlots ? safePrettyJson(trace.expectedSlots) : "",
    );
    setLabelNote(trace.labelNote ?? "");
  }, [trace]);

  const submit = () => {
    let expectedSlots: Record<string, string[]> | null = null;
    if (expectedSlotsJson.trim()) {
      try {
        expectedSlots = JSON.parse(expectedSlotsJson) as Record<
          string,
          string[]
        >;
      } catch {
        toast.error(
          'Expected slots must be valid JSON, e.g. {"mealTime": ["Lunch"]}.',
        );
        return;
      }
    }
    labelTrace.mutate(
      {
        traceId: trace.traceId,
        payload: {
          expectedIntent: expectedIntent || null,
          expectedClarifyAction: expectedClarifyAction || null,
          expectedSlots,
          labelNote: labelNote.trim() || null,
        },
      },
      {
        onSuccess: () => toast.success("Label saved."),
        onError: (error) =>
          toast.error(
            error instanceof ApiError
              ? error.message
              : "Failed to save the label.",
          ),
      },
    );
  };

  return (
    <div className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1.5">
          <Label className="text-xs">Expected intent</Label>
          <Select
            value={expectedIntent}
            onValueChange={(value) => {
              if (value !== null) setExpectedIntent(value);
            }}
          >
            <SelectTrigger className="h-9">
              <SelectValue placeholder="Choose intent" />
            </SelectTrigger>
            <SelectContent>
              {INTENTS.map((intent) => (
                <SelectItem key={intent} value={intent}>
                  {intent}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-1.5">
          <Label className="text-xs">Expected clarify action</Label>
          <Select
            value={expectedClarifyAction}
            onValueChange={(value) => {
              if (value !== null) setExpectedClarifyAction(value);
            }}
          >
            <SelectTrigger className="h-9">
              <SelectValue placeholder="Choose action" />
            </SelectTrigger>
            <SelectContent>
              {CLARIFY_ACTIONS.map((action) => (
                <SelectItem key={action} value={action}>
                  {action}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="space-y-1.5">
        <Label className="text-xs">Expected slots (JSON)</Label>
        <Textarea
          value={expectedSlotsJson}
          onChange={(event) => setExpectedSlotsJson(event.target.value)}
          placeholder='{"mealTime": ["Lunch"], "taste": ["Light"]}'
          className="min-h-16 font-mono text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <Label className="text-xs">Note</Label>
        <Textarea
          value={labelNote}
          onChange={(event) => setLabelNote(event.target.value)}
          placeholder="Optional annotation for reviewers…"
          className="min-h-14"
        />
      </div>
      <div className="flex items-center justify-between gap-2">
        <p className="text-muted-foreground text-xs">
          {trace.labeledAt
            ? `Labeled at ${formatDateTime(trace.labeledAt)}`
            : "Not labeled yet"}
        </p>
        <Button size="sm" onClick={submit} disabled={labelTrace.isPending}>
          <Tag />
          {labelTrace.isPending ? "Saving…" : "Save label"}
        </Button>
      </div>
    </div>
  );
}

export default function TracesPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const initialRange = useMemo(() => defaultRange(), []);
  const [startAt, setStartAt] = useState(initialRange.startAt);
  const [endAt, setEndAt] = useState(initialRange.endAt);
  const [sessionId, setSessionId] = useState("");
  const [onlyUnlabeled, setOnlyUnlabeled] = useState(false);
  const [limit, setLimit] = useState("50");
  const [applied, setApplied] = useState({
    startAt: initialRange.startAt,
    endAt: initialRange.endAt,
    onlyUnlabeled: false,
    limit: 50,
  });
  const [appliedSessionId, setAppliedSessionId] = useState("");
  const [selectedTraceId, setSelectedTraceId] = useState<string | null>(
    searchParams.get("traceId"),
  );

  // A filled session ID switches the list to the per-session endpoint;
  // the time range and the label filter only apply to the window query.
  const traces = useTraces(applied, appliedSessionId === "");
  const sessionTraces = useSessionTraces(appliedSessionId, applied.limit);
  const activeQuery = appliedSessionId ? sessionTraces : traces;
  const detail = useTraceDetail(selectedTraceId);

  const applyFilters = () => {
    setApplied({ startAt, endAt, onlyUnlabeled, limit: Number(limit) || 50 });
    setAppliedSessionId(sessionId.trim());
    // Every fresh query starts from its first trace, like the legacy console.
    setSelectedTraceId(null);
    setSearchParams({}, { replace: true });
  };

  const openTrace = (traceId: string) => {
    setSelectedTraceId(traceId);
    setSearchParams({ traceId }, { replace: true });
  };

  const document = detail.data
    ? parseTraceDocument(detail.data.traceJson)
    : null;
  const rows = activeQuery.data ?? [];

  useEffect(() => {
    if (selectedTraceId || rows.length === 0) return;
    const first = rows[0].traceId;
    setSelectedTraceId(first);
    setSearchParams({ traceId: first }, { replace: true });
  }, [rows, selectedTraceId, setSearchParams]);

  return (
    <div className="mx-auto max-w-7xl space-y-4">
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
            <Label
              className="text-muted-foreground text-xs"
              htmlFor="trace-session-filter"
            >
              Session ID
            </Label>
            <Input
              id="trace-session-filter"
              value={sessionId}
              onChange={(event) => setSessionId(event.target.value)}
              placeholder="Optional, filters by session"
              className="h-9 w-56 font-mono text-xs"
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
                {["20", "50", "100", "200"].map((value) => (
                  <SelectItem key={value} value={value}>
                    {value}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex h-9 items-center gap-2">
            <Switch
              id="only-unlabeled"
              checked={onlyUnlabeled}
              onCheckedChange={setOnlyUnlabeled}
            />
            <Label htmlFor="only-unlabeled" className="text-sm font-normal">
              Only unlabeled
            </Label>
          </div>
          <div className="ml-auto flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => void activeQuery.refetch()}
              disabled={activeQuery.isFetching}
            >
              <RefreshCw
                className={cn(activeQuery.isFetching && "animate-spin")}
              />
              Refresh
            </Button>
            <Button size="sm" onClick={applyFilters}>
              Apply filters
            </Button>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-4 xl:grid-cols-5">
        <Card className="xl:col-span-2">
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Request traces</CardTitle>
            <CardDescription>
              {activeQuery.isPending
                ? "Loading…"
                : appliedSessionId
                  ? `${rows.length} traces in session`
                  : `${rows.length} traces in window`}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {activeQuery.isPending ? (
              <div className="space-y-2">
                {Array.from({ length: 6 }).map((_, index) => (
                  <Skeleton key={index} className="h-10" />
                ))}
              </div>
            ) : activeQuery.isError ? (
              <p className="text-muted-foreground py-8 text-center text-sm">
                Failed to load traces:{" "}
                {activeQuery.error instanceof Error
                  ? activeQuery.error.message
                  : "unknown error"}
              </p>
            ) : rows.length === 0 ? (
              <div className="flex flex-col items-center gap-2 py-10 text-center">
                <Activity className="text-muted-foreground/50 size-8" />
                <p className="text-sm font-medium">
                  {appliedSessionId
                    ? "No traces for this session"
                    : "No traces in this window"}
                </p>
                <p className="text-muted-foreground text-xs">
                  {appliedSessionId
                    ? "Check the session ID, or chat with the agent first."
                    : "Chat with the agent or widen the time range."}
                </p>
              </div>
            ) : (
              <ScrollArea className="h-140">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Trace</TableHead>
                      <TableHead>Session</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead className="text-right">Events</TableHead>
                      <TableHead className="text-right">Duration</TableHead>
                      <TableHead>Created</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rows.map((row) => (
                      <TableRow
                        key={row.traceId}
                        data-state={
                          selectedTraceId === row.traceId
                            ? "selected"
                            : undefined
                        }
                        className="cursor-pointer"
                        onClick={() => openTrace(row.traceId)}
                      >
                        <TableCell>
                          <span className="font-mono text-xs">
                            {shortId(row.traceId, 10)}
                          </span>
                          {row.expectedIntent && (
                            <Badge
                              variant="outline"
                              className="ml-1.5 font-normal"
                            >
                              <Tag className="size-3" />
                              labeled
                            </Badge>
                          )}
                        </TableCell>
                        <TableCell className="text-muted-foreground font-mono text-xs">
                          {shortId(row.sessionId, 12)}
                        </TableCell>
                        <TableCell>
                          <StatusBadge status={row.status} />
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {row.eventCount}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {row.durationMs != null
                            ? `${row.durationMs} ms`
                            : "-"}
                        </TableCell>
                        <TableCell className="text-muted-foreground text-xs">
                          {formatDateTime(row.createdAt)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </ScrollArea>
            )}
          </CardContent>
        </Card>

        <Card className="xl:col-span-3">
          <CardHeader className="pb-3">
            <div className="flex flex-wrap items-center gap-2">
              <CardTitle className="text-base">Trace detail</CardTitle>
              {detail.data && (
                <>
                  <Badge variant="secondary" className="font-mono font-normal">
                    {shortId(detail.data.traceId, 18)}
                  </Badge>
                  <StatusBadge status={detail.data.status} />
                  <span className="text-muted-foreground inline-flex items-center gap-1 text-xs">
                    <Clock className="size-3" />
                    {formatDateTime(detail.data.createdAt)}
                  </span>
                  {document && (
                    <span className="text-muted-foreground ml-auto inline-flex items-center gap-1 text-xs">
                      <Timer className="size-3" />
                      {document.durationMs} ms total
                    </span>
                  )}
                </>
              )}
            </div>
            {!detail.data && (
              <CardDescription>
                Select a trace from the list to inspect its events.
              </CardDescription>
            )}
          </CardHeader>
          <CardContent className="space-y-4">
            {detail.isPending && (
              <div className="space-y-2">
                {Array.from({ length: 5 }).map((_, index) => (
                  <Skeleton key={index} className="h-14" />
                ))}
              </div>
            )}
            {detail.isError && (
              <p className="text-muted-foreground py-8 text-center text-sm">
                Failed to load the trace:{" "}
                {detail.error instanceof Error
                  ? detail.error.message
                  : "unknown error"}
              </p>
            )}
            {detail.data && !document && (
              <p className="text-muted-foreground py-8 text-center text-sm">
                The trace document could not be parsed.
              </p>
            )}
            {detail.data && document && (
              <>
                <div className="text-muted-foreground grid gap-2 text-xs sm:grid-cols-3">
                  <p>
                    Session:{" "}
                    <span className="text-foreground font-mono">
                      {shortId(document.sessionId, 16)}
                    </span>
                  </p>
                  <p>
                    User:{" "}
                    <span className="text-foreground">{document.userId}</span>
                  </p>
                  <p>
                    Events:{" "}
                    <span className="text-foreground">
                      {document.events.length}
                    </span>
                  </p>
                </div>
                <EventList events={document.events} />
                <Separator />
                <div className="space-y-2">
                  <p className="text-sm font-medium">Ground-truth label</p>
                  <LabelForm trace={detail.data} />
                </div>
              </>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
