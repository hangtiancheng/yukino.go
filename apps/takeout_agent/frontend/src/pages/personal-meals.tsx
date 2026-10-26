import { Pencil, Plus, Search, Trash2, UtensilsCrossed, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { MealCard } from "@/components/shared/meal-card";
import { collectSlotEntries } from "@/components/shared/slot-chips";
import { SlotPicker } from "@/components/shared/slot-picker";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useCreateMeal,
  useDeleteMeal,
  usePersonalMeals,
  useSlotOptions,
  useUpdateMeal,
} from "@/hooks/queries";
import { ApiError } from "@/lib/api";
import { SLOT_KEYS, SLOT_LABELS } from "@/lib/format";
import { mealRequestSchema, type MealResponse } from "@/lib/schemas";

type SlotDraft = Record<string, string[]>;

function emptyDraft(): SlotDraft {
  const draft: SlotDraft = { name: [] };
  for (const slot of SLOT_KEYS) {
    draft[slot] = [];
  }
  return draft;
}

function toDraft(meal: MealResponse): SlotDraft {
  const draft = emptyDraft();
  for (const slot of SLOT_KEYS) {
    const values = meal[slot as keyof MealResponse];
    draft[slot] = Array.isArray(values) ? [...values] : [];
  }
  return draft;
}

