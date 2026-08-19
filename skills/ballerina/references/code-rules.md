# Ballerina Code Rules

How code is written inside a `.bal` file. For file layout, packages, workspaces,
dependency management and `Config.toml`, see [project-structure.md](project-structure.md).
For tests, see [tests.md](tests.md) — write them only when the user asks.

## Module-Level Declarations

- Define `configurable` variables for all external values (API keys, hosts, ports, credentials).
  - Allowed types: `string`, `int`, `decimal`, `boolean` only.
  - Never assign hardcoded default values to configurables — reading an environment variable via `os:getEnv` (see Environment Variables below) is not a hardcoded default and is the expected pattern for platform-injected values.
  - A configurable with no default is written `configurable string apiKey = ?;` — `= ?` which means during the runtime, this value must be provided.
- Initialize clients at module level, before any function or service declarations: `final foo:Client fooClient = check new (config);`. A constructor is allowed there; but connector calls are not, wrap those in function body and call them if needed.
- Declare listeners with the `listener` keyword (`listener foo:Listener lsn = new (config);`), not a `final` variable — `service ... on lsn` attachment requires it; a `final foo:Listener` fails to compile.
- An event/streaming listener (change-data-capture, message topic/queue, etc.) attaches its service to a vendor channel/topic string that sits **between the service type and `on`**: `service <pkg>:<ServiceType> "<channel>" on <listener>` — e.g. a Salesforce CDC service binds to a channel like `service salesforce:CdcService "/data/LeadChangeEvent" on lsn`. The channel goes on the **`service` declaration** (its attach path) — **not** as a listener constructor argument; the listener `new (...)` takes only its config. This string isn't in the library API — get it from the connector's readme (`bal library guide <org/name>`) or the vendor docs, and wire it in **before** writing the service. If neither has it, **ask the user — never invent one**: without it the code usually still compiles and the service silently receives nothing. Never ship an event service without its channel.
- Only some of a package's `service object` types are attachable to its listener. `bal library api` is what answers that (its own `--help` says how to read the answer): **never write `on new Listener(...)` for a type the document could not confirm.** An interceptor is the common case — it reaches the runtime as a `createInterceptors()` return from an interceptable service, not as an attachment.
- A template with an empty body is a skeleton, not a complete service. The comment inside it says when the method contract is unpublished, and several listeners reject an empty body — a GraphQL service needs at least one `resource function get`, a Kafka service needs `remote function onConsumerRecord`. Take the required methods from `bal library guide <org/name>`.
- Implement a `main` function OR a service — not both.
- Startup work — creating a table, warming a cache — needs neither. A module-level `final () dbReady = check initDb();` runs before any listener starts, so a failure there fails the service fast.

## Data

- Use records for all data structures. Never use `map<json>`, `map<anydata>`, or raw `json` — unless a library signature declares one. This also, convert into a record as much as possible.
- Prefer closed records (`record {| ... |}`) for data shapes you own. Use an open record only when tolerating extra/unknown fields is deliberate (e.g. a loosely-specified inbound payload).
- Never access or manipulate a `json` variable directly. Define a record, convert json to it (`cloneWithType()` or `fromJsonStringWithType()`), then use the record.
- If a return typedesc is marked `<>` in a signature (`bal library` prints it as `typedesc<T> T = <>`), define a custom record for the expected data shape.
- If a parameter type is `record {|anydata...;|}`, define or reuse an explicit named record — do not pass an anonymous literal.
- If a return type is `record {|anydata...;|}`, decide the shape, declare a named record, and assign to it.
- When accessing a field of a record, assign it to a new typed variable first, then use that variable in the next statement.

## Identifiers

- Always use **two-word camelCase** for ALL identifiers: variables, parameters, record fields (e.g., `userName`, `baseUrl`, `responseBody`).
- Exception: a record whose fields bind to external payload/JSON keys (e.g. via `cloneWithType()`) must use the **exact source key names** — even if that means single-word or PascalCase (e.g. `Name`, `CreatedDate`). The wire contract wins over the naming convention here.
- A reserved word cannot name anything — function, variable, field, parameter, resource path segment or any identifier — without a leading quote. The ones that bite read like ordinary service nouns: `conflict`, `order`, `limit`, `start`, `join`, `outer`, `select`, `from`, `where`, `on`, `by`, `equals`, `let`, `do`, `fail`. 
- Use ' to escape the reserved keyword (int 'limit = 20).

## Function Calls

- Dot notation (`.`) for normal functions. Arrow notation (`->`) for remote and resource functions (Only works inside a function body).
- Resource function invocation: `clientVar->/path/["param"].get(key="value")`. A path whose segment contains a dot is still a path — `clientVar->/some\.operation.post(payload)`, never `clientVar->some\.operation(payload)`. The escaped dot is part of the segment, not a method name. The last `.get(), .post() .. ` is the accessor.
- Use **named arguments** for optional and defaultable parameters: `client->post("/path", message = payload)`. An included-record parameter — written `*SomeConfig` in the signature — is passed positionally as a record literal instead.
- A call returning a non-nil value cannot stand alone as a statement; assign it, discarding with `_` if unused: `ResultType _ = check client->doSomething(arg);`. Only a `()`-returning call may be a bare `check` statement.

