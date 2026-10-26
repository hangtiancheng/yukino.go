import { useAuthStore } from "@/stores/auth";

/**
 * 后端 API 地址（前后端分离部署）：
 * - 生产：构建时注入 VITE_API_BASE_URL（如 https://api.example.com），跨域由后端 CORS 放行；
 * - 开发：留空走相对路径，由 vite proxy 转发 /api、/aigc（见 vite.config.ts）。
 */
const BASE = String(import.meta.env.VITE_API_BASE_URL ?? "").replace(
  /\/+$/,
  "",
);

/** 后端统一响应包：{code, message, data}，code=0 为成功。 */
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
    throw new Error("网络错误，请重试");
  }

  if (resp.status === 401) {
    useAuthStore.getState().clearToken();
    window.location.href = "/login";
    throw new Error("登录已过期，请重新登录");
  }

  let json: Envelope<T>;
  try {
    json = (await resp.json()) as Envelope<T>;
  } catch {
    throw new Error("服务端响应格式错误");
  }

  if (!json || json.code !== 0) {
    throw new Error(json?.message || "请求失败");
  }
  return json.data as T;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body),
};

/** SSE 已知事件名（与 pkg/sse/events.go 对齐）。 */
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
  /** 连接异常断开（供调用方自动重连）。 */
  onDisconnect?: () => void;
  onClose?: () => void;
}

/**
 * 用浏览器原生 EventSource 连接 SSE GET 端点。
 * EventSource 无法设置自定义 Header，鉴权 token 通过 query 参数 ?token= 透传
 * （后端 JWT 中间件支持从 query 读 token）。
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
  // 兜底：未命名 message 事件
  es.onmessage = (e) => dispatch("message", e.data);

  es.onerror = () => {
    // readyState=CONNECTING 时浏览器会自动重连；这里交给调用方决定是否手动重连回放。
    if (!disconnected) {
      disconnected = true;
      callbacks.onDisconnect?.();
    }
    es.close();
    callbacks.onClose?.();
  };

  return es;
}
