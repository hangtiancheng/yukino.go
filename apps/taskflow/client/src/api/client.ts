export interface ScheduledTask {
  id: number;
  name: string;
  description: string;
  cron_expr: string;
  timezone: string;
  prompt: string;
  model: string;
  enabled: boolean;
  last_fire_at: string | null;
  next_fire_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface ScheduledTaskHint extends ScheduledTask {
  next_fire_hint?: string;
}

export interface ConditionTask {
  id: number;
  name: string;
  description: string;
  event_type: string;
  table_name: string;
  prompt: string;
  model: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface Execution {
  id: number;
  task_type: "scheduled" | "condition" | "manual";
  task_id: number;
  task_name: string;
  fire_key: string;
  tx_id?: string;
  status:
    | "pending"
    | "reserved"
    | "queued"
    | "running"
    | "succeeded"
    | "failed"
    | "cancelled";
  trigger_info: string;
  report_path: string;
  report_excerpt: string;
  error: string;
  trace_id: string;
  node_id: string;
  fire_at: string;
  claimed_at: string | null;
  started_at: string | null;
  finished_at: string | null;
  tool_calls: number;
  tokens_prompt: number;
  tokens_output: number;
  created_at: string;
  updated_at: string;
}

export interface RiskRecord {
  id: number;
  title: string;
  content: string;
  source: string;
  created_at: string;
}

export interface DeadLetter {
  id: number;
  topic: string;
  msg_id: string;
  msg_key: string;
  val: string;
  reason: string;
  created_at: string;
}

export interface NodeMember {
  node_id: string;
  weight: number;
  alive: boolean;
  self: boolean;
}

export interface NodesInfo {
  self: string;
  roles: Record<string, string>;
  nodes?: NodeMember[];
  ring_error?: string;
}

export interface AppliedEntry {
  index: number;
  term: number;
  key: string;
  value: string;
  time: string;
}

export interface ConsensusStatus {
  enabled: boolean;
  node_id?: number;
  raft_state?: string;
  leader_id?: number;
  is_leader?: boolean;
  term?: number;
  commit_index?: number;
  applied_entries?: number;
  proposed_entries?: number;
  state_machine_keys?: number;
  recent_entries?: AppliedEntry[];
}

export interface JournalEntry {
  fire_key: string;
  execution_id: number;
  task_type: string;
  task_name: string;
  status: string;
  node: string;
  dispatched_at?: string;
  finished_at?: string;
  error?: string;
  updated_at: string;
}

export interface StatusMirror {
  key: string;
  entries: number;
  items: Record<string, string>;
}

export interface Overview {
  today_status_counts: Record<string, number>;
  scheduled_total: number;
  scheduled_enabled: number;
  condition_total: number;
  condition_enabled: number;
  recent_executions: Execution[];
  nodes: NodesInfo;
  node: string;
}

export interface ExecutionPage {
  items: Execution[];
  total: number;
  page: number;
  page_size: number;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

// Server base URL comes from the environment. Empty means same-origin: the
// vite dev proxy and the nginx image both forward /api to the server, so the
// SPA never hardcodes a host.
const SERVER = import.meta.env.VITE_SERVER_BASE_URL ?? "";
const BASE = `${SERVER}/api/v1`;

export function authHeaders(): Record<string, string> {
  const token = sessionStorage.getItem("taskflow-token");
  return token ? { Authorization: `Bearer ${token}` } : {};
}

function requireAuthentication(status: number) {
  if (status === 401) window.dispatchEvent(new Event("taskflow-auth-required"));
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...authHeaders(),
      ...(init?.headers ?? {}),
    },
  });
  const text = await resp.text();
  let body: { message?: string; data?: T } | null = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = null;
  }
  if (!resp.ok) {
    requireAuthentication(resp.status);
    throw new ApiError(resp.status, body?.message ?? resp.statusText);
  }
  return (body?.data ?? (undefined as T)) as T;
}

function qs(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== "") search.set(k, String(v));
  }
  const s = search.toString();
  return s ? `?${s}` : "";
}

