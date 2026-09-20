/**
 * Copyright (c) 2026 hangtiancheng
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

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
