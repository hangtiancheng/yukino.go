import { LitElement, customElement, property } from "@yukino.js/lit-jsx";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { renderMarkdown } from "./markdown.js";

@customElement("md-render")
export class MdRender extends LitElement {
  @property()
  content = "";

  @property({ attribute: false })
  mdClass?: string;

  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "contents";
  }

  render() {
    const classes =
      this.mdClass ??
      "max-w-none text-sm leading-relaxed wrap-break-word text-ink";
    return (
      <div class={`md-content ${classes}`}>
        {unsafeHTML(renderMarkdown(this.content))}
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "md-render": MdRender;
  }
}
