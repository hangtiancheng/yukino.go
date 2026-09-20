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
import { customElement, property } from "@yukino.js/lit-jsx";
import { TwElement } from "@/styles/base";

/**
 * Modal dialog controlled by the `open` property. Notifies via the
 * `onClose` callback property (backdrop click or Escape).
 * Children are slotted into the centered panel.
 */
@customElement("x-dialog")
export class XDialog extends TwElement {
  @property({ type: Boolean }) open = false;
  onClose?: () => void;

  #escHandler = (e: KeyboardEvent) => {
    if (e.key === "Escape" && this.open) {
      e.stopPropagation();
      this.onClose?.();
    }
  };

  override connectedCallback() {
    super.connectedCallback();
    window.addEventListener("keydown", this.#escHandler);
  }

  override disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("keydown", this.#escHandler);
  }

  render() {
    if (!this.open) return nothing;
    return html`<div
      class="bg-foreground/30 fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm"
      @click=${() => this.onClose?.()}
    >
      <div
        class="bg-card animate-in fade-in zoom-in-95 relative w-full max-w-md rounded-2xl border p-6 shadow-2xl duration-200"
        @click=${(e: Event) => e.stopPropagation()}
      >
        <slot></slot>
      </div>
    </div>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "x-dialog": XDialog;
  }
}

export function DialogHeader({ children }: { children?: unknown }) {
  return <div className="mb-4 flex flex-col gap-1.5">{children}</div>;
}

export function DialogTitle({ children }: { children?: unknown }) {
  return (
    <h3 className="text-foreground text-lg leading-none font-semibold">
      {children}
    </h3>
  );
}

export function DialogFooter({ children }: { children?: unknown }) {
  return (
    <div className="mt-5 flex flex-row-reverse items-center gap-2">
      {children}
    </div>
  );
}

export function InfoRow({
  label,
  value,
}: {
  label: string;
  value: string | number;
}) {
  return (
    <div className="border-border flex justify-between gap-4 border-b py-1.5">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-foreground truncate font-medium">{value}</span>
    </div>
  );
}
