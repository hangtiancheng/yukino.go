import { LitElement, customElement, property } from "@yukino.js/lit-jsx";
import type { OverlayState } from "../chat/chat-store.js";

@customElement("loading-overlay")
export class LoadingOverlay extends LitElement {
  @property({ attribute: false })
  overlay: OverlayState = { show: false, text: "", subtext: "" };

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  render() {
    if (!this.overlay.show) return null;
    return (
      <div class="bg-ink/35 fixed inset-0 z-9999 flex items-center justify-center backdrop-blur-sm">
        <div class="ring-blush-200 rounded-3xl bg-white/95 px-10 py-9 text-center shadow-2xl ring-1">
          <div class="border-blush-200 border-t-blush-500 mx-auto mb-5 h-12 w-12 animate-spin rounded-full border-4"></div>
          <div class="text-ink text-lg font-semibold">{this.overlay.text}</div>
          <div class="text-ink-soft mt-2 text-sm">{this.overlay.subtext}</div>
        </div>
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "loading-overlay": LoadingOverlay;
  }
}
