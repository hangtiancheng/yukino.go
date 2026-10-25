import { Globe, Search, X } from "lucide-react";
import { useState } from "react";

import { MealCard } from "@/components/shared/meal-card";
import { collectSlotEntries } from "@/components/shared/slot-chips";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { usePublicMeals } from "@/hooks/queries";

export default function PublicMealsPage() {
  const meals = usePublicMeals();
  const [keyword, setKeyword] = useState("");

  const list = (meals.data ?? []).filter((meal) => {
    const needle = keyword.trim().toLowerCase();
    if (!needle) return true;
    if (meal.name.toLowerCase().includes(needle)) return true;
    return collectSlotEntries(meal).some((entry) =>
      entry.value.toLowerCase().includes(needle),
    );
  });

  return (
    <div className="mx-auto max-w-6xl space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <span className="bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-xl">
            <Globe className="size-5" />
          </span>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-semibold tracking-tight">
                Public meal library
              </h2>
              {!meals.isPending && !meals.isError && (
                <Badge
                  variant="secondary"
                  className="rounded-full px-2 py-0.5 text-xs font-medium tabular-nums"
                >
                  {keyword
                    ? `${list.length} of ${meals.data?.length ?? 0} dishes`
                    : `${list.length} ${list.length === 1 ? "dish" : "dishes"}`}
                </Badge>
              )}
            </div>
            <p className="text-muted-foreground text-sm">
              Curated dishes shared by the platform. Read-only, and used when
              the chat source mode is public.
            </p>
          </div>
        </div>
        <div className="relative w-full sm:w-64">
          <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
          <Input
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            placeholder="Filter by name or tag…"
            className="h-9 pr-8 pl-8"
          />
          {keyword && (
            <button
              type="button"
              onClick={() => setKeyword("")}
              aria-label="Clear filter"
              className="text-muted-foreground hover:text-foreground absolute top-1/2 right-2 -translate-y-1/2 rounded-sm transition-colors"
            >
              <X className="size-3.5" />
            </button>
          )}
        </div>
      </div>

      {meals.isPending ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }).map((_, index) => (
            <Skeleton key={index} className="h-40 rounded-xl" />
          ))}
        </div>
      ) : meals.isError ? (
        <Card>
          <CardContent className="text-muted-foreground py-10 text-center text-sm">
            Failed to load public meals:{" "}
            {meals.error instanceof Error
              ? meals.error.message
              : "unknown error"}
          </CardContent>
        </Card>
      ) : list.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-14 text-center">
            <span className="bg-primary/10 text-primary flex size-12 items-center justify-center rounded-2xl">
              <Globe className="size-6" />
            </span>
            <p className="font-medium">
              {keyword
                ? "No meals match your filter"
                : "The public library is empty"}
            </p>
            <p className="text-muted-foreground text-sm">
              {keyword
                ? "Try a different keyword."
                : "Import db/diet_db.sql to seed the public library."}
            </p>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {list.map((meal) => (
            <MealCard key={meal.id} meal={meal} showScore={false} />
          ))}
        </div>
      )}
    </div>
  );
}
