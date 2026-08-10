# Ballerina Code Rules

How code is written inside a `.bal` file. For file layout, packages, workspaces,
dependency management and `Config.toml`, see [project-structure.md](project-structure.md).
For tests, see [tests.md](tests.md) — write them only when the user asks.

## Module-Level Declarations

- Define `configurable` variables for all external values (API keys, hosts, ports, credentials).
  - Allowed types: `string`, `int`, `decimal`, `boolean` only.
  - Never assign hardcoded default values to configurables — reading an environment variable via `os:getEnv` (see Environment Variables below) is not a hardcoded default and is the expected pattern for platform-injected values.
- Initialize clients at module level, before any function or service declarations.
- Declare listeners with the `listener` keyword (`listener foo:Listener lsn = new (config);`), not a `final` variable — `service ... on lsn` attachment requires it; a `final foo:Listener` fails to compile.
- An event/streaming listener (change-data-capture, message topic/queue, etc.) attaches its service to a vendor channel/topic string that sits **between the service type and `on`**: `service <pkg>:<ServiceType> "<channel>" on <listener>` — e.g. a Salesforce CDC service binds to a channel like `service salesforce:CdcService "/data/LeadChangeEvent" on lsn`. The channel goes on the **`service` declaration** (its attach path) — **not** as a listener constructor argument; the listener `new (...)` takes only its config. This string isn't in the library API — get it from the connector's guide (`bal-library <org/name>` carries it) or the vendor docs, and wire it in **before** writing the service. If neither has it, **ask the user — never invent one**: without it the code usually still compiles and the service silently receives nothing. Never ship an event service without its channel.
- Implement a `main` function OR a service — not both, unless the requirement explicitly needs both.

## Data

- Use records for all data structures. Never use `map<json>`, `map<anydata>`, or raw `json`.
- Prefer closed records (`record {| ... |}`) for data shapes you own. Use an open record only when tolerating extra/unknown fields is deliberate (e.g. a loosely-specified inbound payload).
- Never access or manipulate a `json` variable directly. Define a record, convert json to it (`cloneWithType()` or `fromJsonStringWithType()`), then use the record.
- If a return typedesc is marked `<>` in a signature (`bal-library` prints it as `typedesc<T> T = <>`), define a custom record for the expected data shape.
- If a parameter type is `record {|anydata...;|}`, define or reuse an explicit named record — do not pass an anonymous literal.
- If a return type is `record {|anydata...;|}`, decide the shape, declare a named record, and assign to it.
- When accessing a field of a record, assign it to a new typed variable first, then use that variable in the next statement.

## Identifiers

- Always use **two-word camelCase** for ALL identifiers: variables, parameters, record fields (e.g., `userName`, `baseUrl`, `responseBody`).
- Exception: a record whose fields bind to external payload/JSON keys (e.g. via `cloneWithType()`) must use the **exact source key names** — even if that means single-word or PascalCase (e.g. `Name`, `CreatedDate`). The wire contract wins over the naming convention here.

## Function Calls

- Dot notation (`.`) for normal functions. Arrow notation (`->`) for remote and resource functions.
- Resource function invocation: `clientVar->/path/["param"].get(key="value")`
- Always use **named arguments**: `client->post("/path", message = payload)` — never positional.

## Type Safety

- Declare types explicitly in all variable declarations and `foreach` statements.
- To narrow a union or optional type: assign to a separate typed variable first, then use it in the `if` condition.
- An **optional field** (`field?: T` — what every non-required OpenAPI property generates) needs optional field access: `payload?.dueDate`, often `payload?.priority ?: "medium"`. Plain `payload.dueDate` fails to compile with *"field access cannot be used to access an optional field of a type that includes nil"*.
- Do not invoke methods on json access expressions — always use a separate statement.

## Imports

- Each `.bal` file must have its own import statements.
- Import only packages your code actually references — `bal build` errors on unused imports. Don't pre-import a connector's dependency module (e.g. `ballerina/sql` behind a database client) unless your code names a type from it.
- Do not import auto-imported langlibs: `lang.string`, `lang.boolean`, `lang.float`, `lang.decimal`, `lang.int`, `lang.map`.
- Packages with dots in names use aliases: `import org/package.one as one;`
- Submodules in `generated/<moduleName>/`: import as `import <packageName>.<moduleName>;` — the import should contain only the package name and submodule name, no path components.
- For SQL databases, import the matching `.driver` package alongside the client so the JDBC driver is on the runtime classpath (also required for GraalVM native builds):
  ```ballerina
  import ballerinax/postgresql;
  import ballerinax/postgresql.driver as _;
  ```
  The same pattern applies to the other SQL connectors — `mysql` + `mysql.driver`, `mssql` + `mssql.driver`, `oracledb` + `oracledb.driver`, `h2` + `h2.driver`.

