export const SLOT_LABELS: Record<string, string> = {
  mealTime: "Meal Time",
  mood: "Mood",
  scene: "Scene",
  healthGoal: "Health Goal",
  cuisine: "Cuisine",
  taste: "Taste",
  convenience: "Convenience",
};

export const SLOT_KEYS = Object.keys(SLOT_LABELS) as (keyof typeof SLOT_LABELS &
  string)[];

export const INTENTS = [
  "MEAL_RECOMMENDATION",
  "CLARIFY_NEEDED",
  "MEAL_ADJUST",
  "MEAL_PLAN",
  "HEALTH_RISK",
  "OTHER",
] as const;

export function formatDateTime(value?: string | null): string {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function toLocalInputValue(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function defaultRange(): { startAt: string; endAt: string } {
  const end = new Date();
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000);
  return { startAt: toLocalInputValue(start), endAt: toLocalInputValue(end) };
}

export function formatScore(value?: number | null): string {
  if (value === null || value === undefined) return "-";
  return value.toFixed(1);
}

export function safePrettyJson(value: unknown): string {
  if (value === null || value === undefined || value === "") return "";
  try {
    const parsed = typeof value === "string" ? JSON.parse(value) : value;
    return JSON.stringify(parsed, null, 2);
  } catch {
    return String(value);
  }
}

export function shortId(value: string, size = 14): string {
  if (!value) return "-";
  return value.length <= size ? value : `${value.slice(0, size)}…`;
}
