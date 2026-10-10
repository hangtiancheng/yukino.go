import { customElement, state } from "@yukino.js/lit-jsx";
import { LightDomElement } from "@/components/base";
import { Icon } from "@/components/icon";
import { PageHeader, formatTime } from "@/components/ui";
import {
  api,
  type ConsensusStatus,
  type DeadLetter,
  type JournalEntry,
  type NodesInfo,
  type StatusMirror,
} from "@/api/client";
import { apiErrorMessage } from "@/store/atoms";

@customElement("monitor-page")
export class MonitorPage extends LightDomElement {
  @state() private nodes: NodesInfo | null = null;
  @state() private stats: Record<string, unknown> | null = null;
  @state() private cache: Record<string, unknown> | null = null;
  @state() private consensus: ConsensusStatus | null = null;
  @state() private mirror: StatusMirror | null = null;
  @state() private deadLetters: DeadLetter[] = [];
  @state() private deadTotal = 0;
  @state() private storage: Awaited<
    ReturnType<typeof api.monitorStorage>
  > | null = null;
  @state() private loadError = "";

  @state() private journalKey = "";
  @state() private journalEntry: JournalEntry | null = null;
  @state() private journalError = "";
  @state() private journalBusy = false;

  connectedCallback(): void {
    super.connectedCallback();
    this.loadAll();
  }

  private async loadAll() {
    try {
      const [nodes, stats, cache, consensus, mirror, dead, storage] =
        await Promise.all([
          api.monitorNodes(),
          api.monitorStats(),
          api.monitorCache(),
          api.monitorConsensus(),
          api.monitorMirror(),
          api.deadLetters(1, 10),
          api.monitorStorage(),
        ]);
      this.nodes = nodes;
      this.stats = stats;
      this.cache = cache;
      this.consensus = consensus;
      this.mirror = mirror;
      this.deadLetters = dead.items;
      this.deadTotal = dead.total;
      this.storage = storage;
      this.loadError = "";
    } catch (err) {
      this.loadError = apiErrorMessage(err);
    }
  }

  private async lookupJournal() {
    const key = this.journalKey.trim();
    if (!key || this.journalBusy) return;
    this.journalBusy = true;
    this.journalError = "";
    this.journalEntry = null;
    try {
      this.journalEntry = await api.monitorJournal(key);
    } catch (err) {
      this.journalError = apiErrorMessage(err);
    } finally {
      this.journalBusy = false;
    }
  }

  private kvRows(obj: Record<string, unknown> | null, skip: string[] = []) {
    if (!obj) return [];
    return Object.entries(obj)
      .filter(
        ([k, v]) => !skip.includes(k) && v !== null && typeof v !== "object",
      )
      .map(([k, v]) => (
        <div class="flex items-center justify-between py-1 text-xs">
          <span class="text-base-content/50">{k}</span>
          <span class="text-base-content/90 font-mono">{String(v)}</span>
        </div>
      ));
  }

  private cacheCard() {
    return (
      <div class="card bg-base-100 ring-base-300/60 ring-1">
        <div class="card-body p-5">
          <div class="mb-1 flex items-center gap-2 text-sm font-semibold">
            <Icon name="list-checks" class="text-primary h-4 w-4" />
            Report Cache
          </div>
          <div class="divide-base-200 mt-2 divide-y">
            {this.kvRows(this.cache, ["name"])}
          </div>
        </div>
      </div>
    );
  }

  private consensusCard() {
    const c = this.consensus;
    const stateCls =
      c?.raft_state === "leader"
        ? "badge-success"
        : c?.raft_state === "follower"
          ? "badge-info"
          : "badge-ghost";
    return (
      <div class="card bg-base-100 ring-base-300/60 ring-1">
        <div class="card-body p-5">
          <div class="mb-1 flex items-center justify-between gap-2">
            <div class="flex items-center gap-2 text-sm font-semibold">
              <Icon name="scroll-text" class="text-primary h-4 w-4" />
              Local decision ledger
            </div>
            {c ? (
              <span class={`badge badge-sm ${stateCls}`}>
                {c.raft_state ?? "unknown"}
              </span>
            ) : null}
          </div>
          <div class="mt-2 grid grid-cols-2 gap-2 text-xs sm:grid-cols-4">
            {[
              ["Term", c?.term],
              ["Commit", c?.commit_index],
              ["Applied", c?.applied_entries],
              ["KV keys", c?.state_machine_keys],
            ].map(([label, val]) => (
              <div class="bg-base-200/70 rounded-lg px-2.5 py-2">
                <div class="text-base-content/50 text-[10px] tracking-wide uppercase">
                  {label}
                </div>
                <div class="font-mono text-sm font-semibold">{val ?? "-"}</div>
              </div>
            ))}
          </div>
          <div class="mt-3 flex flex-col gap-1.5">
            {(c?.recent_entries ?? []).slice(0, 6).map((e) => (
              <div class="border-base-300 flex items-center justify-between rounded-lg border px-2.5 py-1.5 text-[11px]">
                <span class="text-base-content/50 font-mono">
                  #{e.index} t{e.term}
                </span>
                <span
                  class="min-w-0 flex-1 truncate px-2 font-mono"
                  title={e.key}
                >
                  {e.key}
                </span>
                <span class="text-base-content/80 shrink-0 font-mono font-medium">
                  {e.value}
                </span>
              </div>
            ))}
            {(c?.recent_entries ?? []).length === 0 ? (
              <div class="text-base-content/50 py-2 text-center text-[11px]">
                No committed entries yet
              </div>
            ) : null}
          </div>
        </div>
      </div>
    );
  }

