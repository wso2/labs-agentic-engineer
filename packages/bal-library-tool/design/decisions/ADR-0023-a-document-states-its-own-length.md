# ADR-0023 — A document states its own length on line one

Status: accepted · 2026-08-17

## Context

Callers pipe. Measured across five eval sweeps, the number never moved: 19 of 19 calls filtered in the
2026-08-14 run, 19 of 19 in the 2026-08-15 run after the skill added a "never pipe it" instruction, and
on 2026-08-17 — with the instruction still there and the documents now bounded — claims-fhir piped 6 of
6, telemetry-kafka 5 of 5 and intake-fanout 19 of 20.

Reading the transcripts rather than the counts showed why, and it is not about this tool. In every
session that piped, a genuinely noisy command had been piped moments earlier in the same shell:

```
bal tool pull openapi 2>&1 | tail -20
bal openapi -i openapi.yaml --mode service 2>&1 | tail -40
bal library --help 2>&1 | head -60          # <- the habit arrives here
```

The pipe lands on `bal library --help` — a fixed 41-line document — *before* a single byte of any
document has been seen, so no property of the output can have caused it. Both `narrow/` attempts in that
sweep, which run no such command, piped nothing at all: 0 of 8 and 0 of 10.

**That is a correlation and not the whole cause**, and a later attempt says so: `kafka-listener-service`
is a `narrow/` case with no scaffolding step ahead of it and it piped 6 of 6, cutting 2. So the habit
arrives with a neighbouring tool sometimes and simply arrives at other times. Which is the argument for a
mechanism rather than a better sentence: no prose in `--help` or in the skill has moved the number, and
ADR-0017 already had to order the entry document to survive a window for the same reason.

What it costs is invisible. Re-running the sweep's four `-r` calls unfiltered:

| call | true length | window | discarded |
|---|---:|---:|---:|
| `client ballerinax/kafka Producer -r` | 274 | 150 | 45% |
| `client ballerinax/github Client "post …/issues" -r` | 535 | 150 | 72% |
| `client ballerinax/slack Client "post chat.postMessage" -r` | 272 | 100 | 63% |
| `client ballerina/http Client get -r` | 368 | 80 | 78% |

The github one cut eight lines inside `public type Repository record {|`, so the model was handed a
record whose last visible field had no closing brace after it. No run was harmed *this* sweep — none of
them dereferenced a field from the discarded tail — which is precisely the problem: the failure mode is
silent, intermittent, and indistinguishable from a complete answer.

Bounded documents do not help. A budget is per document (ADR-0020); a window is per call, chosen before
the document arrives.

## Decision

**Every document's first line states its own total line count**, in the register it is written in:

```
<!-- bal library overview v1 · 42 lines -->
// ballerinax/github:6.0.0 · 535 lines
```

The count is the whole document, including line one, so a reader compares like with like: 150 lines
arrived, the document says 535, the answer is incomplete. It is the one line every window keeps, which
is why it goes there and not in a footer.

Applied in `Cli` at the single point every document passes through on its way to stdout — not in the
renderers. Three consequences, all of them the point:

1. A view written later carries it without being told.
2. The committed `.bal` corpus snapshots **do not move**, because the renderer is untouched. The
   `api` test asserts `Documents.withLength(identity + snapshot)`, so the snapshot is still the body
   byte for byte and a renderer change still fails there.
3. `--help` is unaffected: it leaves through a different path and has no length to state.

The skill and the `client`/`class`/`funcs` help notes both say what to do with the number, because a
signal nothing reads is not a signal.

## Consequences

- A filter that cut is arithmetic instead of a guess, for the caller and for the eval harness.
- The harness now reports `…piped` and `…piped AND cut` as separate columns. That split immediately
  corrected a standing misreading: the two runs recorded as "19 of 19 piped" actually cut 9 and 5. The
  headline had been overstating its own finding by two to four times, in the direction that invites a
  fix to a problem half the size.
- Two bytes and a number on every document. Accepted: the alternative on offer was more prose asking
  callers not to do something they have done in 100% of measured sessions.
- It does not prevent the pipe, and is not meant to. It makes the consequence legible at the moment it
  happens, to the only party who can re-run the call.

**First measurement after it landed, and the honest reading.** A `catalog-redis` attempt with both the
stamp and the skill's "do the arithmetic" instruction in place piped 11 of 11 calls — and cut exactly
**one**: `client ballerina/http Client get -r | head -80` over a 368-line document. Every other window
was generous enough to lose nothing.

Two things follow, and only one of them is good.

1. **The metric earned its place immediately.** "11 of 11 piped" and "1 of 11 cut" are the same session,
   and the second is the one worth spending effort on. Before the split, this session would have read as
   a total failure of the piping rule.
2. **The behavioural half is unproven.** The agent did not re-run the cut call; its next lookup moved to
   another package. So the length is *visible* and is not yet *acted on*. The run still built green with
   zero signature errors, which is the third time a truncated `-r` has done no harm by luck rather than
   by design. That is not evidence the stamp works on an agent — only that it works as instrumentation.
   Leaving this here rather than in a commit message because the next person to look will otherwise
   re-derive it, or worse, assume the problem was solved.
