import { LitElement } from "@yukino.js/lit-jsx";
import { store } from "@/store/atoms";

type SubscribableAtom = Parameters<typeof store.sub>[0];

export abstract class LightDomElement extends LitElement {
  protected override createRenderRoot(): HTMLElement {
    return this;
  }
}

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
