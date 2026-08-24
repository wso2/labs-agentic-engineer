<!-- bal library overview v1 -->
# ballerina/graphql 0.0.0-fixture

| | |
|---|---|
| Clients | 1 — `Client` — `bal library client ballerina/graphql` |
| Classes | 5 — `Context`, `Field`, `Service`, `Interceptor`, `Listener` — `bal library class ballerina/graphql` |
| Module functions | 2 — `bal library funcs ballerina/graphql` |
| Errors | 9 — `AuthnError`, `AuthzError`, `ClientError`, `Error`, `HttpError`, `InvalidDocumentError`, `PayloadBindingError`, `RequestError`, `ServerError` — read one with `bal library type ballerina/graphql <Name>` |
| Types | 67 declarations (47 records, 6 type aliases, 1 enums, 5 constants, 5 classes and object types, 3 module-level variables), not listed here — read one with `type` |
| Guide | 833 lines — `bal library guide ballerina/graphql` |

## Quickstart

*Quoted from the package's own readme and checked against this version's declarations. A line marked `⚠` names something this version does not declare. The signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerina/graphql readme usage -->

```ballerina
import ballerina/graphql;

listener graphql:Listener graphqlListener = check new(4000);
```

<!-- guide: end ballerina/graphql readme usage -->

11 more examples — `bal library guide ballerina/graphql`

Guide chunks (15): 1. `Create a standalone `graphql:Listener``  2. `Create a `graphql:Listener` using an `http:Listener``  3. `Service`  4. `Query type`  5. `Mutation type`  6. `Subscription Type`  7. `Scalar types`  8. `Enums`  9. `Record types`  10. `Service types`  11. `Arrays`  12. `Nullable types`  13. `Union types`  14. `Errors`  15. `Hierarchical resource paths` — `bal library guide ballerina/graphql <n>`

## Next

- `bal library client ballerina/graphql Client` — the client's whole callable surface
- `bal library class ballerina/graphql` — 5 classes and object types, called with `.`
- `bal library funcs ballerina/graphql` — 2 functions callable without a client
- `bal library overview ballerina/graphql -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerina/graphql <Name> [-r]` — a declaration whole, with the types it names

## Clients — 1

- `Client` — 2 remote · `bal library client ballerina/graphql Client`

## Classes and object types — 5

- `Context` — 8 normal · `bal library class ballerina/graphql Context`
- `Field` — 7 normal · `bal library class ballerina/graphql Field`
- `Service` — nothing callable · `bal library class ballerina/graphql Service`
- `Interceptor` — 1 remote · `bal library class ballerina/graphql Interceptor`
- `Listener` — 5 normal · `bal library class ballerina/graphql Listener`

## Module-level functions — 2, call with `.`

```
__addError    getSdlString
```
