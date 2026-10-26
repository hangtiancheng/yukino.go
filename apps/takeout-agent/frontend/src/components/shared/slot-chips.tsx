import { Badge } from "@/components/ui/badge";
import { SLOT_KEYS, SLOT_LABELS } from "@/lib/format";
import type { MealResponse } from "@/lib/schemas";
import { cn } from "@/lib/utils";

export interface SlotEntry {
  slot: string;
  value: string;
}

// One soft tone per slot dimension so meal cards read at a glance.
const SLOT_TONES: Record<string, string> = {
  mealTime:
    "border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-400",
  mood: "border-pink-500/25 bg-pink-500/10 text-pink-700 dark:text-pink-400",
  scene: "border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-400",
  healthGoal:
    "border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
  cuisine:
    "border-orange-500/25 bg-orange-500/10 text-orange-700 dark:text-orange-400",
  taste: "border-rose-500/25 bg-rose-500/10 text-rose-700 dark:text-rose-400",
  convenience:
    "border-violet-500/25 bg-violet-500/10 text-violet-700 dark:text-violet-400",
};

export function slotTone(slot: string): string {
  return SLOT_TONES[slot] ?? "";
}

export function collectSlotEntries(meal: MealResponse): SlotEntry[] {
  const entries: SlotEntry[] = [];
  for (const slot of SLOT_KEYS) {
    const values = meal[slot as keyof MealResponse];
    if (Array.isArray(values)) {
      for (const value of values) {
        entries.push({ slot, value });
      }
    }
  }
  return entries;
}

export function SlotChips({
  meal,
  max = 10,
  className,
}: {
  meal: MealResponse;
  max?: number;
  className?: string;
}) {
  const entries = collectSlotEntries(meal);
  if (entries.length === 0) {
    return <p className="text-muted-foreground text-xs">No slot tags yet.</p>;
  }
  const visible = entries.slice(0, max);
  const hidden = entries.length - visible.length;

  return (
    <div className={cn("flex flex-wrap gap-1", className)}>
      {visible.map((entry, index) => (
        <Badge
          key={`${entry.slot}-${entry.value}-${index}`}
          variant="outline"
          className={cn("font-normal", slotTone(entry.slot))}
        >
          <span className="opacity-70">
            {SLOT_LABELS[entry.slot] ?? entry.slot}:
          </span>
          {entry.value}
        </Badge>
      ))}
      {hidden > 0 && (
        <Badge variant="outline" className="text-muted-foreground font-normal">
          +{hidden} more
        </Badge>
      )}
    </div>
  );
}
