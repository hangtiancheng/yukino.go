import { customElement, state } from "@yukino.js/lit-jsx";
import { Router } from "@lit-labs/router";
import { store, toastAtom } from "@/store/atoms";
import { Icon, type IconName } from "@/components/icon";
import { LightDomElement } from "@/components/base";
import { api } from "@/api/client";
import "@/pages/dashboard";
import "@/pages/scheduled";
import "@/pages/conditions";
import "@/pages/executions";
import "@/pages/execution-detail";
import "@/pages/records";
import "@/pages/monitor";

interface NavItem {
  path: string;
  label: string;
  icon: IconName;
  tag: string;
}

const navItems: NavItem[] = [
  {
    path: "/",
    label: "Dashboard",
    icon: "layout-dashboard",
    tag: "dashboard-page",
  },
  {
    path: "/scheduled",
    label: "Scheduled tasks",
    icon: "calendar-clock",
    tag: "scheduled-page",
  },
  {
    path: "/conditions",
    label: "Condition tasks",
    icon: "zap",
    tag: "conditions-page",
  },
  {
    path: "/executions",
    label: "Executions",
    icon: "history",
    tag: "executions-page",
  },
  {
    path: "/records",
    label: "Risk records",
    icon: "database",
    tag: "records-page",
  },
  {
    path: "/monitor",
    label: "Cluster monitor",
    icon: "activity",
    tag: "monitor-page",
  },
];

@customElement("app-shell")
export class AppShell extends LightDomElement {
  @state() private toast = store.get(toastAtom);
  @state() private path = window.location.pathname;
  @state() private authenticationRequired = false;
  @state() private token = "";
  @state() private authError = "";
  @state() private authenticating = false;
  private onAuthRequired = () => {
    this.authenticationRequired = true;
  };

  private routes = new Router(
    this,
    [
      { path: "/", render: () => <dashboard-page /> },
      { path: "/scheduled", render: () => <scheduled-page /> },
      { path: "/conditions", render: () => <conditions-page /> },
      { path: "/executions", render: () => <executions-page /> },
      {
        path: "/executions/:id",
        render: (params) => (
          <execution-detail-page executionId={Number(params.id ?? 0)} />
        ),
      },
      { path: "/records", render: () => <records-page /> },
      { path: "/monitor", render: () => <monitor-page /> },
    ],
    {
      fallback: {
        render: () => (
          <div class="text-base-content/50 flex flex-col items-center gap-3 py-24">
            <Icon name="alert-triangle" class="h-10 w-10" />
            <div>Page not found</div>
            <a href="/" class="btn btn-sm btn-primary rounded-full">
              Back to dashboard
            </a>
          </div>
        ),
      },
    },
  );

  private toastTimer: number | undefined;
  private unsubToast: (() => void) | undefined;
  private onPopState = () => {
    this.path = window.location.pathname;
  };

  connectedCallback(): void {
    super.connectedCallback();
    this.unsubToast = store.sub(toastAtom, () => {
      this.toast = store.get(toastAtom);
      window.clearTimeout(this.toastTimer);
      this.toastTimer = window.setTimeout(() => {
        this.toast = null;
      }, 3200);
    });
    window.addEventListener("popstate", this.onPopState);
    document.addEventListener("click", this.trackNav, true);
    window.addEventListener("taskflow-auth-required", this.onAuthRequired);
  }

  disconnectedCallback(): void {
    super.disconnectedCallback();
    this.unsubToast?.();
    window.removeEventListener("popstate", this.onPopState);
    document.removeEventListener("click", this.trackNav, true);
    window.removeEventListener("taskflow-auth-required", this.onAuthRequired);
    window.clearTimeout(this.toastTimer);
  }

  private trackNav = (e: MouseEvent) => {
    const anchor = (e.target as HTMLElement | null)?.closest?.("a");
    if (anchor?.href) {
      window.setTimeout(() => {
        this.path = window.location.pathname;
      }, 0);
    }
  };

  private isActive(item: NavItem): boolean {
    if (item.path === "/") return this.path === "/";
    return this.path.startsWith(item.path);
  }

