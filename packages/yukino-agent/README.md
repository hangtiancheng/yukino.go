<div align="center">

# @yukino-agent (frontend)

**The chat UI for the yukino_agent backend.**

A Lit + JSX single-page app with streaming replies, an AI-Ops panel, RAG file upload, markdown rendering, and browser observability — built on `@yukino.js/lit-jsx` and Tailwind CSS v4.

[![Vite](https://img.shields.io/badge/Vite-8-646CFF?logo=vite&logoColor=white)](https://vite.dev)
[![Lit](https://img.shields.io/badge/Lit-3-324FFF?logo=lit&logoColor=white)](https://lit.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-6-3178C6?logo=typescript&logoColor=white)](https://www.typescriptlang.org)

</div>

---

## Features

- **Streaming and quick modes** — send a question and receive either a single JSON answer or an SSE token stream rendered incrementally.
- **Chat histories** — up to 50 conversations persisted in `localStorage`, validated on read with Zod schemas so stale or corrupted state cannot crash the app.
- **AI Ops** — a dedicated button posts to `/api/ai_ops` and renders the plan-execute-replan report alongside its per-step execution details.
- **Knowledge upload** — attach `.md` / `.txt` files to populate the backend RAG store.
- **Markdown replies** — `markdown-it` for parsing plus `dompurify` for sanitization, with syntax-aware code blocks.
- **Browser monitoring** — `@yukino.js/sentry` with the Performance and Exposure plugins, reporting to the backend's `/api/log` → Prometheus bridge. A dev-only crash seeder exercises error reporting.
- **Tailwind CSS v4** — the Vite plugin, no `tailwind.config.js` required.

## Stack

| Concern    | Choice                                                               |
| ---------- | -------------------------------------------------------------------- |
| Components | Lit 3 via `@yukino.js/lit-jsx` (JSX instead of `html\`\`` templates) |
| Build      | Vite 8                                                               |
| Styling    | Tailwind CSS v4, `prettier-plugin-tailwindcss`                       |
| Validation | Zod v4                                                               |
| Markdown   | `markdown-it` + `dompurify`                                          |
| Icons      | `lucide`                                                             |
| Lint       | `oxlint`                                                             |

## Getting started

This package lives in the repository's pnpm workspace. From the repo root:

```bash
pnpm install
pnpm --filter yukino-agent dev
```

The dev server proxies `/api` to the Go backend at `http://localhost:8123`, so run `yukino_agent` first (see [`yukino_agent/README.md`](../../yukino_agent/README.md)).

### Scripts

| Command                                | Description                                |
| -------------------------------------- | ------------------------------------------ |
| `pnpm --filter yukino-agent dev`       | Start the Vite dev server.                 |
| `pnpm --filter yukino-agent build`     | Type-check and produce a production build. |
| `pnpm --filter yukino-agent preview`   | Preview the production build.              |
| `pnpm --filter yukino-agent typecheck` | `tsc --noEmit`.                            |
| `pnpm --filter yukino-agent lint`      | `oxlint --fix`.                            |
| `pnpm --filter yukino-agent format`    | Prettier (with the Tailwind plugin).       |

## Project structure

```
src/
├── main.tsx                 # mounts <app-router> (and the dev crash seeder)
├── sentry.ts                # SDK init, plugins, oversized-event filtering
├── index.css                # Tailwind entry
├── chat/chat-store.ts       # reactive store + localStorage persistence + schemas
├── schemas/index.ts         # Zod schemas for API responses
├── crash/                   # dev-only error seeder
└── components/
    ├── chat-app.tsx         # top-level layout
    ├── chat-sidebar.tsx     # history list / new chat
    ├── chat-container.tsx   # message thread + input wiring
    ├── chat-input.tsx
    ├── msg-list.tsx         # message bubbles
    ├── md-render.tsx        # sanitized markdown rendering
    ├── markdown.ts          # markdown-it setup
    ├── ai-ops-btn.tsx
    ├── loading-overlay.tsx
    └── icons.ts
```

## Backend contract

All requests are same-origin under `/api`:

| Endpoint                | Purpose                                     |
| ----------------------- | ------------------------------------------- |
| `POST /api/chat`        | Single-shot answer.                         |
| `POST /api/chat_stream` | SSE token stream.                           |
| `POST /api/ai_ops`      | Plan-execute-replan report with `detail[]`. |
| `POST /api/upload`      | Index a document for RAG.                   |
| `POST /api/log`         | Monitoring report sink (the SDK `dsn`).     |

Responses share the `{ message, data }` envelope; `data` is `null` on errors, which is why the Zod schemas are nullable rather than optional.

> [!NOTE]
> Events larger than 50 KB (rrweb screen recordings, dev-mode resource lists) are dropped in `beforeSendBatch` — they exceed the `fetch` keepalive body limit and would wedge retries.

## License

[MIT](../../LICENSE) © hangtiancheng
