import { customElement, property, state } from "@yukino.js/lit-jsx";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { AtomElement } from "@/components/base";
import { Icon } from "@/components/icon";
import {
  StatusBadge,
  TaskTypeBadge,
  formatDuration,
  formatTime,
} from "@/components/ui";
import {
  frontMatterLabel,
  renderMarkdown,
  splitFrontMatter,
} from "@/components/markdown";
import { api, type Execution } from "@/api/client";
import { apiErrorMessage, showToast } from "@/store/atoms";

@customElement("execution-detail-page")
export class ExecutionDetailPage extends AtomElement {
  @property({ type: Number }) executionId = 0;

  @state() private exec: Execution | null = null;
  @state() private report = "";
  @state() private loadError = "";

  protected override async loadData(): Promise<void> {
    try {
      this.exec = await api.getExecution(this.executionId);
      try {
        this.report = await api.getReport(this.executionId);
      } catch {
        this.report = "";
      }
    } catch (err) {
      this.loadError = apiErrorMessage(err);
    }
  }

  private async reload() {
    this.loadError = "";
    await this.loadData();
  }

  private async cancel() {
    if (!this.exec) return;
    try {
      await api.cancelExecution(this.exec.id);
      showToast("success", "Execution cancelled");
      await this.reload();
    } catch (err) {
      showToast("error", apiErrorMessage(err));
    }
  }

  private infoRow(label: string, value: unknown) {
    if (value === null || value === undefined || value === "") return null;
    return (
      <div class="flex items-start justify-between gap-4 py-1.5 text-sm">
        <span class="text-base-content/50 shrink-0">{label}</span>
        <span class="text-base-content/90 min-w-0 text-right font-mono text-xs break-all">
          {String(value)}
        </span>
      </div>
    );
  }

  private triggerInfoNode() {
    if (!this.exec?.trigger_info) return null;
    let pretty = this.exec.trigger_info;
    try {
      pretty = JSON.stringify(JSON.parse(this.exec.trigger_info), null, 2);
    } catch {}
    return (
      <div class="mt-4">
        <div class="text-base-content/50 mb-1 text-xs font-semibold tracking-wide uppercase">
          Trigger Context (trigger_info)
        </div>
        <pre class="bg-base-200 max-h-64 overflow-auto rounded-lg p-3 text-[12px] leading-relaxed">
          <code>{pretty}</code>
        </pre>
      </div>
    );
  }

  private reportNode() {
    if (!this.report) {
      const running =
        this.exec &&
        ["pending", "reserved", "queued", "running"].includes(this.exec.status);
      return (
        <div class="text-base-content/50 flex flex-col items-center gap-3 py-16">
          <Icon name="file-text" class="h-10 w-10" />
          <div class="text-sm">
            {running
              ? "Report is being generated, refresh shortly..."
              : "No report yet"}
          </div>
          {this.exec?.error ? (
            <div class="alert alert-error max-w-md text-xs">
              {this.exec.error}
            </div>
          ) : null}
        </div>
      );
    }

    const fm = splitFrontMatter(this.report);
    const body = fm ? fm.rest : this.report;
    return (
      <div>
        {fm ? (
          <div class="mb-5 flex flex-wrap gap-2">
            {fm.entries.map(([k, v]) => (
              <div class="bg-base-200/80 rounded-lg px-2.5 py-1.5 text-[11px]">
                <span class="text-base-content/50">
                  {frontMatterLabel[k] ?? k}
                </span>
                <span class="text-base-content/90 ml-1.5 font-mono font-medium">
                  {v}
                </span>
              </div>
            ))}
          </div>
        ) : null}
        <div class="report-body">{unsafeHTML(renderMarkdown(body))}</div>
      </div>
    );
  }