  private navLink(item: NavItem) {
    const active = this.isActive(item);
    return (
      <a
        aria-label={item.label}
        aria-current={active ? "page" : undefined}
        href={item.path}
        yukino-sentry-view="app-nav"
        yukino-sentry-ev={`nav${item.path === "/" ? "-dashboard" : item.path.replace(/\//g, "-")}`}
        yukino-sentry-msg={item.label}
        class={`group flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition-colors ${
          active
            ? "bg-base-300/70 text-base-content"
            : "text-base-content/70 hover:bg-base-200 hover:text-base-content"
        }`}
      >
        <Icon name={item.icon} class="h-[18px] w-[18px]" />
        <span class="hidden whitespace-nowrap sm:inline">{item.label}</span>
      </a>
    );
  }

  private toastNode() {
    if (!this.toast) return null;
    const palette = {
      success: "alert-success",
      error: "alert-error",
      info: "alert-info",
    }[this.toast.kind];
    const icon: IconName =
      this.toast.kind === "success"
        ? "check-circle"
        : this.toast.kind === "error"
          ? "x-circle"
          : "activity";
    return (
      <div
        role="status"
        aria-live="polite"
        class="pointer-events-none fixed bottom-6 left-1/2 z-50 max-w-[calc(100vw-2rem)] -translate-x-1/2"
      >
        <div class={`alert shadow-lg ${palette}`}>
          <Icon name={icon} class="h-4 w-4" />
          <span class="text-sm">{this.toast.text}</span>
        </div>
      </div>
    );
  }

  render() {
    return (
      <div class="bg-base-100 flex min-h-screen flex-col lg:flex-row">
        <a
          href="#main-content"
          class="focus:bg-base-100 sr-only focus:not-sr-only focus:fixed focus:top-4 focus:left-4 focus:z-50 focus:rounded-lg focus:p-3"
        >
          Skip to content
        </a>
        <aside class="border-base-300/50 bg-base-200 sticky top-0 z-20 flex w-full shrink-0 flex-col border-b px-3 py-3 lg:h-screen lg:w-64 lg:border-r lg:border-b-0 lg:py-7">
          <a href="/" class="mb-3 flex items-center gap-3 px-3 lg:mb-8">
            <span class="text-base-content border-base-300 bg-base-100 flex h-9 w-9 items-center justify-center rounded-lg border">
              <Icon name="zap" class="h-5 w-5" />
            </span>
            <span class="flex flex-col leading-tight">
              <span class="text-lg font-semibold tracking-tight">Taskflow</span>
            </span>
          </a>

          <nav
            aria-label="Main navigation"
            class="flex flex-1 flex-row gap-1 overflow-x-auto lg:flex-col"
          >
            {navItems.map((item) => this.navLink(item))}
          </nav>
        </aside>

        <main
          id="main-content"
          class="min-w-0 flex-1 px-4 py-6 sm:px-8 lg:px-12 lg:py-12"
        >
          <div class="mx-auto max-w-6xl">{this.routes.outlet()}</div>
        </main>

        {this.toastNode()}
        {this.authenticationRequired ? (
          <div class="bg-base-content/20 fixed inset-0 z-50 flex items-center justify-center p-4 backdrop-blur-sm">
            <form
              class="bg-base-100 w-full max-w-sm rounded-xl p-6 shadow-xl"
              role="dialog"
              aria-modal="true"
              aria-labelledby="auth-title"
              onSubmit={async (event: SubmitEvent) => {
                event.preventDefault();
                if (this.authenticating) return;
                this.authenticating = true;
                this.authError = "";
                sessionStorage.setItem("taskflow-token", this.token.trim());
                try {
                  await api.overview();
                  location.reload();
                } catch {
                  this.authError = "Invalid access token";
                  sessionStorage.removeItem("taskflow-token");
                } finally {
                  this.authenticating = false;
                }
              }}
            >
              <h2 id="auth-title" class="mb-5 text-xl font-semibold">
                Connect to Taskflow
              </h2>
              <label class="mb-2 block text-sm" htmlFor="access-token">
                Access token
              </label>
              <input
                id="access-token"
                type="password"
                class="input mb-4 w-full"
                required
                autocomplete="current-password"
                value={this.token}
                onInput={(event: Event) => {
                  this.token = (event.target as HTMLInputElement).value;
                }}
              />
              {this.authError ? (
                <p role="alert" class="text-error mb-3 text-sm">
                  {this.authError}
                </p>
              ) : null}
              <button
                class="btn btn-primary w-full rounded-full"
                disabled={this.authenticating}
                type="submit"
              >
                Connect
              </button>
            </form>
          </div>
        ) : null}
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "app-shell": AppShell;
  }
}
