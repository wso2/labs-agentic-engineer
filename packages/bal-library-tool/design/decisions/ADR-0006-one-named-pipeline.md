# ADR-0006 — the pipeline is one named function, not a call sequence each caller repeats

Status: accepted · 2026-08-12

## Context

Building the IR from a Central payload took three steps in a fixed order: `FromCentral.fromCentral`,
then `Patches.applyPatches`, then — as of the callable-surface work — `Defaults.markUnwritable`.

Three places wrote that sequence out by hand: `Loader` for the CLI, `FixtureCorpus` for the nine recorded
packages, and `Payload` for the construct suite. Nothing tied them together, and nothing could have
noticed if they disagreed.

They disagreed the first time a stage added a step. `Defaults` was wired into `Loader` and the product
behaved correctly; the corpus, which is the oracle, went on rendering without it and stayed green. The
symptom was a package whose constructor the change was specifically written to annotate showing a
zero-line diff — the sort of result that reads as "no effect" rather than as "not measured". A snapshot
suite that assembles its own pipeline stops describing the product the moment the product gains a stage,
and it does so silently, which is the worst available failure mode for an oracle.

## Decision

`Pipeline.build(module)` is the only way to turn a parsed module into a `Library`. `Loader`,
`FixtureCorpus` and `Payload` all call it. The stage order lives in its body with the reason for each
edge written down: patches run after the transform because a correction needs something to correct, and
defaults run after patches because a patch can inject the very declaration that decides whether a printed
default names something the document has.

It also owns the one input the passes cannot derive from the IR: the names Central publishes that no
section renders — today `variables` and `configurables`. `Defaults` needs them to avoid claiming a package
does not export a name it does export, and the IR does not carry them, so `Pipeline` reads them off the
module and hands them over. When they gain a rendering that argument disappears and so does the parameter.

## Consequences

- Adding a stage is one edit, and the corpus measures it on the next run without anyone remembering to
  wire it in twice.
- The construct suite's payloads travel the same path as a real package, so a case cannot pass by taking a
  shortcut the product does not take.
- `Loader` no longer imports `Patches` or `Defaults`, which is the right dependency direction: loading is
  about fetching and coordinates, not about what the IR is made of.
- The cost is one more type in `model`, and one more indirection when reading `Loader`. Worth it: the
  alternative was three copies of an ordering whose correctness is not local to any of them.
