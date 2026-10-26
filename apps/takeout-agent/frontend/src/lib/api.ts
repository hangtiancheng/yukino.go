import {
  chatResponseSchema,
  createSessionResponseSchema,
  evaluationReportSchema,
  mealResponseSchema,
  slotOptionsSchema,
  traceRowSchema,
  type ChatResponse,
  type EvaluationReport,
  type MealRequest,
  type MealResponse,
  type SlotOptions,
  type TraceRow,
} from "@/lib/schemas";

const API_BASE = "/api/v1/takeout";
export const USER_ID_KEY = "takeout.userId";

export function getUserId(): string {
  return localStorage.getItem(USER_ID_KEY) || "1";
}

export class ApiError extends Error {
  status: number;

  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

async function request(
  path: string,
  options: RequestInit = {},
): Promise<unknown> {
  const headers = new Headers(options.headers);
  headers.set("X-User-Id", getUserId());
  if (options.body !== undefined) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(`${API_BASE}${path}`, { ...options, headers });
  if (!response.ok) {
    throw new ApiError(await readError(response), response.status);
  }
  if (response.status === 204) {
    return null;
  }
  const text = await response.text();
  if (!text) {
    return null;
  }
  return JSON.parse(text);
}

async function readError(response: Response): Promise<string> {
  const text = await response.text();
  if (!text) {
    return `Request failed: ${response.status}`;
  }
  try {
    const payload = JSON.parse(text) as { message?: string; error?: string };
    return payload.message || payload.error || text;
  } catch {
    return text;
  }
}

function toQuery(params: object): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null && value !== "") {
      search.set(key, String(value));
    }
  }
  const query = search.toString();
  return query ? `?${query}` : "";
}

function parseList<T>(raw: unknown, schema: { parse(data: unknown): T }): T[] {
  const array = Array.isArray(raw) ? raw : [];
  return array.map((item) => schema.parse(item));
}

export interface ChatPayload {
  sessionId: string | null;
  message: string;
  sourceMode: "PERSONAL" | "PUBLIC";
  context?: Record<string, unknown>;
}

export interface TraceListParams {
  startAt?: string;
  endAt?: string;
  onlyUnlabeled?: boolean;
  limit?: number;
}

export interface TraceLabelPayload {
  expectedIntent?: string | null;
  expectedSlots?: Record<string, string[]> | null;
  expectedClarifyAction?: string | null;
  labelNote?: string | null;
}

export interface EvaluationPayload {
  startAt: string;
  endAt: string;
  includeLlmJudge?: boolean;
  limit?: number;
}

export interface FeedbackPayload {
  sessionId: string;
  itemId?: number | null;
  action: string;
  rating?: number | null;
  reason?: string | null;
}

export const TakeoutApi = {
  createSession: async (): Promise<{ sessionId: string }> =>
    createSessionResponseSchema.parse(
      await request("/sessions", { method: "POST" }),
    ),

  chat: async (payload: ChatPayload): Promise<ChatResponse> =>
    chatResponseSchema.parse(
      await request("/chat", { method: "POST", body: JSON.stringify(payload) }),
    ),

  listPersonalMeals: async (): Promise<MealResponse[]> =>
    parseList(await request("/meals/personal"), mealResponseSchema),

  createPersonalMeal: async (payload: MealRequest): Promise<MealResponse> =>
    mealResponseSchema.parse(
      await request("/meals/personal", {
        method: "POST",
        body: JSON.stringify(payload),
      }),
    ),

  updatePersonalMeal: async (
    mealId: number,
    payload: MealRequest,
  ): Promise<MealResponse> =>
    mealResponseSchema.parse(
      await request(`/meals/personal/${mealId}`, {
        method: "PUT",
        body: JSON.stringify(payload),
      }),
    ),

  deletePersonalMeal: async (mealId: number): Promise<void> => {
    await request(`/meals/personal/${mealId}`, { method: "DELETE" });
  },

  listPublicMeals: async (): Promise<MealResponse[]> =>
    parseList(await request("/meals/public"), mealResponseSchema),

  slotOptions: async (): Promise<SlotOptions> =>
    slotOptionsSchema.parse(await request("/slot-options")),

  saveFeedback: async (payload: FeedbackPayload): Promise<void> => {
    await request("/feedback", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  listTraces: async (params: TraceListParams): Promise<TraceRow[]> =>
    parseList(await request(`/debug/traces${toQuery(params)}`), traceRowSchema),

  getTrace: async (traceId: string): Promise<TraceRow> =>
    traceRowSchema.parse(
      await request(`/debug/traces/${encodeURIComponent(traceId)}`),
    ),

  listSessionTraces: async (
    sessionId: string,
    limit?: number,
  ): Promise<TraceRow[]> =>
    parseList(
      await request(
        `/debug/sessions/${encodeURIComponent(sessionId)}/traces${toQuery({ limit })}`,
      ),
      traceRowSchema,
    ),

  labelTrace: async (
    traceId: string,
    payload: TraceLabelPayload,
  ): Promise<void> => {
    await request(`/debug/traces/${encodeURIComponent(traceId)}/label`, {
      method: "PUT",
      body: JSON.stringify(payload),
    });
  },

  evaluate: async (payload: EvaluationPayload): Promise<EvaluationReport> =>
    evaluationReportSchema.parse(
      await request("/evaluations", {
        method: "POST",
        body: JSON.stringify(payload),
      }),
    ),
};
