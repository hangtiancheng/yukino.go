import { cn } from "@/lib/utils";

interface DivProps {
  className?: string;
  children?: unknown;
}

export function Card({ className, children }: DivProps) {
  return (
    <div
      className={cn(
        "border-border/70 bg-card text-card-foreground shadow-primary/10 rounded-2xl border shadow-xl",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function CardHeader({ className, children }: DivProps) {
  return (
    <div className={cn("flex flex-col gap-1.5 p-6", className)}>{children}</div>
  );
}

export function CardTitle({ className, children }: DivProps) {
  return (
    <h2 className={cn("text-2xl font-semibold tracking-tight", className)}>
      {children}
    </h2>
  );
}

export function CardDescription({ className, children }: DivProps) {
  return (
    <p className={cn("text-muted-foreground text-sm", className)}>{children}</p>
  );
}

export function CardContent({ className, children }: DivProps) {
  return <div className={cn("p-6", className)}>{children}</div>;
}

export function CardFooter({ className, children }: DivProps) {
  return (
    <div className={cn("flex items-center p-6 pt-0", className)}>
      {children}
    </div>
  );
}

export type BadgeVariant =
  "default" | "secondary" | "outline" | "success" | "warning" | "destructive";

const BADGE_VARIANTS: Record<BadgeVariant, string> = {
  default: "bg-primary text-primary-foreground border-transparent",
  secondary: "bg-secondary text-secondary-foreground border-transparent",
  outline: "text-foreground border-border",
  success: "bg-success/15 text-success border-transparent",
  warning: "bg-warning/15 text-warning border-transparent",
  destructive: "bg-destructive/15 text-destructive border-transparent",
};

export interface BadgeProps {
  variant?: BadgeVariant;
  className?: string;
  children?: unknown;
}

export function Badge({
  variant = "default",
  className,
  children,
}: BadgeProps) {
  return (
    <span
      className={cn(
        "inline-flex w-fit items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium whitespace-nowrap",
        BADGE_VARIANTS[variant],
        className,
      )}
    >
      {children}
    </span>
  );
}
