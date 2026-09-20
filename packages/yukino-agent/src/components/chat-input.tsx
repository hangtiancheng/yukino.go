import {
  LitElement,
  customElement,
  property,
  query,
  state,
  type PropertyValues,
} from "@yukino.js/lit-jsx";
import { ChevronDown, Ellipsis, Paperclip, Send } from "lucide";
import { icon } from "./icons.js";
import type { Mode } from "../chat/chat-store.js";

const MODES: Mode[] = ["quick", "stream"];

@customElement("chat-input")
export class ChatInput extends LitElement {
  @property({ type: Boolean })
  isStreaming = false;

  @property()
  mode: Mode = "quick";

  @property({ attribute: false })
  onModeChange?: (m: Mode) => void;

  @property({ attribute: false })
  onSend?: (text: string) => void;

  @property({ attribute: false })
  onUpload?: (file: File) => void;

  @state()
  private _text = "";

  @state()
  private _showTools = false;

  @state()
  private _showMode = false;

  @query("textarea")
  private _textarea!: HTMLTextAreaElement;

  @query("input[type=file]")
  private _fileInput!: HTMLInputElement;

  @query("[data-input-container]")
  private _container!: HTMLDivElement;

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
    document.addEventListener("mousedown", this.#handleClickOutside);
    document.addEventListener("keydown", this.#handleEscape);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener("mousedown", this.#handleClickOutside);
    document.removeEventListener("keydown", this.#handleEscape);
  }

  // Close dropdowns on outside click or Escape key.
  #handleClickOutside = (e: MouseEvent) => {
    if (!this._showTools && !this._showMode) return;
    if (this._container && !this._container.contains(e.target as Node)) {
      this._showTools = false;
      this._showMode = false;
    }
  };

  #handleEscape = (e: KeyboardEvent) => {
    if (!this._showTools && !this._showMode) return;
    if (e.key === "Escape") {
      this._showTools = false;
      this._showMode = false;
    }
  };

  protected updated(changedProperties: PropertyValues) {
    // Auto-resize textarea to fit content (up to ~10 lines).
    if (changedProperties.has("_text") && this._textarea) {
      this._textarea.style.height = "auto";
      this._textarea.style.height = `${this._textarea.scrollHeight}px`;
    }
  }

  #send() {
    const t = this._text.trim();
    if (!t || this.isStreaming) return;
    this.onSend?.(t);
    this._text = "";
  }

  render() {
    return (
      <div
        data-input-container
        class="border-blush-200 shadow-blush-200/50 focus-within:border-blush-400 relative rounded-3xl border bg-white/85 p-3 shadow-xl backdrop-blur-md transition"
      >
        <textarea
          value={this._text}
          onInput={(e: Event) => {
            this._text = (e.target as HTMLTextAreaElement).value;
          }}
          onKeyDown={(e: KeyboardEvent) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              this.#send();
            }
          }}
          disabled={this.isStreaming}
          placeholder="Ask the Yukino Agent OnCall assistant"
          class="text-ink placeholder:text-blush-400 max-h-40 w-full resize-none bg-transparent text-base outline-none"
          rows={1}
        ></textarea>
        <div class="mt-2 flex items-center justify-between">
          <div class="relative">
            <button
              onClick={() => {
                this._showTools = !this._showTools;
              }}
              class="text-ink-soft hover:bg-blush-100 hover:text-ink flex h-9 w-9 items-center justify-center rounded-full transition"
              aria-label="Tools"
              aria-expanded={this._showTools}
            >
              {icon(Ellipsis, "h-5 w-5")}
            </button>
            {this._showTools ? (
              <div class="border-blush-200 shadow-blush-300/30 absolute bottom-full left-0 mb-2 rounded-2xl border bg-white p-1.5 shadow-xl">
                <button
                  onClick={() => {
                    this._fileInput?.click();
                    this._showTools = false;
                  }}
                  class="text-ink hover:bg-blush-50 flex w-48 items-center gap-3 rounded-xl px-3 py-2 text-sm transition"
                >
                  {icon(Paperclip, "h-5 w-5 text-blush-500")}
                  <span>Upload file</span>
                </button>
              </div>
            ) : null}
          </div>
          <div class="flex items-center gap-2">
            <div class="relative">
              <button
                onClick={() => {
                  this._showMode = !this._showMode;
                }}
                class="text-ink-soft hover:text-ink flex items-center gap-1 text-sm transition"
                aria-expanded={this._showMode}
                aria-label="Chat mode"
              >
                <span>{this.mode === "quick" ? "Quick" : "Stream"}</span>
                {icon(ChevronDown)}
              </button>
              {this._showMode ? (
                <div class="border-blush-200 shadow-blush-300/30 absolute right-0 bottom-full mb-2 rounded-2xl border bg-white p-1.5 shadow-xl">
                  {MODES.map((m) => (
                    <button
                      key={m}
                      onClick={() => {
                        this.onModeChange?.(m);
                        this._showMode = false;
                      }}
                      class={`${m === this.mode ? "bg-blush-100 text-blush-700 font-medium" : "text-ink hover:bg-blush-50"} block w-40 rounded-xl px-3 py-2 text-left text-sm`}
                    >
                      {m === "quick" ? "Quick" : "Stream"}
                    </button>
                  ))}
                </div>
              ) : null}
            </div>
            <button
              onClick={() => this.#send()}
              disabled={this.isStreaming || !this._text.trim()}
              class="from-blush-500 to-blush-600 shadow-blush-500/30 hover:from-blush-400 hover:to-blush-500 flex h-9 w-9 items-center justify-center rounded-full bg-linear-to-br text-white shadow-md transition active:scale-95 disabled:opacity-40"
              aria-label="Send"
            >
              {icon(Send, "h-5 w-5")}
            </button>
          </div>
        </div>
        <input
          type="file"
          accept=".txt,.md,.markdown"
          class="hidden"
          onChange={(e: Event) => {
            const input = e.target as HTMLInputElement;
            const f = input.files?.[0];
            if (f) this.onUpload?.(f);
            input.value = "";
          }}
        />
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "chat-input": ChatInput;
  }
}