## Type Safety

- Declare types explicitly in all variable declarations and `foreach` statements.
- To narrow a union or optional type: assign to a separate typed variable first, then use it in the `if` condition.
- Narrow with **sequential early-return `if`s, never an `else if` chain** — for any union, not just `T|error`, a narrowing does not survive an `if`/`else if` that has no final `else`: afterwards the value is still the whole union and `x.field` fails (*"does not support field access"*) even though every branch returned.
- Better still, guard for what you want and return the rest in one line — `if r !is Success { return r; }` — so a helper that maps failures to results returns the whole union and each caller narrows once before the happy path.
- An **optional field** (`field?: T` — what every non-required OpenAPI property generates) needs optional field access: `payload?.dueDate`, often `payload?.priority ?: "medium"`. Plain `payload.dueDate` fails to compile with *"field access cannot be used to access an optional field of a type that includes nil"*.
- Do not invoke methods on json access expressions — always use a separate statement.

## Imports

- Each `.bal` file must have its own import statements.
- Import only packages your code actually references — `bal build` errors on unused imports. Don't pre-import a connector's dependency module (e.g. `ballerina/sql` behind a database client) unless your code names a type from it.
- Do not import a langlib whose name is a basic type — the type keyword itself puts the prefix in scope, so the import is an error. That is `lang.boolean`, `lang.decimal`, `lang.error`, `lang.float`, `lang.function`, `lang.future`, `lang.int`, `lang.map`, `lang.object`, `lang.stream`, `lang.string`, `lang.table`, `lang.typedesc` and `lang.xml`. The rule is the keyword, not the `lang.` prefix: **`lang.value`, `lang.array` and `lang.regexp` DO need importing** — `import ballerina/lang.value;`, and `undefined module` if you leave it out. Measured against the compiler, one module per line. For `lang.regexp` that means when you name a `regexp:` symbol; the bare `re` literal needs nothing — see Regular Expressions.
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

Every `http:<StatusName>` record is `record {| *CommonResponse; … |}` and takes `body?`, `headers?` and `mediaType?` — `http:Ok`, `http:Created`, `http:BadRequest`, `http:Unauthorized`, `http:NotFound`, `http:Conflict` and `http:InternalServerError` alike. It is a family, not a list: you do not need a lookup to confirm one exists. `http:NoContent` takes `headers?` only — no body — and `http:NO_CONTENT` is its empty value.

`check` needs an `error` member in the enclosing return type, so a resource returning `UserList|http:NotFound` cannot use it — that is *"invalid usage of the 'check' expression operator: no matching error return type(s) in the enclosing invokable"*, and it catches `.close()` and langlib calls as much as client actions. Add `|error` to the union and `http` maps a returned error to 500, which is the right shape for an upstream failure the contract never modelled; otherwise assign to `T|error` and branch with the early-return guards under Type Safety.

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

- Use the `ballerina/log` module for logging: `log:printInfo`, `log:printError`, `log:printWarn`, `log:printDebug`. Attach context as structured key-value pairs (named arguments) rather than concatenating into the message — e.g. `log:printError("order failed", id = orderId, 'error = err)`.
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

# Ballerina Langlib Reference

