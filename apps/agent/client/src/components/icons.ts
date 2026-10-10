import { createElement, type IconNode } from "lucide";

export function icon(node: IconNode, classes = "h-4 w-4"): SVGElement {
  const el = createElement(node);
  el.setAttribute("class", classes);
  return el;
}
