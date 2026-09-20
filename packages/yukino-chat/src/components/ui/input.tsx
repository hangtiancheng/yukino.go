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

export interface InputProps {
  id?: string;
  type?: string;
  value?: string;
  placeholder?: string;
  maxLength?: number;
  accept?: string;
  disabled?: boolean;
  className?: string;
  ariaLabel?: string;
  onValue?: (value: string) => void;
  onChange?: (e: Event) => void;
}

export function Input({
  id,
  type = "text",
  value = "",
  placeholder,
  maxLength,
  accept,
  disabled,
  className,
  ariaLabel,
  onValue,
  onChange,
}: InputProps) {
  return (
    <input
      id={id}
      type={type}
      value={value}
      placeholder={placeholder}
      maxLength={maxLength}
      accept={accept}
      disabled={disabled}
      aria-label={ariaLabel}
      onInput={
        onValue
          ? (e: Event) => onValue((e.target as HTMLInputElement).value)
          : undefined
      }
      onChange={onChange}
      className={cn(
        "border-input bg-card placeholder:text-muted-foreground/60 focus-visible:ring-ring/40 flex h-9 w-full rounded-lg border px-3 py-1 text-sm transition-colors focus-visible:ring-2 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
    />
  );
}

export interface TextareaProps {
  id?: string;
  value?: string;
  placeholder?: string;
  rows?: number;
  maxLength?: number;
  className?: string;
  onValue?: (value: string) => void;
  onKeyDown?: (e: KeyboardEvent) => void;
}

export function Textarea({
  id,
  value = "",
  placeholder,
  rows = 3,
  maxLength,
  className,
  onValue,
  onKeyDown,
}: TextareaProps) {
  return (
    <textarea
      id={id}
      rows={rows}
      value={value}
      placeholder={placeholder}
      maxLength={maxLength}
      onInput={
        onValue
          ? (e: Event) => onValue((e.target as HTMLTextAreaElement).value)
          : undefined
      }
      onKeyDown={onKeyDown}
      className={cn(
        "border-input bg-card placeholder:text-muted-foreground/60 focus-visible:ring-ring/40 flex w-full resize-none rounded-lg border px-3 py-2 text-sm transition-colors focus-visible:ring-2 focus-visible:outline-none",
        className,
      )}
    />
  );
}

export interface LabelProps {
  htmlFor?: string;
  className?: string;
  children?: unknown;
}

export function Label({ htmlFor, className, children }: LabelProps) {
  return (
    <label
      htmlFor={htmlFor}
      className={cn(
        "text-foreground text-sm leading-none font-medium select-none",
        className,
      )}
    >
      {children}
    </label>
  );
}
