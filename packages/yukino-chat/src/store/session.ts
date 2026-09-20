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
import type { SessionItem } from "../types";

export interface SessionSnapshot {
  userSessions: SessionItem[];
  groupSessions: SessionItem[];
  refreshTick: number;
}

export const sessionStore = signal<SessionSnapshot>({
  userSessions: [],
  groupSessions: [],
  refreshTick: 0,
});

export function setUserSessions(list: SessionItem[]): void {
  sessionStore.set({ ...sessionStore.get(), userSessions: list || [] });
}

export function setGroupSessions(list: SessionItem[]): void {
  sessionStore.set({ ...sessionStore.get(), groupSessions: list || [] });
}

export function bumpSessionRefresh(): void {
  sessionStore.set({
    ...sessionStore.get(),
    refreshTick: sessionStore.get().refreshTick + 1,
  });
}

export function clearSessions(): void {
  sessionStore.set({ userSessions: [], groupSessions: [], refreshTick: 0 });
}
