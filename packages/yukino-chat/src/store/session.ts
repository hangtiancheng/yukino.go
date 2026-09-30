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