export const api = {
  health: () => request<{ status: string; node: string }>("/health"),
  overview: () => request<Overview>("/overview"),

  listScheduled: () => request<ScheduledTaskHint[]>("/scheduled-tasks"),
  createScheduled: (payload: Partial<ScheduledTask>) =>
    request<ScheduledTask>("/scheduled-tasks", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  updateScheduled: (id: number, payload: Record<string, unknown>) =>
    request<ScheduledTask>(`/scheduled-tasks/${id}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    }),
  deleteScheduled: (id: number) =>
    request<{ deleted: number }>(`/scheduled-tasks/${id}`, {
      method: "DELETE",
    }),
  triggerScheduled: (id: number, key = crypto.randomUUID()) =>
    request<{
      execution_id: number;
      fire_key: string;
      dispatched: boolean;
      trace_id?: string;
    }>(`/scheduled-tasks/${id}/trigger`, {
      method: "POST",
      headers: { "Idempotency-Key": key },
    }),

  listCondition: () => request<ConditionTask[]>("/condition-tasks"),
  createCondition: (payload: Partial<ConditionTask>) =>
    request<ConditionTask>("/condition-tasks", {
      method: "POST",
      body: JSON.stringify(payload),
    }),
  updateCondition: (id: number, payload: Record<string, unknown>) =>
    request<ConditionTask>(`/condition-tasks/${id}`, {
      method: "PATCH",
      body: JSON.stringify(payload),
    }),
  deleteCondition: (id: number) =>
    request<{ deleted: number }>(`/condition-tasks/${id}`, {
      method: "DELETE",
    }),
  testCondition: (id: number, recordId?: number, key = crypto.randomUUID()) =>
    request<{ execution_id: number; record_id: number; dispatched: boolean }>(
      `/condition-tasks/${id}/test`,
      {
        method: "POST",
        headers: { "Idempotency-Key": key },
        body: JSON.stringify({ record_id: recordId ?? 0 }),
      },
    ),

  listExecutions: (filter: {
    page?: number;
    page_size?: number;
    task_type?: string;
    status?: string;
    task_id?: number;
  }) => request<ExecutionPage>(`/executions${qs(filter)}`),
  getExecution: (id: number) => request<Execution>(`/executions/${id}`),
  getReport: async (id: number): Promise<string> => {
    const resp = await fetch(`${BASE}/executions/${id}/report`, {
      headers: authHeaders(),
    });
    if (!resp.ok) {
      requireAuthentication(resp.status);
      const text = await resp.text();
      let message = resp.statusText;
      try {
        message = JSON.parse(text)?.message ?? message;
      } catch {
        /* keep statusText */
      }
      throw new ApiError(resp.status, message);
    }
    return resp.text();
  },
  cancelExecution: (id: number) =>
    request<{ cancelled: number }>(`/executions/${id}/cancel`, {
      method: "POST",
    }),

  listRecords: (page = 1, pageSize = 20) =>
    request<{ items: RiskRecord[]; total: number }>(
      `/risk-records${qs({ page, page_size: pageSize })}`,
    ),
  createRecord: (payload: {
    title: string;
    content: string;
    source?: string;
  }) =>
    request<{
      record: RiskRecord;
      event_delivery: string;
      trace_id?: string;
    }>("/risk-records", { method: "POST", body: JSON.stringify(payload) }),

  monitorNodes: () => request<NodesInfo>("/monitor/nodes"),
  monitorStats: () => request<Record<string, unknown>>("/monitor/stats"),
  monitorCache: () => request<Record<string, unknown>>("/monitor/cache"),
  monitorConsensus: () => request<ConsensusStatus>("/monitor/consensus"),
  monitorMirror: () => request<StatusMirror>("/monitor/mirror"),
  monitorStorage: () =>
    request<{
      pending_changes: number;
      pending_outbox: number;
      mysql_replicas: Record<string, unknown>[];
      redis: Record<string, string>;
    }>("/monitor/storage"),
  monitorJournal: (fireKey: string) =>
    request<JournalEntry>(`/monitor/journal${qs({ fire_key: fireKey })}`),
  deadLetters: (page = 1, pageSize = 20) =>
    request<{ items: DeadLetter[]; total: number }>(
      `/monitor/dead-letters${qs({ page, page_size: pageSize })}`,
    ),
};
