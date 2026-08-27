# ADR-0018 — A nameless field is a rest field, and only the last one is real

Status: accepted, implemented.

## Context

`ballerina/time` was unreadable by **every verb**. All six failed identically:

```
overview   -> exit=1 {"kind":"schema-drift","qualified":"ballerina/time:2.8.1", …
type       -> exit=1 {"kind":"schema-drift", …
api        -> exit=1 {"kind":"schema-drift", …
guide      -> exit=1 {"kind":"schema-drift", …
ops        -> exit=1 {"kind":"schema-drift", …

issues: docsData.modules.0.records.{0,1,3}.fields.{3,7}.name
        "expected a string, received nothing"
```

`schema-drift` is the kind reserved for a human, so the agent correctly did not retry. What it did
instead is the whole problem. From the 2026-08-16 sweep transcript:

> "This schema-drift looks like a tool bug affecting the whole time package, so rather than improvise
> I should fall back on known API behavior… I recall that `time:utcToString` returns a plain string
> with no error."

The tool exists so that a signature is read rather than remembered. Here it forced the substitution
it was built to prevent — on a package any service with a timestamp reaches for. The recollection
happened to be right that time; the neighbouring one would not have been, because
`utcFromString` returns `Utc|Error` and unhandled `T|error` was the sweep's **top error class**
(9 of 24).

Two independent defects sat behind it.

### 1. Central spells an absent name two ways

A rest field (`anydata...;`) has no name to publish. Most packages send `"name": ""` — an empty
string, which `requiredString` accepts. `ballerina/time` omits the key entirely.

| package | rest fields | spelling | parsed |
|---|---|---|---|
| `ballerina/http` | 1 (`QueryParams`) | `"name": ""` | yes |
| `ballerinax/github` | 2 | `"name": ""` | yes |
| `ballerinax/slack` | 1 | `"name": ""` | yes |
| `ballerina/time` | 6 | key absent | **no** |

Validation is whole-document, so one such field failed every verb — `guide` included, which only
wants the readme and never touches `records[]`.

This is narrow. Across 30 sampled packages — stdlib and connectors, including `http`, `sql`,
`mime`, `graphql`, `grpc`, `jwt`, `crypto`, `salesforce`, `github`, `slack`, `twilio`, `mongodb`,
`kafka`, `aws.s3` — `ballerina/time` is the only one affected. Narrow and total.

### 2. A flattened inclusion carries the included record's rest field in with it

Accepting the absent name exposed the second defect. `time:Civil` is `*Date; *TimeOfDay;` in
source, and Central splices those in as members — copying each included record's implicit
`anydata...` along with them. `Civil` therefore arrives with **two** rest fields, at positions 3 and
7 of 11. Rendered verbatim that is:

```ballerina
public type Civil record {|
    int year; int month; int day;
    anydata...;
    int hour; int minute; Seconds second;
    anydata...;
    ZoneOffset utcOffset?;
    …
|};
```

which the compiler rejects four times over with `more record fields after rest field`. A document
that parses but teaches a record that cannot compile is worse than the refusal it replaced —
ADR-0010 holds that a named gap beats a plausible guess, and this would have been a guess wearing
the tool's own authority.

## Decision

**A field's name is required unless the type node says it is a rest field.** The type is read first
because it decides whether the name is required; a named field that loses its name is still drift.

**A rest field is dropped unless it is the last member.** `T...;` is legal only as a record's final
member, so a rest field with declarations after it is not something a source ever wrote — it is the
flattening artefact. Dropping it costs nothing: the record stays inclusive, and `record { … }`
already means `anydata...`.

The positional rule is the one to state, not an `anydata` one. Both are true of `time`, but every
genuine rest field in the corpus is the final member while naming a specific element type
(`QueryParamType`, `string`, `int`), and one of them — the `fields/rest` construct — is `anydata`
and correct. Keying on the element type would have deleted a legal declaration; keying on position
is the grammar's own rule.

## Consequences

`ballerina/time` answers on all six verbs. `Civil` renders as the inclusive record its source
declares, and the tool's own output compiles:

```ballerina
time:Civil civil = time:utcToCivil(now);
time:Utc parsed = check time:utcFromString(stamp);   // the signature that had to be guessed
```

Nothing else moved: 578 tests green, the eleven corpus snapshots byte-identical, and
`http:QueryParams` still renders `QueryParamType...;` inside closed braces.

Two constructs pin the pair — `fields/rest` (a lone trailing rest field renders faithfully) and
`fields/rest-stranded` (one with members after it is dropped) — and `KeySpaceTest` covers both
spellings of the absent name, including the negative that an ordinary field missing its name is
still drift.

The whole-document validation that turned one field into a six-verb outage is **not** addressed
here. Per-view validation, so a defect in `records[]` cannot take down `guide`, is the standing
follow-up.
