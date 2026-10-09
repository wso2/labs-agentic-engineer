# ADR-0001: Anchored search/replace for agent file edits

- **Status:** Accepted · **Date:** 2026-06-26 · **Scope:** `@aep/ae-design-agent` main agent

## Context

The main agent mutates a spec bundle (markdown + YAML frontmatter + OpenAPI YAML)
inlined in the prompt. The dominant cost is LLM **output-token decode**; prefill is
cache-cheap. So re-emitting a whole file to change a few lines makes latency scale
with **file** size, not **edit** size. Each tool call is also one round-trip, so a
failed call costs as much as a good one — error results must enable one-step
self-correction.

## Decision

Edit via **anchored literal search/replace** — `addFile` / `editFile` / `removeFile`
over flat byte strings. (A dedicated `setFrontmatterField` tool existed early on for
flat frontmatter keys; it was retired once component config moved to `design.json`
and no spec file carried frontmatter — frontmatter, if ever present, is edited with
`editFile` like any other text.)

- `editFile(path, oldString, newString)`: `oldString` matched **literally** (CRLF→LF
  only) and must hit **exactly once** — indentation preserved byte-for-byte, no
  serializer reflow.
- **YAML reparse guard** after every `*.yaml`/frontmatter write: parse-only; on
  failure the edit is rejected and the bundle left unchanged.
- **Corrective error codes** for one-round-trip recovery — esp. `NOT_UNIQUE`, which
  echoes line+text of every match (the seed `openapi.yaml` has `"Hello, World!"` 3×).

Exact semantics live in `src/agents/main/bundle.ts` + `tool.ts` (the source of
truth). This ADR records *why*.

## Alternatives rejected

- **Single-call `applyEdits([...ops])` batch** — best round-trip economy, but kills
  the live per-edit diff and regresses fresh generation to full re-emit. Deferred.
- **Path-addressed OpenAPI patches** (JSON-pointer + YAML printer) — best structured
  safety, but deterministic re-serialization reflows hand-authored key order
  (serializer drift). `openapi.yaml` stays on anchored `editFile` + reparse guard.

## Consequences

- Output tokens scale with the **edit**, not the file (~14× fewer on a one-line
  change to the ~45-line seed; ~50–100× on a 300-line file).
- `NOT_UNIQUE` candidate echo is **load-bearing** — keeps multi-site edits to one
  corrective round-trip.
- Accepted residual risk: an edit at a wrong-but-parseable column passes the guard;
  mitigated by the YAML reparse guard + copying leading whitespace verbatim.
