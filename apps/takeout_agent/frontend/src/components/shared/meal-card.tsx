import { Globe, Sparkles, UserRound } from "lucide-react";

import { SlotChips } from "@/components/shared/slot-chips";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import type { MealResponse } from "@/lib/schemas";
import { cn } from "@/lib/utils";

interface MealCardProps {
  meal: MealResponse;
  showScore?: boolean;
  footer?: React.ReactNode;
  className?: string;
}

export function MealCard({
  meal,
  showScore = true,
  footer,
  className,
}: MealCardProps) {
  const isPersonal = meal.sourceType === "PERSONAL";

  return (
    <Card
      className={cn(
        "hover:border-primary/30 flex h-full flex-col gap-3 rounded-xl transition-[border-color,box-shadow] hover:shadow-md",
        className,
      )}
    >
      <CardHeader className="pb-0">
        <div className="flex items-start justify-between gap-2">
          <div className="flex min-w-0 items-start gap-2.5">
            <span
              className={cn(
                "flex size-9 shrink-0 items-center justify-center rounded-lg",
                isPersonal
                  ? "bg-primary/10 text-primary"
                  : "bg-sky-500/10 text-sky-600 dark:text-sky-400",
              )}
            >
              {isPersonal ? (
                <UserRound className="size-4.5" />
              ) : (
                <Globe className="size-4.5" />
              )}
            </span>
            <CardTitle className="line-clamp-2 text-base leading-snug">
              {meal.name}
            </CardTitle>
          </div>
          <Badge
            variant={isPersonal ? "default" : "outline"}
            className="shrink-0"
          >
            {isPersonal ? "Personal" : "Public"}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="flex-1">
        <SlotChips meal={meal} />
      </CardContent>
      {(showScore || footer) && (
        <CardFooter className="mt-auto flex items-center justify-between gap-2 pt-0">
          {showScore ? (
            <span className="text-primary inline-flex items-center gap-1.5 text-sm font-medium">
              <Sparkles className="size-3.5" />
              {meal.matchScore > 0
                ? `${Math.round(meal.matchScore * 100)}% match`
                : "Base item"}
            </span>
          ) : (
            <span />
          )}
          {footer}
        </CardFooter>
      )}
    </Card>
  );
}
