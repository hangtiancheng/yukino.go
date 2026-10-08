/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Server origin for API calls; empty = same-origin (proxied). */
  readonly VITE_SERVER_BASE_URL?: string;
  /** yukino-sentry report endpoint; empty = dev mock / proxy path. */
  readonly VITE_SENTRY_DSN?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