## Contents
- [Type Conversion](#type-conversion)
- [JSON Conversion](#json-conversion)
- [Arrays](#arrays)
- [Strings](#strings)
- [Maps](#maps)
- [Numbers](#numbers)
- [Errors](#errors)
- [Sleep](#sleep)
- [Query Expressions](#query-expressions)
- [XML](#xml)
- [Regular Expressions](#regular-expressions)

---

## Type Conversion

```ballerina
int age = check int:fromString("25");
float price = check float:fromString("19.99");
decimal exact = check decimal:fromString("10.50");
boolean flag = check boolean:fromString("true");
xml bookXml = check xml:fromString("<book>Hamlet</book>");

// Special float values
float notANumber = check float:fromString("NaN");
float infinity = check float:fromString("Infinity");
```

---

## JSON Conversion

```ballerina
// Parse JSON string
json data = check jsonText.fromJsonString();

// JSON string → typed array
int[] nums = check jsonArray.fromJsonStringWithType();

// JSON string → record
type Config record {| int port; int timeout; |};
Config cfg = check configText.fromJsonStringWithType(Config);

// Record → JSON
json result = person.toJson();
string jsonStr = person.toJsonString();

// json → record (cloneWithType)
json raw = {port: 8080};
Config config = check raw.cloneWithType();

// Validate field type (ensureType)
json[] subjects = check student.subjects.ensureType();

// Clone a value
int[] copy = original.clone();
```

---

## Arrays

```ballerina
numbers.length()        // count
numbers.push(4)         // append
numbers.pop()           // remove last, returns it
numbers.unshift(0)      // prepend
numbers.shift()         // remove first, returns it
numbers.indexOf(30)     // int? — index or ()
numbers.sort()          // ascending
numbers.sort("descending")
```

---

## Strings

```ballerina
text.length()
text.substring(0, 5)
text.indexOf("World")           // int? — index or ()
text.includes("World")          // boolean
text.includes("o", 5)           // search from index
text.startsWith("Hello")
text.endsWith("World")
text.trim()
text.toUpperAscii()
text.toLowerAscii()
text.toBytes()                  // byte[]
string:fromBytes(data)          // check — byte[] → string
string:'join(", ", "a", "b")    // "a, b"
"Hello".concat(" ", "World")

// Code points
int code = string:toCodePointInt("A");           // 65
string char = check string:fromCodePointInt(65); // "A"
int[] codes = "Hello".toCodePointInts();
string text = check string:fromCodePointInts([72, 101, 108, 108, 111]);
int code = "Hello".getCodePoint(0);              // 72
```

---

## Maps

```ballerina
scores.length()
scores.get("Alice")             // panics if missing
scores.hasKey("Alice")          // boolean
scores.keys()                   // string[]
scores.toArray()                // value[]
scores.remove("Alice")          // returns value, panics if missing
scores.removeIfHasKey("Carol")  // returns value? — safe remove
scores.removeAll()
```

---

## Numbers

```ballerina
(255).toHexString()             // "ff"
int value = check int:fromHexString("ff"); // 255
```

---

## Errors

```ballerina
err.message()                   // string
err.detail()                    // map<value:Cloneable> & readonly
err.cause()                     // error?
```

**Branching on an HTTP client error.** The most-looked-up thing in this whole skill, so it is here rather than behind a fetch. `http:Client` operations return `T|http:ClientError`, and the status code lives on the detail record of the three errors that carry one:

```ballerina
FullRepository|error result = github->/repos/[owner]/[repo];
if result is http:ClientRequestError {
    int status = result.detail().statusCode;   // 4xx
} else if result is http:RemoteServerError {
    int status = result.detail().statusCode;   // 5xx
} else if result is error {
    log:printError("call failed", result);
}
```

The hierarchy is `ClientRequestError` and `RemoteServerError` under `ApplicationResponseError` under `ClientError` under `Error`, and **only those three carry `detail().statusCode`** — `http:SslError` and the rest reach `Error` without one, so `is` on the specific type is what makes `.detail()` legal. `bal library type ballerina/http ClientRequestError -r` prints the whole chain and the detail record.

---

## Sleep

```ballerina
import ballerina/lang.runtime;
runtime:sleep(2); // pause for 2 seconds
```

---

## Query Expressions

```ballerina
// Filter array
int[] even = from int n in numbers where n % 2 == 0 select n;

// Transform records
string[] names = from var p in people where p.age > 23 select p.name;

// Process stream
int[] filtered = from int num in numberStream where num > 2 select num;
```

---

## XML

```ballerina
xml element = xml `<book><title>Hamlet</title></book>`;
xml books = xml `<book>Book1</book>` + xml `<book>Book2</book>`;
xml combined = xml:concat(xml `<item>First</item>`, xml `<item>Second</item>`);
xml parsed = check xml:fromString(xmlText);
int count = items.length();

// For XML ↔ record conversion, use ballerina/data.xmldata
```

---

## Regular Expressions

Import `ballerina/lang.regexp` only when your code NAMES a `regexp:` symbol —
`regexp:Span`, `regexp:Groups`, `regexp:fromString`, `regexp:Flags`. The `re` literal
is syntax and its methods are on `string:RegExp`, so
`re \`,\`.split(line)`, `.replaceAll(...)`, `.isFullMatch(...)` and `.findAll(...)`
need no import at all — adding one there is `unused module prefix 'regexp'` and a
failed build. (Measured twice: it was the only compile error in a whole
maintenance-service run, and the only one in the eval case that reproduces it.)

```ballerina
// No import needed — `re` is a literal, the methods are on string:RegExp
string[] fields = re `,`.split("a,b,c");
string cleaned = re `[0-9]+`.replaceAll("a1b2", "X");

// Create pattern
string:RegExp pattern = re `[0-9]+`;
string:RegExp pattern = check regexp:fromString("[0-9]+");

// Find
regexp:Span? match = pattern.find("Hello123");
regexp:Span[] all = re `[0-9]+`.findAll("a1b2c3");

// Capture groups
regexp:Groups? groups = re `([a-z]+)([0-9]+)`.findGroups("abc123");
if groups is regexp:Groups {
    string full = groups[0].substring();   // "abc123"
    string letters = groups[1].substring(); // "abc"
    string numbers = groups[2].substring(); // "123"
}

// Validate full match
boolean valid = re `[0-9]+`.isFullMatch("123");

// Replace
string result = re `[0-9]+`.replace("a1b2", "X");      // "aXb2"
string result = re `[0-9]+`.replaceAll("a1b2", "X");   // "aXbX"

// Split
string[] parts = re `,\s*`.split("a, b, c"); // ["a", "b", "c"]
```
