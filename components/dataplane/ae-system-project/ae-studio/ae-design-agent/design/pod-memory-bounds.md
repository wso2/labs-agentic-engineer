<!--
Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
Licensed under the Apache License, Version 2.0.
-->

# Pod memory bounds

Every conversation, turn and record the design agent holds lives in the
process. The container's memory limit is 1 Gi, and an OOM kill skips the
SIGTERM path: running turns end with no terminal and no usage record, and
every project's thread history is lost. So each in-process collection has a
bound, and each bound is a named constant beside the code that applies it.

| What | Bound | Where |
|---|---|---|
| Project thread messages | Rotate past 80 % of the connection's declared context window (`409 conversation_rotated`). With no window declared, rotate past `THREAD_FALLBACK_BYTES` (8 MiB of stored messages, base64 attachments counted). | `conversations/thread-book.ts` |
| Project threads | Threads no turn was admitted to: `MAX_UNUSED_THREADS` (1000) in the pod, least recently used evicted first (a read of the current thread is a use). Opening one resolves nothing, so any well-formed project name mints one. A thread with an admitted turn is never evicted: its turn resolved the project, so those are bounded by the org's projects. An evicted thread holds no messages; a send to its id is `409 conversation_rotated` and its history is 404. | `conversations/thread-book.ts` |
| Marketplace conversations | Evicted after `MARKETPLACE_IDLE_MS` (2 h) unused. An owner keeps `MARKETPLACE_PER_OWNER` (5), and a new one evicts the owner's least recently used. An evicted id is 404, and the console's 404 recovery starts a new conversation. A conversation whose turn runs is never evicted. | `conversations/marketplace-book.ts` |
| Replay buffers | 16 384 parts or 16 MiB per turn, kept 120 s after the turn ends. | `turns/replay-buffer.ts` |
| Turn statuses | The last 20 per scope, or 1 h. | `turns/turn-desk.ts` |
| Usage outbox | 200 records. Past the cap, the oldest is dropped. | `usage/outbox.ts` |
| Prototype render checks | `MAX_RENDER_CHECKS` (1) at once in the pod, each a Node child with a 384 MiB heap (the kit's `RENDER_HEAP_MB`) and a 15 s limit. Later checks wait in call order; a turn's own writes already queue behind its pending verdict, so at most one check waits per running turn. | `prototype/render-check.ts` |

## Choices

- **Cap unused threads, not resolve on open.** Resolving the project on
  `conversations/current` and rotate would make every chat open wait on the
  tools sidecar and aep-api, and fail while either is down. The cap needs no
  call: a junk name costs an empty entry until 1000 newer ones push it out,
  and a real project's thread is safe once its first turn is admitted.

- **One render check at a time.** Two children would hold 768 MiB of heap
  beside the agent's own in a 1 Gi container. A wait costs a turn at most a
  few seconds per check ahead of it, which beats an OOM kill of every
  project's threads.

- **Rotation, not truncation.** A thread that grows past its bound rotates
  through the existing path. The console already handles
  `conversation_rotated`, and a rotated thread's messages leave the store. No
  history is cut mid-thread, so the model never reads half a conversation.
- **The byte bound applies only without a declared window.** A connection that
  declares its window rotates on measured context tokens, which tracks what
  the model actually reads. Bytes are the fallback for a connection that gives
  no window.
- **A busy marketplace conversation is not evicted.** Its run would save the
  messages back after the eviction, leaving an entry no one can name. It goes
  on the first create or use after its turn ends. The per-owner cap is exceeded
  only while more of an owner's turns run at once than the cap allows.
- **Any call of the owner is a use.** A history read, a turn start, and a
  status or stream read all count. A conversation someone is looking at is not
  idle.

## Not bounded here

- A multipart turn start is buffered whole (about 16 MiB raw plus its copies),
  and nothing caps how many run at once. That is a load question for capacity planning.
