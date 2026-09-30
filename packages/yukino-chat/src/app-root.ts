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
