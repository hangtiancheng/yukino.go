import { Check } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

interface SlotPickerProps {
  slot: string;
  label: string;
  options: string[];
  selected: string[];
  onChange: (values: string[]) => void;
  required?: boolean;
}

export function SlotPicker({
  slot,
  label,
  options,
  selected,
  onChange,
  required,
}: SlotPickerProps) {
  const toggle = (value: string) => {
    if (selected.includes(value)) {
      onChange(selected.filter((item) => item !== value));
    } else {
      onChange([...selected, value]);
    }
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <Label className="text-sm font-medium">
          {label}
          {required && <span className="text-destructive ml-1">*</span>}
        </Label>
        {selected.length > 0 && (
          <button
            type="button"
            onClick={() => onChange([])}
            className="text-muted-foreground hover:text-foreground text-xs underline-offset-2 hover:underline"
          >
            Clear
          </button>
        )}
      </div>
      {options.length === 0 ? (
        <p className="text-muted-foreground text-xs">
          No options available for this slot.
        </p>
      ) : (
        <div
          className="flex flex-wrap gap-1.5"
          role="group"
          aria-label={`${label} options`}
        >
          {options.map((option) => {
            const active = selected.includes(option);
            return (
              <Badge
                key={`${slot}-${option}`}
                role="checkbox"
                aria-checked={active}
                tabIndex={0}
                variant={active ? "default" : "outline"}
                className={cn(
                  "cursor-pointer px-2.5 py-1 font-normal transition-colors select-none",
                  !active && "hover:bg-accent hover:text-accent-foreground",
                )}
                onClick={() => toggle(option)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    toggle(option);
                  }
                }}
              >
                {active && <Check className="size-3" />}
                {option}
              </Badge>
            );
          })}
        </div>
      )}
    </div>
  );
}
