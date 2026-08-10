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
- One declaration per env var, all in `config.bal` — don't scatter `os:getEnv` calls across other files.

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

The hierarchy is `ClientRequestError` and `RemoteServerError` under `ApplicationResponseError` under `ClientError` under `Error`, and **only those three carry `detail().statusCode`** — `http:SslError` and the rest reach `Error` without one, so `is` on the specific type is what makes `.detail()` legal. `bal-library type ballerina/http ClientRequestError --deps` prints the whole chain and the detail record.

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

Must import: `import ballerina/lang.regexp;`

```ballerina
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
