import { customElement } from "@yukino.js/lit-jsx";
import { AtomElement } from "@/components/base";
import { Icon } from "@/components/icon";
import {
  EmptyState,
  LoadingRow,
  PageHeader,
  StatusBadge,
  TaskTypeBadge,
  formatTime,
  relativeTime,
} from "@/components/ui";
import {
  overviewAtom,
  refreshOverview,
  apiErrorMessage,
  showToast,
} from "@/store/atoms";
import { store } from "@/store/atoms";

const statusOrder = [
  "succeeded",
  "failed",
  "running",
  "queued",
  "pending",
  "reserved",
  "cancelled",
];
const statusLabel: Record<string, string> = {
  succeeded: "Succeeded",
  failed: "Failed",
  running: "Running",
  queued: "Queued",
  pending: "Pending",
  reserved: "Reserved",
  cancelled: "Cancelled",
};

@customElement("dashboard-page")
export class DashboardPage extends AtomElement {
  protected override setupWatches(): void {
    this.watch(overviewAtom);
  }

  protected override loadData(): void {
    refreshOverview().catch((err) => {
      showToast("error", `Failed to load overview: ${apiErrorMessage(err)}`);
    });
  }

  private statCards() {
    const overview = store.get(overviewAtom);
    if (!overview) return [];

    const counts = overview.today_status_counts ?? {};
    const succeeded = counts.succeeded ?? 0;
    const failed = counts.failed ?? 0;
    const running =
      (counts.running ?? 0) + (counts.queued ?? 0) + (counts.reserved ?? 0);

    return [
      {
        label: "Succeeded today",
        value: succeeded,
        icon: "check-circle" as const,
        cls: "text-success",
        bg: "bg-success/10",
      },
      {
        label: "Running / queued",
        value: running,
        icon: "loader-circle" as const,
        cls: "text-info",
        bg: "bg-info/10",
      },
      {
        label: "Failed today",
        value: failed,
        icon: "alert-triangle" as const,
        cls: "text-error",
        bg: "bg-error/10",
      },
      {
        label: "Enabled tasks",
        value: overview.scheduled_enabled + overview.condition_enabled,
        icon: "zap" as const,
        cls: "text-primary",
        bg: "bg-primary/10",
      },
    ].map((card) => (
      <div class="card bg-base-100 ring-base-300/60 ring-1 transition-none">
        <div class="card-body flex-row items-center gap-4 p-5">
          <span
            class={`flex h-11 w-11 items-center justify-center rounded-xl ${card.bg} ${card.cls}`}
          >
            <Icon name={card.icon} class="h-5 w-5" />
          </span>
          <div class="flex flex-col">
            <span class="text-2xl leading-7 font-bold">{card.value}</span>
            <span class="text-base-content/60 text-xs">{card.label}</span>
          </div>
        </div>
      </div>
    ));
  }

