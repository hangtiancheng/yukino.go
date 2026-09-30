import { signal } from "@lit-labs/signals";
import { getToken } from "./auth";
import { WS_URL } from "../config";

export type WsStatus = "disconnected" | "connecting" | "connected";

export const wsStore = signal<{ status: WsStatus }>({ status: "disconnected" });

let rawSocket: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let reconnectDelay = 1000;
let intentionalClose = false;
let onMessage: ((msg: MessageEvent) => void) | null = null;

const MAX_RECONNECT_DELAY = 30_000;

function scheduleReconnect(uuid: string) {
  if (intentionalClose) return;
  reconnectTimer = setTimeout(() => {
    reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY);
    doConnect(uuid);
  }, reconnectDelay);
}

function doConnect(uuid: string) {
  const token = getToken();
  if (!token) return;
  if (rawSocket) {
    rawSocket.onclose = null;
    rawSocket.close();
  }
  wsStore.set({ status: "connecting" });
  const ws = new WebSocket(
    `${WS_URL}/wss?client_id=${encodeURIComponent(uuid)}&token=${encodeURIComponent(token)}`,
  );
  ws.onopen = () => {
    reconnectDelay = 1000;
    wsStore.set({ status: "connected" });
  };
  ws.onmessage = (msg: MessageEvent) => {
    onMessage?.(msg);
  };
  ws.onclose = () => {
    rawSocket = null;
    wsStore.set({ status: "disconnected" });
    scheduleReconnect(uuid);
  };
  ws.onerror = () => {
    ws.close();
  };
  rawSocket = ws;
}

export function setWsHandler(handler: ((msg: MessageEvent) => void) | null) {
  onMessage = handler;
}

export function connectWs(uuid: string) {
  intentionalClose = false;
  reconnectDelay = 1000;
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  doConnect(uuid);
}

export function disconnectWs() {
  intentionalClose = true;
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  if (rawSocket) {
    rawSocket.onclose = null;
    rawSocket.close();
    rawSocket = null;
  }
  wsStore.set({ status: "disconnected" });
}

export function sendWs(data: unknown) {
  if (rawSocket && rawSocket.readyState === WebSocket.OPEN) {
    rawSocket.send(JSON.stringify(data));
  }
}
