<div align="center">

# yukino_chatbot

**A RAG chatbot backend that exercises the whole yukino.go stack.**

An LLM chat service built on `yukino_http` (HTTP + SSE), `yukino_rpc` (a standalone AI service), `yukino_orm` (MongoDB), and a per-user vector store — with JWT auth, streaming replies, and document-grounded answers.

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Module](https://img.shields.io/badge/module-yukino__chatbot-blue)](go.mod)

</div>

---

## Architecture

```text
     Client
       │  HTTP + SSE
       V
 ┌─────────────────┐        yukino_rpc (custom TCP binary protocol)
 │  HTTP Gateway   │  ──────────────────────────────────────────────>  ┌──────────────┐
 │  yukino_http    │        Complete / CompleteStream                  │  AIService   │
 └────────┬────────┘                                                   │ (RPC server) │
          │                                                            └──────┬───────┘
          │ yukino_orm                                                        │
          V                                                                   V
   ┌─────────────┐                                                    ┌─────────────────┐
   │   MongoDB   │<───────────────────────────────────────────────────│  ai.Manager     │
   │ users,      │        sessions + messages                         │  agents, RAG    │
   │ sessions,   │                                                    │  langchaingo    │
   │ messages    │                                                    └────────┬────────┘
   └─────────────┘                                                             │ OpenAI-compatible
                                                                               V
                                                                     ┌─────────────────┐
                                                                     │ openai / OpenAI │
                                                                     └─────────────────┘
```

The binary starts **both** processes in one OS process for convenience: the RPC server listens on `rpc_addr`, then the HTTP server dials it locally and serves the API.

## Features

- **Two model modes** — `openai` for plain chat and `openai-rag` for document-grounded answers. Switch per request via `model_type`.
- **Streaming replies** — server-side streaming over `yukino_rpc` (`CompleteStream`) is piped out as Server-Sent Events by `yukino_http`.
- **Per-user RAG** — uploads are chunked (1000 chars, 200 overlap) and embedded into an in-memory vector store keyed by username; retrieval is cosine similarity over `nomic-embed-text`-style embeddings.
- **JWT auth** — HS256 tokens; every AI/file route requires a bearer token, while `login` / `register` stay open.
- **Conversation persistence** — sessions and messages are stored in MongoDB through `yukino_orm`; history is restored into agent memory at startup by `LoadMessagesInto`.
- **Cache-aside session lists** — session and history reads go through a small cache interface that is invalidated on new messages.
- **Uniform error codes** — responses carry a stable `code` enum (`1000` OK, `2xxx` client, `4xxx` server, `5xxx` model).

## Install & run

```bash
go get github.com/hangtiancheng/yukino.go/yukino_chatbot

# 1. edit config.json
# 2. run
go run .
```

`config.json`:

```json
{
  "app_name": "yukino-chatbot",
  "app_host": "0.0.0.0",
  "app_port": "8088",
  "rpc_addr": "127.0.0.1:19090",
  "mongo_uri": "mongodb://localhost:27017/",
  "mongo_database": "yukino_chatbot",
  "jwt_expire_hours": 8760,
  "jwt_issuer": "yukino-chatbot",
  "jwt_subject": "yukino-chatbot",
  "jwt_key": "change-me",
  "rag_docs_dir": "./docs",
  "ai_model_name": "qwen3",
  "ai_embed_model": "nomic-embed-text",
  "ai_base_url": "http://localhost:11434"
}
```

> [!NOTE]
> `ai_base_url` must be an **OpenAI-compatible** endpoint. openai exposes one at `http://localhost:11434`; any OpenAI-compatible gateway works too.

## HTTP API

Base path: `/api/v1`

| Method | Path                                              | Description                                             |
| ------ | ------------------------------------------------- | ------------------------------------------------------- |
| `POST` | `/user/login`                                     | Exchange username/password for a JWT.                   |
| `POST` | `/user/register`                                  | Register with an email; returns a JWT and username.     |
| `GET`  | `/ai/chat/get-user-sessions-by-username`          | List the caller's sessions (cached).                    |
| `POST` | `/ai/chat/create-session-and-send-message`        | Create a session and get a reply.                       |
| `POST` | `/ai/chat/create-session-and-send-message-stream` | Same, streamed as SSE.                                  |
| `POST` | `/ai/chat/send-message-2-session`                 | Continue an existing session.                           |
| `POST` | `/ai/chat/send-message-stream-2-session`          | Same, streamed as SSE.                                  |
| `POST` | `/ai/chat/get-chat-history-list`                  | Fetch message history for a session.                    |
| `POST` | `/file/upload`                                    | Upload a `.txt`/`.md` file into the caller's RAG store. |

Requests use a `model_type` field (`openai` or `openai-rag`) and a `session_id` where applicable.

```bash
# login
curl -s localhost:8088/api/v1/user/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo","password":"secret"}'

# streamed answer grounded in uploaded docs
curl -N localhost:8088/api/v1/ai/chat/create-session-and-send-message-stream \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"question":"Summarize the onboarding guide","model_type":"openai-rag"}'
```

## RPC surface

The AI service exposes two methods, both dispatched reflectively by `yukino_rpc`:

```go
type AIService struct{}

func (s *AIService) Complete(ctx context.Context, req *AIRequest) (*AIResponse, error)
func (s *AIService) CompleteStream(req *AIRequest, stream rpc.ServerStream) error
```

`AIRequest` carries `Username`, `SessionID`, `Question`, and `ModelType`; streamed chunks are `AIStreamChunk{Content}`.

## Layout

```
yukino_chatbot/
├── main.go                    # boots the RPC server + HTTP gateway
├── config.json
└── internal/
    ├── ai/                    # Manager + per-session Agent (LLM calls, memory)
    ├── app/                   # yukino_http routes, middleware, handlers
    ├── auth/                  # JWT issue/parse + password hashing
    ├── cache/                 # cache interface
    ├── code/                  # response code enum + messages
    ├── config/                # config loading
    ├── model/                 # User / Session / Message
    ├── rag/                   # per-user vector store (cosine similarity)
    ├── rpc_client/            # request/response DTOs
    ├── service/               # business logic
    └── store/                 # MongoDB persistence via yukino_orm
```

## Testing

```bash
go test ./...
```

Tests cover the HTTP handlers, auth, services, and store. `internal/test_util/mongo.go` spins up an isolated MongoDB database for the store tests.

## Notes

> [!WARNING]
> The RAG vector store is **in-memory and per-process**. Documents indexed by one instance are invisible to another, and a restart re-loads them from `uploads/<username>/`. For multi-instance deployments, back it with a shared vector database.

> [!TIP]
> Skip the embed model (`ai_embed_model` unset or the embedder failing to initialize) and the service still runs in plain `openai` mode — RAG is disabled rather than fatal.

## License

[MIT](../LICENSE) © hangtiancheng
