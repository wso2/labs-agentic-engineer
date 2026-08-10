<!-- bal-library ops v1 -->
# Operations — ballerinax/sap `Client`

| | |
|---|---|
| Source | central |
| Path | `(root)` → descended to `{...path}` (7 of 7) |

## 7 operations at this path

```ballerina
# The client resource function to send HTTP POST requests to SAP HTTP endpoints.
resource function post [http:PathParamType... path](http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

# The client resource function to send HTTP PUT requests to SAP HTTP endpoints.
resource function put [http:PathParamType... path](http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

# The client resource function to send HTTP PATCH requests to SAP HTTP endpoints.
resource function patch [http:PathParamType... path](http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

# The client resource function to send HTTP DELETE requests to SAP HTTP endpoints.
resource function delete [http:PathParamType... path](http:RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

# The client resource function to send HTTP HEAD requests to SAP HTTP endpoints.
resource function head [http:PathParamType... path](map<string|string[]> headers = (), http:QueryParams params) returns http:Response|ClientError; // Special Agent Note: QueryParams, Response FROM ballerina/http package

# The client resource function to send HTTP GET requests to SAP HTTP endpoints.
resource function get [http:PathParamType... path](map<string|string[]> headers = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: QueryParams FROM ballerina/http package

# The client resource function to send HTTP OPTIONS requests to SAP HTTP endpoints.
resource function options [http:PathParamType... path](map<string|string[]> headers = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: QueryParams FROM ballerina/http package
```

## Next

- signatures: `bal-library ops ballerinax/sap '{...path}' --sigs` — 7 operations, 2,546 bytes
- a declaration named in a signature: `bal-library type ballerinax/sap <Name> [--deps]`
