import {
  LitElement,
  customElement,
  property,
  query,
  type PropertyValues,
} from "@yukino.js/lit-jsx";
import { repeat } from "lit/directives/repeat.js";
import { LoaderCircle, Sparkles } from "lucide";
import { icon } from "./icons.js";
import type { ChatMessage } from "../chat/chat-store.js";
import "./md-render.js";

@customElement("msg-list")
export class MsgList extends LitElement {
  @property({ attribute: false })
  messages: ChatMessage[] = [];

  @property({ type: Boolean })
  isStreaming = false;

  @query("[data-scroller]")
  private _scroller?: HTMLDivElement;

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  protected updated(changedProperties: PropertyValues) {
    if (changedProperties.has("messages") && this._scroller) {
      this._scroller.scrollTop = this._scroller.scrollHeight;
    }
  }

  render() {
    return (
      <div data-scroller class="flex-1 overflow-y-auto px-6 py-6">
        <div class="mx-auto w-full max-w-3xl">
          {repeat(
            this.messages,
            (_, i) => i,
            (m) => this.#renderMessage(m),
          )}
        </div>
      </div>
    );
  }

  #renderMessage(message: ChatMessage) {
    if (message.type === "user") {
      return (
        <div class="mb-6 flex flex-col items-end">
          <div class="bg-blush-900 text-blush-50 max-w-[75%] rounded-2xl rounded-br-md px-4 py-2.5 text-sm whitespace-pre-wrap shadow-sm">
            {message.content}
          </div>
        </div>
      );
    }
    return (
      <div class="mb-6 flex items-start gap-3">
        <div class="from-blush-400 to-blush-600 ring-blush-100 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-linear-to-br shadow-sm ring-2">
          {icon(Sparkles, "h-5 w-5 text-white")}
        </div>
        <div class="min-w-0 flex-1">
          {message.detail && message.detail.length > 0 ? (
            <details class="border-blush-200 bg-blush-50 mb-3 rounded-xl border px-4 py-3 text-sm">
              <summary class="text-blush-600 hover:text-blush-500 cursor-pointer font-medium transition">
                View details ({message.detail.length} steps)
              </summary>
              <div class="mt-2.5 flex flex-col gap-2">
                {message.detail.map((d, idx) => (
                  <div
                    key={idx}
                    class="border-blush-400 text-ink/80 rounded-r-lg border-l-2 bg-white/70 p-2.5 text-xs"
                  >
                    <strong class="text-blush-600 font-semibold">
                      Step {idx + 1}:
                    </strong>
                    <md-render
                      content={d}
                      mdClass="max-w-none text-xs leading-relaxed wrap-break-word text-ink/80"
                    ></md-render>
                  </div>
                ))}
              </div>
            </details>
          ) : null}
          <div class="text-ink text-sm">
            {message.pending ? (
              <div class="text-ink-soft flex items-center gap-2 py-1">
                {icon(LoaderCircle, "h-4 w-4 animate-spin text-blush-500")}
                <span>Thinking…</span>
              </div>
            ) : (
              <md-render content={message.content}></md-render>
            )}
          </div>
        </div>
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "msg-list": MsgList;
  }
}
