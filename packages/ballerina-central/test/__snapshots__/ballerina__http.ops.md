<!-- bal-library ops v1 -->
# Operations — ballerina/http `Client`

| | |
|---|---|
| Source | central |
| Path | `(root)` → descended to `{...path}` (7 of 7) |

## 7 operations at this path

```ballerina
# The client resource function to send HTTP GET requests to HTTP endpoints.
resource function get [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The client resource function to send HTTP POST requests to HTTP endpoints.
resource function post [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The client resource function to send HTTP PUT requests to HTTP endpoints.
resource function put [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The client resource function to send HTTP DELETE requests to HTTP endpoints.
resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The client resource function to send HTTP PATCH requests to HTTP endpoints.
resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The client resource function to send HTTP HEAD requests to HTTP endpoints.
resource function head [PathParamType ...path](map<string|string[]> headers = (), QueryParams params) returns Response|ClientError;

# The client resource function to send HTTP OPTIONS requests to HTTP endpoints.
resource function options [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;
```

## Next

- signatures: `bal-library ops ballerina/http '{...path}' --sigs` — 7 operations, 1,848 bytes
- a declaration named in a signature: `bal-library type ballerina/http <Name> [--deps]`
