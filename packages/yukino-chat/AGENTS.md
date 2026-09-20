<!-- intent-skills:start -->

# Yukino Intent - before editing files, run the matching guidance command.

yukinoIntent:

- id: "@yukino.js/sentry#yukino-sentry"
  run: "npx @tanstack/intent@latest load @yukino.js/sentry#yukino-sentry"
  for: "Integration guide for @yukino.js/sentry, a browser monitoring and analytics SDK.
  Use this skill whenever the user mentions @yukino.js/sentry, yukino-sentry, frontend monitoring,
  frontend error tracking, browser performance monitoring, declarative click tracking,
  exposure tracking, white-screen detection, screen recording, Web Vitals, PV/dwell-time,
  offline report caching, or any task involving integrating browser observability into
  a React, Vue, or vanilla TypeScript/JavaScript project. Also trigger when the user
  asks about yukino-sentry-* attributes, ReactErrorBoundary from this SDK, vuePlugin, the
  Vite dev-server mock plugin (sentryPlugin / sentryPlugin7), the webpack dev-server mock
  plugin (SentryWebpackPlugin / sentryMiddleware), or dev-time source map resolution of
  reported errors. Even if the user simply says "add monitoring" or "add tracking" in a
  frontend context, consult this skill first."
- id: "@yukino.js/lit-jsx#yukino-lit-jsx"
  run: "npx @tanstack/intent@latest load @yukino.js/lit-jsx#yukino-lit-jsx"
  for: "Authoritative reference for @yukino.js/lit-jsx (lit-jsx/ in this repo), a
  JSX runtime for Lit (lit@3.x only — no older-Lit compatibility). Write
  Lit/web-component applications with React-style JSX instead of html`
string templates. Covers the automatic JSX runtime (jsx/jsxs/jsxDEV,
Fragment/<>), createElement's JSX-prop → Lit-expression semantics (onXxx →
@event listeners on primitives, class/className → .className property,
style → styleMap, ref → lit ref directive, hyphenated props → attributes,
booleans → ?boolean-attribute plus property, everything else → property
assignment even when undefined, key stripped, false/null/undefined children
skipped), createRoot/Root (render/unmount/duplicate-container warning),
the element registry (assignElements/resetElements tag overrides accepting
plain strings or Lit StaticValues, default div fallback), the local
customElement decorator that registers tag names for class-component JSX,
re-exported Lit decorators (property, state, query, queryAll, queryAsync,
queryAssignedElements, queryAssignedNodes, eventOptions), the spread
directive, the full JSX→DOM event-name map, and the JSX type layer
(JSX.IntrinsicElements over HTMLElementTagNameMap). Testing setup:
vitest + jsdom (tests/). Trigger tokens: @yukino.js/lit-jsx, lit-jsx,
jsxImportSource "@yukino.js/lit-jsx", jsx-runtime, createRoot(,
assignElements(, resetElements(, customElementRegistry, jsx(",
Fragment(, yukino-lit-jsx. Use whenever a file in this repo uses JSX with
LitElement/web components, configures jsxImportSource, or when the user
mentions lit-jsx, JSX props not applying, onXxx handlers not firing on
custom elements, tag overrides, or lit-jsx tests. Do NOT use for React,
Next.js, Preact, Solid or Vue rendering; for Lit html` templates without
  JSX; for lit's own decorators via "lit/decorators.js" (import them from
  @yukino.js/lit-jsx so tag names register); or for non-JSX Lit apps."

<!-- intent-skills:end -->
