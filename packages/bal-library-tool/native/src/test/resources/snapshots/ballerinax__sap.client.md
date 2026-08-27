<!-- bal library client v1 -->
# Clients — ballerinax/sap `Client`

| | |
|---|---|
| Container | `Client` — 7 resource, 7 remote |
| Showing | 15 signatures |

## Next

- one call and every type it needs: `bal library client ballerinax/sap Client init -r`

## Constructor — 1

```ballerina
# Gets invoked to initialize the `client`. During initialization, the configurations provided through the `config`
# record is used to determine which type of additional behaviours are added to the endpoint (e.g.
# security, circuit breaking). Caching is enabled always.
isolated function init(string url, http:ClientConfiguration config) returns ClientError?; // Special Agent Note: ClientConfiguration FROM ballerina/http module
```

## Resource functions — 7, call with `-> and a path`

```ballerina
# The client resource function to send HTTP POST requests to SAP HTTP endpoints.
isolated resource function post [http:PathParamType... path](http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http module

# The client resource function to send HTTP PUT requests to SAP HTTP endpoints.
isolated resource function put [http:PathParamType... path](http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http module

# The client resource function to send HTTP PATCH requests to SAP HTTP endpoints.
isolated resource function patch [http:PathParamType... path](http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http module

# The client resource function to send HTTP DELETE requests to SAP HTTP endpoints.
isolated resource function delete [http:PathParamType... path](http:RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http module

# The client resource function to send HTTP HEAD requests to SAP HTTP endpoints.
isolated resource function head [http:PathParamType... path](map<string|string[]>? headers = (), *http:QueryParams params) returns http:Response|ClientError; // Special Agent Note: QueryParams, Response FROM ballerina/http module

# The client resource function to send HTTP GET requests to SAP HTTP endpoints.
isolated resource function get [http:PathParamType... path](map<string|string[]>? headers = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: QueryParams FROM ballerina/http module

# The client resource function to send HTTP OPTIONS requests to SAP HTTP endpoints.
isolated resource function options [http:PathParamType... path](map<string|string[]>? headers = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: QueryParams FROM ballerina/http module
```

## Remote functions — 7, call with `->`

```ballerina
# The `Client.post()` function can be used to send HTTP POST requests to SAP HTTP endpoints.
isolated remote function post(string path, http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http module

# The `Client.put()` function can be used to send HTTP PUT requests to SAP HTTP endpoints.
isolated remote function put(string path, http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http module

# The `Client.patch()` function can be used to send HTTP PATCH requests to SAP HTTP endpoints.
isolated remote function patch(string path, http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http module

# The `Client.delete()` function can be used to send HTTP DELETE requests to SAP HTTP endpoints.
isolated remote function delete(string path, http:RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http module

# The `Client.head()` function can be used to send HTTP HEAD requests to SAP HTTP endpoints.
isolated remote function head(string path, map<string|string[]>? headers = ()) returns http:Response|ClientError; // Special Agent Note: Response FROM ballerina/http module

# The `Client.get()` function can be used to send HTTP GET requests to SAP HTTP endpoints.
isolated remote function get(string path, map<string|string[]>? headers = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError;

# The `Client.options()` function can be used to send HTTP OPTIONS requests to SAP HTTP endpoints.
isolated remote function options(string path, map<string|string[]>? headers = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError;
```
