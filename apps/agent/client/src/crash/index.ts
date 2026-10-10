import { LitElement, customElement } from "@yukino.js/lit-jsx";

const ROLL_INTERVAL_MS = 20_000;

const CRASH_PROBABILITY = 0.04;

@customElement("random-crash")
export class RandomCrash extends LitElement {
  #timerId: ReturnType<typeof setInterval> | undefined;

  createRenderRoot() {
    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    this.style.display = "none";
    this.#timerId = setInterval(() => {
      if (Math.random() < CRASH_PROBABILITY) {
        console.log("[error-seeder] firing: Lit render crash");
        setTimeout(() => {
          throw new Error("Seeded Lit render crash: probe component exploded");
        }, 0);
      }
    }, ROLL_INTERVAL_MS);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    if (this.#timerId !== undefined) {
      clearInterval(this.#timerId);
      this.#timerId = undefined;
    }
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "random-crash": RandomCrash;
  }
}
