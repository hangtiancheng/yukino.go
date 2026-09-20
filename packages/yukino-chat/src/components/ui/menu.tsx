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

import { html, nothing } from "@yukino.js/lit-jsx";
import { customElement, property, state } from "@yukino.js/lit-jsx";
import { TwElement } from "@/styles/base";
import { cn } from "@/lib/utils";

/**
 * Dropdown menu. The trigger goes in slot "trigger"; items are default
 * slotted and the menu closes when one is clicked, when clicking outside,
 * or on Escape.
 */
@customElement("x-menu")
export class XMenu extends TwElement {
  @state() private open = false;
  @property() align: "start" | "end" = "end";

  #docClick = (e: MouseEvent) => {
    if (!this.open) return;
    if (!e.composedPath().includes(this)) this.open = false;
  };

  #escHandler = (e: KeyboardEvent) => {
    if (e.key === "Escape" && this.open) this.open = false;
  };

  override connectedCallback() {
    super.connectedCallback();
    document.addEventListener("click", this.#docClick, true);
    window.addEventListener("keydown", this.#escHandler);
  }

  override disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener("click", this.#docClick, true);
    window.removeEventListener("keydown", this.#escHandler);
  }

  render() {
    return html`<div class="relative">
      <span @click=${() => (this.open = !this.open)}
        ><slot name="trigger"></slot
      ></span>
      ${
        this.open
          ? html`<div
              class=${cn(
                "bg-popover text-popover-foreground animate-in fade-in zoom-in-95 border-border absolute top-full z-50 mt-1.5 min-w-44 overflow-hidden rounded-xl border p-1 shadow-lg duration-150",
                this.align === "end" ? "right-0" : "left-0",
              )}
              @click=${() => (this.open = false)}
            >
              <slot></slot>
            </div>`
          : nothing
      }
    </div>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "x-menu": XMenu;
  }
}

export interface MenuItemProps {
  destructive?: boolean;
  onClick?: (e: Event) => void;
  children?: unknown;
}

export function MenuItem({ destructive, onClick, children }: MenuItemProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "hover:bg-accent flex w-full cursor-pointer items-center rounded-lg px-2.5 py-1.5 text-left text-sm transition-colors",
        destructive
          ? "text-destructive hover:bg-destructive/10"
          : "text-foreground",
      )}
    >
      {children}
    </button>
  );
}

export function MenuSeparator() {
  return <div className="bg-border my-1 h-px"></div>;
}

export function MenuLabel({ children }: { children?: unknown }) {
  return (
    <div className="text-primary-deep px-2.5 pt-1.5 pb-1 text-[11px] font-semibold tracking-wider uppercase">
      {children}
    </div>
  );
}

/** CSS-only hover/focus tooltip. */
export function Tooltip({
  label,
  side = "right",
  className,
  children,
}: {
  label: string;
  side?: "top" | "bottom" | "left" | "right";
  className?: string;
  children?: unknown;
}) {
  const positions = {
    top: "bottom-full left-1/2 -translate-x-1/2 mb-1.5",
    bottom: "top-full left-1/2 -translate-x-1/2 mt-1.5",
    left: "right-full top-1/2 -translate-y-1/2 mr-1.5",
    right: "left-full top-1/2 -translate-y-1/2 ml-1.5",
  } as const;
  return (
    <span className={cn("group/tt relative inline-flex", className)}>
      {children}
      <span
        role="tooltip"
        className={cn(
          "bg-foreground text-background pointer-events-none absolute z-50 rounded-md px-2 py-1 text-xs font-medium opacity-0 shadow-md transition-all duration-150 group-focus-within/tt:opacity-100 group-hover/tt:opacity-100",
          positions[side],
        )}
      >
        {label}
      </span>
    </span>
  );
}
