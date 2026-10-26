# yukino-creative-agent

yukino-creative-agent 后端（`../server`）的前端：登录 / 注册 / 首页 / 智能对话（SSE 流式）。

由 go-learn 的静态页面（`fe/*.html`）迁移而来。

## 技术栈

- React 19 + TypeScript + Vite 8（React Compiler 已启用）
- Tailwind CSS v4 + shadcn/ui（Base UI 底座，nova 主题）+ lucide-react
- TanStack Router（路由）+ TanStack Query（数据请求）
- Zustand（auth / conversation 状态，persist 到 localStorage）
- SSE：原生 EventSource（`GET /aigc/conversation`，token 走 query 参数）

## 开发

```bash
pnpm install
pnpm dev        # http://localhost:5173，/api 与 /aigc 代理到后端（默认 localhost:18080，可用 VITE_BACKEND_URL 覆盖）
                # 生产构建用 VITE_API_BASE_URL 指定后端地址（见下方"部署"）
pnpm build      # tsc -b + vite build → dist/
pnpm lint       # eslint
```

后端启动见 `../server/README.md`（`make run`）。

## 部署（前后端分离）

后端（`../server`）只提供 API，不托管前端静态产物。本项目独立构建、独立部署：

```bash
# 构建时注入后端地址（跨域由后端 CORS 中间件放行）
VITE_API_BASE_URL=https://api.example.com pnpm build

# dist/ 部署到任意静态服务器（nginx / Vercel / OSS+CDN ...）
```

注意：SPA 使用 history 路由（`/login` `/register` `/conversation`），
静态服务器需把未命中的路径回退到 `index.html`（nginx 示例：`try_files $uri /index.html;`）。

## 结构

```
src/
  main.tsx               # QueryClientProvider + RouterProvider
  router.tsx             # TanStack Router 路由表（/login /register / /conversation，登录守卫）
  lib/
    api.ts               # 统一响应包 {code,message,data} 封装 + EventSource SSE helper
    jwt.ts               # JWT payload 解析（用户名 / user_id）
  stores/
    auth.ts              # zustand：token + login/register/logout（persist）
    conversation.ts      # zustand：消息列表 + SSE 编排（断线重连 msgId/lastSeq、点卡确认、HIL resume）
  routes/
    login.tsx            # 登录页
    register.tsx         # 注册页
    home.tsx             # 首页（欢迎 + /api/v1/example，TanStack Query）
    conversation.tsx     # 智能对话页
  components/
    ui/                  # shadcn 组件（button/card/input/field/alert/...）
    conversation/        # 消息列表 + 热点卡 / 脚本确认卡 / HIL 卡 + 输入框
```

## SSE 事件

与后端 `pkg/sse/events.go` 对齐：`message-start` / `text-delta` / `tool-call-start` /
`tool-call-result` / `task-step` / `task-done` / `interrupt` / `reconnect` /
`ui-block-start` / `ui-block-complete` / `message-complete` / `error`。

断线时若存在未完成的 `msgId`，自动携带 `reconnect=1&msgId=&lastSeq=` 重连，服务端断点续拉回放。
