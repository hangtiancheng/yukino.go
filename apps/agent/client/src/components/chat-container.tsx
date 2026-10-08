import { LitElement, customElement, property } from "@yukino.js/lit-jsx";
import { Sparkles } from "lucide";
import { icon } from "./icons.js";
import type { ChatMessage, Mode } from "../chat/chat-store.js";
import "./msg-list.js";
import "./chat-input.js";

@customElement("chat-container")
export class ChatContainer extends LitElement {
  @property({ attribute: false })
  messages: ChatMessage[] = [];

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

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  render() {
    const centered = this.messages.length === 0;
    return (
      <div
        class={`${centered ? "items-center justify-center" : ""} flex flex-1 flex-col overflow-hidden`}
      >
        {centered ? (
          <div class="flex max-w-md flex-col items-center px-6 text-center">
            <div class="from-blush-300 via-blush-400 to-blush-600 shadow-blush-300/50 flex h-16 w-16 items-center justify-center rounded-3xl bg-linear-to-br shadow-lg">
              {icon(Sparkles, "h-8 w-8 text-white")}
            </div>
            <h1 class="text-ink mt-5 text-2xl font-semibold tracking-tight text-balance">
              Hi! I'm the Yukino Agent OnCall assistant
            </h1>
            <p class="text-ink-soft mt-3 text-sm leading-relaxed">
              First time here? Upload a file from the docs directory via the
              "..." menu before chatting — otherwise you may hit a search error.
            </p>
          </div>
        ) : (
          <msg-list
            messages={this.messages}
            isStreaming={this.isStreaming}
          ></msg-list>
        )}
        <div class="mx-auto w-full max-w-3xl px-6 pb-6">
          <chat-input
            isStreaming={this.isStreaming}
            mode={this.mode}
            onModeChange={this.onModeChange}
            onSend={this.onSend}
            onUpload={this.onUpload}
          ></chat-input>
        </div>
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "chat-container": ChatContainer;
  }
}
