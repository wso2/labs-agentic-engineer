# ADR-0003 — `find` keeps Central's relevance and demotes unadopted packages

**Status:** Accepted. The verb was called `search` when this was written and is `find` since the
2026-08-17 kind split (ADR-0019); the ranking decision is unchanged, and `views/Find` says so.

## Context

`find` is the one verb the addressed verbs cannot cover: every other verb needs `org/name` before it can
say anything. Central serves it at `registry/search-packages?q=`, live rather than as a baked
index.

Central's ordering is relevance-based, and it has one defect. Measured on `q=http client`, which returns
1,351 hits:

| # | Package | Pulls |
|---|---|---|
| 1 | `ballerina/http` | 1,862,507 |
| 2 | `ballerinax/client.config` | 133,297 |
| 3 | `ballerinax/health.clients.fhir` | 12,509 |
| **4** | **`tharmigank/http.client.wrapper`** | **1** |
| 5 | `ballerina/sql` | 171,335 |
| 6 | `ballerina/websocket` | 127,084 |
| 7 | `sabtharm/http` | 18 |
| 8 | `lakshansivagnanasothy/client_stubs` | 122 |

The top is right. The problem is abandoned packages salted through the middle: an agent reading top-down
picks `tharmigank/http.client.wrapper` before it reaches `ballerina/sql`.

## Decision

**Keep Central's relevance. Move packages under 1,000 pulls to the end. Print the pull count beside every
hit, and say in the document that the order is partly ours.**

### Sorting the whole list by pull count was tried and is worse

It was the obvious fix and it was wrong, which is why it is recorded here. Central publishes `pullCount`
in the same response, so a descending sort is one line — and on `q=kafka messaging` it produces:

```
ballerinax/twilio      2,715,970 pulls    ← first
ballerina/http         1,862,507
ballerina/crypto       1,684,059
...
ballerinax/kafka          60,747 pulls    ← tenth
```

Popularity is not relevance. The most-pulled package that merely matched is almost never the answer, and
burying `ballerinax/kafka` at tenth for a Kafka query is a worse failure than surfacing a one-pull package
at fourth. Central's own order puts `ballerinax/kafka` **first** for that query and `ballerina/http` first
for `http client`; that judgement is good and worth keeping.

### So the correction is the narrowest one that fixes the actual defect

A **stable partition** on a pull floor. Adopted packages keep Central's order; the rest follow in theirs.
Nothing else moves.

### The floor is 1,000, and it separates two populations

Not "small" from "large" — measured, the two groups do not overlap:

| Below the floor (demoted) | Above it (kept in place) |
|---|---|
| `tharmigank/http.client.wrapper` — 1 | `ballerina/mqtt` — 2,460 |
| `sabtharm/http` — 18 | `choreo/mediation.log_message` — 2,890 |
| `lakshansivagnanasothy/client_stubs` — 122 | `ballerinax/confluent.cregistry` — 37,752 |
| `ballerinax/health.clients.hl7` — 189 | `ballerinax/kafka` — 60,747 |

The lowest-pull packages a caller might legitimately want sit clearly above 1,000; everything below it in
the measured samples is a personal experiment.

### Demoted, not dropped

A low pull count is a fact about adoption, not a verdict on quality, and the tool is not entitled to
decide that a package is not the answer. It is moved and the count is printed, so the judgement stays with
the caller.

### The ordering is disclosed

The facts table says `Central's relevance, with unadopted packages moved to the end`, and a `## Ranking`
section explains it with the measured case. Re-ordering silently would leave an agent unable to tell whose
judgement it is reading, in a document whose whole purpose is to be trustworthy.

## Consequences

`find` never reads or writes the cache: the query space is unbounded and the answer is the one thing
about Central that genuinely changes.

The floor is a hand-maintained claim about Central's population, so it can rot. Three tests pin it in both
directions: the one-pull package must be demoted, `ballerinax/kafka` must stay first for a Kafka query,
and `ballerina/mqtt` at 2,460 pulls must stay where Central put it.
