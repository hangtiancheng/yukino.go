请你实现**企业级**的条件任务和定时任务项目，前端 client 使用 lucide-static (不要手写 svg), tailwindcss (尽量不要手写 css), lit, @yukino.js/lit-jsx (调用 yukino-lit-jsx 技能) 和 @yukino.js/sentry (调用 yukino-sentry 技能), @lit-labs/router，jotai 实现精美的亮色模式页面；后端 server MUST 使用 yukino-http (调用 yukino-http 技能), yukino-cache (调用 yukino-cache-go 技能)、gorm、redis、mysql、mq (MUST **尽可能**的使用消息队列, 可以使用 components/ 下提供的手写 mq)

后端是单体服务、分布式部署, MUST **尽可能**的修复 (如果有缺陷) 并使用 components/ 下提供的所有后端服务/分布式中会使用到的组件, 例如分布式锁、分布式缓存、tcc 分布式事务、time wheel 时间轮等等, 不要用社区依赖; MUST 做好**幂等键**, 保证某个条件的条件任务/某个时刻的定时任务不会被触发多次

条件任务和定时任务是指通过 openai/openai-go 连接大模型 API, 根据预设的提示词, 执行一些工具调用, 输出结构化的 markdown 文件

- demo 定时任务: 每天 10:00 调用大模型查询 mysql 中的表数量, 以及对比昨天 10:00 新增/删除的记录数量
- demo 条件任务: mysql 插入记录时, 调用大模型分析插入记录中是否包含持久型 xss 等安全风险

需要实现的工具**至少**包括

- mysql_tool, 执行 sql 操作 mysql; 本机器的 mysql: mysql -uroot -p<密码>
- redis_tool, 操作 redis; 本机器的 redis: brew services restart redis

开始实施, 做好版本控制
对 server 后端服务要做好 sentry 监控 (sentry/go) 和分布式 trace 追踪 (telemetry), MUST 做好多个数据库 (redis 集群, mysql 集群) 间的数据同步, 同时做好 Docker 一键部署 (前端 + 后端)

- 本项目是纯英文项目, 不要使用除 、「」外的其他任何全角标点, 使用半角标点
- 注意高并发设计和并发安全
- lit 组件放弃 shadow DOM, 渲染到 light DOM
- UI 上不要有任何冗余文案
