# yukino-creative-agent — 智能体对话 + 内容创作/风控基座

go-learn（智能体对话：ReAct 引擎 + SEDA 图调度 + Streams 异步管道 + 多 Agent 子任务编排）与 base-server（creation 创作流程 / risk 风控 / points 积分 / monitor 离线任务）合并的单二进制服务。前端为独立 React 应用（`../client`），前后端分离部署：本服务只提供 API，前端独立构建托管。

## 技术栈

- **Web 框架**: yukino_http（路由 + 中间件 + SSE）
- **ORM**: GORM + MySQL（AutoMigrate；users + cg\_\* + creation/risk/points 表）
- **消息队列**: Redis Streams（go-redis/v9，消费组 + ACK）；无 Redis 时降级进程内队列
- **缓存/锁**: Redis + yukino_cache（ChunkStore/StreamQueue/Locker 均有 Redis 与进程内双底座）
- **认证**: JWT (golang-jwt)
- **LLM**: eino ChatModel（OpenAI 兼容 / Anthropic Claude；未配置回退 FakeLLM）
- **可观测性**: OpenTelemetry → Jaeger（HTTP → Worker → 子任务 → LLM 全链路）+ Prometheus /metrics + sentry-go
- **日志**: Zap（结构化 JSON）
- **测试**: 标准库 testing + pkg/testx（断言）+ pkg/testx/suite（suite）；sqlite 用例需 CGO_ENABLED=1
- **配置**: JSON + go tag（环境变量 > secrets.json > config.json > 默认值）

## 快速开始

```bash
# 本地开发前需启动 MySQL + Redis + Jaeger
docker compose up -d mysql redis jaeger

# 敏感配置（MySQL 密码 / LLM Key / Sentry DSN）
cp config/secrets.example.json config/secrets.json

# 然后启动服务
make run
make test                     # 运行所有测试（CGO_ENABLED=1）
make build                    # 编译到 build/creative-agent
make ping                     # 远程 MySQL/Redis 连通性探测
```

## 目录结构

```
cmd/api/main.go       # 唯一入口（加载配置 → app.New → app.Run）
cmd/checkdb/main.go   # 调试工具：按 session 打印 task_run/task_step 状态
config/               # 配置加载（环境变量 > secrets.json > config.json > 默认值）
internal/
  core/                  # 【引擎层】纯算法、零业务：SEDA 图引擎 + ReAct 循环
    seda/                #   图引擎：Flow 图调度 + Slice 流式通道 + 注册表
    react/               #   ReAct 循环：Supervisor 五分支 + 组件接口 + Checkpoint
  appkit/                # 【复用骨架层】跑一个 agent 的机器（无业务 prompt）
    runner/              #   SupervisorRunner（ReAct 运行流程 + SSE 帧 + PlanOutput）
    provider/            #   注入层（按功能类别拆子包）
      llm/               #     LLM 实现（eino ChatModel + 全局注入点 DefaultRealLLM/DefaultManagerLLM）
      tools/             #     工具系统核心（ToolRegistry/ToolsProvider/内建工具/composite）
      skill/             #     SKILL.md 动态工具源（SkillProvider/FSRepository）
      fake/              #     测试桩（FakeLLM/FakeStrategy/FakeHooks + DefaultLLM 工厂）
  business/              # 【业务层】对话与子任务
    conversation/        #   域1：对话主链路（同步图 + 异步 worker）
      flow.go            #     flow 图定义 + 算子注册（业务入口门面）
      ops/               #     SEDA 算子：con_reader / supervisor_agent / con_writer / con_end
      manager/           #     管家约束链（generate_response 协议 / 校验 / 管家 prompt）
      memory/            #     IntentMemory + FlowStage
      fastpath/          #     快捷路径（HIL resume / Retry）
      worker/            #     对话异步 worker（Consumer / Recovery / heartbeat / attach）
    subtask/             #   域2：子任务执行与调度（纯异步）
      pipeline.go        #     产线定义（mix_video / article）
      executor/          #     TaskExecutor / SubAgent / Submitter
      taskflow/          #     Consumer / Poller / CronTaskPoller
  auth/                  # 【base-server】认证 DDD 模块（未接线路由，保留）
  creation/              # 【base-server】创作流程（预创建/创建/后处理/回调/补偿）
  risk/                  # 【base-server】风控审计
  points/                # 【base-server】积分记账
  monitor/               # 【base-server】离线任务（超时/补偿/风控离线 job）
  model/                 # 【基础设施】GORM 模型 + Snowflake ID（标准库实现）
  repository/            # 【基础设施】数据访问层（接口 + GORM 实现）
  infra/                 # 【基础设施】ChunkStore / StreamQueue / Locker（Redis 与进程内双底座）
  service/               # 用户/认证业务（JWT 签发）
  handler/               # 【边界】HTTP 处理器（认证 / 对话 / 点卡确认 / 回调）
  router/                # 【边界】路由注册（API + /aigc + /inner + /metrics）
  middleware/            # 【边界】中间件（RequestID → OTel → Logger → CORS → Recovery → JWT）
  database/              # MySQL 初始化 + AutoMigrate
  app/                   # 【边界】启动编排 + 各层接线 + 优雅关机 + 异步 Worker 启动
pkg/
  sse/                # SSE Writer + 事件类型
  otelx/              # OTel 初始化 + GORM zap logger + GORM SQL tracing 插件
  tracex/             # traceparent 跨进程透传 + 日志 helper
  cache/              # Redis 客户端
  logger/             # Zap 封装
  response/           # 统一响应格式 + 错误码
  testx/              # 标准库断言助手 + suite 运行器（取代 testify）
skills/               # SKILL.md 动态工具源（如 weather）
docs/
  go-learn/           # go-learn 文档（学习笔记 / 源码分析 / 实现备忘 / 面试 QA）
  base-server/        # base-server 文档
Makefile              # 构建/测试/运行/连通性探测
Dockerfile            # 多阶段构建
docker-compose.yml    # MySQL + Redis + Jaeger
.env.example          # 环境变量模板
```

