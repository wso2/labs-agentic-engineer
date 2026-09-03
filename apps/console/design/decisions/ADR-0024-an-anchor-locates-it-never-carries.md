# ADR-0024: An anchor locates; it never carries content

- **Status:** Accepted
- **Date:** 2026-08-30 (wayfinder map
  [#650](https://github.com/wso2/labs-agentic-engineer/issues/650), settled on
  [#653](https://github.com/wso2/labs-agentic-engineer/issues/653); first drawn
  on by [#666](https://github.com/wso2/labs-agentic-engineer/issues/666))
- **Context:** when a user aims the agent at part of a spec, something has to
  tell the turn what they pointed at. The obvious shape is to send the selected
  content — it always resolves, and the model needs the text anyway.

  This codebase has already tried that and reversed it. `dependencyResolutionMessage.ts`
  once embedded a dependency's full JSON entry plus a resolution playbook;
  [#252](https://github.com/wso2/labs-agentic-engineer/issues/252) Task 17
  stripped it back to naming the dependency and the component, because *"the
  chat agent reads the dependency's CURRENT entry straight from design.json in
  its own turn snapshot"* — the embedded copy was redundant noise against a
  better, live source.

  The same argument is stronger for a document selection. The agent joins the
  spec collab room as a live peer, so between the selection and the turn
  starting the user may keep typing and a teammate may edit too. Content
  captured at selection time is a photograph of a document that has since moved.

## Decision

- **An anchor names what was pointed at; the agent resolves the name against the
  live document.** It never ships the selected content.

  ```ts
  interface Anchor     { file: string; nodes: AnchorNode[] }
  interface AnchorNode { name: string; kind: string; context?: string }
  ```

- **One type covers prose and structured views alike**, with `kind` carrying the
  difference between a name that was authored as a name (`POST /rounds`) and a
  sentence pressed into service as one.
- **Markdown names a block by a bounded excerpt of its RENDERED text** — at most
  80 characters, cut at a word boundary. The bound is the load-bearing part, not
  the number: an unbounded excerpt is "carry" arriving through the back door.
  Rendered rather than source, because the source carries `**bold**`, `*assumed*`,
  links and hard wraps that the reader never saw, so a source-exact excerpt
  fails to match for most blocks. The agent matches on prose, tolerantly.
- **No positions and no indices reach the wire.** In a live `Y.XmlFragment` an
  index is wrong within a keystroke of anyone inserting a block above it, and a
  stale index that looks authoritative is worse than none — it hands the agent a
  confident wrong answer to prefer over the text match. Positions are held
  client-side while composing and snapshotted to text at send.
- **Every view resolves to exactly one authored file**, so `file` is always
  known and one anchor never spans two files.
- **The anchor is journaled and rehydrates**, like chat attachment names. A
  turn journal that records the words but not the target is not a record of what
  happened.
- **The transcript's tag is frozen and never re-validated.** It records what was
  pointed at when the message was sent, which stays true whatever happens to the
  file afterwards.

## Consequences

- The model always receives current text, because the agent read it from the
  live document during its own turn. There is no path by which a stale
  quotation reaches it.
- Staleness surfaces as the agent saying it cannot find what was named — in its
  reply, dated, in the transcript where it happened. It never surfaces as a UI
  state that changes under the user. A tag that re-checked itself would make
  scrolling back through months of conversation show chips flipping to "missing"
  as specs evolved, turning a record into a live query with a confusing answer.
- Ambiguity is resolved by the agent asking, not by the console guessing. Two
  blocks sharing an 80-character prefix inside one section are near-duplicate
  prose; `context` narrows it, and a question closes it.
- **The rule is checkable in review:** an anchor field whose size grows with the
  size of the selection is a defect. `name` is capped, `nodes` counts what was
  selected, and nothing else scales.
- The anchor is metadata, never prose. It is not folded into `instruction` —
  a transcript cannot render a tag for something it has to regex back out of a
  message, and the lexicon forbids the console adding words to a user's line.
  What the model reads is rendered from the anchor **service-side**, which keeps
  the user's message the user's.
- Intent lives beside the anchor, not inside it. The anchor says what was
  pointed at; what the user wants done with it is a separate field.
