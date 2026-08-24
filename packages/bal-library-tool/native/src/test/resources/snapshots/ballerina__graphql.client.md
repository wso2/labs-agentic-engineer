<!-- bal library client v1 -->
# Clients — ballerina/graphql `Client`

| | |
|---|---|
| Container | `Client` — 2 remote |
| Showing | 3 signatures |

## Next

- one call and every type it needs: `bal library client ballerina/graphql Client init -r`

## Constructor — 1

```ballerina
# Gets invoked to initialize the `connector`.
isolated function init(string serviceUrl, *ClientConfiguration clientConfig) returns ClientError?;
```

## Remote functions — 2, call with `->`

```ballerina
# Executes a GraphQL document and data binds the GraphQL response to a record with data and extensions
# which is a subtype of GenericResponse.
@deprecated
isolated remote function executeWithType(string document, map<anydata>? variables = (), string? operationName = (), map<string|string[]>? headers = (), typedesc<GenericResponse|record {}|json> targetType = <>) returns targetType|ClientError;

# Executes a GraphQL document and data binds the GraphQL response to a record with data, extensions and errors
# which is a subtype of GenericResponseWithErrors.
isolated remote function execute(string document, map<anydata>? variables = (), string? operationName = (), map<string|string[]>? headers = (), typedesc<GenericResponseWithErrors|record {}|json> targetType = <>) returns targetType|ClientError;
```
