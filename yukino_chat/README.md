<div align="center">

# yukino_chat

**A self-contained chat server — accounts, sessions, groups, contacts, files, and calls.**

A complete IM backend built end to end on the yukino.go stack: `yukino_http` for HTTP + WebSocket, `yukino_orm` for MongoDB, and `yukino_cache` for the in-process read-through cache and its live dashboard.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-yukino__chat-blue)](go.mod)

</div>

---

## Capabilities

- **Accounts & auth** — telephone/password sign-up with salted SHA-256 hashes and HS256 JWTs (`auth.jwtSecret`). Every POST route except `/login`, `/register`, and `/user/update-password` requires an `Authorization` token; admin routes additionally require `is_admin`.
- **Sessions** — unread counts, last-message previews, activity ordering, and `/session/mark-session-read`. Direct and group messages auto-create or restore the receiver's session.
- **Contacts** — tag lists, note names, online presence, keyword search, apply/pass/refuse flows, and blacklisting.
- **Groups** — creation with initial members and a welcome message, invitations, member lists with join and last-speak times, add modes, dismiss, and admin moderation.
- **Messaging** — types `0` text, `1` image, `2` file, `3` AV signaling, `4` video, `5` system notification (content is a `topic:` such as `contact` / `group` / `apply` / `session` / `online`).
- **Audio & video calls** — 1v1 and group mesh calls signaled over `/wss` with type-3 frames; the server tracks call rooms and busy state, and `/chatroom/get-callers` lists room members.
- **Chunked uploads** — instant upload and resume via `/file/verify`, `/file/upload-chunk`, and `/file/merge`, with chunks capped at 10 MiB.
- **Live cache dashboard** — `yukino_cache.DashboardHandler()` is mounted at `/dashboard/ws` and streams cache/group statistics over WebSocket.

## Architecture

```text
   Client (packages/yukino-chat)
        │  HTTP + WS
        V
 ┌─────────────────────────────────────────────────────────────┐
 │  yukino_http Application                                    │
 │   middleware: CORS ──> Auth (JWT)                           │
 │   routers: /user /group /session /contact /message /file    │
 │            /chatroom  +  GET /wss  +  GET /dashboard/ws     │
 ├─────────────────────────────────────────────────────────────┤
 │  services: user, contact, group, session, message,          │
 │            chat_server (hub), call_manager                  │
 ├─────────────────────────────────────────────────────────────┤
 │  dao: mongo (yukino_orm), cache (yukino_cache), indexes,    │
 │       soft delete, transactions                             │
 └───────────────┬──────────────────────────────┬──────────────┘
                 V                              V
            ┌─────────┐                   ┌──────────────┐
            │ MongoDB │                   │  in-process  │
            └─────────┘                   │ cache + hub  │
                                          └──────────────┘
```

## Run

```bash
# backend (reads ./config.json)
go run ./cmd

# frontend (Lit + Tailwind, in the pnpm workspace)
pnpm --filter yukino-chat dev
```

`config.json`:

```json
{
  "app": { "host": "0.0.0.0", "port": 8000 },
  "mongo": { "uri": "mongodb://localhost:27017", "database": "yukino_chatbot" },
  "cache": { "maxBytes": 67108864, "expiration": 300 },
  "static": {
    "avatarPath": "./static/avatars",
    "filePath": "./static/files",
    "chunkPath": "./static/chunks"
  },
  "auth": { "jwtSecret": "change-me", "tokenExpireHours": 336 }
}
```

If `auth.jwtSecret` is empty, an ephemeral secret is generated at boot and all tokens are invalidated on restart.

## API overview

The full, runnable request collection lives in [`yukino_chat.http`](./yukino_chat.http). Highlights:

| Group     | Representative routes                                                                                        |
| --------- | ------------------------------------------------------------------------------------------------------------ |
| Auth      | `POST /login`, `POST /register`                                                                              |
| User      | `/user/update-user-info`, `/user/search-user`, `/user/get-user-info-list` (admin), `/user/set-admin` (admin) |
| Group     | `/group/create-group`, `/group/invite-group-members`, `/group/get-group-member-list`, `/group/dismiss-group` |
| Session   | `/session/open-session`, `/session/get-user-session-list`, `/session/mark-session-read`                      |
| Contact   | `/contact/apply-contact`, `/contact/pass-contact-apply`, `/contact/black-contact`, `/contact/add-tag`        |
| Message   | `/message/get-message-list`, `/message/get-group-message-list`, `/message/upload-file`                       |
| File      | `/file/verify`, `/file/upload-chunk`, `/file/merge`                                                          |
| Chatroom  | `/chatroom/get-online-users`, `/chatroom/get-callers`                                                        |
| WebSocket | `GET /wss` (messaging + calls), `GET /dashboard/ws` (cache dashboard)                                        |

## Testing

```bash
go test ./...
```

MongoDB-backed tests expect a local instance (`mongodb://localhost:27017`).

## Deployment constraints

> [!CAUTION]
> **This server is single-instance by design.** The message bus is an in-process channel, and the WebSocket connection table, call rooms, and cache all live in process memory. A message sent to a user connected to another instance would never be delivered. Plan capacity for one instance, or add a shared pub/sub layer before scaling out.

> [!WARNING]
> **No TLS.** The server speaks plain HTTP/WS. For anything beyond an internal network, terminate TLS at a gateway (nginx, Caddy, …) in front of it.

> [!WARNING]
> `/user/update-password` is unauthenticated **by design** (legacy forgot-password parity — no email/SMS verification exists). Anyone who knows a telephone number can reset that account's password. Add a verification step before exposing it publicly.

> [!WARNING]
> The WebSocket endpoints (`/wss`, `/dashboard/ws`) do not validate tokens; `client_id` is trusted. Restrict access in production.

> [!NOTE]
> MongoDB **transactions require a replica set**. On a standalone `mongod` the server automatically falls back to sequential, non-transactional writes.

## License

[MIT](../LICENSE) © hangtiancheng
