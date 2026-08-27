# Design notes

`internal-docs/system-design.md` describes the architecture as it stands: what each module hides, the
two document registers, and the cache.

The decisions recorded here are the ones a future reader would otherwise re-litigate. They are written
after the fact and describe the final state.

| ADR | Decision |
|---|---|
| [0001](decisions/ADR-0001-central-instead-of-the-language-server.md) | Read Ballerina Central directly instead of wrapping the language server |
| [0002](decisions/ADR-0002-verb-first-with-no-implicit-default.md) | Verb-first CLI, no implicit default verb, per-verb flags enforced by declaration |
| [0003](decisions/ADR-0003-find-keeps-centrals-relevance.md) | `find` keeps Central's relevance and demotes unadopted packages |
| [0004](decisions/ADR-0004-construct-tests-hold-both-answers.md) | Construct tests record both today's output and the correct declaration; coverage is a map, not a gate |
| [0005](decisions/ADR-0005-one-shape-per-family-not-per-category.md) | One schema shape per family, not one per Central category: 32 keys, three shapes |
| [0006](decisions/ADR-0006-one-named-pipeline.md) | The pipeline is one named function, so the recorded corpus cannot measure a pipeline the tool does not run |
| [0007](decisions/ADR-0007-a-correction-names-the-fact-central-drops.md) | A per-package correction must name the fact Central drops, the oracle that has it, and be pinned both ways |
| [0008](decisions/ADR-0008-the-registers-document-and-the-views-quote.md) | `api` and `type` print a declaration's parameter documentation; the compact views quote the signature, because their budget is what makes them compact |
| [0009](decisions/ADR-0009-a-foreign-reference-is-three-facts.md) | A foreign reference carries org, module and version; the import path, the CLI coordinate and "needs no import" are three derivations, not one string |
| [0010](decisions/ADR-0010-a-named-gap-beats-a-plausible-guess.md) | A declaration that does not compile is never printed; where Central drops the signal, the document names the gap |
| [0011](decisions/ADR-0011-the-help-text-is-the-whole-agent-contract.md) | `--help` carries how to read the output, not just the grammar; the agent skill points at it instead of holding a copy that drifts — *narrowed by 0013, then by 0022* |
| [0012](decisions/ADR-0012-the-usage-lists-are-rendered-from-the-grammar.md) | Every list in `--help` is rendered from the picocli model and every paragraph is written once; the README stops repeating the contract |
| [0013](decisions/ADR-0013-a-rule-is-printed-where-it-applies.md) | A rule about something the output prints is printed by the view beside it; `--help` keeps only the one rule an absence cannot print — *narrowed by 0022* |
| [0014](decisions/ADR-0014-a-module-resolves-through-the-package-containing-it.md) | A module has no registry row, so its version resolves through the package containing it; a 404 blames the half the caller can act on |
| [0015](decisions/ADR-0015-one-failure-code-and-the-kind-is-the-branch.md) | Every failure is exit 1 and the JSON `kind` is what a caller branches on; a usage request is answered on stdout at exit 0 |
| [0017](decisions/ADR-0017-the-entry-document-is-ordered-to-survive-a-window.md) | `overview` leads with facts and navigation and ends with the unbounded quotation; the readme's prose becomes the `guide` verb, and the map generates no signature at all |
| [0018](decisions/ADR-0018-a-nameless-field-is-a-rest-field-and-only-the-last-one-is-real.md) | A nameless record field is a rest field, and only the last one is real |
| [0019](decisions/ADR-0019-the-callable-surface-is-split-by-how-it-is-called.md) | The callable surface is `client`/`class`/`funcs`, split by call form over a derived partition; a wrong kind guess costs one printed line — *supersedes the `ops` verb, whose measurements are its appendix; a second appendix carries the 2026-08-18 spelling tolerance* |
| [0020](decisions/ADR-0020-the-budget-is-in-bytes-and-never-paginates.md) | Three byte budgets, document-wide and kind-blind; an over-budget document degrades to a coarser tier and never paginates, and `--all` is hidden |
| [0021](decisions/ADR-0021-version-resolution-is-internal.md) | No version syntax in the grammar and no disclosure in any document; the project's `Dependencies.toml` decides, and `package-not-found` names what is published |
| [0022](decisions/ADR-0022-help-describes-the-command-not-the-reader.md) | The root text ends with the verb list; the rules a caller carries *into* a lookup — a note is an import, a `## Next` block is a pointer, a `kind` is a branch — are the agent skill's, and the applied halves still print — *narrows 0011 and 0013* |
| [0023](decisions/ADR-0023-a-document-states-its-own-length.md) | Line one states the document's total line count, in both registers, stamped at the single CLI seam; a filtered call that cut is arithmetic rather than a guess — *piping measured at 100% of sessions and unmoved by prose* |
| [0024](decisions/ADR-0024-the-quickstart-is-every-ballerina-block.md) | Every `ballerina`/`bal` block in the readme is quoted, in the readme's own order and last — no classifier, no cap, no name check — *supersedes how 0017 selected and placed it* |

There is no 0016. It recorded the `ops` verb, which ADR-0019 replaced two days later and before any of
this shipped in the platform; the measurements it was built on are ADR-0019's appendix.

## The reader this was ported from

`@aep/ballerina-central` in the parent monorepo was the TypeScript reader this tool was ported from.
It has been **deleted**: callers have moved to `bal library`, and two live implementations of one
contract drift — the whole argument for the port was that there is one place a signature comes from.
Its history is in the monorepo's git log if a fixture ever needs re-deriving.

What it leaves behind is the corpus. `native/src/test/resources/` began as that reader's
`test/__fixtures__/*.json.gz` and `test/__snapshots__/*.bal`, and re-recording now happens here
against Central directly. `CorpusTest` covers the thirteen corpus packages offline and deterministically,
which is what replaced the cross-implementation parity run. Those `.bal` snapshots did not move by a
byte through the 2026-08-17 interface change, which is what proves that change touched the addressing
and not the renderer.

Three decisions from that reader carry over **unchanged**, and the test corpus
is what holds that line:

- **A schema-first boundary.** One description of what Central sends; fields we read are required and
  unknown keys are stripped. A rename becomes a located parse error rather than a subtly wrong
  signature. See `central/schema/`.
- **Four addressed documents in two registers.** A document either *is* Ballerina or it *describes* a
  package. See the register table in `internal-docs/system-design.md`, enforced by `RegisterTest`.
  The one place a report carries somebody else's Markdown — the package's guide — is wrapped in
  `<!-- guide: begin … -->` / `<!-- guide: end … -->`. Demoting the guide's headings keeps this
  document's outline, but it does not tell a reader which of the two Markdowns they are in, and both
  halves of an overview look alike. Comments rather than a fence, because a guide carries its own
  fences and has to stay copyable. `RegisterTest` cuts on the same markers, so the boundary the
  reader sees is the boundary the tests trust — and so does `ViewsAgreeTest`, whose signature oracle
  must never be fed the package author's example code as though this tool had generated it. There
  are now two quotations rather than one: the whole readme, which `guide` prints, and the worked
  blocks `overview` quotes under `## Quickstart` (ADR-0017, ADR-0024).
- **A raw-payload cache keyed by coordinates, silent about itself.** Not a speed optimisation — it is
  what makes an addressed lookup possible at all, and cache trouble is never the caller's problem. See
  `cache/`. Silent now includes the header: a document no longer carries a `Source` row naming the
  cache or the network, because a hit and a miss produce the same answer and the row cost every reader
  a line to say so. What survives is the `Warning` row — the version was never confirmed against the
  registry — which is the one thing a caller cannot work out for themselves.
