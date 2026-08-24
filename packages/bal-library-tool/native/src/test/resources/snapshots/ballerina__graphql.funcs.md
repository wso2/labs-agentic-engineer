<!-- bal library funcs v1 -->
# Module functions — ballerina/graphql

| | |
|---|---|
| Showing | 2 signatures |

## Next

- one call and every type it needs: `bal library funcs ballerina/graphql __addError -r`

## Module-level functions — 2, call with `.`

```ballerina
# Adds an error to the GraphQL response. Using this to add an error is not recommended.
# + context - The context of the GraphQL request.
# + errorDetail - The error to be added to the response.
public isolated function __addError(Context context, ErrorDetail errorDetail);

# Obtains the schema representation of a federated subgraph, expressed in the SDL format.
# + encodedSchemaString - Compile time auto generated schema
# + return - Subgraph schema in SDL format as a string on success, or an error otherwise
public isolated function getSdlString(string encodedSchemaString) returns string|error;
```
