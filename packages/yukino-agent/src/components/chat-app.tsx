import { LitElement, customElement } from "@yukino.js/lit-jsx";
import {
  ChatStore,
  type ChatMessage,
  type NotificationType,
} from "../chat/chat-store.js";
import "./chat-sidebar.js";
import "./chat-container.js";
import "./ai-ops-btn.js";
import "./loading-overlay.js";

const NOTIFY_COLORS: Record<NotificationType, string> = {
  info: "bg-blush-500",
  success: "bg-green-500",
  warning: "bg-amber-500",
  error: "bg-red-500",
};

@customElement("chat-app")
export class ChatApp extends LitElement {
  #chat = new ChatStore(this);

  /* Render into light DOM so global Tailwind utilities apply. */
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.className =
      "flex h-screen w-screen overflow-hidden bg-blush-50 text-ink";
  }

  #handleAIOps = async () => {
    const chat = this.#chat;
    if (chat.isStreaming) {
      chat.showNotification(
        "Please wait for the current operation to finish",
        "warning",
      );
      return;
    }
    chat.newChat();
    const r = await chat.triggerAIOps();
    if (r) {
      const msg: ChatMessage = {
        type: "assistant",
        content: r.result,
        detail: r.detail,
      };
      chat.addMessage(msg);
    }
  };

  #handleUpload = async (file: File) => {
    const msg = await this.#chat.uploadFile(file);
    if (msg) this.#chat.addMessage({ type: "assistant", content: msg });
  };

  render() {
    const chat = this.#chat;
    return (
      <>
        <chat-sidebar
          histories={chat.histories}
          activeId={chat.sessionId}
          onNewChat={chat.newChat}
          onSelectHistory={chat.loadChatHistory}
          onDelete={chat.deleteChatHistory}
        ></chat-sidebar>
        <main class="bg-blush-50 relative flex flex-1 flex-col overflow-hidden">
          <ai-ops-btn
            onTrigger={this.#handleAIOps}
            disabled={chat.isStreaming}
          ></ai-ops-btn>
          <chat-container
            messages={chat.messages}
            isStreaming={chat.isStreaming}
            mode={chat.mode}
            onModeChange={chat.setMode}
            onSend={chat.sendMessage}
            onUpload={this.#handleUpload}
          ></chat-container>
        </main>
        <loading-overlay overlay={chat.overlay}></loading-overlay>
        {chat.notification ? (
          <div
            class={`${NOTIFY_COLORS[chat.notification.type]} fixed top-5 right-5 z-10000 max-w-xs rounded-xl px-4 py-3 text-sm font-medium text-white shadow-lg ring-1 ring-black/5`}
          >
            {chat.notification.message}
          </div>
        ) : null}
      </>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "chat-app": ChatApp;
  }
}
