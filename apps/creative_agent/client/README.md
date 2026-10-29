# yukino-creative-agent

Frontend for the yukino-creative-agent backend (`../server`): login / register / home / agent chat (SSE streaming).

Migrated from the static pages of go-learn (`fe/*.html`).

## Tech stack

- React 19 + TypeScript + Vite 8 (React Compiler enabled)
- Tailwind CSS v4 + shadcn/ui (Base UI foundation, nova theme) + lucide-react
- TanStack Router (routing) + TanStack Query (data fetching)
- Zustand (auth / conversation state, persisted to localStorage)
- SSE: native EventSource (`GET /aigc/conversation`, token passed via query parameter)

## Development

```bash
pnpm install
pnpm dev        # http://localhost:5173, /api and /aigc are proxied to the backend (defaults to localhost:18080, override with VITE_BACKEND_URL)
                # for production builds set the backend address with VITE_API_BASE_URL (see "Deployment" below)
pnpm build      # tsc -b + vite build → dist/
pnpm lint       # eslint
```

See `../server/README.md` for starting the backend (`make run`).

## Deployment (frontend/backend separation)

The backend (`../server`) serves the API only and does not host frontend static assets. This project builds and deploys independently:

```bash
# Inject the backend address at build time (cross-origin is allowed by the backend CORS middleware)
VITE_API_BASE_URL=https://api.example.com pnpm build

# Deploy dist/ to any static server (nginx / Vercel / OSS+CDN ...)
```

Note: the SPA uses history routing (`/login` `/register` `/conversation`),
so the static server must fall back unknown paths to `index.html` (nginx example: `try_files $uri /index.html;`).

## Structure

```
src/
  main.tsx               # QueryClientProvider + RouterProvider
  router.tsx             # TanStack Router route table (/login /register / /conversation, auth guard)
  lib/
    api.ts               # Unified response envelope {code,message,data} wrapper + EventSource SSE helper
    jwt.ts               # JWT payload parsing (username / user_id)
  stores/
    auth.ts              # zustand: token + login/register/logout (persist)
    conversation.ts      # zustand: message list + SSE orchestration (reconnect msgId/lastSeq, card confirmation, HIL resume)
  routes/
    login.tsx            # Login page
    register.tsx         # Register page
    home.tsx             # Home page (welcome + /api/v1/example, TanStack Query)
    conversation.tsx     # Agent chat page
  components/
    ui/                  # shadcn components (button/card/input/field/alert/...)
    conversation/        # Message list + hot-topic card / script confirmation card / HIL card + input box
```

## SSE events

Aligned with the backend `pkg/sse/events.go`: `message-start` / `text-delta` / `tool-call-start` /
`tool-call-result` / `task-step` / `task-done` / `interrupt` / `reconnect` /
`ui-block-start` / `ui-block-complete` / `message-complete` / `error`.

On disconnect, if an unfinished `msgId` exists, the client automatically reconnects with
`reconnect=1&msgId=&lastSeq=` and the server replays from the breakpoint.
