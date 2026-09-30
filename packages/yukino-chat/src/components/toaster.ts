import { LitElement, html, nothing } from "@yukino.js/lit-jsx";
import { state } from "@yukino.js/lit-jsx";
import type { IconNode } from "lucide";
import {
  CircleAlert,
  CircleCheck,
  Info,
  TriangleAlert,
  X as XIcon,
} from "lucide";
import { icon } from "./icons";
import { cn } from "../lib/utils";

export type ToastType = "info" | "success" | "warning" | "error";

interface ToastItem {
  id: number;
  message: string;
  type: ToastType;
  leaving: boolean;
}

const TOAST_ICONS: Record<ToastType, IconNode> = {
  info: Info,
  success: CircleCheck,
  warning: TriangleAlert,
  error: CircleAlert,
};

const TOAST_ICON_STYLE: Record<ToastType, string> = {
  info: "size-4 text-info",
  success: "size-4 text-success",
  warning: "size-4 text-warning",
  error: "size-4 text-destructive",
};

const TOAST_ACCENTS: Record<ToastType, string> = {
  info: "border-l-info",
  success: "border-l-success",
  warning: "border-l-warning",
  error: "border-l-destructive",
};

let nextId = 1;
let instance: XToaster | null = null;

export class XToaster extends LitElement {
  @state() private toasts: ToastItem[] = [];

  createRenderRoot() {
    // Light DOM so the document-level Tailwind styles apply (mounted on body).
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.className =
      "pointer-events-none fixed inset-x-0 top-3 z-[100] flex flex-col items-center gap-2 px-4 sm:items-end sm:right-4 sm:left-auto sm:px-0";
  }

  push(message: string, type: ToastType = "info") {
    const id = nextId++;
    this.toasts = [...this.toasts, { id, message, type, leaving: false }];
    setTimeout(() => this.dismiss(id), 4200);
  }

  private dismiss(id: number) {
    this.toasts = this.toasts.map((t) =>
      t.id === id ? { ...t, leaving: true } : t,
    );
    setTimeout(() => {
      this.toasts = this.toasts.filter((t) => t.id !== id);
    }, 220);
  }

  render() {
    return html`${this.toasts.map(
      (t) => html`
        <div
          class=${cn(
            "bg-card/95 pointer-events-auto flex w-full max-w-sm items-center gap-2.5 rounded-xl border py-2.5 pr-2 pl-3.5 shadow-lg backdrop-blur transition-all duration-200",
            "border-border border-l-4",
            TOAST_ACCENTS[t.type],
            t.leaving
              ? "animate-out fade-out slide-out-to-right-4"
              : "animate-in fade-in slide-in-from-top-2 slide-in-from-right-2",
          )}
          role="status"
        >
          <span class="shrink-0"
            >${icon(TOAST_ICONS[t.type], TOAST_ICON_STYLE[t.type])}</span
          >
          <span
            class=${cn(
              "flex-1 text-sm leading-snug",
              t.type === "error" && "text-destructive",
            )}
          >
            ${t.message}
          </span>
          <button
            type="button"
            aria-label="Dismiss notification"
            class="text-muted-foreground hover:bg-accent hover:text-foreground flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-md transition-colors"
            @click=${() => this.dismiss(t.id)}
          >
            ${icon(XIcon, "size-3.5")}
          </button>
        </div>
      `,
    )}${nothing}`;
  }
}

if (!customElements.get("x-toaster")) {
  customElements.define("x-toaster", XToaster);
}

export function ensureToaster(): XToaster {
  if (!instance) {
    instance = document.createElement("x-toaster") as XToaster;
    document.body.appendChild(instance);
  }
  return instance;
}
