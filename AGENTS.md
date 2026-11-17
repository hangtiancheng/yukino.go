Do NOT add license header manually!

The user will add license by himself.

- Repo layout:
  - libs/\*: published Go infrastructure libraries
    - libs/yukino_cache (Invoke yukino-cache-go skill)
    - libs/yukino_http (Invoke yukino-http skill)
    - libs/yukino_orm (Invoke yukino-orm skill)
    - libs/yukino_rpc (Invoke yukino-rpc skill)
    - libs/yukino_log
    - libs/yukino_skiplist
  - components/\*: hand-written distributed components (time_wheel, redis_lock, redis_mq, consistent_hash, cache_aside, lsm_tree, raft, tcc), consumed as Go modules by apps/task_flow
  - apps/\*: deployable Go servers, cmd/ + internal/ layout
    - apps/task_flow: real enterprise conditional & scheduled task platform (server/ + client/ + docker-compose.yml), NOT a demo
    - apps/scheduled_task: distributed cron / scheduled task service
    - apps/cloud_storage: cloud storage service and log backup daemon
    - apps/raft_kv: linearizable Raft key-value store
  - packages/\*: TypeScript clients
  - scripts/\*: repo tooling (tag.js, release.js, rename.js)
- Client: packages/yukino-agent; Server: apps/agent
- Client: packages/yukino-chat; Server: apps/chat
