import { LitElement } from "@yukino.js/lit-jsx";
import { store } from "@/store/atoms";

type SubscribableAtom = Parameters<typeof store.sub>[0];

/**
 * Base element that renders into the light DOM instead of a Shadow DOM.
 * The project stylesheet is global Tailwind CSS, and utility classes would
 * not apply inside an encapsulated shadow root, so every custom element opts
 * out of Shadow DOM by rendering into `this`.
 */
export abstract class LightDomElement extends LitElement {
  protected override createRenderRoot(): HTMLElement {
    return this;
  }
}

/**
 * Base class for elements driven by jotai atoms: declare subscriptions in
 * setupWatches() and initial fetches in loadData(). Subscriptions are torn
 * down on disconnect and re-established on reconnect, matching the router
 * outlet lifecycle.
 */
export abstract class AtomElement extends LightDomElement {
  private _unsubs: Array<() => void> = [];

  protected watch(atom: SubscribableAtom): void {
    this._unsubs.push(store.sub(atom, () => this.requestUpdate()));
  }

  protected setupWatches(): void {}

  protected loadData(): void {}

  connectedCallback(): void {
    super.connectedCallback();
    this.setupWatches();
    this.loadData();
  }

  disconnectedCallback(): void {
    super.disconnectedCallback();
    for (const unsub of this._unsubs) unsub();
    this._unsubs = [];
  }
}