  private recentRows() {
    const overview = store.get(overviewAtom);
    if (!overview) return <LoadingRow />;
    if (overview.recent_executions.length === 0) {
      return (
        <EmptyState
          icon="history"
          title="No executions yet"
          hint="Trigger a task manually, or wait for a scheduled fire / condition event"
        />
      );
    }
    return (
      <div class="min-w-0 overflow-x-auto">
        <table class="table">
          <thead>
            <tr class="text-base-content/60 text-xs uppercase">
              <th>ID</th>
              <th>Task</th>
              <th>Type</th>
              <th>Status</th>
              <th>Fire Time</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {overview.recent_executions.map((exec) => (
              <tr class="hover">
                <td class="font-mono text-xs">#{exec.id}</td>
                <td class="max-w-[220px] truncate font-medium">
                  {exec.task_name}
                </td>
                <td>
                  <TaskTypeBadge type={exec.task_type} />
                </td>
                <td>
                  <StatusBadge status={exec.status} />
                </td>
                <td class="text-base-content/60 text-xs">
                  {formatTime(exec.fire_at)}
                </td>
                <td class="text-right">
                  <a
                    href={`/executions/${exec.id}`}
                    class="btn btn-ghost btn-xs rounded-full"
                    yukino-sentry-ev="dashboard-execution-open"
                    yukino-sentry-msg={exec.task_name}
                    yukino-sentry-execution-id={String(exec.id)}
                  >
                    Details
                    <Icon name="chevron-right" class="h-3 w-3" />
                  </a>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }

  private nodeCard() {
    const overview = store.get(overviewAtom);
    const nodes = overview?.nodes;
    return (
      <div class="card bg-base-100 ring-base-300/60 ring-1 transition-none">
        <div class="card-body p-5">
          <div class="mb-1 flex items-center gap-2 text-sm font-semibold">
            <Icon name="server" class="text-primary h-4 w-4" />
            Cluster nodes
          </div>
          <div class="mt-3 flex flex-col gap-2">
            {(nodes?.nodes ?? []).map((n) => (
              <div class="bg-base-200/70 flex items-center justify-between rounded-lg px-3 py-2 text-xs">
                <span class="flex items-center gap-2 font-mono">
                  <span
                    class={`h-2 w-2 rounded-full ${n.alive ? "bg-success" : "bg-error"}`}
                  ></span>
                  {n.node_id}
                  {n.self ? (
                    <span class="badge badge-primary badge-xs">self</span>
                  ) : null}
                </span>
                <span class="text-base-content/50">weight {n.weight}</span>
              </div>
            ))}
            {nodes?.nodes?.length === 0 || !nodes ? (
              <div class="text-base-content/50 text-xs">
                No node information
              </div>
            ) : null}
          </div>
          {nodes?.roles ? (
            <div class="mt-3 grid grid-cols-2 gap-2 text-[11px]">
              {Object.entries(nodes.roles).map(([role, owner]) => (
                <div class="border-base-300 rounded-lg border px-2.5 py-2">
                  <div class="text-base-content/70 font-semibold">
                    {role.split(":").pop()}
                  </div>
                  <div
                    class="text-base-content/50 truncate font-mono"
                    title={owner}
                  >
                    {owner}
                  </div>
                </div>
              ))}
            </div>
          ) : null}
        </div>
      </div>
    );
  }

  render() {
    const overview = store.get(overviewAtom);
    return (
      <div yukino-sentry-view="dashboard">
        <PageHeader
          title="Dashboard"
          subtitle={overview ? `Current node ${overview.node}` : undefined}
          actions={
            <button
              class="btn btn-sm btn-ghost gap-1 rounded-full"
              yukino-sentry-ev="dashboard-refresh"
              yukino-sentry-msg="Refresh dashboard"
              onClick={() => this.loadData()}
            >
              <Icon name="refresh-cw" class="h-3.5 w-3.5" />
              Refresh
            </button>
          }
        />

        <div class="mb-6 grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {overview ? this.statCards() : <LoadingRow />}
        </div>

        <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
          <div class="card bg-base-100 ring-base-300/60 min-w-0 ring-1 xl:col-span-2">
            <div class="card-body min-w-0 p-5">
              <div class="mb-2 flex items-center justify-between">
                <div class="flex items-center gap-2 text-sm font-semibold">
                  <Icon name="history" class="text-primary h-4 w-4" />
                  Recent executions
                </div>
                <a
                  href="/executions"
                  class="btn btn-ghost btn-xs rounded-full"
                  yukino-sentry-ev="dashboard-all-records"
                  yukino-sentry-msg="Open all executions"
                >
                  All records
                  <Icon name="external-link" class="h-3 w-3" />
                </a>
              </div>
              {this.recentRows()}
            </div>
          </div>

          <div class="flex flex-col gap-4">
            {this.nodeCard()}
            <div class="card bg-base-100 ring-base-300/60 ring-1 transition-none">
              <div class="card-body p-5">
                <div class="mb-2 flex items-center gap-2 text-sm font-semibold">
                  <Icon name="clock" class="text-primary h-4 w-4" />
                  Today
                </div>
                <div class="flex flex-wrap gap-2">
                  {overview
                    ? statusOrder
                        .filter(
                          (s) => (overview.today_status_counts?.[s] ?? 0) > 0,
                        )
                        .map((s) => (
                          <div class="bg-base-200/70 rounded-lg px-3 py-1.5 text-xs">
                            <span class="font-semibold">{statusLabel[s]}</span>
                            <span class="text-base-content/60 ml-1.5 font-mono">
                              {overview.today_status_counts?.[s]}
                            </span>
                          </div>
                        ))
                    : null}
                  {overview &&
                  Object.keys(overview.today_status_counts ?? {}).length ===
                    0 ? (
                    <span class="text-base-content/50 text-xs">
                      No executions today
                    </span>
                  ) : null}
                </div>
                <div class="text-base-content/50 mt-3 text-[11px]">
                  Updated {relativeTime(new Date().toISOString())}
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "dashboard-page": DashboardPage;
  }
}
