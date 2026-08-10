# @aep/ballerina-central

Reads a Ballerina package's public API off [Ballerina
Central](https://central.ballerina.io) and renders the whole thing — clients,
resource and remote functions, records, enums, unions, services, annotations —
as one Ballerina syntax string.

Ships as the `bal-library` command the `ballerina` skill drives.

```bash
bal-library ballerinax/github > /tmp/github-api.bal
grep -n 'client class' /tmp/github-api.bal

bal-library ballerinax/github --readme > /tmp/github-readme.md   # the package's own guide
```

## Why it exists

A coding agent writing Ballerina needs one thing the toolchain will not give it
until too late: the exact signature of a connector call. Before the first
successful `bal build` there is no `.bala` tree to read, and after one there is
a 900KB `types.bal` to page through.

Measured on a one-component service against `ballerinax/github`, two runs per
arm with the connector pre-pulled: with this capability, **51 and 51 turns,
$1.47 and $1.35**; without it, **59 and 68 turns, $1.61 and $1.99** — roughly a
fifth fewer turns, a fifth cheaper, and far tighter variance. Both arms produced
correct, clean-compiling code; the difference is that one arm reached the
signature by grep and the other spent six calls navigating to the `.bala` tree
and then read a 902KB `types.bal` three times. Wall clock was serving-latency
noise at that sample size and no claim is made about it.

## Contract

| stream / code | content |
|---|---|
| stdout | the requested document, and nothing else |
| stderr | on failure, one JSON object matching `Failure` |
| exit 0 | success |
| exit 1 | Central could not answer, or the package published no guide |
| exit 2 | the arguments are wrong, or `--help` |

```
bal-library <org/name> [version] [--readme] [--project-dir <dir>]
```

Version resolution: an explicit argument, then `--project-dir`'s
`Dependencies.toml` if a build has written one, then Central's per-package
versions endpoint. It is shared by both documents — a guide describing a
different version than the one the component compiles against is worse than no
guide.

## Two documents

A package answers two different questions, so the command prints two things
from the one payload it already fetches:

| | what it answers | shape |
|---|---|---|
| default | *what is this called?* — the exact signature | Ballerina source, thousands of lines, meant to be grepped |
| `--readme` | *how is this used?* — auth, config, the shape of a call | Markdown, a couple of hundred lines, meant to be read from the top |

`--readme` is `module.description` from the docs payload, which is byte-identical
to the `docs/README.md` a published `.bala` carries — verified against
`ballerinax/kafka@4.6.5`. Reading it here rather than off disk is what makes it
available *before* a build has resolved the package, which is when a connector
you have never written against is hardest to guess at. Absent a guide the
command exits 1 rather than printing an empty document; the API document is
unaffected, which is why that one field is the package's only optional read.

## Delivery

The bundle is one dependency-free `.mjs` plus a two-line `sh` launcher, so
installing it is a copy and a `PATH` entry:

| where | how |
|---|---|
| runner image | `--build-context balcli=…/dist` → `/opt/ballerina-central`, on `PATH` |
| playground, docker mode | the same `dist/` bind-mounted over it — edit, rebuild the package, run |
| playground, host mode | `dist/` prepended to the coding child's `PATH` |

```bash
pnpm --filter @aep/ballerina-central build   # ~2s; no image rebuild in either mode
```

## Tests

`test/corpus.test.ts` renders nine recorded Central payloads and diffs each
against a committed snapshot. Fixtures span **shape space**, not popularity:

| fixture | what it pins |
|---|---|
| `ballerina/http` | scale, the generic-service injector, annotations, 85 classes |
| `ballerina/graphql` | generic service + the ErrorDetail patch |
| `ballerinax/github` | 903 resource functions, 21,818 lines of output |
| `ballerinax/googleapis.gmail` | cross-package type notes, optional fields |
| `ballerinax/googleapis.sheets` | the Range 2D-array patch, enums |
| `ballerinax/kafka` | listener + service template (`service X on new Y(…)`) |
| `ballerinax/postgresql` | client + driver, 125 classes, non-remote client methods |
| `ballerinax/sap` | the injected-typedef patch, a tiny module |
| `ballerinax/slack` | the OkTrueDef patch, 174 resource functions |

Fixtures are stored gzipped (12MB → 332KB for `github`) because nobody reads a
diff of minified JSON; snapshots are plain text because the diff **is** the
artifact.

`test/keyspace.test.ts` snapshots the payload's whole key space, which is how a
Central field that appears or vanishes becomes a reviewable diff rather than a
silent behaviour change. Re-record and review it after refreshing fixtures:

```bash
pnpm record-fixture ballerinax/kafka          # add or refresh one package
AEP_BAL_UPDATE_KEYSPACE=1 pnpm test           # accept a reviewed shape change
```

Design notes and the fork decision: [`design/`](design/).
