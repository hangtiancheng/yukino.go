<div align="center">

# @yukino-chat (frontend)

**The web client for the yukino_chat IM backend.**

A Lit + JSX single-page app with sessions, contacts, groups, streaming chat, WebRTC audio/video calls, an admin console, and a live cache dashboard — all over the backend's HTTP and WebSocket APIs.

[![Vite](https://img.shields.io/badge/Vite-8-646CFF?logo=vite&logoColor=white)](https://vite.dev)
[![Lit](https://img.shields.io/badge/Lit-3-324FFF?logo=lit&logoColor=white)](https://lit.dev)
[![TypeScript](https://img.shields.io/badge/TypeScript-6-3178C6?logo=typescript&logoColor=white)](https://www.typescriptlang.org)

</div>

---

## Features

- **Authentication** — login and register screens with a token stored client-side and attached as a bearer header to every request.
- **Reactive stores** — `@lit-labs/signals` powers `auth`, `session`, `chat`, `ws`, and `dashboard` stores, so UI updates follow state without manual wiring.
- **Sessions & contacts** — session list with unread counts and previews, contact list with tags, search, apply/pass flows, and profile editing.
- **Chat** — direct and group conversations, message bubbles, file and avatar uploads, and markdown/emoji rendering.
- **Audio & video calls** — 1v1 and group mesh calls via `RtcManager` (`RTCPeerConnection`), with signaling frames exchanged over the backend's `/wss` channel.
- **Admin console** — user and group moderation screens for accounts with the admin flag.
- **Cache dashboard** — connects to the backend's `yukino_cache` WebSocket dashboard at `/dashboard/ws` and can delete individual cached keys.
- **Reconnect-aware WebSocket client** — a shared `wsStore` tracks connection status and reconnects automatically.

## Stack

| Concern    | Choice                                                       |
| ---------- | ------------------------------------------------------------ |
| Components | Lit 3 via `@yukino.js/lit-jsx` (JSX)                         |
| Routing    | `@lit-labs/router`                                           |
| State      | `@lit-labs/signals`                                          |
| Styling    | Tailwind CSS v4, `clsx` + `tailwind-merge`, `tw-animate-css` |
| Icons      | `lucide`                                                     |
| Build      | Vite 8                                                       |
| Lint       | ESLint 10 + `typescript-eslint`                              |

## Getting started

This package lives in the repository's pnpm workspace. From the repo root:

```bash
pnpm install
pnpm --filter yukino-chat dev
```

The app talks to the Go backend at `http://localhost:8000` / `ws://localhost:8000` (see [`src/config.ts`](./src/config.ts)); start `yukino_chat` first (see [`yukino_chat/README.md`](../../yukino_chat/README.md)).

### Scripts

| Command                                       | Description                                |
| --------------------------------------------- | ------------------------------------------ |
| `pnpm --filter yukino-chat dev`               | Start the Vite dev server.                 |
| `pnpm --filter yukino-chat build`             | Type-check and produce a production build. |
| `pnpm --filter yukino-chat preview`           | Preview the production build.              |
| `pnpm --filter yukino-chat typecheck`         | `tsc -b --noEmit`.                         |
| `pnpm --filter yukino-chat lint` / `lint:fix` | ESLint.                                    |
| `pnpm --filter yukino-chat format`            | Prettier (with the Tailwind plugin).       |

## Routes

| Path                  | Page                                 |
| --------------------- | ------------------------------------ |
| `/login`, `/register` | Auth screens                         |
| `/chat/sessions`      | Session list (default after login)   |
| `/chat/contacts`      | Contact list                         |
| `/chat/profile`       | Own profile                          |
| `/chat/:id`           | Conversation with a contact or group |
| `/manager`            | Admin console                        |
| `/dashboard`          | Live cache dashboard                 |
| `*`                   | Not found                            |

Authenticated routes run through a `requireAuth` guard that redirects to `/login`.

## Project structure

```
src/
├── main.ts                      # mounts <yukino-app>
├── app-root.ts                  # <yukino-app>: router + WS bootstrap
├── router.ts                    # navigate() / router registry
├── config.ts                    # backend HTTP + WS base URLs
├── service/api.ts               # fetch wrapper (auth headers, timeouts)
├── store/                       # auth, session, chat, ws, dashboard (signals)
├── pages/                       # login, register, chat, contact-list,
│                                #   session-list, own-info, manager, dashboard
├── components/                  # nav bar, sidebars, message bubble,
│   │                            #   video-call, toaster, ui primitives
│   └── ui/                      # button, card, dialog, input, table, ...
├── utils/                       # rtc, avatar, format, validate, toast, logout
└── types.ts
```

## Backend endpoints used

The client calls the same routes documented in the backend README — auth (`/login`, `/register`), user, group, session, contact, message, and file groups — plus two WebSocket endpoints:

- `GET /wss` — real-time messaging and call signaling.
- `GET /dashboard/ws` — live `yukino_cache` statistics and key management.

> [!NOTE]
> Because the backend is single-instance (in-process hub and cache), this client assumes one server. See the backend README's deployment constraints before scaling out.

## License

[MIT](../../LICENSE) © hangtiancheng
