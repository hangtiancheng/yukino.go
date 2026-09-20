/**
 * Copyright (c) 2026 hangtiancheng
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

import { html } from "@yukino.js/lit-jsx";
import { customElement } from "@yukino.js/lit-jsx";
import { Router } from "@lit-labs/router";
import { isLoggedIn, currentUser } from "@/store/auth";
import { connectWs } from "@/store/ws";
import { navigate, setRouter } from "@/router";
import { TwElement } from "@/styles/base";

// Side-effect imports: register page elements before first render.
import "./pages/login";
import "./pages/register";
import "./pages/session-list";
import "./pages/contact-list";
import "./pages/own-info";
import "./pages/chat";
import "./pages/manager";
import "./pages/dashboard";
import "./pages/not-found";

/** Guard protected routes: bounce to /login (replacing history) when anonymous. */
function requireAuth(): boolean {
  if (isLoggedIn()) return true;
  navigate("/login", { replace: true });
  return false;
}

@customElement("yukino-app")
export class YukinoApp extends TwElement {
  private router = new Router(
    this,
    [
      {
        path: "/",
        enter: () => {
          navigate("/chat/sessions", { replace: true });
          return false;
        },
      },
      { path: "/login", render: () => html`<sc-login></sc-login>` },
      { path: "/register", render: () => html`<sc-register></sc-register>` },
      {
        path: "/chat/sessions",
        enter: requireAuth,
        render: () => html`<sc-session-list></sc-session-list>`,
      },
      {
        path: "/chat/contacts",
        enter: requireAuth,
        render: () => html`<sc-contact-list></sc-contact-list>`,
      },
      {
        path: "/chat/profile",
        enter: requireAuth,
        render: () => html`<sc-profile></sc-profile>`,
      },
      {
        path: "/chat/:id",
        enter: requireAuth,
        render: (params) => html`<sc-chat .contactId=${params.id}></sc-chat>`,
      },
      {
        path: "/manager",
        enter: requireAuth,
        render: () => html`<sc-manager></sc-manager>`,
      },
      {
        path: "/dashboard",
        enter: requireAuth,
        render: () => html`<sc-dashboard></sc-dashboard>`,
      },
    ],
    { fallback: { render: () => html`<sc-not-found></sc-not-found>` } },
  );

  override connectedCallback() {
    // Set the module router ref before super.connectedCallback(), which
    // triggers the initial Router.goto — route guards call navigate().
    setRouter(this.router);
    super.connectedCallback();
    // Reconnect websocket on reload when already logged in.
    if (isLoggedIn()) {
      connectWs(currentUser().uuid);
    }
  }

  override disconnectedCallback() {
    setRouter(null);
    super.disconnectedCallback();
  }

  override render() {
    return this.router.outlet();
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "yukino-app": YukinoApp;
  }
}
