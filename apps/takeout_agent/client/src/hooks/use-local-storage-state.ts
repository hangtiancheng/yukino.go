import { useCallback, useSyncExternalStore } from "react";
import type { SetStateAction } from "react";

type Listener = () => void;

const listeners = new Map<string, Set<Listener>>();

function notify(key: string) {
  listeners.get(key)?.forEach((listener) => listener());
}

function subscribe(key: string, listener: Listener) {
  let keyListeners = listeners.get(key);
  if (!keyListeners) {
    keyListeners = new Set();
    listeners.set(key, keyListeners);
  }
  keyListeners.add(listener);

  const onStorage = (event: StorageEvent) => {
    if (event.key === null || event.key === key) listener();
  };
  window.addEventListener("storage", onStorage);

  return () => {
    keyListeners.delete(listener);
    if (keyListeners.size === 0) listeners.delete(key);
    window.removeEventListener("storage", onStorage);
  };
}

export function useLocalStorageState(key: string, defaultValue?: string) {
  const subscribeToKey = useCallback(
    (listener: Listener) => subscribe(key, listener),
    [key],
  );
  const read = useCallback(() => localStorage.getItem(key), [key]);
  const stored = useSyncExternalStore(subscribeToKey, read, () => null);

  const setValue = useCallback(
    (next: SetStateAction<string | undefined>) => {
      const prev = localStorage.getItem(key) ?? undefined;
      const value = typeof next === "function" ? next(prev) : next;
      if (value === undefined) {
        localStorage.removeItem(key);
      } else {
        localStorage.setItem(key, value);
      }
      notify(key);
    },
    [key],
  );

  return [stored ?? defaultValue, setValue] as const;
}
