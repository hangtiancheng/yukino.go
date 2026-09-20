/**
 * Copyright (c) 2026 hangtiancheng
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

import { signal } from "@lit-labs/signals";

export interface EntrySnapshot {
  key: string;
  size: number;
  expire_at: number;
  level: number;
}

export interface GroupSnapshot {
  name: string;
  stats: Record<string, unknown>;
  entries: EntrySnapshot[];
}

export type DashboardStatus = "disconnected" | "connecting" | "connected";

export interface DashboardSnapshot {
  groups: GroupSnapshot[];
  status: DashboardStatus;
}

export const dashboardStore = signal<DashboardSnapshot>({
  groups: [],
  status: "disconnected",
});

let ws: WebSocket | null = null;
let retryTimer: ReturnType<typeof setTimeout> | null = null;
let intentionalClose = false;

export function connectDashboard(url: string): void {
  intentionalClose = false;
  if (ws) ws.close();
  if (retryTimer) clearTimeout(retryTimer);
  dashboardStore.set({ ...dashboardStore.get(), status: "connecting" });

  ws = new WebSocket(url);
  ws.onopen = () => {
    dashboardStore.set({ ...dashboardStore.get(), status: "connected" });
  };
  ws.onmessage = (e: MessageEvent) => {
    const data = JSON.parse(e.data);
    if (data.type === "snapshot") {
      dashboardStore.set({
        ...dashboardStore.get(),
        groups: data.groups ?? [],
      });
    }
  };
  ws.onclose = () => {
    ws = null;
    dashboardStore.set({ ...dashboardStore.get(), status: "disconnected" });
    if (!intentionalClose) {
      retryTimer = setTimeout(() => connectDashboard(url), 3000);
    }
  };
  ws.onerror = () => {
    dashboardStore.set({ ...dashboardStore.get(), status: "disconnected" });
  };
}

export function disconnectDashboard(): void {
  intentionalClose = true;
  if (retryTimer) clearTimeout(retryTimer);
  if (ws) ws.close();
  ws = null;
  dashboardStore.set({ groups: [], status: "disconnected" });
}

export function deleteDashboardKey(group: string, key: string): void {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify({ action: "delete", group, key }));
  }
}
