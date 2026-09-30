import { LitElement } from "@yukino.js/lit-jsx";
import { SignalWatcher } from "@lit-labs/signals";
import type { CSSResultOrNative } from "lit";
import { twSheet } from "./tw";

/**
 * Base class for all app custom elements: adopts the shared compiled
 * Tailwind stylesheet into every shadow root and reacts to signal reads
 * made during render.
 */
export class TwElement extends SignalWatcher(LitElement) {
  static override styles: CSSResultOrNative[] = [twSheet];
}
