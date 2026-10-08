import { customElement } from "@yukino.js/lit-jsx";
import { AtomElement } from "@/components/base";
import { Icon } from "@/components/icon";
import {
  PageHeader,
  StatusBadge,
  TaskTypeBadge,
  formatDuration,
  formatTime,
} from "@/components/ui";
import {
  apiErrorMessage,
  execFilterAtom,
  executionsAtom,
  refreshExecutions,
  showToast,
  store,
} from "@/store/atoms";

const statusOptions = [
  { value: "", label: "All statuses" },
  { value: "pending", label: "Pending" },
  { value: "queued", label: "Queued" },
  { value: "running", label: "Running" },
  { value: "succeeded", label: "Succeeded" },
  { value: "failed", label: "Failed" },
  { value: "cancelled", label: "Cancelled" },
];

const typeOptions = [
  { value: "", label: "All types" },
  { value: "scheduled", label: "Scheduled" },
  { value: "condition", label: "Condition" },
  { value: "manual", label: "Manual" },
];

@customElement("executions-page")
export class ExecutionsPage extends AtomElement {
  protected override setupWatches(): void {
    this.watch(executionsAtom);
    this.watch(execFilterAtom);
  }

  protected override loadData(): void {
    refreshExecutions().catch((err) =>
      showToast("error", apiErrorMessage(err)),
    );
  }

  private applyFilter(partial: {
    taskType?: string;
    status?: string;
    page?: number;
  }) {
    const filter = store.get(execFilterAtom);
    refreshExecutions({
      ...filter,
      ...(partial.taskType !== undefined ? { taskType: partial.taskType } : {}),
      ...(partial.status !== undefined ? { status: partial.status } : {}),
      ...(partial.page !== undefined ? { page: partial.page } : {}),
      ...(partial.taskType !== undefined || partial.status !== undefined
        ? { page: 1 }
        : {}),
    }).catch((err) => showToast("error", apiErrorMessage(err)));
  }

  render() {
    const page = store.get(executionsAtom);
    const filter = store.get(execFilterAtom);
    const totalPages = Math.max(1, Math.ceil(page.total / filter.pageSize));

    return (
      <div yukino-sentry-view="executions">
        <PageHeader
          title="Executions"
          subtitle="History of every task run and its report"
          actions={
            <button
              class="btn btn-ghost btn-sm gap-1 rounded-full"
              yukino-sentry-ev="executions-refresh"
              yukino-sentry-msg="Refresh executions"
              onClick={() => this.loadData()}
            >
              <Icon name="refresh-cw" class="h-3.5 w-3.5" />
              Refresh
            </button>
          }
        />

        <div class="mb-4 flex flex-wrap items-center gap-2">
          <select
            class="select select-bordered select-sm"
            yukino-sentry-ev="executions-filter-type"
            yukino-sentry-msg="Filter executions by task type"
            onChange={(e: Event) =>
              this.applyFilter({
                taskType: (e.target as HTMLSelectElement).value,
              })
            }
          >
            {typeOptions.map((o) => (
              <option value={o.value} selected={filter.taskType === o.value}>
                {o.label}
              </option>
            ))}
          </select>
          <select
            class="select select-bordered select-sm"
            yukino-sentry-ev="executions-filter-status"
            yukino-sentry-msg="Filter executions by status"
            onChange={(e: Event) =>
              this.applyFilter({
                status: (e.target as HTMLSelectElement).value,
              })
            }
          >
            {statusOptions.map((o) => (
              <option value={o.value} selected={filter.status === o.value}>
                {o.label}
              </option>
            ))}
          </select>
          <span class="text-base-content/50 ml-auto text-xs">
            {page.total} total
          </span>
        </div>

        <div class="card bg-base-100 ring-base-300/60 ring-1">
          <div class="overflow-x-auto">
            <table class="table">
              <thead>
                <tr class="text-base-content/60 text-xs uppercase">
                  <th>ID</th>
                  <th>Task</th>
                  <th>Type</th>
                  <th>Status</th>
                  <th>Fire Time</th>
                  <th>Duration</th>
                  <th>Tools/Tokens</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {page.items.map((exec) => (
                  <tr class="hover">
                    <td class="font-mono text-xs">#{exec.id}</td>
                    <td class="min-w-[180px]">
                      <div class="max-w-[240px] truncate font-medium">
                        {exec.task_name}
                      </div>
                      <div
                        class="text-base-content/50 mt-0.5 max-w-[240px] truncate font-mono text-[11px]"
                        title={exec.fire_key}
                      >
                        {exec.fire_key}
                      </div>
                    </td>
                    <td>
                      <TaskTypeBadge type={exec.task_type} />
                    </td>
                    <td>
                      <StatusBadge status={exec.status} />
                    </td>
                    <td class="text-base-content/70 text-xs">
                      {formatTime(exec.fire_at)}
                    </td>
                    <td class="text-base-content/70 text-xs">
                      {formatDuration(exec.started_at, exec.finished_at)}
                    </td>
                    <td class="text-base-content/70 text-xs">
                      {exec.tool_calls} calls /{" "}
                      {(
                        exec.tokens_prompt + exec.tokens_output
                      ).toLocaleString()}
                    </td>
                    <td class="text-right">
                      <a
                        href={`/executions/${exec.id}`}
                        class="btn btn-ghost btn-xs gap-1 rounded-full"
                        yukino-sentry-ev="execution-open"
                        yukino-sentry-msg={exec.task_name}
                        yukino-sentry-execution-id={String(exec.id)}
                      >
                        Details
                        <Icon name="chevron-right" class="h-3 w-3" />
                      </a>
                    </td>
                  </tr>
                ))}
                {page.items.length === 0 ? (
                  <tr>
                    <td
                      colspan={8}
                      class="text-base-content/50 py-12 text-center text-sm"
                    >
                      No executions match the filter
                    </td>
                  </tr>
                ) : null}
              </tbody>
            </table>
          </div>

          <div class="border-base-300 flex items-center justify-between border-t px-4 py-2.5">
            <button
              class="btn btn-ghost btn-xs rounded-full"
              disabled={filter.page <= 1}
              yukino-sentry-ev="executions-prev-page"
              yukino-sentry-msg="Previous page"
              onClick={() => this.applyFilter({ page: filter.page - 1 })}
            >
              <Icon name="chevron-left" class="h-3 w-3" />
              Previous
            </button>
            <span class="text-base-content/60 text-xs">
              {filter.page} / {totalPages}
            </span>
            <button
              class="btn btn-ghost btn-xs rounded-full"
              disabled={filter.page >= totalPages}
              yukino-sentry-ev="executions-next-page"
              yukino-sentry-msg="Next page"
              onClick={() => this.applyFilter({ page: filter.page + 1 })}
            >
              Next
              <Icon name="chevron-right" class="h-3 w-3" />
            </button>
          </div>
        </div>
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "executions-page": ExecutionsPage;
  }
}
