import { LitElement, customElement, property } from "@yukino.js/lit-jsx";
import { repeat } from "lit/directives/repeat.js";
import { Plus, Sparkles, X } from "lucide";
import { icon } from "./icons.js";
import type { ChatHistory } from "../chat/chat-store.js";

@customElement("chat-sidebar")
export class ChatSidebar extends LitElement {
  @property({ attribute: false })
  histories: ChatHistory[] = [];

  @property()
  activeId = "";

  @property({ attribute: false })
  onNewChat?: () => void;

  @property({ attribute: false })
  onSelectHistory?: (id: string) => void;

  @property({ attribute: false })
  onDelete?: (id: string) => void;

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  render() {
    return (
      <aside class="border-blush-200 bg-blush-100/70 flex w-60 flex-col border-r">
        <div class="border-blush-200 flex items-center gap-2.5 border-b px-4 py-3">
          <div class="from-blush-400 to-blush-600 flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-linear-to-br shadow-sm">
            {icon(Sparkles, "h-4 w-4 text-white")}
          </div>
          <h2 class="text-ink text-sm font-semibold tracking-tight">
            Yukino Agent
          </h2>
        </div>
        <div class="flex flex-1 flex-col gap-1.5 p-2.5">
          <button
            onClick={() => this.onNewChat?.()}
            class="border-blush-300/80 text-blush-700 hover:border-blush-400 hover:text-blush-600 flex w-full items-center justify-center gap-1.5 rounded-xl border bg-white/70 px-3 py-2 text-sm font-medium shadow-sm transition hover:bg-white"
          >
            {icon(Plus, "h-4 w-4")}
            <span>New chat</span>
          </button>
          <div class="mt-1 flex-1 overflow-y-auto">
            <div class="text-ink-soft px-2.5 py-1.5 text-xs font-medium">
              Recent
            </div>
            <div class="flex flex-col gap-0.5">
              {this.histories.length === 0 ? (
                <div class="text-ink-soft/80 px-2.5 py-2 text-xs">
                  No conversations yet
                </div>
              ) : null}
              {repeat(
                this.histories,
                (h) => h.id,
                (h) => (
                  <div
                    class={`group ${h.id === this.activeId ? "ring-blush-200 bg-white ring-1" : ""} hover:bg-blush-200/50 flex items-center rounded-lg px-2.5 py-1.5 shadow-sm transition`}
                  >
                    <button
                      onClick={() => this.onSelectHistory?.(h.id)}
                      class={`${h.id === this.activeId ? "text-ink font-medium" : "text-ink/80"} flex-1 truncate text-left text-sm`}
                    >
                      {h.title}
                    </button>
                    <button
                      onClick={() => this.onDelete?.(h.id)}
                      class="text-blush-400 ml-1.5 opacity-0 transition group-hover:opacity-100 hover:text-red-500"
                      aria-label="Delete"
                    >
                      {icon(X, "h-3.5 w-3.5")}
                    </button>
                  </div>
                ),
              )}
            </div>
          </div>
        </div>
      </aside>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "chat-sidebar": ChatSidebar;
  }
}
