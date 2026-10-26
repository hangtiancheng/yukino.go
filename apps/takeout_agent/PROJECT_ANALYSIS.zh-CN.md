# takeout_agent 项目解析报告

> 一个基于 Go + 多 Agent（LLM）的外卖/点餐推荐服务，从 Java Spring Boot 版本迁移而来。
> 后端构建于自研 HTTP 框架 `yukino_http` 与 MySQL（GORM）之上，LLM 能力由 AgentScope-Go（DashScope / 通义千问）驱动；前端是 React 19 + Vite + Tailwind CSS 4 + shadcn/ui 的单页应用。

- 代码位置：`apps/takeout_agent`
- 后端入口：`server/cmd/takeout_agent/main.go`
- 前端入口：`client/src/main.tsx`
- 数据库脚本：`server/db/takeout_db.sql`
- 默认监听：`http://127.0.0.1:8090`（8080 被本地中间件 nginx 容器占用）

---

## 目录

1. [项目定位与整体印象](#1-项目定位与整体印象)
2. [技术栈总览](#2-技术栈总览)
3. [整体架构与请求生命周期](#3-整体架构与请求生命周期)
4. [目录结构](#4-目录结构)
5. [数据模型与数据库设计](#5-数据模型与数据库设计)
6. [后端深度解析](#6-后端深度解析)
7. [Agent / LLM 工程技术亮点（重点）](#7-agent--llm-工程技术亮点重点)
8. [前端深度解析](#8-前端深度解析)
9. [测试与工程质量](#9-测试与工程质量)
10. [API 一览](#10-api-一览)
11. [部署与配置](#11-部署与配置)
12. [亮点总结与改进建议](#12-亮点总结与改进建议)
13. [Java→Go 迁移对齐审查与修复记录](#13-javago-迁移对齐审查与修复记录)

---

## 1. 项目定位与整体印象

`takeout_agent` 是一个「**对话式点餐推荐助手**」。用户用自然语言描述自己的**用餐时段、心情、场景、健康目标、口味、菜系、便利性**等诉求，系统通过一条**多 Agent 编排流水线**理解意图、抽取槽位、必要时反问澄清，然后从**个人菜品库**或**公共菜品库**中检索、打分、排序，最后生成自然语言推荐语 + 菜品卡片返回给前端。

它的价值不止于"推荐菜品"本身，更在于它是一套**完整、可观测、可评估的 LLM 应用工程范式**：

- **多 Agent 分工**：意图识别、澄清、推荐话术、套餐规划、离线评审各司其职。
- **规则 + LLM 混合决策**：关键路径（是否澄清、如何排序、是否拦截）由确定性规则兜底，LLM 只负责"理解"与"表达"，保证可控、可降级。
- **全链路 Trace**：每一次对话的每个状态机步骤、每次 LLM 调用（prompt、输出、耗时、token）都落库，可回放。
- **离线评估闭环**：Trace → 人工打标（ground truth）→ 规则指标 + LLM-as-Judge + 用户反馈加权评分。
- **从 Java 迁移而来**：代码中大量注释显式记录了与 Java/Spring/MyBatis/Jackson 的行为对齐点，是一份难得的"跨语言迁移对照样本"。

整体代码质量高：**强类型、不可变值对象、清晰的单一职责服务、充分的单元测试、详尽的注释**。

---

## 2. 技术栈总览

### 后端（Go）

| 维度          | 选型                                                | 说明                                                        |
| ------------- | --------------------------------------------------- | ----------------------------------------------------------- |
| 语言 / 工具链 | Go `1.26.0`                                         | 见 `server/go.mod`                                          |
| HTTP 框架     | `libs/yukino_http`（自研）                          | 洋葱中间件、Trie 路由、延迟渲染、TakeOver、静态文件、SSE/WS |
| ORM           | `gorm.io/gorm` + `gorm.io/driver/mysql`             | MySQL 8，JSON 列 + `JSON_OVERLAPS`                          |
| LLM SDK       | `github.com/agentscope-ai/agentscope-go/v2 v2.0.11` | DashScope（通义千问）OpenAI 兼容 `chat/completions`         |
| 配置          | `goccy/go-yaml` + 环境变量覆盖 + 内置 `.env` 解析   | 见 `internal/config`                                        |
| ID 生成       | `github.com/google/uuid`                            | `sess_` / `trace_` 前缀的 32 位 hex                         |
| 日志          | 标准库 `log/slog`                                   | 结构化文本日志                                              |

### 前端（TypeScript / React）

| 维度       | 选型                                                             |
| ---------- | ---------------------------------------------------------------- |
| 框架       | React `19.3` + Vite `7` + TypeScript（strict）                   |
| 样式       | Tailwind CSS `4`（oklch 设计令牌）+ `tw-animate-css`             |
| 组件库     | shadcn/ui 风格组件（基于 `@base-ui/react`）+ `lucide-react` 图标 |
| 路由       | `react-router-dom@7`（`HashRouter`）                             |
| 服务端状态 | TanStack Query（`@tanstack/react-query`）                        |
| 虚拟化     | TanStack Virtual（Trace 事件列表）                               |
| 偏好状态   | TanStack Store（主题 / 侧边栏）                                  |
| 聊天状态   | zustand                                                          |
| 本地持久化 | ahooks `useLocalStorageState`（用户 ID）                         |
| 校验       | zod（所有 API 响应运行时校验）                                   |
| 通知       | sonner                                                           |

---

## 3. 整体架构与请求生命周期

### 3.1 分层架构

```
┌───────────────────────────────────────────────────────────────┐
│  client/  React SPA（聊天、菜品管理、Trace、评估、仪表盘）       │
│           TanStack Query + zod 校验 + zustand                  │
└───────────────────────────┬───────────────────────────────────┘
                            │  HTTP JSON（/api/v1/takeout，X-User-Id）
┌───────────────────────────▼───────────────────────────────────┐
│  internal/api        yukino_http 路由 + 中间件（CORS/日志/恢复/  │
│                      body 限流）+ 统一错误适配 + 静态资源         │
├───────────────────────────────────────────────────────────────┤
│  internal/takeout    领域层                                     │
│   Orchestrator ── 状态机编排 + 会话锁                            │
│   ├─ IntentAgentService（意图识别，light 模型）                  │
│   ├─ IntentReviseService（规则修正）                            │
│   ├─ SlotMergeService（槽位合并）                               │
│   ├─ ClarifyAgentService（澄清：规则判定 + LLM 措辞）            │
│   ├─ MealSearchService / MealRankService（召回 + 打分排序）      │
│   ├─ MealPlanService（多餐规划）                                │
│   ├─ RecommendResponseAgentService（推荐话术，main 模型）        │
│   ├─ PlanResponseAgentService（套餐话术，main 模型）             │
│   ├─ RiskGuardService（健康风险护栏）                           │
│   ├─ TraceService（全链路埋点）                                 │
│   └─ EvaluationService / EvaluationJudgeService（离线评估）      │
├───────────────────────────────────────────────────────────────┤
│  internal/takeout/store.go   GORM 持久层（MySQL）                │
└───────────────────────────┬───────────────────────────────────┘
                            │
                 ┌──────────▼──────────┐     ┌──────────────────────┐
                 │  MySQL 8 (takeout_db)│     │  DashScope / 千问 LLM  │
                 └─────────────────────┘     └──────────────────────┘
```

### 3.2 一次 `/chat` 请求的完整生命周期

`Orchestrator.TakeoutChat`（`internal/takeout/orchestrator.go`）驱动整轮对话：

```
TakeoutChat(userID, ChatRequest)
 ├─ 生成 traceID = trace_<32hex>
 ├─ 校验 message 非空、sourceMode ∈ {PERSONAL, PUBLIC}
 ├─ SessionStateService.LoadOrCreate   ← 读取/初始化会话状态（phase/slots/lastRecommendations）
 ├─ TraceService.OpenTrace(ctx, ...)   ← 打开 Trace 作用域（绑定到 context）
 ├─ RecordEvent(REQUEST_RECEIVED)
 ├─ acquireSessionLock(sessionID)      ← 每会话引用计数互斥锁，串行化同一会话的并发轮次
 │
 └─ handleTurn（在会话锁内）
     ├─ AppendMessage(user)            ← 用户消息落库
     ├─ RecordEvent(USER_MESSAGE_RECORDED)
     ├─ [PERSONAL] HasPersonalMeals 预检 ← 空库直接提示先添加菜品
     ├─ RecentConversationTurns(3)     ← 取最近 3 轮（截断到 120 字符）作为短期上下文
     ├─ IntentAgent.Recognize          ← LLM 意图识别 + 槽位抽取（失败降级关键词规则）
     ├─ IntentRevise.Revise            ← 规则修正意图
     ├─ RecordEvent(INTENT_REVISED / ROUTE_SELECTED)
     │
     └─ 按意图路由：
         ├─ MEAL_RECOMMENDATION / CLARIFY_NEEDED → handleRecommendation
         │     ├─ SlotMerge（历史槽位 + 本轮槽位）
         │     ├─ ClarifyAgent.Decide  ← 规则判定 ASK/READY；ASK 才调 LLM 措辞反问
         │     ├─ ASK  → completeAsk（返回 CLARIFY 响应 + missingSlots）
         │     └─ READY→ completeRecommendation
         ├─ MEAL_ADJUST → handleAdjust（排除上轮已推荐的菜，重跑推荐）
         ├─ MEAL_PLAN   → handlePlan（解析餐次 → 每餐检索+排序 → 套餐话术）
         ├─ HEALTH_RISK → handleHealthRisk（返回固定保守回复，不调 LLM）
         └─ OTHER       → handleChitchat（固定引导语，不调 LLM）

 completeRecommendation（核心推荐管线）
     ├─ MealSearch.Search              ← JSON_OVERLAPS 召回（≤50 条）
     ├─ MealRank.Rank                  ← 七维重叠度打分，排除已推荐，取 Top10
     ├─ RecommendResponseAgent         ← LLM 生成每道菜理由 + 口语化回复（Top3）
     ├─ RiskGuard.Check                ← 风险护栏；不过则改写为保守回复
     ├─ SessionState.Save + AppendMessage(assistant)
     └─ 返回 ChatResponse（speechText + displayBlocks 菜品卡片）

 最终：TraceScope.Close() → 将整条 trace_json 一次性写入 takeout_request_trace
```

**关键设计**：Trace 作用域先于会话锁打开，`defer scope.Close()` 保证无论成功失败都会落库一条完整 Trace；`REQUEST_FINISHED` 事件记录端到端耗时。

---

## 4. 目录结构

```
takeout_agent/
├─ README.md
├─ client/                          # React + Vite + shadcn/ui 前端
│  ├─ index.html / vite.config.ts / package.json / tsconfig*.json
│  └─ src/
│     ├─ main.tsx / App.tsx / index.css
│     ├─ components/
│     │  ├─ layout/     app-shell.tsx（侧边栏+顶栏+用户切换）、theme-toggle.tsx
│     │  ├─ shared/     meal-card / slot-chips / slot-picker / date-time-picker
│     │  └─ ui/         shadcn/ui 基础组件（badge/button/card/dialog/select/table/...）
│     ├─ hooks/        queries.ts（TanStack Query 封装）、use-user-id.ts
│     ├─ lib/          api.ts（HTTP 客户端）、schemas.ts（zod）、format.ts、utils.ts
│     ├─ pages/        dashboard / chat / personal-meals / public-meals / traces / evaluations
│     └─ stores/       chat-store.ts（zustand）、preferences.ts（TanStack Store）
└─ server/
   ├─ cmd/takeout_agent/main.go     # 装配、配置加载、优雅关停
   ├─ config.example.yaml / config.yaml（git-ignored）/ .env.example / .env（git-ignored）
   ├─ db/takeout_db.sql                # 建库脚本 + 93 个槽位选项 + 公共菜品种子
   └─ internal/
      ├─ api/         server.go（路由/中间件/静态）、handlers.go、server_test.go
      ├─ config/      config.go、config_test.go
      └─ takeout/     领域层
         ├─ orchestrator.go  intent.go  clarify.go  slot.go  recommend.go
         ├─ plan.go  risk.go  meal.go  session.go  trace.go  store.go
         ├─ evaluation.go  feedback.go  llm.go  prompt.go  jsonutil.go
         ├─ errors.go  types.go
         ├─ prompts/   intent.txt clarify.txt recommend_response.txt
         │             plan_response.txt evaluation_judge.txt（go:embed 内嵌）
         └─ *_test.go  logic_test.go llm_test.go
```

---

## 5. 数据模型与数据库设计

数据库 `takeout_db`（MySQL 8，`utf8mb4`），脚本 `server/db/takeout_db.sql`。核心是**用 JSON 列承载多值槽位标签**，并用 `JSON_OVERLAPS` 做召回。

### 5.1 表清单

| 表                      | 作用              | 关键字段                                                                                   |
| ----------------------- | ----------------- | ------------------------------------------------------------------------------------------ |
| `takeout_sessions`      | 会话状态          | `id`(sess\_…)、`user_id`、`phase`、`slots`(json)、`last_recommendations`(json)             |
| `takeout_messages`      | 对话消息          | `session_id`、`role`、`content`、`intent`、`agent_trace_id`                                |
| `meal_item`             | 菜品（个人/公共） | `source_type`、`owner_user_id`、`name` + 7 个 JSON 槽位列                                  |
| `takeout_slot_option`   | 槽位字典          | `slot_name`、`option_value`、`sort_order`、`enabled`                                       |
| `takeout_request_trace` | 请求全链路追踪    | `trace_id`、`status`、`event_count`、`duration_ms`、`trace_json`(json)、`expected_*`(打标) |
| `recommend_feedback`    | 推荐反馈          | `session_id`、`item_id`、`action`、`rating`、`reason`                                      |

### 5.2 七个槽位维度（`SlotBundle`）

`types.go` 中 `SlotNames` 定义了稳定顺序的七维：

| 维度          | 含义     | 示例选项                                       |
| ------------- | -------- | ---------------------------------------------- |
| `mealTime`    | 餐次     | Breakfast / Lunch / Dinner / Three Meals …     |
| `mood`        | 心情     | Tired / Happy / Stressed / Treat Myself …      |
| `scene`       | 场景     | Work / Campus / Overtime / Post-Workout …      |
| `healthGoal`  | 健康目标 | Fat Loss / Light / High Protein / Low Sugar …  |
| `cuisine`     | 菜系     | Sichuan / Cantonese / Japanese / Fast Food …   |
| `taste`       | 口味     | Light / Spicy / Sweet & Sour / Umami …         |
| `convenience` | 便利性   | Quick / Easy Takeout / Solo Meal / Meal Prep … |

种子数据共 **93 个槽位选项**（`grep -c "^('" takeout_db.sql` = 93），并预置 4 道公共菜 + 1 道用户 1 的个人菜。

### 5.3 关键设计点

- **JSON 多值标签**：`meal_item` 的每个槽位列都是 JSON 数组（如 `'["Lunch","Dinner"]'`），读写经 `jsonStringArray` / `parseStringArray` 序列化。
- **`JSON_OVERLAPS` 召回**（`store.go:SearchMeals`）：对每一维拼一个条件 `(? = '[]' OR JSON_OVERLAPS(col, ?))`——**查询维度为空数组则视为"不限制"**，否则要求菜品标签与查询标签有交集。七维 AND 组合，`LIMIT 50`。
- **`slots` 列内嵌 `_meta`**：`takeout_sessions.slots` 除七个维度外还带 `_meta.sourceMode` / `_meta.currentIntent`（`session.go:slotsDocument`），把"数据源模式""当前意图"也持久化进会话。
- **`clientFoundRows=true`**（`config.go:DSN`）：为对齐 Java Connector/J 的 `CLIENT_FOUND_ROWS` 行为——Go 驱动默认返回"被修改行数"，而所有写路径把 0 行当作"记录不存在或无权限"。不加该标志，一次"值未变的 UPDATE"会被误判为保存失败/越权。这是迁移对齐的典型细节。
- **Trace 表带打标字段**：`expected_intent` / `expected_slots` / `expected_clarify_action` / `labeled_by` / `labeled_at` / `label_note`，把"真值标注"直接挂在 Trace 行上，供评估流水线消费。

---

## 6. 后端深度解析

### 6.1 配置系统（`internal/config`）

- **YAML + 环境变量覆盖**：`Config` 分 `Server / Database / LLM / Session / Static` 五段。每个敏感项都有 `*_env` 字段指向环境变量名，`envOr(key, fallback)` 优先取环境变量——**密钥永不写进 YAML**。
- **内置 `.env` 解析**：`LoadDotEnv` 手写 KEY=VALUE 解析（支持 `export ` 前缀、引号、注释、空行），且**已存在的环境变量优先**，`.env` 缺失不报错。
- **空文件保留默认值**：`goccy/go-yaml` 把空/纯注释文档解析为 null 会清零结构体，代码显式用 `parser.ParseBytes` 判断文档是否为空，空则跳过反序列化以保留 `Default()`——注释里明确记录了与 `gopkg.in/yaml.v3` 的行为差异。
- **`TAKEOUT_CONFIG`** 覆盖配置文件路径；`Default()` 提供合理默认（含 LLM 超时 60s）。

### 6.2 HTTP 层（`internal/api`，基于 `yukino_http`）

- **路由分组**：`app.Router("/api/v1/takeout")` 下注册全部业务路由；`/healthz` 探活；`/` 服务前端静态资源。
- **`adapt` 统一处理器**：把 `func(ctx, userID) (any, error)` 的 typed handler 包装成中间件，**替代 Spring 的 `@RestControllerAdvice`**——`nil` 结果渲染 204，业务错误渲染 400（可携带自定义 status，如 404），未知错误渲染 500，统一 `{"message": ...}` 结构。
- **`X-User-Id` 解析**：缺省为 `1`（对齐 Java controller）；**存在但非法则 400 拒绝**，而不是悄悄当成用户 1。
- **CORS 中间件**：回显 `Origin`、允许 `Content-Type, X-User-Id`、短路 `OPTIONS` 预检、`Vary: Origin`。
- **请求体限流**：`bodyLimitMiddleware` 用 `http.MaxBytesReader` 把 body 上限设为 **1 MiB**——注释点明 `yukino` 的 `BindJSON` 本身无大小限制，防止恶意客户端灌入无限负载。
- **静态资源 + TakeOver**：`registerStatic` 在 `/` 用 `ctx.TakeOver()` 关闭延迟渲染后直接 `http.ServeFile(index.html)`，并显式 `SetStatus(200)`，避免访问日志把每次页面加载记成 404；其余资源走 `app.Static("/", dir)` 通配路由。
- **优雅关停**：`main.go` 监听 `SIGINT/SIGTERM`，`server.Shutdown(10s)`。

### 6.3 编排状态机（`orchestrator.go`）

- **每会话引用计数锁**（`sessionLock{mu, refs}`）：`acquireSessionLock` / `releaseSessionLock` 用 `refs` 计数，**当无人持有/等待时从 map 中删除条目**，避免锁表随会话数无限增长——注释明确指出 Java 版本从不清理 `sessionLocks`。
- **意图路由**：`switch intent.Intent` 分发到推荐/调整/规划/健康风险/闲聊五条支线。
- **`MEAL_ADJUST` 去重**：`SessionState.AppendLastRecommendations` 累积已推荐菜品 ID（去重、保序），调整轮次把它们作为 `excludeMealIDs` 传给排序层，实现"换一批"。
- **`MEAL_PLAN`**：`ResolveMealTimes` 把空/`Three Meals` 归一为默认三餐；≥2 个具体餐次则按用户顺序；单个餐次仍规划默认三餐。`PlanMeals` 对每个餐次独立"检索+排序"，取 Top1 且排除前面餐次已选菜品，避免一道菜重复出现在多餐。
- **空结果兜底**：检索为空 / 组不成套餐时返回引导文案（区分个人/公共库），而不是空卡片。

### 6.4 各 Agent 职责与模型分层（`llm.go`）

`ModelSet` 持有**两个 DashScope 模型**，`AgentSet` 装配五个无状态 Agent：

| Agent                    | 模型层 | 职责                    |
| ------------------------ | ------ | ----------------------- |
| `IntentAgent`            | light  | 意图分类 + 槽位抽取     |
| `ClarifyAgent`           | light  | 澄清反问的措辞          |
| `RecommendResponseAgent` | main   | 单餐推荐理由 + 口语回复 |
| `PlanResponseAgent`      | main   | 多餐套餐理由 + 口语回复 |
| `EvaluationJudgeAgent`   | light  | 离线 LLM-as-Judge       |

- **无状态 Agent**：`Agent.Call` 每次只发 `system + user` 一轮。注释说明 Java 版按会话缓存 ReActAgent 并在每次调用前清内存；由于这里每次都是单轮无状态调用，**每个角色共享一个实例即可等价**——减少资源占用。
- **每次调用超时**：`timeout`（默认 60s）通过 `context.WithTimeout` 约束。**注释强调其重要性**：若 provider 挂起，会永远占住会话锁、阻塞该会话。
- **Token 用量归一化**：`AgentReply` 捕获 `InputTokens/OutputTokens/HasUsage`，供 Trace 记录与评估成本指标使用。
- **Prompt 内嵌**：`prompt.go` 用 `//go:embed all:prompts` 把 5 个英文提示词编译进二进制，`MustLoad` 在装配期 panic 快速失败——对齐 Java classpath 资源，保证二进制自包含。

### 6.5 意图识别与修正（`intent.go`）

- **LLM 优先 + 关键词降级**：`Recognize` 调 LLM，任何失败（取不到字典 / 调用失败 / 解析失败）都降级到 `fallback` 关键词规则，**绝不让一轮对话因 LLM 故障而失败**。
- **关键词匹配器**（`jsonutil.go:keywordMatcher`）：对每个关键词编译 `\b…\b` 词边界正则、忽略大小写，避免短词误匹配（如 "hi" 不会命中 "this"）。
- **降级优先级**：HEALTH_RISK > MEAL_ADJUST > MEAL_PLAN > OTHER > MEAL_RECOMMENDATION > CLARIFY_NEEDED。
- **`IntentReviseService` 规则修正**（在 LLM 之后、路由之前）：
  1. 健康风险关键词**永远最高优先级**；
  2. `MEAL_ADJUST` 但没有历史推荐 → 降级为普通推荐；
  3. 套餐关键词强制 `MEAL_PLAN`；
  4. 置信度 < `0.4` 的推荐意图 → 降级为 `CLARIFY_NEEDED`。

### 6.6 槽位系统（`slot.go` / `types.go`）

- **`SlotBundle` 不可变值对象**：所有字段构造即归一化（trim、去空、去重、保序），`With*` 方法返回副本。
- **`SlotMergeService`**：逐维合并历史与本轮槽位，**本轮非空则覆盖，否则保留历史**。
- **`SlotOptionService`**：从 `takeout_slot_option` 读启用项（按 `sort_order`），`Validate` 拒绝字典外标签。

### 6.7 澄清节点（`clarify.go`）

- **规则决定 ASK/READY，LLM 只负责措辞**：`ClarifyRuleService.MissingSlots` 规定——`mealTime` 必填；`healthGoal` 必填，**除非已有强食物偏好**（cuisine/taste/scene/convenience 任一非空）。
- 缺槽时才调 LLM 生成自然反问；LLM 失败或返回空则用 `FallbackQuestion` 模板。**这最大限度减少了不必要的 LLM 调用**。

### 6.8 检索与排序（`meal.go`）

- **召回**：`Search` → `store.SearchMeals`（JSON_OVERLAPS，≤`searchLimit=50`）。
- **排序**：`MealRankService.Rank` 过滤排除 ID → 对每个候选计算 `slotScore` → 稳定降序 → 取 `rankLimit=10`。
- **打分公式**：`slotScore = (Σ 七维 overlapRatio) / 7`；`overlapRatio = 命中标签数 / 查询标签数`（查询维度为空记 0）。即**七维重叠率的平均**，取值 [0,1]。
- **话术只用 Top3**：`RecommendResponseAgent` 只把前 3 条交给 LLM，控制 token。

### 6.9 风险护栏（`risk.go`）

- **`RiskGuardService.Check`** 在 LLM 生成回复**之后**做最终合规检查：扫描"用户输入 + 助手回复"全文，命中以下任一组即拦截并改写为固定保守回复 `ConservativeMessage`：
  - 医疗诊断/治疗承诺（cure/treatment/prescription…）
  - 极端节食（starve/fasting/eat nothing…）
  - 绝对化健康承诺（guarantee/healthiest/definitely lose weight…）
  - 特殊人群/慢病（pregnant/diabetes/hypertension/minor…）
  - 或意图本身就是 `HEALTH_RISK`
- **`HEALTH_RISK` / `OTHER` 意图完全不触发 LLM**，直接返回固定文案，既安全又省成本。
- **健康免责声明**：`NeedsDisclaimer` 检测 healthGoal 含减脂/低糖/低碳/养胃等关键词时，在回复末尾追加 `DisclaimerSuffix`。

### 6.10 全链路 Trace（`trace.go`）

- **上下文作用域**：`OpenTrace` 把 `TraceScope` 挂到 `context`，`ScopeFromContext` 读取——**对齐 Java 的 ThreadLocal 作用域**。服务在无 Trace 场景下也可独立使用（scope 为 nil 时忽略埋点）。
- **事件模型**：`TraceEvent` 含 `stepOrder / eventType / phase / agentName / modelName / inputPayload / outputPayload / latencyMs / inputTokens / outputTokens / totalTokens / errorMessage / createdAt`。
- **`CallAgent` 自动埋点**：每次 Agent 调用记录 `AGENT_CALL` 事件（模型名、耗时、token）。
- **负载截断**：`maxPayloadLength=20000`，超长 payload 截断，控制 `trace_json` 体积。
- **一次性落库**：事件先累积在内存，`Close()` 时序列化为 `trace_json` 一次性 `InsertTrace`；`markFailed` 把整条 Trace 置为 FAILED。
- **查询限额**：默认 200、最大 1000（`clampLimit`）。

### 6.11 评估流水线（`evaluation.go`，全文件最大 709 行）

这是项目**最有工程含量**的模块之一，实现"Trace → 指标 → 加权评分"：

**（a）从 `trace_json` 反解快照**（`parseTrace`）：遍历事件，提取预测意图、槽位、澄清动作、token 成本、最终回复文本、`ranked` 候选 ID、`response` 展示 ID、排除 ID、是否触发降级等。

**（b）规则指标（0–1）**：

| 指标                       | 计算方式                                        |
| -------------------------- | ----------------------------------------------- |
| `intentAccuracy`           | 预测意图 == 标注意图                            |
| `slotAccuracy`             | 按维度比对标注槽位，匹配维度数 / 有标注的维度数 |
| `clarifyNecessityAccuracy` | 预测澄清动作 == 标注动作                        |
| `tokenCostScore`           | ≤1000 token→1.0，≥3000→0.0，线性                |
| `latencyScore`             | ≤3000ms→1.0，≥8000ms→0.0，线性                  |
| `fallbackScore`            | `1 - fallbackRate`（是否走了降级/失败）         |
| `safetyCompliance`         | 最终回复不含禁用医疗/绝对化措辞                 |
| `hallucinationControl`     | 展示菜品 ID ⊆ 召回候选 ID（无"凭空菜"）         |
| `multiTurnConsistency`     | 仅 MEAL_ADJUST：排除集非空且与展示集无交集      |

**（c）幻觉控制的巧思**：`hallucinationFree = responseIDs 为空 或 rankedIDs 完全包含 responseIDs`——因为 prompt 已约束"mealId 必须来自候选"，这里用**集合包含关系**在离线阶段自动验证 LLM 是否越界编造菜品。

**（d）安全合规的豁免**：`safetyCompliant` 对固定保守回复 `ConservativeMessage` 直接放行——它提到 "treatment" 只是为了拒绝给出医疗建议。**注释明确指出 Java 原版把这条最安全的回复误判为违规，导致所有 HEALTH_RISK Trace 的 safetyCompliance 恒为 0**，Go 版修复了该缺陷。

**（e）LLM-as-Judge（可选）**：`EvaluationJudgeService` 用 light 模型对 Trace 摘要打 `explanationQuality` 与 `naturalness`（1–5，`clampJudgeScore` 截断），只评表达质量，不重复评意图/槽位/安全/幻觉（那些交给规则）。

**（f）用户反馈分**：`rating/5`；无 rating 时按 action 映射——LIKE/UP/ADOPT/ACCEPT=1.0，DISLIKE/DOWN/REJECT=0.0，SWITCH/CHANGE/REFRESH=0.4。

**（g）加权合成**（`weightedScore`）：**规则 60% + Judge 10% + 反馈 30%**，按"实际存在的权重"归一化；最终 `toPercent` 转百分制。`metricAverages` 对每个指标跨 Trace 求平均（nil 跳过）。

### 6.12 持久层（`store.go`）

- GORM 配置：`Logger.Silent`（应用用 slog 统一日志）、`SkipDefaultTransaction`（每次写都是单语句，跳过默认事务、保持 autocommit）。
- 连接池：`MaxOpenConns/MaxIdleConns` 可配，`ConnMaxLifetime=1h`，启动即 `Ping`。
- 所有查询都**带 `user_id` 作用域**（如 `FindSessionByID(id, userID)`、`ListRecentMessages` 通过 JOIN `takeout_sessions` 强制所有权），天然防越权。
- 写操作返回 `RowsAffected`，上层据此判断"不存在/无权限"。

---

## 7. Agent / LLM 工程技术亮点（重点）

这一节集中回答"这个项目的 Agent 工程做得好在哪"。

### 7.1 结构化的 LLM 输出与健壮解析（`jsonutil.go`）

LLM 输出天然不可靠，项目用一整套工具把"脏输出"洗成结构化数据：

- **`ExtractJSONObject/Array`**：剥离 ` ```json ` 代码围栏与前后散文，定位首个顶层 `{}`/`[]` 再解析，容忍 LLM 多嘴。
- **`PickSlotLabels`**：从 LLM JSON 里按字段抽槽位标签，**只保留字典内合法值**；支持数组或分隔符连接的字符串；**大小写不敏感并回填字典标准拼写**（"fat loss"→"Fat Loss"）——注释指出 Java 版大小写敏感会丢标签，Go 版修复。
- **分隔符兼容全角**：`labelSeparator` 正则包含 `，、；` 等全角符号，因为 LLM 即使在英文输出里偶尔也会用。
- **类型强转对齐 Jackson**：`jsonFloat/jsonInt64/jsonText` 接受数字或数字字符串（`"0.9"`），模拟 Jackson 的 `asDouble/asLong/asText` 宽容行为。

### 7.2 多级降级，永不"整轮失败"

- **意图识别**：LLM 失败 → 关键词规则（`fallbackIntent`）。
- **澄清措辞**：LLM 失败/空 → 模板反问（`FallbackQuestion`）。
- **推荐/套餐话术**：LLM 失败 → 模板理由（`TemplateMealReason`/`templatePlanReason`）+ 模板口语（`TemplateRecommendSpeech`/`TemplatePlanSpeech`）。
- **健康风险/闲聊**：根本不调 LLM，固定文案。

这种"**LLM 负责锦上添花，规则负责雪中送炭**"的分层，是生产级 LLM 应用的关键韧性来源。

### 7.3 幻觉抑制：候选集约束 + 离线集合校验

- **在线约束**：prompt 明确"mealId 必须来自输入候选，绝不编造"；`parseRecommendOutput` 只接受出现在 `topMeals` 中的 ID，其余丢弃并回退模板理由；套餐场景"选菜完全由 Go 规划器决定，LLM 只给理由"，**ID 不可能跨餐次串位**。
- **离线校验**：评估阶段用 `responseIDs ⊆ rankedIDs` 自动检出"凭空菜"，量化幻觉率。

### 7.4 安全合规双保险

- **前置**：`HEALTH_RISK` 意图直接短路到保守回复。
- **后置**：`RiskGuardService` 对"输入+输出"全文做关键词拦截，命中即整段改写。
- **免责声明**：触及减脂等目标自动附加免责语。
- **评估豁免**：保守回复不被误判为违规（修复 Java 缺陷）。

### 7.5 成本与延迟的显式治理

- **双模型分层**：轻量任务（意图/澄清/评审）用 light 模型，重表达（推荐/套餐话术）用 main 模型。
- **历史截断**：只取最近 3 轮、每条消息截断到 120 字符，控制意图 prompt 体积。
- **Top-N 收敛**：召回 50 → 排序 10 → 话术 3，逐级收窄送入 LLM 的上下文。
- **把成本/延迟做成评估指标**：`tokenCostScore`/`latencyScore` 线性映射到 0–1，进入规则总分——**用指标驱动"又快又省"**。

### 7.6 可观测性与评估闭环

- 每轮对话落一条含全部事件、token、延迟的 `trace_json`，前端可回放。
- Trace 上可直接打"真值标注"（意图/槽位/澄清动作），评估流水线据此计算准确率。
- **规则 + LLM-Judge + 用户反馈三路加权**，兼顾客观正确性、表达质量与真实满意度。
- 这套"埋点→标注→评估"闭环，使模型/Prompt 的每次迭代都可被量化回归。

### 7.7 并发与资源治理

- **每会话引用计数锁** + **LLM 调用超时**，共同保证"挂起的 provider 不会永久霸占会话锁"。
- **无状态共享 Agent 实例**，避免按会话缓存 Agent 的内存膨胀。
- **Trace 负载截断**、**查询限额**，控制存储与内存。

### 7.8 Prompt 工程

五个提示词（`prompts/*.txt`）结构统一为 `[Role] / [Requirements|Intents|Rules] / [Input format] / [Output format]`：

- `intent.txt`：明确六类意图定义与示例、上下文相关调整规则、"只能从 slotOptions 选值、不得造标签、可语义归一"、严格 JSON 输出样例。
- `recommend_response.txt` / `plan_response.txt`：限定理由字数、禁用"best/guaranteed"等绝对化措辞、要求 mealId 来自候选、规定 speechText 结构（开场/主体/收尾）。
- `clarify.txt`：要求"像朋友聊天一样自然、不重复用户已说的、只输出一个问题文本"。
- `evaluation_judge.txt`：限定只评 explanationQuality/naturalness，不越权重评其它维度，信息不足给中间分。

---

## 8. 前端深度解析

### 8.1 应用骨架

- `App.tsx` 用 `HashRouter`（适配静态文件/`file://` 部署），路由挂在 `AppShell` 布局下：`/`（仪表盘）、`/chat`、`/meals/personal`、`/meals/public`、`/traces`、`/evaluations`。
- `QueryClient` 默认 `retry:1`、`refetchOnWindowFocus:false`、`staleTime:30s`。
- `AppShell` 提供可折叠侧边栏、主题切换、**顶栏可切换 `X-User-Id`**（改动即 `invalidateQueries` 全站刷新，方便多用户测试）。

### 8.2 数据层

- `lib/api.ts`：统一 `request` 封装——自动注入 `X-User-Id`、`Content-Type`，解析 `204`/空响应，错误时抛 `ApiError(status)` 并优先读取后端 `message`。
- `lib/schemas.ts`：**每个 API 响应都有 zod schema**，`TakeoutApi` 的每个方法都对返回值 `.parse()`，把运行时类型不安全挡在边界。
- `hooks/queries.ts`：把各端点封装成 `useQuery/useMutation`，写操作成功后 `invalidateQueries` 精准失效对应缓存键。

### 8.3 状态管理

- **聊天**：zustand `chat-store`（sessionId、sourceMode、sending、messages）。
- **偏好**：TanStack Store `preferences`（theme、sidebarCollapsed），持久化到 localStorage；`applyTheme` 切换 `document.documentElement` 的 `.dark` 类。
- **用户 ID**：ahooks `useLocalStorageState`。

### 8.4 页面解析

- **Dashboard**：Hero 渐变区 + 四步流水线说明（理解意图→填充槽位→排序推荐→反馈学习）+ 个人/公共库计数 + 最近 Trace 概览 + 快捷入口。
- **Chat**（`chat.tsx`）：
  - **数据源切换**（Personal/Public）：因后端把 sourceMode 绑定到会话，**切换即开新会话**并 toast 说明，避免"表面切换、实际仍查旧库"。
  - 快捷提问、空态引导、"Thinking…" 动画、消息气泡（用户右/助手左）。
  - **CLARIFY 响应**展示 `missingSlots` 徽章；**ANSWER** 渲染菜品卡片网格。
  - **每道菜 LIKE / ADOPT / DISLIKE 反馈**按钮，调用 `/feedback` 并本地高亮；与旧版一致，反馈同时携带 `rating`（DISLIKE→2，其余→5），使评估管线可以按评分列计分。
  - 每条助手消息带 **trace 短链**，跳转 `/traces?traceId=…`。
  - Enter 发送、Shift+Enter 换行。
- **Personal Meals**：CRUD 对话框，`SlotPicker` 多选芯片（按槽位维度），zod 校验（菜名必填、至少一个餐次），关键词过滤，删除二次确认。
- **Public Meals**：只读网格，按名称/标签本地过滤。
- **Traces**（`traces.tsx`）：
  - 时间范围 + **会话 ID（可选）** + only-unlabeled + limit 过滤；填写会话 ID 后改走 `/debug/sessions/:sessionId/traces` 按会话查询（与旧版行为一致）；
  - 左侧 Trace 列表（**会话**/状态/事件数/耗时/创建时间，含 "labeled" 徽章），每次查询后自动选中第一条（对齐旧版）；
  - 右侧详情：解析 `traceJson` → **TanStack Virtual 虚拟化事件列表**（`estimateSize:64`、`overscan:6`、`measureElement` 动态测高），每个事件卡可展开查看 input/output payload（`safePrettyJson` 美化）、耗时、token、错误；
  - **Ground-truth 打标表单**（期望意图/澄清动作/槽位 JSON/备注）。
- **Evaluations**（`evaluations.tsx`，510 行）：
  - 时间范围 + LLM-Judge 开关 + limit；
  - 汇总卡片（总数/已标注/平均分）+ **指标均值面板**（按 `METRIC_ORDER` 规范顺序展示，raw 指标带单位，0–1 指标转百分制着色）；
  - 每条 Trace 可展开看全量指标 + detail JSON。
  - **细节**：Go 的 `metricAverages` 是 JSON 对象、键按字母序到达，前端用 `orderedMetrics` 恢复规则管线的规范顺序。

### 8.5 UI/UX 亮点

- **槽位色彩系统**：`slot-chips.tsx` 为七个维度各配一套柔和色调（amber/pink/sky/emerald/orange/rose/violet），菜品卡片一眼可读。
- **设计令牌**：`index.css` 用 oklch 定义完整 light/dark 主题变量（含 sidebar/chart），`@custom-variant dark` 支持类名切换。
- **Match 分数**：菜品卡片展示 `matchScore` 百分比（"X% match"）或 "Base item"。
- **可访问性**：反馈按钮带 `aria-label`/`aria-pressed`，思考动画带 `role="status"`。

---

## 9. 测试与工程质量

- **单元测试覆盖面广**（`logic_test.go` 580 行，28 个用例）：槽位合并/归一化、澄清规则、排序与排除、降级意图优先级、意图修正规则、风险护栏、JSON 提取、槽位标签过滤/全角分隔/大小写归一、餐次解析、会话状态往返、意图解析（flat/nested/字符串置信度）、评估打分助手、Trace 快照解析（含幻觉检出）、安全合规豁免、免责声明、sourceMode 归一、**会话锁释放后被清理**、404 状态携带、标量强转等。
- **LLM 层测试**（`llm_test.go`）：用 `httptest` 起 mock OpenAI 端点，验证**超时生效**（挂起 provider 被 50ms 超时掐断）与**正常返回文本+usage**。
- **API 层测试**：`parseUserID`、body 限流拒绝超大请求、业务错误渲染。
- **配置测试**：DSN 含 `clientFoundRows`、超时换算、默认 LLM 超时、空文件保留默认、部分文件合并默认。
- 实测 `go build ./... && go vet ./... && go test ./...` **全部通过**。
- **注释文化极佳**：大量注释解释"为什么"而非"是什么"，尤其集中记录与 Java 版的行为对齐/缺陷修复点，可维护性强。

---

## 10. API 一览

Base path：`/api/v1/takeout`；请求头 `X-User-Id`（缺省 1，非法 400）；body 上限 1 MiB；业务错误 `400 {"message"}`，未预期 `500`，无返回体 `204`。

| Method | Path                                | 说明                                           |
| ------ | ----------------------------------- | ---------------------------------------------- |
| POST   | `/chat`                             | 跑一轮编排（sessionId 为空自动建会话）         |
| POST   | `/sessions`                         | 创建空会话                                     |
| GET    | `/meals/personal`                   | 当前用户个人菜品                               |
| POST   | `/meals/personal`                   | 新增个人菜品                                   |
| PUT    | `/meals/personal/:mealId`           | 更新个人菜品                                   |
| DELETE | `/meals/personal/:mealId`           | 删除个人菜品（204）                            |
| GET    | `/meals/public`                     | 公共菜品库                                     |
| GET    | `/slot-options`                     | 按维度分组的槽位字典                           |
| POST   | `/feedback`                         | 保存 LIKE/ADOPT/DISLIKE 反馈（204）            |
| POST   | `/evaluations`                      | 对时间窗内 Trace 评分（规则 + 可选 LLM-Judge） |
| GET    | `/debug/traces`                     | 列表（startAt/endAt/onlyUnlabeled/limit）      |
| GET    | `/debug/traces/:traceId`            | 单条 Trace（含完整事件文档）                   |
| GET    | `/debug/sessions/:sessionId/traces` | 某会话的 Trace                                 |
| PUT    | `/debug/traces/:traceId/label`      | 保存真值标注                                   |
| GET    | `/healthz`                          | 存活探针                                       |

---

## 11. 部署与配置

### 启动步骤

```bash
# 1. 数据库
mysql -u root -p < server/db/takeout_db.sql

# 2. 后端
cd server
cp config.example.yaml config.yaml
cp .env.example .env        # 填 LLM_API_KEY / MYSQL_PASSWORD
go run ./cmd/takeout_agent  # 监听 127.0.0.1:8090

# 3. 前端
cd client && pnpm install && pnpm dev   # http://localhost:5173，/api 代理到 8090

# 生产：pnpm build 产出 client/dist，Go 服务经 static.dir 自动托管
```

### 关键环境变量（`.env`）

| 变量              | 对应配置                | 用途                                 |
| ----------------- | ----------------------- | ------------------------------------ |
| `LLM_API_KEY`     | `llm.api_key_env`       | OpenAI 兼容端点 Bearer Key           |
| `LLM_BASE_URL`    | `llm.base_url_env`      | 端点 base；空=DashScope 兼容模式默认 |
| `LLM_MAIN_MODEL`  | `llm.main_model_env`    | 推荐/套餐话术模型                    |
| `LLM_LIGHT_MODEL` | `llm.light_model_env`   | 意图/澄清/评审模型                   |
| `MYSQL_PASSWORD`  | `database.password_env` | MySQL 密码                           |

> LLM 客户端讲 OpenAI 兼容 `chat/completions` 协议，任何兼容 provider 均可；`config.yaml`/`.env` 已被 `.gitignore` 排除，密钥不入库。

---

## 12. 亮点总结与改进建议

### 12.1 核心亮点

1. **规则 + LLM 的清晰分工**：确定性逻辑（澄清判定、排序、拦截、去重）全部规则化，LLM 只做理解与表达，系统可控、可降级、可测试。
2. **生产级韧性**：多级模板降级、每调用超时、会话锁引用计数清理、请求体限流、越权作用域查询。
3. **一流的可观测性**：全链路 Trace（含 prompt/输出/token/延迟）落库可回放。
4. **完整的离线评估闭环**：规则指标 + LLM-as-Judge + 用户反馈加权，含幻觉集合校验与安全豁免，把"好不好"变成可回归的数字。
5. **严谨的跨语言迁移**：大量注释记录与 Java/Spring/MyBatis/Jackson 的对齐点及缺陷修复（`clientFoundRows`、保守回复安全豁免、槽位大小写归一、会话锁清理），是优秀的迁移工程样本。
6. **前端工程化到位**：zod 边界校验、TanStack Query 缓存治理、虚拟化大列表、主题/多用户切换、槽位色彩系统。

### 12.2 可考虑的改进方向

- **鉴权**：当前 `X-User-Id` 仅是明文头、无鉴权，适合内网/演示；生产应接入真实身份认证与鉴权。
- **流式响应**：`yukino_http` 已支持 SSE，可把 `/chat` 升级为流式输出，显著改善 LLM 长回复的等待体验。
- **召回升级**：目前纯标签 `JSON_OVERLAPS` 精确匹配，可引入向量/语义召回或标签同义词扩展，提升模糊表达的命中率。
- **排序可解释性**：`slotScore` 是七维等权平均，可为高价值维度（如 healthGoal）加权，并把各维命中明细回传前端增强解释。
- **评估指标持久化**：评估结果当前即时计算不落库，可考虑保存历史评估报告以便趋势对比。
- **国际化**：后端文案与前端均为英文、提示词为英文，若面向中文用户可做 i18n 与中文 Prompt/槽位字典。
- **并发写一致性**：会话状态更新依赖"单写者 + RowsAffected 检测"，高并发多实例部署时可引入乐观锁版本号。

---

## 13. Java→Go 迁移对齐审查与修复记录

本节记录对照旧版 Java 工程（`.legacy/Diet-agent`）的**逐文件全量** review（非抽查）结论与修复。

### 13.1 审查范围

- **Java 侧**：7 个 Controller、17 个 Service、5 个 Agent Builder、5 份 Prompt、5 个 MyBatis Mapper + XML、全部 Model/Enum、工具类（JsonService/LlmJsonService/SlotJsonPicker）、配置、`diet_db.sql`、静态前端（index.html / api.js / app.js / app.css）逐行阅读。
- **Go 侧**：server 全部 21 个 `.go` 文件（domain / api / config / cmd / 测试）、5 份 Prompt、`takeout_db.sql`、client 全部约 50 个源文件（pages / components / hooks / stores / lib）逐行阅读。
- **逐项比对**：端点契约、状态机分支、规则阈值、SQL 语义、序列化细节、降级路径、Trace 事件、评估公式、前端交互。

### 13.2 确认对齐（无需改动）

| 维度 | 结论 |
| --- | --- |
| API 端点 | 15 个业务端点 + `/healthz` 与 Java 一一对应（前缀 `diet`→`takeout`，端口 8080→8090） |
| 编排状态机 | 分支、Trace 事件顺序、phase 迁移完全一致 |
| 意图修正 | 4 条规则 + 0.4 置信度阈值一致；关键词回退优先级一致 |
| 澄清规则 | mealTime 必填、healthGoal 可被强偏好（cuisine/taste/scene/convenience）豁免，一致 |
| 槽位合并 | 当前轮非空覆盖、空则保留历史，一致 |
| 召回 SQL | `? = '[]' OR JSON_OVERLAPS(col, ?)` 七维语义一致；DSN `clientFoundRows=true` 对齐 Connector/J 的 matched-rows 语义 |
| 排序 | 七维 overlap 均值、50→10→Top3、MEAL_ADJUST 排除已推荐，一致 |
| 多餐规划 | 默认早/午/晚、≥2 个具体餐次保留用户顺序、跨餐次去重，一致 |
| 风险护栏 | 5 组规则关键词语义对齐（英文关键词按词边界匹配，避免 "curry" 误触 "cure"） |
| Trace | 事件类型、作用域、payload 截断（20000）、limit（默认 200 / 上限 1000）一致 |
| 评估 | 9 项规则指标、60/10/30 加权、token（≤1000→1，≥3000→0）与延迟（≤3s→1，≥8s→0）分段线性、反馈 rating 优先 + action 映射（LIKE=1/DISLIKE=0/SWITCH=0.4）、Judge 1–5 截断，一致 |
| 会话持久化 | `slots._meta`（sourceMode/currentIntent）序列化、空数组而非 null，一致 |
| 前端页面 | 6 页导航与旧版一致；数据源切换即开新会话、快捷提问、打标表单、指标规范排序等交互一致 |

### 13.3 Java 旧版存量缺陷（已在 Go 版修复，本轮复核确认）

1. `DietExceptionHandler` 的 `basePackages="com.diet.newdiet"` 指错包，全局异常处理**从未生效**（死代码）→ Go 的 `adapt` + `writeError` 正确渲染 400/404/500。
2. `sessionLocks` Map 只增不减 → 内存泄漏；Go 用引用计数锁并在释放后清理（含单测 `TestSessionLocksArePrunedAfterRelease`）。
3. 评估 `safetyCompliance` 把含 "治疗" 字样的**固定保守回复**误判为违规 → Go 豁免 `ConservativeMessage`。
4. `parseTrace` 漏收 `MEAL_PLAN_SEARCHED` 的候选 ID → 规划类 trace 被误判幻觉 → Go 已补齐。
5. `SlotJsonPicker` 大小写敏感导致合法标签被丢弃 → Go 大小写不敏感归一到字典规范拼写。
6. `updatePersonalMeal` 潜在 NPE → Go nil-safe。
7. Java 针对 AgentScope 端点类型的 workaround 在 Go 不再需要（agentscope-go v2 统一 OpenAI 兼容端点）。

### 13.4 本轮新发现并修复的问题

**A. 前端功能对齐（旧版有、Go 版缺失）**

1. **Traces 页会话过滤**：旧版支持按会话 ID 查询（填写后改调 `listSessionTraces`）→ 已在 `traces.tsx` 补 Session ID 输入框、`queries.ts` 补 `useSessionTraces`，并按会话/时间范围分流查询（会话查询只带 limit，与旧版一致）。
2. **Traces 列表 Session 列 + 查询后自动选中第一条**：对齐旧版表格列与选择行为。
3. **Chat 反馈携带 `rating`**：旧版发送 `rating`（DISLIKE→2，其余→5）→ 已补齐，使评估管线可按评分列计分（后端 `singleFeedbackScore` 本就 rating 优先）。

**B. 英文文案缺陷（"diet→takeout" 批量替换残留）**

4. `risk.go`："extreme takeouting advice"→"extreme dieting advice"；"registered takeoutitian"→"registered dietitian"；关键词 "extreme takeout"→"extreme dieting"（变量同步更名 `extremeDietingKeywords`）。
5. `intent.go`：健康风险关键词 "extreme takeout"→"extreme dieting"。
6. `recommend.go`：免责声明 "a takeoutitian"→"a dietitian"。
7. `prompts/intent.txt`："extreme takeouting"→"extreme dieting"；示例 "starvation takeout"→"starvation diet"。

**C. 工程一致性**

8. `server/db/diet_db.sql` → `server/db/takeout_db.sql`：文件名与内容（`CREATE DATABASE takeout_db`）、README、前端空态提示（"Import db/takeout_db.sql"）矛盾 → 已重命名并同步 README。

### 13.5 修复后验证

- server：`go build ./... && go vet ./... && go test ./...` 全部通过。
- client：`tsc -b --noEmit` 通过、`pnpm build` 成功、`prettier --check` 通过。

---

_报告基于对 `apps/takeout_agent` 全部后端 Go 源码、SQL 脚本、前端 TS/TSX 源码、`libs/yukino_http` 框架，以及 `.legacy/Diet-agent` 全部 Java 源码的逐文件对照阅读整理；修复后 `go build / vet / test` 与前端 `typecheck / build` 均已本地验证通过。_
