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

import { html, css, nothing } from "@yukino.js/lit-jsx";
import { customElement, property, state } from "@yukino.js/lit-jsx";
import { TwElement } from "@/styles/base";

/**
 * Avatar element: renders `src`, falling back to the first character of
 * `name` when the image is missing or fails to load.
 *
 * Sizing/shaping classes belong on the host (e.g. className="size-10").
 */
@customElement("x-avatar")
export class XAvatar extends TwElement {
  static override styles = [
    ...TwElement.styles,
    css`
      :host {
        display: inline-block;
      }
    `,
  ];
  @property() src = "";
  @property() name = "";
  @state() private failed = false;

  protected override willUpdate(changed: Map<string, unknown>) {
    if (changed.has("src")) this.failed = false;
  }

  override render() {
    const initial = (this.name.trim().charAt(0) || "?").toUpperCase();
    return html`<span
      class="bg-secondary text-secondary-foreground ring-primary/25 flex size-full items-center justify-center overflow-hidden rounded-full text-sm font-semibold select-none"
    >
      ${
        this.src && !this.failed
          ? html`<img
              class="size-full object-cover"
              .src=${this.src}
              alt=${this.name || "avatar"}
              @error=${() => (this.failed = true)}
            />`
          : html`<span aria-hidden="true">${initial}</span>`
      }${nothing}
    </span>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "x-avatar": XAvatar;
  }
}
