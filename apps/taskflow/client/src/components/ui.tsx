import type { JSX } from "@yukino.js/lit-jsx/jsx-runtime";
import { Icon, type IconName } from "@/components/icon";
import type { Execution } from "@/api/client";

const statusMeta: Record<
  Execution["status"],
  { label: string; cls: string; icon: IconName; spin?: boolean }
> = {
  pending: { label: "Pending", cls: "badge-ghost", icon: "clock" },
  reserved: {
    label: "Reserved",
    cls: "badge-outline badge-warning",
    icon: "hourglass",
  },
  queued: {
    label: "Queued",
    cls: "badge-outline badge-info",
    icon: "hourglass",
  },
  running: {
    label: "Running",
    cls: "badge-info",
    icon: "loader-circle",
    spin: true,
  },
  succeeded: { label: "Succeeded", cls: "badge-success", icon: "check-circle" },
  failed: { label: "Failed", cls: "badge-error", icon: "x-circle" },
  cancelled: { label: "Cancelled", cls: "badge-ghost", icon: "ban" },
};

export function StatusBadge(props: { status: Execution["status"] }) {
  const meta = statusMeta[props.status] ?? {
    label: props.status,
    cls: "badge-ghost",
    icon: "clock" as IconName,
  };
  return (
    <span class={`badge badge-sm gap-1 ${meta.cls}`}>
      <Icon
        name={meta.icon}
        class={`h-3 w-3 ${meta.spin ? "animate-spin" : ""}`}
      />
      {meta.label}
    </span>
  );
}

const taskTypeMeta: Record<string, { label: string; cls: string }> = {
  scheduled: { label: "Scheduled", cls: "badge-primary badge-outline" },
  condition: { label: "Condition", cls: "badge-secondary badge-outline" },
  manual: { label: "Manual", cls: "badge-accent badge-outline" },
};

export function TaskTypeBadge(props: { type: string }) {
  const meta = taskTypeMeta[props.type] ?? {
    label: props.type,
    cls: "badge-ghost",
  };
  return <span class={`badge badge-sm ${meta.cls}`}>{meta.label}</span>;
}

export function PageHeader(props: {
  title: string;
  subtitle?: string;
  actions?: unknown;
}): JSX.Element {
  return (
    <div class="mb-6 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h1 class="text-base-content text-3xl font-semibold tracking-tight">
          {props.title}
        </h1>
      </div>
      <div class="flex items-center gap-2">{props.actions}</div>
    </div>
  );
}

export function EmptyState(props: {
  icon: IconName;
  title: string;
  hint?: string;
}): JSX.Element {
  return (
    <div class="text-base-content/50 flex flex-col items-center justify-center gap-3 py-16">
      <Icon name={props.icon} class="h-10 w-10" />
      <div class="text-sm font-medium">{props.title}</div>
    </div>
  );
}

export function LoadingRow(): JSX.Element {
  return (
    <div class="text-base-content/50 flex items-center justify-center gap-2 py-12">
      <span class="loading loading-spinner loading-sm"></span>
      <span class="text-sm">Loading...</span>
    </div>
  );
}

const pad = (n: number) => String(n).padStart(2, "0");

export function formatTime(iso: string | null | undefined): string {
  if (!iso) return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function formatDuration(
  startIso: string | null,
  endIso: string | null,
): string {
  if (!startIso) return "-";
  const start = new Date(startIso).getTime();
  if (Number.isNaN(start)) return "-";
  const end = endIso ? new Date(endIso).getTime() : Date.now();
  const ms = Math.max(0, end - start);
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  return `${m}m${Math.round(s % 60)}s`;
}

export function relativeTime(iso: string | null | undefined): string {
  if (!iso) return "-";
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return String(iso);
  const diff = Date.now() - t;
  const abs = Math.abs(diff);
  const units: Array<[number, string]> = [
    [60_000, "minute"],
    [3_600_000, "hour"],
    [86_400_000, "day"],
  ];
  let value = abs / 1000;
  let unit = "second";
  for (const [limit, name] of units) {
    if (abs < limit) break;
    value = abs / limit;
    unit = name;
  }
  const rounded = value >= 10 ? Math.round(value) : Math.round(value * 10) / 10;
  const plural = rounded === 1 ? unit : `${unit}s`;
  return diff >= 0 ? `${rounded} ${plural} ago` : `in ${rounded} ${plural}`;
}
