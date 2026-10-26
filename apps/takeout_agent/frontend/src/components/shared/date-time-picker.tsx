import { CalendarIcon } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Separator } from "@/components/ui/separator";
import { toLocalInputValue } from "@/lib/format";
import { cn } from "@/lib/utils";

const pad = (value: number) => String(value).padStart(2, "0");
const clamp = (value: number, min: number, max: number) =>
  Math.min(max, Math.max(min, value));

function parseLocalDateTime(value: string): Date {
  const [datePart, timePart] = value.split("T");
  const [year, month, day] = (datePart ?? "").split("-").map(Number);
  const [hour, minute] = (timePart ?? "00:00").split(":").map(Number);
  const parsed = new Date(
    year || 1970,
    (month || 1) - 1,
    day || 1,
    hour || 0,
    minute || 0,
  );
  return Number.isNaN(parsed.getTime()) ? new Date() : parsed;
}

function formatDisplay(date: Date): string {
  return date.toLocaleString(undefined, {
    month: "short",
    day: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

interface DateTimePickerProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  disabled?: boolean;
  align?: "start" | "center" | "end";
  className?: string;
}

export function DateTimePicker({
  value,
  onChange,
  placeholder = "Pick a date and time",
  disabled,
  align = "start",
  className,
}: DateTimePickerProps) {
  const selected = parseLocalDateTime(value);
  const [open, setOpen] = useState(false);
  const [hourText, setHourText] = useState(() => pad(selected.getHours()));
  const [minuteText, setMinuteText] = useState(() =>
    pad(selected.getMinutes()),
  );

  useEffect(() => {
    if (open) {
      const current = parseLocalDateTime(value);
      setHourText(pad(current.getHours()));
      setMinuteText(pad(current.getMinutes()));
    }
    // Resync the draft time fields each time the popover opens.
  }, [open]);

  const emit = (date: Date) => {
    onChange(toLocalInputValue(date));
  };

  const commitTime = (nextHourText: string, nextMinuteText: string) => {
    const hour = Number(nextHourText);
    const minute = Number(nextMinuteText);
    if (
      nextHourText === "" ||
      nextMinuteText === "" ||
      Number.isNaN(hour) ||
      Number.isNaN(minute)
    ) {
      return;
    }
    const base = parseLocalDateTime(value);
    emit(
      new Date(
        base.getFullYear(),
        base.getMonth(),
        base.getDate(),
        clamp(Math.trunc(hour), 0, 23),
        clamp(Math.trunc(minute), 0, 59),
      ),
    );
  };

  const handleDaySelect = (day: Date | undefined) => {
    if (!day) return;
    const hour = Number(hourText);
    const minute = Number(minuteText);
    const safeHour = Number.isNaN(hour)
      ? selected.getHours()
      : clamp(Math.trunc(hour), 0, 23);
    const safeMinute = Number.isNaN(minute)
      ? selected.getMinutes()
      : clamp(Math.trunc(minute), 0, 59);
    emit(
      new Date(
        day.getFullYear(),
        day.getMonth(),
        day.getDate(),
        safeHour,
        safeMinute,
      ),
    );
  };

  const handleNow = () => {
    const now = new Date();
    setHourText(pad(now.getHours()));
    setMinuteText(pad(now.getMinutes()));
    emit(now);
  };

  return (
    <Popover open={open} onOpenChange={(nextOpen) => setOpen(nextOpen)}>
      <PopoverTrigger
        disabled={disabled}
        className={cn(
          "border-input bg-background focus-visible:border-ring focus-visible:ring-ring/50 flex h-9 items-center justify-between gap-2 rounded-md border px-3 text-sm shadow-xs transition-[color,box-shadow] outline-none focus-visible:ring-[3px] disabled:cursor-not-allowed disabled:opacity-50",
          className,
        )}
      >
        <span
          className={cn(
            "truncate tabular-nums",
            !value && "text-muted-foreground",
          )}
        >
          {value ? formatDisplay(selected) : placeholder}
        </span>
        <CalendarIcon className="text-muted-foreground size-4 shrink-0 opacity-60" />
      </PopoverTrigger>
      <PopoverContent align={align} className="w-auto p-0">
        <Calendar
          mode="single"
          selected={selected}
          onSelect={handleDaySelect}
          defaultMonth={selected}
        />
        <Separator />
        <div className="flex items-center gap-2 p-3">
          <Label className="text-muted-foreground text-xs">Time</Label>
          <Input
            type="number"
            min={0}
            max={23}
            value={hourText}
            onChange={(event) => {
              setHourText(event.target.value);
              commitTime(event.target.value, minuteText);
            }}
            onBlur={() => setHourText(pad(selected.getHours()))}
            className="h-8 w-14 text-center tabular-nums"
            aria-label="Hour"
          />
          <span className="text-muted-foreground text-sm">:</span>
          <Input
            type="number"
            min={0}
            max={59}
            value={minuteText}
            onChange={(event) => {
              setMinuteText(event.target.value);
              commitTime(hourText, event.target.value);
            }}
            onBlur={() => setMinuteText(pad(selected.getMinutes()))}
            className="h-8 w-14 text-center tabular-nums"
            aria-label="Minute"
          />
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto"
            onClick={handleNow}
          >
            Now
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