  private mirrorCard() {
    const m = this.mirror;
    const items = Object.entries(m?.items ?? {});
    return (
      <div class="card bg-base-100 ring-base-300/60 ring-1">
        <div class="card-body p-5">
          <div class="mb-1 flex items-center justify-between gap-2">
            <div class="flex items-center gap-2 text-sm font-semibold">
              <Icon name="refresh-cw" class="text-primary h-4 w-4" />
              Status Mirror
            </div>
            <span class="badge badge-ghost badge-sm">
              {m?.entries ?? 0} keys
            </span>
          </div>
          <div class="mt-2 flex flex-col gap-1.5">
            {items.slice(0, 8).map(([id, raw]) => {
              let parsed: { status?: string; fire_key?: string } = {};
              try {
                parsed = JSON.parse(raw);
              } catch {}
              return (
                <div class="bg-base-200/70 flex items-center justify-between rounded-lg px-2.5 py-1.5 text-[11px]">
                  <span class="text-base-content/50 font-mono">exec #{id}</span>
                  <span
                    class="min-w-0 flex-1 truncate px-2 font-mono"
                    title={parsed.fire_key}
                  >
                    {parsed.fire_key}
                  </span>
                  <span class="text-base-content/80 shrink-0 font-semibold">
                    {parsed.status ?? raw}
                  </span>
                </div>
              );
            })}
            {items.length === 0 ? (
              <div class="text-base-content/50 py-2 text-center text-[11px]">
                Mirror is empty
              </div>
            ) : null}
          </div>
        </div>
      </div>
    );
  }

  private journalCard() {
    const e = this.journalEntry;
    return (
      <div class="card bg-base-100 ring-base-300/60 ring-1">
        <div class="card-body p-5">
          <div class="mb-1 flex items-center gap-2 text-sm font-semibold">
            <Icon name="file-text" class="text-primary h-4 w-4" />
            Audit Journal
          </div>
          <div class="mt-3 flex items-center gap-2">
            <input
              class="input input-bordered input-sm flex-1 font-mono"
              placeholder="cond:1:risk_records:4:1791254848"
              value={this.journalKey}
              onInput={(ev: Event) =>
                (this.journalKey = (ev.target as HTMLInputElement).value)
              }
              onKeyDown={(ev: KeyboardEvent) => {
                if (ev.key === "Enter") this.lookupJournal();
              }}
            />
            <button
              class={`btn btn-primary btn-sm rounded-full ${this.journalBusy ? "loading" : ""}`}
              yukino-sentry-ev="monitor-journal-lookup"
              yukino-sentry-msg="Look up fire key in the audit journal"
              onClick={() => this.lookupJournal()}
            >
              Lookup
            </button>
          </div>
          {this.journalError ? (
            <div class="alert alert-warning mt-2 py-2 text-xs">
              {this.journalError}
            </div>
          ) : null}
          {e ? (
            <div class="divide-base-200 border-base-300 mt-3 divide-y rounded-lg border px-3">
              {this.kvRows(e as unknown as Record<string, unknown>)}
            </div>
          ) : null}
        </div>
      </div>
    );
  }

