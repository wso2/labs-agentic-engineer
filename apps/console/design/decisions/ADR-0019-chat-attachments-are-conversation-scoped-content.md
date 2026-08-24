# ADR-0019: Chat attachments are conversation-scoped model content, not stored platform state

Status: Accepted. Amends one forward-looking consequence of **ADR-0017**, which
predicted that the next attach-a-file feature would "inherit this shape: store,
overlay, no surface, no commit". The first three do not survive contact with a
mid-conversation attachment; only "no commit" does.

## Context

ADR-0017 settled reference documents: attached at project create, stored off-git
per project, overlaid into every turn's snapshot, never committed. Feature #428
asks the adjacent question — hand the agent a file **mid-conversation**: an error
screenshot, a revised PDF, a CSV of example data, a sketch for `/design`.

The obvious move is to copy ADR-0017: a conversation-scoped directory on the
shared volume, TTL-swept, descriptors on the turn. That is what #428's own
design section proposed on 2026-08-12, and it is what ADR-0017 told the next
feature to do.

It is wrong here, and the reason is a difference in **lifetime**, not in size or
type.

A reference must persist because `/start` can be re-run, from another device, by
a teammate, days later — the documents have to still be there. A chat attachment
has no such requirement: it rides exactly one message, and the moment that turn
runs, the bytes are **already durable in the conversation history** as a message
part. A store would hold them for the few hundred milliseconds between POST and
dispatch, then hold a redundant second copy forever.

Two facts make that window as small as it sounds. The turn runs in a **detached
goroutine started at POST time** (`genai_service.go`) — not off a queue, not in
another process. And the edge already admits an 80 MiB body, sized in #497 for
exactly this shape of upload.

## Decisions

1. **A chat attachment is model CONTENT, not platform STATE.** It exists in the
   request that carries it and in the conversation history that request produces.
   No conversation-scoped directory, no TTL sweep, no quota hook, no fenced path
   resolver, no second endpoint. Bytes ride the multipart request into the
   in-memory turn job and out again on the dispatch, and are never written to
   disk by the platform.

2. **Every cap derives from the model-side encoded budget.** #384 fixed the real
   ceiling: 20 MiB **encoded** per turn
   (`MAX_REFERENCE_ATTACHMENT_ENCODED_BYTES`), past which attachments are warned
   and skipped. Every other number is that number restated for its layer — the
   console screens ≤15 MiB raw per message, the agents turn parser admits ~24 MB.

   **A cap must not exceed what the layer below it honours.** #428 as written
   asked for 10 × 5 MiB = 50 MiB, three times what a turn can carry: the budget
   loop would have silently dropped the last seven files. A limit the platform
   does not keep is worse than a smaller one it does.

3. **The create view remains the only door to the reference store.** A file
   attached in the chat composer is conversation-scoped **in every case** —
   including when the message is `/start`. One control does not get two storage
   semantics chosen by the first character the user typed.

   This is also what keeps `put-project-references` honest: it **replaces** the
   project's whole set, so a chat-side write into that store would silently wipe
   what the create view put there, and merge-vs-replace is precisely the problem
   ADR-0017 decision 2 avoided by making the upload a single post-create call.

## Consequences

- **Two attachment channels exist, and they are not variants of each other.**
  Reference documents: project-scoped, stored, overlaid into the snapshot, read
  as workspace files or native parts, persist across turns and re-runs. Chat
  attachments: conversation-scoped, unstored, carried as message parts, durable
  only as history. A future feature must pick a channel by **lifetime** —
  "does this need to survive the request?" — not by file type or size.
- ADR-0017's "the next feature inherits store + overlay" consequence is
  **amended, not reversed**: "never committed" carries over intact. "No surface"
  does NOT: a sent message shows its attachments as chips, and they survive a
  reload. That is a deliberate difference — a reference is superseded by the
  requirements it seeds, while an attachment is part of what someone SAID, and a
  thread that hides it shows the agent discussing a document that appears nowhere.