export default function PersonalMealsPage() {
  const meals = usePersonalMeals();
  const slotOptions = useSlotOptions();
  const createMeal = useCreateMeal();
  const updateMeal = useUpdateMeal();
  const deleteMeal = useDeleteMeal();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<MealResponse | null>(null);
  const [name, setName] = useState("");
  const [draft, setDraft] = useState<SlotDraft>(emptyDraft);
  const [confirmDelete, setConfirmDelete] = useState<MealResponse | null>(null);
  const [keyword, setKeyword] = useState("");

  const options = slotOptions.data ?? {};
  const busy = createMeal.isPending || updateMeal.isPending;

  const openCreate = () => {
    setEditing(null);
    setName("");
    setDraft(emptyDraft());
    setDialogOpen(true);
  };

  const openEdit = (meal: MealResponse) => {
    setEditing(meal);
    setName(meal.name);
    setDraft(toDraft(meal));
    setDialogOpen(true);
  };

  const submit = () => {
    const parsed = mealRequestSchema.safeParse({
      name,
      ...Object.fromEntries(SLOT_KEYS.map((slot) => [slot, draft[slot] ?? []])),
    });
    if (!parsed.success) {
      toast.error(parsed.error.issues[0]?.message ?? "Please fix the form.");
      return;
    }
    const onError = (error: unknown) =>
      toast.error(
        error instanceof ApiError ? error.message : "Failed to save the meal.",
      );
    if (editing) {
      updateMeal.mutate(
        { mealId: editing.id, payload: parsed.data },
        {
          onSuccess: () => {
            toast.success(`Updated "${parsed.data.name}".`);
            setDialogOpen(false);
          },
          onError,
        },
      );
    } else {
      createMeal.mutate(parsed.data, {
        onSuccess: () => {
          toast.success(`Added "${parsed.data.name}" to your library.`);
          setDialogOpen(false);
        },
        onError,
      });
    }
  };

  const remove = (meal: MealResponse) => {
    deleteMeal.mutate(meal.id, {
      onSuccess: () => {
        toast.success(`Deleted "${meal.name}".`);
        setConfirmDelete(null);
      },
      onError: (error) =>
        toast.error(
          error instanceof ApiError
            ? error.message
            : "Failed to delete the meal.",
        ),
    });
  };

  const allMeals = useMemo(() => meals.data ?? [], [meals.data]);
  const list = useMemo(() => {
    const needle = keyword.trim().toLowerCase();
    if (!needle) return allMeals;
    return allMeals.filter((meal) => {
      if (meal.name.toLowerCase().includes(needle)) return true;
      return collectSlotEntries(meal).some((entry) =>
        entry.value.toLowerCase().includes(needle),
      );
    });
  }, [allMeals, keyword]);

  useEffect(() => {
    if (!dialogOpen) {
      setEditing(null);
    }
  }, [dialogOpen]);

  return (
    <div className="mx-auto max-w-6xl space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <span className="bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-xl">
            <UtensilsCrossed className="size-5" />
          </span>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="text-lg font-semibold tracking-tight">
                Personal meal library
              </h2>
              {!meals.isPending && !meals.isError && (
                <Badge
                  variant="secondary"
                  className="rounded-full px-2 py-0.5 text-xs font-medium tabular-nums"
                >
                  {keyword
                    ? `${list.length} of ${allMeals.length} meals`
                    : `${allMeals.length} ${allMeals.length === 1 ? "meal" : "meals"}`}
                </Badge>
              )}
            </div>
            <p className="text-muted-foreground text-sm">
              Dishes you added yourself. The agent ranks these first when the
              source mode is personal.
            </p>
          </div>
        </div>
        <div className="flex w-full items-center gap-2 sm:w-auto">
          <div className="relative min-w-0 flex-1 sm:w-64 sm:flex-none">
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
          <Button onClick={openCreate} className="shrink-0">
            <Plus />
            New meal
          </Button>
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
            Failed to load personal meals:{" "}
            {meals.error instanceof Error
              ? meals.error.message
              : "unknown error"}
          </CardContent>
        </Card>
      ) : allMeals.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-14 text-center">
            <span className="bg-primary/10 text-primary flex size-12 items-center justify-center rounded-2xl">
              <UtensilsCrossed className="size-6" />
            </span>
            <div>
              <p className="font-medium">Your library is empty</p>
              <p className="text-muted-foreground mt-1 text-sm">
                Add the dishes you cook or order often so recommendations feel
                personal.
              </p>
            </div>
            <Button variant="outline" size="sm" onClick={openCreate}>
              <Plus />
              Add your first meal
            </Button>
          </CardContent>
        </Card>
      ) : list.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center gap-3 py-14 text-center">
            <span className="bg-primary/10 text-primary flex size-12 items-center justify-center rounded-2xl">
              <Search className="size-6" />
            </span>
            <p className="font-medium">No meals match your filter</p>
            <p className="text-muted-foreground text-sm">
              Try a different keyword.
            </p>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {list.map((meal) => (
            <MealCard
              key={meal.id}
              meal={meal}
              showScore={false}
              footer={
                <div className="flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    className="size-7"
                    aria-label={`Edit ${meal.name}`}
                    onClick={() => openEdit(meal)}
                  >
                    <Pencil className="size-3.5" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="text-muted-foreground hover:text-destructive size-7"
                    aria-label={`Delete ${meal.name}`}
                    onClick={() => setConfirmDelete(meal)}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              }
            />
          ))}
        </div>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editing ? "Edit meal" : "New meal"}</DialogTitle>
            <DialogDescription>
              Tag the dish with slot values. More accurate tags lead to better
              ranking.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-5 py-1">
            <div className="space-y-2">
              <Label htmlFor="meal-name">
                Name<span className="text-destructive ml-1">*</span>
              </Label>
              <Input
                id="meal-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="e.g. Steamed salmon with quinoa"
              />
            </div>

            {SLOT_KEYS.map((slot) => (
              <SlotPicker
                key={slot}
                slot={slot}
                label={SLOT_LABELS[slot]}
                options={options[slot] ?? []}
                selected={draft[slot] ?? []}
                required={slot === "mealTime"}
                onChange={(values) =>
                  setDraft((prev) => ({ ...prev, [slot]: values }))
                }
              />
            ))}
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDialogOpen(false)}
              disabled={busy}
            >
              Cancel
            </Button>
            <Button onClick={submit} disabled={busy}>
              {busy ? "Saving…" : editing ? "Save changes" : "Create meal"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={confirmDelete !== null}
        onOpenChange={(open) => !open && setConfirmDelete(null)}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Delete this meal?</DialogTitle>
            <DialogDescription>
              "{confirmDelete?.name}" will be removed from your personal
              library. This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmDelete(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => confirmDelete && remove(confirmDelete)}
              disabled={deleteMeal.isPending}
            >
              {deleteMeal.isPending ? "Deleting…" : "Delete"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
