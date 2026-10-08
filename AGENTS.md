Do NOT add license header manually!

The user will add license by himself.

- Repo layout:
  - libs/\*: published Go infrastructure libraries
    - libs/yukino_cache (Invoke yukino-cache-go skill)
    - libs/yukino_http (Invoke yukino-http skill)
    - libs/yukino_orm (Invoke yukino-orm skill)
    - libs/yukino_rpc (Invoke yukino-rpc skill)
  - components/\*: hand-written distributed components (time_wheel, redis_lock, red_mq, consistent_hash, consistent_cache, lsm_tree, raft, tcc, timer), consumed as Go modules by services/taskflow
  - services/\*: deployable Go servers, cmd/ + internal/ layout
    - services/taskflow: real enterprise conditional & scheduled task platform (server/ + client/ + docker-compose.yml), NOT a demo
  - packages/\*: TypeScript clients
  - scripts/\*: repo tooling (tag.js, release.js, rename.js)
- Client: packages/yukino-agent; Server: services/agent
- Client: packages/yukino-chat; Server: services/chat