  render() {
    const exec = this.exec;
    return (
      <div yukino-sentry-view="execution-detail">
        <div class="mb-6 flex flex-wrap items-center justify-between gap-3">
          <div class="flex items-center gap-3">
            <a
              href="/executions"
              class="btn btn-ghost btn-sm btn-circle rounded-full"
              yukino-sentry-ev="execution-back"
              yukino-sentry-msg="Back to executions"
            >
              <Icon name="chevron-left" class="h-4 w-4" />
            </a>
            <div>
              <div class="flex items-center gap-2 text-lg font-semibold">
                Execution #{this.executionId}
                {exec ? <StatusBadge status={exec.status} /> : null}
              </div>
              <div class="text-base-content/60 mt-0.5 flex items-center gap-2 text-sm">
                {exec?.task_name}
                {exec ? <TaskTypeBadge type={exec.task_type} /> : null}
              </div>
            </div>
          </div>
          <div class="flex items-center gap-2">
            {exec &&
            ["pending", "reserved", "queued", "running"].includes(
              exec.status,
            ) ? (
              <button
                class="btn btn-error btn-outline btn-sm gap-1 rounded-full"
                yukino-sentry-ev="execution-cancel"
                yukino-sentry-msg="Cancel execution"
                yukino-sentry-execution-id={String(this.executionId)}
                onClick={() => this.cancel()}
              >
                <Icon name="ban" class="h-3.5 w-3.5" />
                Cancel Execution
              </button>
            ) : null}
            <button
              class="btn btn-ghost btn-sm gap-1 rounded-full"
              yukino-sentry-ev="execution-refresh"
              yukino-sentry-msg="Refresh execution"
              yukino-sentry-execution-id={String(this.executionId)}
              onClick={() => this.reload()}
            >
              <Icon name="refresh-cw" class="h-3.5 w-3.5" />
              Refresh
            </button>
          </div>
        </div>

        {this.loadError ? (
          <div class="alert alert-error mb-4">{this.loadError}</div>
        ) : null}

        {exec ? (
          <div class="grid grid-cols-1 gap-4 xl:grid-cols-3">
            <div class="card bg-base-100 ring-base-300/60 h-fit ring-1">
              <div class="card-body p-5">
                <div class="mb-1 flex items-center gap-2 text-sm font-semibold">
                  <Icon name="terminal" class="text-primary h-4 w-4" />
                  Execution Info
                </div>
                <div class="divide-base-200 divide-y">
                  {this.infoRow("Idempotency key (fire_key)", exec.fire_key)}
                  {this.infoRow("Status", exec.status)}
                  {this.infoRow("Scheduled fire", formatTime(exec.fire_at))}
                  {this.infoRow("Started", formatTime(exec.started_at))}
                  {this.infoRow("Finished", formatTime(exec.finished_at))}
                  {this.infoRow(
                    "Duration",
                    formatDuration(exec.started_at, exec.finished_at),
                  )}
                  {this.infoRow("Node", exec.node_id)}
                  {this.infoRow("Trace ID", exec.trace_id)}
                  {this.infoRow("Transaction ID", exec.tx_id)}
                  {this.infoRow("Tool calls", exec.tool_calls)}
                  {this.infoRow(
                    "Tokens",
                    `${exec.tokens_prompt} in / ${exec.tokens_output} out`,
                  )}
                  {this.infoRow("Report file", exec.report_path)}
                </div>
                {this.triggerInfoNode()}
              </div>
            </div>

            <div class="card bg-base-100 ring-base-300/60 ring-1 xl:col-span-2">
              <div class="card-body p-6">
                <div class="mb-2 flex items-center gap-2 text-sm font-semibold">
                  <Icon name="scroll-text" class="text-primary h-4 w-4" />
                  Structured Report (markdown)
                </div>
                {this.reportNode()}
              </div>
            </div>
          </div>
        ) : (
          !this.loadError && (
            <div class="text-base-content/50 flex items-center justify-center gap-2 py-20">
              <span class="loading loading-spinner loading-sm"></span>
              Loading...
            </div>
          )
        )}
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "execution-detail-page": ExecutionDetailPage;
  }
}
