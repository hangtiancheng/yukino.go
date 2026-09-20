import { LitElement, customElement } from "@yukino.js/lit-jsx";
import { setupSentry } from "./sentry.js";
import "./index.css";
import "./components/chat-app.js";

if (import.meta.env.DEV) {
  import("./crash/index.js");
}

setupSentry();

@customElement("app-router")
export class AppRouter extends LitElement {
  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  render() {
    return (
      <>
        <chat-app></chat-app>
        {import.meta.env.DEV ? <random-crash></random-crash> : null}
      </>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "app-router": AppRouter;
  }
}