## 架构要点

- **一个 ReAct 引擎三处复用**：`core/react` 引擎 + `appkit/runner` 装配，同步路径 / 异步 Worker / 子 Agent，仅注入不同 LLM/Strategy/Hooks。
- **管家约束链**：`business/conversation/manager/manager_strategy.go` 强制 Function Calling + 工具名白名单 + Schema 校验 + 失败闭环（Retry 兜底）。
- **LLM 决定 agent 链**：`business/subtask/pipeline.go` 定义产线 → 合法 agent 集合；LLM 填 agents 数组，服务端只翻译（校验/补 input/剔除 idea_king）。
- **异步可靠性**：先落库再投递 + NX 锁（Redis/内存）+ MySQL CAS 抢占 + 心跳 + RecoveryScanner 僵尸恢复 + lastSeq 断点续拉。
- **多 Agent 推进**：对话路径 = 点卡确认 → FlowStage（intent_memory）→ 快路径重投递；Cron 路径 = CronTaskPoller 每 30s 自动推进。
- **真实 LLM 开关**：配置 `llm.api_key`/`llm.base_url` 才启用（管家强制 tool_choice=required，子 Agent auto）；未配置回退 FakeLLM 查表模拟。
- **双底座降级**：Redis 不可达时 app.New 自动切换 MemoryChunkStore（yukino_cache）/ MemoryStreamQueue / MemoryLocker，单二进制可无 Redis 运行。

## 开发指引

| 规则       | 说明                                                                                                     |
| ---------- | -------------------------------------------------------------------------------------------------------- |
| 分层       | Handler 只做参数绑定和调用 service，业务逻辑放 service 层                                                |
| 中间件顺序 | RequestID → OTel → Logger → CORS → Recovery → 业务中间件（JWT）                                          |
| 配置       | JSON + go tag；环境变量 > secrets.json > config.json > 默认值                                            |
| 测试       | 标准库 testing + pkg/testx + pkg/testx/suite；测试文件使用 `_test` 外部包名；sqlite 用例需 CGO_ENABLED=1 |
| 日志       | 运行时全部走 zap（JSON），不直接使用 `log.Printf`                                                        |

## API

| 方法     | 路径                                              | 说明                                                                                  |
| -------- | ------------------------------------------------- | ------------------------------------------------------------------------------------- |
| GET      | /api/v1/ping                                      | 健康检查（公开）                                                                      |
| POST     | /api/v1/register                                  | 用户注册（公开）                                                                      |
| POST     | /api/v1/login                                     | 登录获取 JWT（公开）                                                                  |
| GET      | /api/v1/example                                   | 示例（需 Authorization: Bearer token）                                                |
| POST     | /api/v1/logout                                    | 登出 token 拉黑（需认证）                                                             |
| POST/GET | /aigc/conversation                                | 智能体对话（SSE 流；GET 供 EventSource，带断点续拉）                                  |
| POST     | /aigc/agent/submit_clarification                  | 点卡确认（推进 FlowStage）                                                            |
| POST     | /aigc/callback/task_detail                        | 外部任务回调（豁免 JWT）                                                              |
| POST     | /inner/creation/\*                                | 创作流程（pre_create / create / sync_post_create / async_post_create / handle_error） |
| POST     | /inner/callback/task_detail、/inner/callback/risk | 创作/风控回调                                                                         |
| GET      | /metrics                                          | Prometheus 指标                                                                       |

> 完整版 README 见 [README.md](README.md)。
