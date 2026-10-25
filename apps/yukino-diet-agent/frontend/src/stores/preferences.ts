import { Store } from "@tanstack/react-store";

export type ThemeMode = "light" | "dark";

export interface PreferencesState {
  theme: ThemeMode;
  sidebarCollapsed: boolean;
}

const THEME_KEY = "***";
const SIDEBAR_KEY = "diet.s…psed";

function readTheme(): ThemeMode {
  const stored = localStorage.getItem(THEME_KEY);
  if (stored === "light" || stored === "dark") return stored;
  if (
    typeof window !== "undefined" &&
    window.matchMedia?.("(prefers-color-scheme: dark)").matches
  ) {
    return "dark";
  }
  return "light";
}

function readSidebarCollapsed(): boolean {
  return localStorage.getItem(SIDEBAR_KEY) === "1";
}

export const preferencesStore = new Store<PreferencesState>({
  theme: readTheme(),
  sidebarCollapsed: readSidebarCollapsed(),
});

export function applyTheme(theme: ThemeMode): void {
  document.documentElement.classList.toggle("dark", theme === "dark");
}

export function setTheme(theme: ThemeMode): void {
  preferencesStore.setState((prev) => ({ ...prev, theme }));
  localStorage.setItem(THEME_KEY, theme);
  applyTheme(theme);
}

export function toggleTheme(): void {
  setTheme(preferencesStore.state.theme === "dark" ? "light" : "dark");
}

export function toggleSidebar(): void {
  preferencesStore.setState((prev) => {
    const sidebarCollapsed = !prev.sidebarCollapsed;
    localStorage.setItem(SIDEBAR_KEY, sidebarCollapsed ? "1" : "0");
    return { ...prev, sidebarCollapsed };
  });
}
