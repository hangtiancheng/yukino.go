import { cn } from "@/lib/utils";
import { icon, icons } from "@/components/icons";

export interface CheckboxProps {
  checked?: boolean;
  className?: string;
  ariaLabel?: string;
  onCheckedChange?: (checked: boolean) => void;
}

export function Checkbox({
  checked = false,
  className,
  ariaLabel,
  onCheckedChange,
}: CheckboxProps) {
  return (
    <input
      type="checkbox"
      checked={checked}
      aria-label={ariaLabel}
      onChange={(e: Event) =>
        onCheckedChange?.((e.target as HTMLInputElement).checked)
      }
      className={cn(
        "accent-primary-deep border-input size-4 shrink-0 cursor-pointer rounded transition-colors",
        className,
      )}
    />
  );
}

export interface RadioGroupOption {
  value: string;
  label: string;
}

export interface RadioGroupProps {
  name: string;
  value?: string;
  options: RadioGroupOption[];
  onValueChange?: (value: string) => void;
  className?: string;
}

export function RadioGroup({
  name,
  value,
  options,
  onValueChange,
  className,
}: RadioGroupProps) {
  return (
    <div role="radiogroup" className={cn("flex gap-4", className)}>
      {options.map((option) => (
        <label
          key={option.value}
          className="text-foreground flex cursor-pointer items-center gap-2 text-sm"
        >
          <input
            type="radio"
            name={name}
            value={option.value}
            checked={value === option.value}
            onChange={() => onValueChange?.(option.value)}
            className="accent-primary-deep size-4 cursor-pointer"
          />
          {option.label}
        </label>
      ))}
    </div>
  );
}

/** Collapsible section header + body used by the sidebars. */
export interface CollapsibleSectionProps {
  title: string;
  count: number;
  open: boolean;
  onToggle?: () => void;
  children?: unknown;
}

export function CollapsibleSection({
  title,
  count,
  open,
  onToggle,
  children,
}: CollapsibleSectionProps) {
  return (
    <div>
      <button
        type="button"
        aria-expanded={open ? "true" : "false"}
        aria-label={`${title} (${count})`}
        onClick={onToggle}
        className="hover:bg-accent/50 flex w-full cursor-pointer items-center justify-between border-b px-3 py-2.5 text-sm font-medium transition-colors"
      >
        <span>
          {title}
          <span className="text-muted-foreground ml-2 text-xs font-normal">
            {count}
          </span>
        </span>
        <span
          className={cn(
            "text-muted-foreground transition-transform duration-200",
            open && "rotate-180",
          )}
        >
          {icon(icons.ChevronDown, "size-4")}
        </span>
      </button>
      {open && (
        <div className="animate-in fade-in slide-in-from-top-1 overflow-hidden duration-200">
          {children}
        </div>
      )}
    </div>
  );
}
