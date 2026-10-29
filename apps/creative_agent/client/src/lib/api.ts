import { useAuthStore } from "@/stores/auth";

/**
 * Backend API base URL (split frontend/backend deployment):
 * - Production: inject VITE_API_BASE_URL at build time (e.g. https://api.example.com); cross-origin is allowed by backend CORS.
 * - Development: leave empty to use relative paths, forwarded by the vite proxy for /api and /aigc (see vite.config.ts).
 */
const BASE = String(import.meta.env.VITE_API_BASE_URL ?? "").replace(
  /\/+$/,
  "",
);

/** Unified backend response envelope: {code, message, data}; code=0 means success. */
interface Envelope<T> {
  code: number;
  message: string;
  data?: T;
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  const token = useAuthStore.getState().token;
  if (token) headers.Authorization = `Bearer ${token}`;

  let resp: Response;
  try {
    resp = await fetch(BASE + path, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new Error("Network error, please try again");
  }

  if (resp.status === 401) {
    useAuthStore.getState().clearToken();
    window.location.href = "/login";
    throw new Error("Session expired, please sign in again");
  }

  let json: Envelope<T>;
  try {
    json = (await resp.json()) as Envelope<T>;
  } catch {
    throw new Error("Invalid response from server");
  }

  if (!json || json.code !== 0) {
    throw new Error(json?.message || "Request failed");
  }
  return json.data as T;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body),
};

/** Known SSE event names (aligned with pkg/sse/events.go). */
export const SSE_EVENTS = [
  "message-start",
  "text-delta",
  "tool-call-start",
  "tool-call-result",
  "message-complete",
  "task-step",
  "task-done",
  "interrupt",
  "reconnect",
  "error",
  "ui-block-start",
  "ui-block-complete",
] as const;

export type SseEventName = (typeof SSE_EVENTS)[number] | "message";

export interface SseCallbacks {
  onOpen?: () => void;
  onEvent: (name: SseEventName, data: unknown) => void;
  /** The connection dropped unexpectedly (lets the caller auto-reconnect). */
  onDisconnect?: () => void;
  onClose?: () => void;
}

/**
 * Connects to the SSE GET endpoint with the browser-native EventSource.
 * EventSource cannot set custom headers, so the auth token is passed via the ?token= query
 * parameter (the backend JWT middleware supports reading the token from the query).
 */
export function openSse(
  path: string,
  params: Record<string, string | number>,
  callbacks: SseCallbacks,
): EventSource {
  const token = useAuthStore.getState().token;
  const query: Record<string, string | number> = { ...params };
  if (token) query.token = token;

  const qs = Object.entries(query)
    .map(
      ([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`,
    )
    .join("&");
  const es = new EventSource(`${BASE}${path}?${qs}`);

  let disconnected = false;

  es.onopen = () => callbacks.onOpen?.();

  const dispatch = (name: SseEventName, raw: string) => {
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      parsed = raw;
    }
    callbacks.onEvent(name, parsed);
  };

  for (const name of SSE_EVENTS) {
    es.addEventListener(name, (e) => dispatch(name, (e as MessageEvent).data));
  }
  // Fallback: unnamed message events
  es.onmessage = (e) => dispatch("message", e.data);

  es.onerror = () => {
    // The browser auto-reconnects while readyState=CONNECTING; here the caller decides whether to manually reconnect and replay.
    if (!disconnected) {
      disconnected = true;
      callbacks.onDisconnect?.();
    }
    es.close();
    callbacks.onClose?.();
  };

  return es;
}
