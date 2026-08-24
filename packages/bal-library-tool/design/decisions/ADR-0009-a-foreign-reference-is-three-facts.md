# ADR-0009 — a foreign reference is three facts, not one string

Status: accepted · 2026-08-12

## Context

A reference to another module used to travel as a single pre-formatted string, `TypeRef.Link.External.libraryName`.
Three different answers were derived from it, and they do not agree:

| Question | Answer for `ballerina/lang.int` |
|---|---|
| What does an `import` statement take? | `ballerina/lang.'int` — a keyword segment must be quoted |
| What does this CLI's `<org/name>` take? | `ballerina/lang.int` — the validator rejects the apostrophe |
| Is an import needed at all? | **No.** `int:Signed32` compiles in a file with no imports |

One string cannot hold three answers, and collapsing them produced three separate findings:

- **GMAIL-01** — nine record fields carried `// Special Agent Note: Signed32 FROM ballerina/lang.int package`.
  Following it literally is three compiler errors (`cannot resolve module 'ballerina/lang. as int'`), and the
  type needed no import in the first place. The reader special-cased exactly one path (`client.config`) for
  the quoting and left the twelve `lang.*` paths that need the same treatment alone.
- **S3-01** — the `--deps` footer printed `bal library type ballerinax/aws.auth AuthConfig --deps`, built from
  a module path that is not a package. `ballerinax/aws.auth` is a module of `ballerinax/aws`, the registry has
  no row for it, and version resolution goes through the registry — so the command the document printed for the
  type of the only required field of the only record `init` takes was a dead end at exit 1.
- **SAP-08** — no version travelled with the reference, so a follow-up resolved Central's latest. sap's
  signatures are documented against `ballerina/http` 2.15.4 and the printed command read 2.16.6.

## Decision

`model/ModuleRef` carries the three facts Central publishes — `orgName`, `moduleName`, `version` — and every
rendering is derived from them:

- `importPath()` — `org/module`, with each dot-segment quoted if it is a Ballerina keyword. The keyword set is
  the compiler's own: `SyntaxKind`'s 103 `*_KEYWORD` constants for 2201.13.2, less `!is` and `_`.
- `coordinate()` — `org/module`, unquoted. What `QualifiedName` accepts, and what Central's **docs** endpoint
  answers for even when the registry does not.
- `pinnedVersion()` — the version, absent when Central sends `0.0.0` (which it does for every langlib
  reference).
- `isPredeclared()` — whether the module's prefix is in scope with no import.

**`isPredeclared` is measured, not guessed, and the obvious rule is wrong.** It is not
`moduleName.startsWith("lang.")`. A `ballerina/lang.X` module is in scope without an import exactly when `X`
names a basic type, because it is the type keyword itself that puts the prefix in scope. Compiled, one module
per line: `int`, `string`, `float`, `decimal`, `boolean`, `xml`, `error`, `function`, `future`, `object`,
`map`, `stream`, `table`, `typedesc` need no import; `value`, `array` and `regexp` report `undefined module`
and do. All three of the second group are `lang.*` too.

## Consequences

- Nine agent notes disappear (gmail's `lang.int`) and three keep theirs (graphql's `lang.value`), which is the
  split the compiler draws rather than the one the module name suggests.
- The note's noun changed from "package" to "module" on all 330 of them. An `import` names a module, and 15 of
  the corpus's notes name a module that is not its package at all — `ballerina/http.httpscerr`,
  `ballerina/graphql.parser`, `ballerina/graphql.dataloader`.
- The `--deps` footer prints `<-  ballerina/http 2.15.4` and a command pinned with `--version 2.15.4`, which
  requires `--version` on every verb that reads a package (KAFKA-08, same batch). That combination is what makes
  `ballerinax/aws.auth` readable at all: running the printed command now returns the seven-member `AuthConfig`
  union the register recorded as unreachable.
- A pre-declared module is excluded from the footer as well as from the note. The footer's contract is to name
  a cross-package edge and hand back the follow-up; for a pre-declared type there is no import to add, and the
  follow-up is a measured dead end (`type ballerina/lang.int Signed32` answers `// Unknown type: Signed32`).
- The keyword set is data from the language, not a heuristic, and it replaces a one-element allow-list. If a
  future langlib module's name is a type keyword we do not list, it draws a note it does not need — visible and
  harmless, where the reverse (withholding a needed import) is not.
