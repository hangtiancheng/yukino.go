# yukino-creative-agent — 智能体对话 + 内容创作/风控基座

由两个 Go 服务合并为**单二进制**：

- **go-learn**：智能体对话系统（creative-gravity 复刻）——ReAct 循环引擎 + SEDA 图调度 + Redis Streams 异步管道 + 多 Agent 子任务编排。
- **base-server**：内容创作/风控基座——creation 创作流程（预创建/创建/后处理/回调）、risk 风控审计、points 积分记账、monitor 离线任务（超时/补偿/风控离线）。

前端为独立 React 应用（`../yukino-creative-agent`），**前后端分离部署**：本服务只提供 API（CORS 已放行跨域），前端独立构建后由任意静态服务器托管（nginx / Vercel / OSS+CDN 等）。

## 技术栈

| 类别     | 技术                                                                                           | 用途                                                                         |
| -------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| Web 框架 | [yukino_http](https://github.com/hangtiancheng/yukino.go/tree/main/libs/yukino_http)           | HTTP 路由 + 中间件 + SSE                                                     |
| ORM      | GORM + MySQL                                                                                   | 业务表 AutoMigrate（users + cg\_\* + creation/risk/points）                  |
| 消息队列 | Redis Streams（go-redis/v9）                                                                   | 异步对话/子任务投递（消费组 + ACK）；无 Redis 时降级进程内队列               |
| 缓存/锁  | Redis + [yukino_cache](https://github.com/hangtiancheng/yukino.go/tree/main/libs/yukino_cache) | chunk 中转、心跳 NX 锁、token 拉黑；chunk store 支持 yukino_cache 进程内底座 |
| LLM      | eino（OpenAI 兼容 / Anthropic Claude）                                                         | 管家 + 子 Agent（未配置回退 FakeLLM）                                        |
| 可观测性 | OpenTelemetry → Jaeger + Prometheus `/metrics`                                                 | 全链路 trace（HTTP → Worker → 子任务 → LLM）+ 指标                           |
| 错误上报 | sentry-go                                                                                      | panic/错误上报（DSN 配置后启用）                                             |
| 日志     | Zap（结构化 JSON）                                                                             | 运行时日志 + trace_id 关联                                                   |
| 配置     | JSON + go tag                                                                                  | 环境变量 > secrets.json > config.json > 默认值                               |
| ID 生成  | Snowflake（标准库实现）                                                                        | task_run / task_step / message 业务主键                                      |
| 测试     | 标准库 testing + `pkg/testx`                                                                   | 断言助手（testx）+ suite 运行器（testx/suite）                               |

## 快速开始

```bash
# 1. 启动依赖（MySQL + Redis + Jaeger）
docker compose up -d mysql redis jaeger

# 2. 复制并填写 secrets（MySQL 密码 / LLM Key / Sentry DSN）
cp config/secrets.json.example config/secrets.json

# 3. 启动服务
make run

# 4. 测试 / 构建 / 连通性探测（sqlite 测试驱动依赖 cgo）
make test          # 全部单元测试
make test-race     # 竞态检测
make build         # 编译到 build/creative-agent
make ping          # 探测远程 MySQL/Redis 连通性（集成测试）

```

前端独立仓库 `../yukino-creative-agent`（前后端分离，不由本服务托管）：开发用 `pnpm dev`（vite proxy 转发 /api、/aigc），
生产构建 `VITE_API_BASE_URL=<后端地址> pnpm build` 后把 `dist/` 部署到任意静态服务器，见其 README。

> 数据库连接见 `config/config.json` 的 mysql/redis 段，`AutoMigrate` 首次会较慢，属正常现象。

## 项目结构

```
cmd/
  api/main.go            # 唯一入口（加载配置 → app.New → app.Run）
  checkdb/main.go        # 调试工具：按 session 打印 task_run/task_step 状态
config/
  config.go              # 配置加载（优先级：环境变量 > secrets.json > config.json > 默认值）
  config.json            # 公开配置（主机/端口，含 MySQL/Redis/Jaeger/LLM 地址）
  secrets.json.example   # 敏感配置模板（密码/API Key，Git 忽略）
internal/
  core/                    # 【引擎层】纯算法、零业务：SEDA 图引擎 + ReAct 循环
    seda/                  #   图引擎：Flow 图调度 + Slice 流式通道 + 注册表
    react/                 #   ReAct 循环：Supervisor 五分支 + 组件接口 + Checkpoint
  appkit/                  # 【复用骨架层】跑一个 agent 的机器（无业务 prompt）
    runner/                #   SupervisorRunner（ReAct 运行流程 + SSE 帧 + PlanOutput）
    provider/              #   注入层（按功能类别拆子包）
      llm/                 #     LLM 实现（eino ChatModel：OpenAI 兼容 / Claude）
      tools/               #     工具系统核心（ToolRegistry/ToolsProvider/内建工具/composite）
      skill/               #     SKILL.md 动态工具源（SkillProvider/FSRepository）
      fake/                #     测试桩（FakeLLM/FakeStrategy/FakeHooks + DefaultLLM 工厂）
  business/                # 【业务层】对话与子任务
    conversation/          #   域1：对话主链路（同步图 + 异步 worker）
      flow.go              #     flow 图定义 + 算子注册（业务入口门面）
      ops/                 #     SEDA 算子：con_reader / supervisor_agent / con_writer / con_end
      manager/             #     管家约束链（generate_response 协议 / 校验 / 管家 prompt）
      memory/              #     IntentMemory + FlowStage
      fastpath/            #     快捷路径（HIL resume / Retry）
      worker/              #     对话异步 worker（Consumer / Recovery / heartbeat / attach）
    subtask/               #   域2：子任务执行与调度（纯异步）
      pipeline.go          #     产线定义（mix_video / article）
      executor/            #     TaskExecutor / SubAgent / Submitter
      taskflow/            #     Consumer / Poller / CronTaskPoller
  auth/                    # 【base-server】认证 DDD 模块（application/domain/infrastructure/interfaces）
  creation/                # 【base-server】创作流程 DDD 模块（预创建/创建/后处理/回调/补偿）
  risk/                    # 【base-server】风控审计 DDD 模块
  points/                  # 【base-server】积分记账 DDD 模块
  monitor/                 # 【base-server】离线任务（超时/补偿/风控离线 job）
  model/                   # 【基础设施】GORM 模型 + Snowflake ID
  repository/              # 【基础设施】数据访问层（接口 + GORM 实现）
  infra/                   # 【基础设施】ChunkStore / StreamQueue / Locker（Redis 与进程内双底座）
  service/                 # 用户/认证业务（JWT 签发）
  handler/                 # 【边界】HTTP 处理器（认证 / 对话 / 点卡确认 / 回调）
  router/                  # 【边界】路由注册（API + /aigc + /inner + /metrics）
  middleware/              # 【边界】中间件（RequestID → OTel → Logger → CORS → Recovery → JWT）
  database/                # MySQL 初始化 + AutoMigrate
  app/                     # 【边界】启动编排 + 各层接线 + 优雅关机 + 异步 Worker 启动
pkg/
  sse/                   # SSE Writer + 事件类型
  otelx/                 # OTel 初始化（内存/OTLP exporter）+ GORM zap logger + GORM SQL tracing 插件
  tracex/                # traceparent 跨进程透传 + 日志 helper（trace_id 关联）
  cache/                 # Redis 客户端
  logger/                # Zap 封装
  response/              # 统一响应格式 + 错误码
  testx/                 # 标准库断言助手（取代 testify）+ suite 运行器（testx/suite）
skills/                  # SKILL.md 动态工具源（如 weather）
docs/
  go-learn/              # go-learn 文档（分章学习笔记 / 源码分析 / 实现备忘 / 面试 QA）
  base-server/           # base-server 文档（同上）
```

## 架构总览

```
                    ┌────────────────────────────────────────────────────┐
                    │                      HTTP 层（yukino_http）        │
                    │            POST/GET /aigc/conversation             │
                    └──────────────┬─────────────────────────────────────┘
                                   │ body + trace
                    ┌──────────────▼─────────────────────────────────────┐
                    │       SEDA 图调度（core/seda，flow.go 接线）       │
                    │  con_reader → supervisor_agent → con_writer → end  │
                    │      （fork / join / 短路 / Slice 流式通道）       │
                    └──────────────┬──────────────────┬──────────────────┘
                                   │ 同步             │ 异步
                                   │                  │ 投 Redis Stream
                    ┌─────────────────────┐   ┌──────────────────────────┐
                    │     ReAct 引擎      │   │  Worker（conversation/   │
                    │    core/react +     │   │         worker）         │
                    │    appkit/runner    │   │  CAS 抢占 + 心跳 + 恢复  │
                    │      五分支：       │   │   跑同一个 ReAct 引擎    │
                    │ Continue/Terminal/  │   │ chunk 写入 store → 前端  │
                    │  Interrupt/Relay/   │   │     attach 断点续拉      │
                    │        Retry        │   └───────────────┬──────────┘
                    └──────────┬──────────┘                   │ new_agents_plan
                               │                              │
                    ┌──────────▼──────────────────────────────▼──────────┐
                    │       子任务系统（business/subtask）               │
                    │   管家产出 plan → Submitter 投递 → TaskExecutor    │
                    │    跑子 Agent → Poller 轮询 task_step 回流进度     │
                    └──────────┬──────────────────────────┬──────────────┘
                               │ 对话推进                 │ Cron 推进
                               │ 卡片确认→快路径          │ CronTaskPoller 每 30s
                               └──────────────────────────┘

创作/风控基座（base-server 域，同进程）：
  /inner/creation/*  预创建（风控前置+积分冻结）→ 创建 → 同步/异步后处理 → 回调
  /inner/callback/*  外部任务回调（task_detail / risk）→ 推进 task_run/task_step
  monitor jobs       超时扫描 / 补偿 / 风控离线审计
```

### 三个核心引擎的复用

同一个 ReAct 引擎（`core/react`）由 `appkit/runner` 装配，被 **三处复用**，仅注入不同组件：

| 调用点          | 注入的 LLM            | Strategy                       | 职责                |
| --------------- | --------------------- | ------------------------------ | ------------------- |
| 同步 supervisor | FakeLLM / 真实 LLM    | FakeStrategy / ManagerStrategy | 请求内直接回复      |
| 异步 Worker     | 同上                  | 同上 + FakeHILStrategy         | 管家决策，产出 plan |
| 子 Agent        | FakeSubLLM / 真实 LLM | 有工具就 Continue              | 执行单个产线任务    |

### 数据流：一次"帮我生成一条 15s 短视频"

```
1. 用户 POST /aigc/conversation
2. con_reader：建 session/message_record/async_request（Pending），写 Stream
3. Worker CAS 抢占（Pending→InProgress）→ 心跳 → 跑管家 ReAct
4. 管家 LLM 调用 generate_response(new_agents_plan, agents_pipeline=mix_video)
5. 服务端"只翻译"：校验 agent 属于产线 → 展开 plan_steps（剔除 idea_king）
6. Submitter 先落库 task_run(Pending) 再投 Stream
7. TaskExecutor 消费 → 跑子 Agent（gold_scriptwriter）→ 建 task_step
8. Poller 轮询 task_step 把进度帧推回前端（task-step / task-done）
9. 全部完成后 → message-complete → chat_text 落库 → 前端释放
```

## 核心设计要点

### 1. ReAct 引擎（`internal/core/react/supervisor.go`）

零业务的循环引擎：`调 LLM → Strategy 决策 → 按 Action 分发`。五分支：

- **Continue**：执行工具，结果当 observation 塞回，继续循环
- **Terminal**：正常终止，输出 friendly_reply
- **Interrupt**：HIL 断点，保存 checkpoint 后返回（前端可"继续"恢复）
- **Relay**：服务端自问自答，注入新 query 重建 prompt
- **Retry**：输出不合规，注入纠错提示，不消耗步数重来

所有业务行为（prompt / 工具 / 决策 / 钩子）通过 LLM、Tools、Strategy、Hooks 注入。

### 2. 管家约束链（`internal/business/conversation/manager/manager_strategy.go`）

管家 LLM 必须用 `generate_response` 工具与 server 通信，四重约束：

1. **强制 Function Calling**：无 tool_calls 直接 Retry
2. **工具名白名单**：只允许 generate_response / idea_king_api
3. **Schema 校验**：task_type 枚举 + agent 白名单 + 参数 JSON 修复
4. **失败闭环**：所有失败注入纠错提示（引擎最多重试 2 次）

### 3. 多 Agent 产线（`internal/business/subtask/pipeline.go` + `pipeline_registry.go`）

**LLM 决定链怎么装配，服务端只翻译**：

- 产线模板固定：`mix_video`（idea_king → gold_scriptwriter → edit_master）、`article`
- LLM 在 `task_arguments.agents` 里决定链的顺序/组成
- 服务端只做：校验 agent 属于所选产线（防越权）、补缺失 input、剔除选题 agent（走 idea_king_api 工具）
- 链的推进：**对话路径**靠"点卡确认 → FlowStage 推进 → 快路径重投递"；**Cron 路径**靠 CronTaskPoller 自动推进

### 4. 异步管道可靠性

| 机制           | 说明                                                                                             |
| -------------- | ------------------------------------------------------------------------------------------------ |
| 先落库再投递   | task_run 建成 Pending 即持久化，投递失败标记 Failed 不阻断主流程                                 |
| NX 锁          | `cg:hb:{msgID}` 防重复执行（Redis SetNX / 进程内 MemoryLocker）                                  |
| MySQL CAS 抢占 | async_request Pending→InProgress，AttemptCount 原子 +1                                           |
| 心跳           | 每 5s 刷新 heartbeat_at，超过 15s 认定死亡                                                       |
| 僵尸恢复       | RecoveryScanner 每 30s 扫 InProgress 过期记录：有 EndTurn → 补 Completed；无 → 重置 Pending 重投 |
| 断点续拉       | 前端带 lastSeq 游标从 chunk store 续拉（重连不丢帧）                                             |

### 5. 双底座基础设施（`internal/infra`）

ChunkStore / StreamQueue / Locker 均有两套实现，`app.New` 按 Redis 可用性自动选择：

- **Redis 底座**：跨进程部署（生产）
- **进程内底座**：MemoryChunkStore（yukino_cache Group）/ MemoryStreamQueue / MemoryLocker（单二进制、无 Redis、单元测试）

### 6. 工具系统（`internal/appkit/provider/tools/` + `internal/appkit/provider/skill/`）

- **ToolRegistry**（`provider/tools/tools.go`）：Go 代码编译期 Register（内建工具：search / generate_image / create_video_task / dispatch_agent）
- **SkillProvider**（`provider/skill/skill.go`）：扫描 `skills/*/SKILL.md`，`skill_load` 懒加载后 API 动态进入工具列表；`${VAR}` 环境变量展开；`injected: true` 参数服务端注入
- **CompositeProvider**（`provider/tools/composite.go`）：合并内建 + 动态工具源
- 工具失败不中断循环，错误字符串当 observation 塞回让 LLM 自己决策

### 7. 可观测性

全链路 trace：HTTP span → `supervisor.react` → LLM 调用（otelhttp 出站）→ Stream 透传 traceparent → Worker `worker.process` → 子任务 `task.process`。日志通过 `tracex.Logger` 带 trace_id 关联；`/metrics` 暴露 Prometheus 指标；panic 与错误经 sentry-go 上报（配置 DSN 后）。

## API

### REST（`/api/v1`）

| 方法 | 路径               | 说明               | 认证         |
| ---- | ------------------ | ------------------ | ------------ |
| GET  | `/api/v1/ping`     | 健康检查           | 公开         |
| POST | `/api/v1/register` | 用户注册           | 公开         |
| POST | `/api/v1/login`    | 登录获取 JWT       | 公开         |
| GET  | `/api/v1/example`  | 受保护示例         | Bearer token |
| POST | `/api/v1/logout`   | 登出（token 拉黑） | Bearer token |

### 对话（`/aigc`）

| 方法     | 路径                               | 说明                                                                    |
| -------- | ---------------------------------- | ----------------------------------------------------------------------- |
| POST/GET | `/aigc/conversation`               | 智能体对话（SSE 流；GET 供 EventSource，带 reconnect/lastSeq 断点续拉） |
| POST     | `/aigc/agent/submit_clarification` | 点卡确认（写回 intent_memory，推进 FlowStage）                          |
| POST     | `/aigc/callback/task_detail`       | 外部任务回调（豁免 JWT，服务端调用）                                    |

### 创作/风控（`/inner`，base-server 域）

| 方法 | 路径                                | 说明                          |
| ---- | ----------------------------------- | ----------------------------- |
| POST | `/inner/creation/pre_create`        | 预创建（风控前置 + 积分冻结） |
| POST | `/inner/creation/create`            | 创建创作任务                  |
| POST | `/inner/creation/sync_post_create`  | 同步后处理                    |
| POST | `/inner/creation/async_post_create` | 异步后处理                    |
| POST | `/inner/creation/handle_error`      | 错误处理（退款/补偿）         |
| POST | `/inner/callback/task_detail`       | 创作任务回调                  |
| POST | `/inner/callback/risk`              | 风控回调                      |

### 运维

| 方法 | 路径       | 说明                                         |
| ---- | ---------- | -------------------------------------------- |
| GET  | `/metrics` | Prometheus 指标（`metrics.enabled=true` 时） |

请求示例（对话 POST）：

```bash
curl -N -X POST http://localhost:8080/aigc/conversation \
  -H "Content-Type: application/json" -H "Authorization: Bearer <token>" \
  -d '{"sessionId":"s1","agentId":"demo","userId":1,"query":"帮我生成一条15s的混剪短视频"}'
```

响应为 SSE 事件流：

```
data: {"type":"message-start","msgId":"2084621017589747712"}

data: {"type":"text-delta","content":"好的，我将为你生成","textId":"t1"}

data: {"type":"task-step","msgId":"...","agentId":"gold_scriptwriter","status":"running"}

data: {"type":"message-complete","msgId":"2084621017589747712"}
```

SSE 事件类型：

| event                                  | 说明                           |
| -------------------------------------- | ------------------------------ |
| `message-start`                        | 对话开始，带 msgId             |
| `text-delta`                           | 流式文本增量                   |
| `tool-call-start` / `tool-call-result` | 工具调用开始 / 结果            |
| `interrupt`                            | HIL 断点（前端展示"继续"按钮） |
| `task-step` / `task-done`              | 子任务进度 / 完成              |
| `message-complete`                     | 对话完成                       |
| `error`                                | 执行出错                       |

## 配置

加载优先级：**环境变量 > `config/secrets.json` > `config/config.json` > `config.go` 默认值**。
环境变量按 json 路径派生（大写 + 下划线），`env` tag 可覆盖（如 `IDEA_KING_BASE_URL`）。

| 环境变量                                       | 默认值                          | 说明                                           |
| ---------------------------------------------- | ------------------------------- | ---------------------------------------------- |
| `SERVER_PORT`                                  | `8080`                          | HTTP 端口                                      |
| `JWT_SECRET`                                   | `dev-secret-change-me`          | JWT 签名密钥                                   |
| `MYSQL_HOST` / `MYSQL_PORT` / `MYSQL_DATABASE` | `127.0.0.1:3306` / `all_in_one` | MySQL                                          |
| `REDIS_HOST` / `REDIS_PORT` / `REDIS_DB`       | `127.0.0.1:6379` / `0`          | Redis（不可达时降级进程内底座）                |
| `ASYNC_ENABLED`                                | `true`                          | 异步管道开关（关闭则纯同步模式）               |
| `LLM_PROVIDER`                                 | `openai`                        | `openai`（OpenAI 兼容）/ `claude`（Anthropic） |
| `LLM_BASE_URL` / `LLM_API_KEY` / `LLM_MODEL`   | —                               | 真实 LLM。配置了才启用，否则 FakeLLM           |
| `CRON_POLL_INTERVAL`                           | `30s`                           | Cron 多 Agent 自动推进间隔                     |
| `OTEL_ENABLED` / `OTEL_EXPORTER_ENDPOINT`      | `false` / `localhost:4317`      | OpenTelemetry（OTLP gRPC）                     |
| `SENTRY_DSN`                                   | —                               | Sentry 错误上报（空则不启用）                  |
| `METRICS_ENABLED`                              | `true`                          | Prometheus `/metrics` 开关                     |
| `LOG_LEVEL`                                    | `info`                          | 日志级别                                       |

完整字段见 [config/config.go](config/config.go)。

## 开发指引

| 规则       | 说明                                                                                            |
| ---------- | ----------------------------------------------------------------------------------------------- |
| 分层       | Handler 只做参数绑定和调用 service，业务逻辑放 service 层                                       |
| 中间件顺序 | RequestID → OTel → Logger → CORS → Recovery → 业务中间件（JWT）                                 |
| 配置       | JSON + go tag；环境变量 > secrets.json > config.json > 默认值                                   |
| 测试       | 标准库 testing + `pkg/testx`（断言）+ `pkg/testx/suite`（suite）；sqlite 用例需 `CGO_ENABLED=1` |
| 日志       | 运行时全部走 zap（JSON），不直接使用 `log.Printf`                                               |
| 提交       | git 提交信息用中文                                                                              |

## 学习资料

- [docs/go-learn/learn/](docs/go-learn/learn/) — go-learn 分章笔记（01 SEDA 框架 → 08 计划约束链）+ mini 实现
- [docs/go-learn/source_impl/](docs/go-learn/source_impl/) — creative-gravity 源码分析
- [docs/go-learn/impl/](docs/go-learn/impl/) — 实现备忘（bugs / phase3 / phase4 / 异步恢复）
- [docs/go-learn/meeting/](docs/go-learn/meeting/) — 面试 QA
- [docs/base-server/](docs/base-server/) — base-server 域文档
