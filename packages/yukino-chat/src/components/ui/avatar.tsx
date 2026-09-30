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