## HTTP Service Design

When creating an HTTP service, define resource function signatures with possible return types:

```ballerina
resource function get users() returns UserList|http:NotFound {
    return http:NOT_FOUND;
}
```

Params bind from the signature, no annotation needed: a plain typed param is a query param (`?` or a default makes it optional), `@http:Header` binds a header. Escape a reserved word or a hyphen with a leading quote — `int 'limit = 20`, `@http:Header string x\-user\-id`.

`http:Ok`, `http:Created`, `http:BadRequest`, `http:Unauthorized`, `http:NotFound` are `record {| *CommonResponse; … |}`, so each takes `body?`, `headers?` and `mediaType?`. `http:NoContent` takes `headers?` only — no body — and `http:NO_CONTENT` is its empty value.

```ballerina
resource function get items(string? status, int 'limit = 20) returns http:Ok|http:BadRequest {
    return <http:Ok>{body: rows, mediaType: "application/json"};
}

resource function post items(@http:Header string x\-user\-id, ItemInput payload) returns http:Created {
    return <http:Created>{body: item, headers: {location: "/items/" + item.id}};
}
```

## SQL Queries

- Values interpolate into a `sql:ParameterizedQuery` backtick template — that *is* the parameter binding, so never assemble SQL by string concatenation.
- Add a conditional clause with `sql:queryConcat(q, ` AND status = ${status}`)`.
- `dbClient->query(q)` returns `stream<RowType, sql:Error?>`. `queryRow(q)` returns one row, or `sql:NoRowsError` when nothing matched — that is a 404, never a 500. `execute(q)` returns `sql:ExecutionResult` (`affectedRowCount`, `lastInsertId`).
- A `timestamptz` column binds to `time:Utc` (`sql:TimestampValue` wraps `string|time:Utc?`); a plain `timestamp` binds to `time:Civil` (`sql:DateTimeValue`).

## Time

`time:utcNow()` gives a `time:Utc`. `time:utcToString` / `time:utcFromString` are the RFC3339 round trip an OpenAPI `date-time` field needs. `time:utcToCivil` / `time:utcFromCivil` convert to and from `time:Civil` — there is no `civilToUtc`.

## Environment Variables

- Read a platform-injected environment variable (e.g. a dependency's `envBindings` in `design.json`) as a one-line `configurable` in `config.bal`, using `ballerina/os`:
  ```ballerina
  import ballerina/os;

  configurable string envVar = os:getEnv("MY_ENV_VAR");
  ```
- Read an env var through a `configurable`, never `os:getEnv` scattered through the code. Where the declarations live is [project-structure.md](project-structure.md)'s rule, not repeated here.

## Logging & Observability

- Use the `ballerina/log` module for logging: `log:printInfo`, `log:printError`, `log:printWarn`, `log:printDebug`. Attach context as structured key-value pairs (named arguments) rather than concatenating into the message — e.g. `log:printError("order failed", id = orderId)`.
- Never log secrets, auth tokens/headers, or raw request/response payloads — redact or hash sensitive values before logging.
- Prefer Ballerina's built-in runtime observability (metrics + distributed tracing) over hand-rolled instrumentation. It needs no code changes — enable it via `Config.toml`, or pass `--observability-included` to `bal run`/`bal build` (already set in the `Ballerina.toml` generated by `bal new`):
  ```toml
  [ballerina.observe]
  enabled = true
  provider = "<provider>"   # or set metricsEnabled/metricsReporter and tracingEnabled/tracingProvider separately
  ```
  Metrics (e.g. Prometheus) and traces (e.g. Jaeger) are then emitted automatically for services and clients.
- Only add custom spans/metrics via the `ballerina/observe` module when the built-in instrumentation isn't enough.

## Other Rules

- No dynamic listener registrations.
- No code that requires assigning values to function parameters.
- Propagate errors with `check`, or handle them with a `do`/`on fail` block; never use `checkpanic` to silence an error return in real code.
- `//` for single-line comments only. Keep comments minimal.

---

## Langlib

The calls above cover what most code needs. For the full surface — every type
conversion, and the array, string, map and number operations — load
[langlib.md](langlib.md). Do not run `bal-library` for a `lang.*` module: it is
part of the language rather than a package on Central.
