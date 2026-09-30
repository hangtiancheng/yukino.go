import type { Router } from "@lit-labs/router";

let router: Router | null = null;

export function setRouter(r: Router | null) {
  router = r;
}

/** Push (or replace) a history entry and render it through the router. */
export function navigate(path: string, opts?: { replace?: boolean }) {
  if (opts?.replace) {
    history.replaceState({}, "", path);
  } else {
    history.pushState({}, "", path);
  }
  void router?.goto(path);
}