  render() {
    const stats = this.stats as Record<string, unknown> | null;
    const todayCounts = (stats?.today_executions ?? {}) as Record<
      string,
      number
    >;
    return (
      <div yukino-sentry-view="cluster-monitor">
        <PageHeader
          title="Cluster monitor"
          subtitle="Node membership, scheduler stats and subsystem health"
          actions={
            <button
              class="btn btn-ghost btn-sm gap-1 rounded-full"
              yukino-sentry-ev="monitor-refresh"
              yukino-sentry-msg="Refresh cluster monitor"
              onClick={() => this.loadAll()}
            >
              <Icon name="refresh-cw" class="h-3.5 w-3.5" />
              Refresh
            </button>
          }
        />

        {this.loadError ? (
          <div class="alert alert-error mb-4">{this.loadError}</div>
        ) : null}

        <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <div class="card bg-base-100 ring-base-300/60 ring-1">
            <div class="card-body p-5">
              <div class="mb-2 flex items-center gap-2 text-sm font-semibold">
                <Icon name="server" class="text-primary h-4 w-4" />
                Nodes &amp; Singleton Roles
              </div>
              <div class="flex flex-col gap-2">
                {(this.nodes?.nodes ?? []).map((n) => (
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
              </div>
              {this.nodes?.roles ? (
                <div class="mt-3 flex flex-col gap-2">
                  {Object.entries(this.nodes.roles).map(([role, owner]) => (
                    <div class="border-base-300 flex items-center justify-between rounded-lg border px-3 py-2 text-xs">
                      <span class="font-semibold">{role}</span>
                      <span class="text-base-content/60 font-mono">
                        {owner}
                      </span>
                    </div>
                  ))}
                </div>
              ) : null}
              {this.nodes?.ring_error ? (
                <div class="alert alert-warning mt-3 text-xs">
                  {this.nodes.ring_error}
                </div>
              ) : null}
            </div>
          </div>

          <div class="card bg-base-100 ring-base-300/60 ring-1">
            <div class="card-body p-5">
              <div class="mb-2 flex items-center gap-2 text-sm font-semibold">
                <Icon name="activity" class="text-primary h-4 w-4" />
                Scheduler Stats
              </div>
              <div class="divide-base-200 divide-y">
                <div class="flex items-center justify-between py-1 text-xs">
                  <span class="text-base-content/50">
                    Today's status breakdown
                  </span>
                  <span class="flex flex-wrap justify-end gap-1.5">
                    {Object.entries(todayCounts).map(([s, c]) => (
                      <span class="bg-base-200 rounded px-1.5 py-0.5 font-mono">
                        {s}:{c}
                      </span>
                    ))}
                    {Object.keys(todayCounts).length === 0 ? (
                      <span>-</span>
                    ) : null}
                  </span>
                </div>
                {this.kvRows(stats, ["today_executions", "available", "node"])}
              </div>
            </div>
          </div>

          {this.consensusCard()}
          {this.cacheCard()}
          {this.mirrorCard()}
          {this.journalCard()}
          <div class="card bg-base-100 ring-base-300/60 ring-1 xl:col-span-2">
            <div class="card-body p-5">
              <div class="mb-3 flex items-center gap-2 text-sm font-semibold">
                <Icon name="database" class="text-primary h-4 w-4" />
                Database synchronization
              </div>
              <div class="grid gap-4 sm:grid-cols-4">
                {[
                  ["Pending changes", this.storage?.pending_changes],
                  ["Pending events", this.storage?.pending_outbox],
                  ["MySQL replicas", this.storage?.mysql_replicas.length],
                  [
                    "Redis replicas",
                    this.storage?.redis.connected_slaves ??
                      this.storage?.redis.connected_replicas ??
                      "0",
                  ],
                ].map(([label, value]) => (
                  <div>
                    <p class="text-base-content/60 text-xs">{label}</p>
                    <p class="mt-1 text-2xl font-semibold">{value ?? "-"}</p>
                  </div>
                ))}
              </div>
              {this.storage?.mysql_replicas.map((replica) => (
                <div class="border-base-300 mt-3 flex flex-wrap items-center justify-between gap-2 border-t pt-3 text-sm">
                  <span>{String(replica.address)}</span>
                  <span
                    class={`badge badge-sm ${replica.healthy ? "badge-success" : "badge-warning"}`}
                  >
                    {replica.healthy ? "Replicating" : "Unavailable"}
                  </span>
                  <span class="text-base-content/60">
                    {String(replica.Seconds_Behind_Source ?? "-")}s lag
                  </span>
                </div>
              ))}
            </div>
          </div>

          <div class="card bg-base-100 ring-base-300/60 ring-1 xl:col-span-2">
            <div class="card-body p-5">
              <div class="mb-2 flex items-center gap-2 text-sm font-semibold">
                <Icon name="mail-warning" class="text-error h-4 w-4" />
                Dead Letters ({this.deadTotal})
              </div>
              <div class="flex flex-col gap-2">
                {this.deadLetters.map((dl) => (
                  <div class="border-error/30 bg-error/5 rounded-lg border p-2.5 text-xs">
                    <div class="flex items-center justify-between">
                      <span class="font-mono font-semibold">{dl.topic}</span>
                      <span class="text-base-content/50">
                        {formatTime(dl.created_at)}
                      </span>
                    </div>
                    <div class="text-base-content/60 mt-1 truncate font-mono">
                      {dl.val}
                    </div>
                    <div class="text-base-content/50 mt-1">{dl.reason}</div>
                  </div>
                ))}
                {this.deadLetters.length === 0 ? (
                  <div class="text-base-content/50 flex items-center gap-2 py-6 text-xs">
                    <Icon name="check-circle" class="text-success h-4 w-4" />
                    No dead letters
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
    "monitor-page": MonitorPage;
  }
}