- **"Nothing is stored" is about BYTES, and only bytes.** File NAMES are
  retained, in the conversation's turn journal, for the life of the conversation
  — that is what the chips are read from. So the honest statement of the
  privacy property is: the platform never writes attachment CONTENT to disk and
  never commits it; it does retain the names as message metadata. Any feature
  quoting decision 1 should quote it that way.
- **Chat attachments cannot be re-sent by the server.** A failed send means the
  browser still holds the only copy, which is why the composer must retain its
  cards on failure rather than clearing. This is a UI obligation created by this
  ADR, not an incidental nicety.
- **The agents turn-endpoint parser is no longer a few hundred bytes of IDs.**
  `256kb → ~24 MB` widens an authenticated internal endpoint's exposure by ~94×.
  The number is defensible only because it is derived (decision 2); any future
  request to raise it further must first raise the model-side budget it derives
  from.
- **Attachment tokens ride every subsequent turn of the conversation**,
  prompt-cached. A conversation is the retention unit: rotating it is what frees
  the context. There is no way to remove one attachment from a live conversation,
  and this ADR does not create one.
- The journal carries attachment **names only**. `projectDisplayHistory` replaces
  each user row with the journal's text, so without names a reload would show the
  agent discussing a document that appears nowhere in the transcript.
- **Chat attachments are EXEMPT from the history dedupe that references obey.**
  Both channels produce native file parts, so it is tempting to treat them
  alike — and doing so is a correctness bug. A reference is re-listed
  automatically by a flow with content the store decides, so re-sending its bytes
  says nothing new and costs a 5 MB round trip. An attachment is a deliberate
  per-message act: someone who revises a PDF, keeps its name and re-attaches it
  MEANS the new bytes, and filtering by name would silently serve the model the
  stale copy from history. Sameness of mechanism is not sameness of intent.

## Rejected

- **A conversation-scoped store on the shared volume** (#428's own design, and
  the shape ADR-0017 predicted). #497's store earned quota accounting and
  delete-with-project *for free* by living under `repos/<orgId>` —
  `enforceOrgQuotas` walks that subtree and `TrashRepo` takes it along. A sibling
  `attachments/` tree inherits neither, so it owes a sweeper, a quota hook and a
  fenced resolver, all to hold bytes that are redundant the instant the turn runs.
- **Overlaying into the turn snapshot**, to reuse `readReferenceAttachments`
  unchanged. The cheapest option in new code and the worst in behaviour:
  snapshots are **sha-keyed and reused across turns**, so conversation-scoped
  bytes would leak into later turns of the same sha.
- **Keeping #428's 10 × 5 MiB and raising the agents parser to ~72 MB.** Widens a
  DoS guard by 280× in order to advertise a ceiling that stays unreachable.
- **Text attachments fenced into the prompt** (#428's wording). A multi-MB CSV
  buries the user's own message inside its own bubble, and fenced text carries no
  `filename` — so it is invisible to every mechanism that reasons about
  attachments by name, from the chips to the budget accounting.
- **Routing `/start` attachments into the reference store.** Tidy-looking, and it
  re-opens merge-vs-replace (decision 3).
- **The issue-assets bridge** (#428 design item 5): committing a conversation's
  attachments under `.aep/issue-assets/<n>/` when the plan flow writes GitHub
  issues. Not rejected on merit — it is **unimplementable under decision 1**,
  because the plan tap runs off the stream after the turn and would resolve bytes
  from a store that no longer exists. It is also the one part of #428 that puts
  bytes in git. It needs its own issue and its own grilling.

## Note on ADR-0017's rejected "browser-held references"

ADR-0017 rejected "browser-held references re-sent with every turn" because a
reload, a different device, or a teammate loses them and a `/start` re-run
silently has none. That reasoning is intact and does **not** apply here, for one
reason: a reference is an input to a turn that can be **re-run**, so it must
outlive any single request. A chat attachment is an input to a **message**, which
happens exactly once — and once it happens, the conversation history holds it for
good. The browser is the only holder for the span of one send, not for the life
of the project.
