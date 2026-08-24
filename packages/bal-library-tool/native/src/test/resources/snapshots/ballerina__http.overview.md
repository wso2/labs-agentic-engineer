<!-- bal library overview v1 -->
# ballerina/http 0.0.0-fixture

| | |
|---|---|
| Clients | 10 — `ClientObject`, `StatusCodeClientObject`, `Caller`, `Client`, `ClientOAuth2Handler`, `FailoverClient`, `ListenerLdapUserStoreBasicAuthHandler`, `ListenerOAuth2Handler`, `LoadBalanceClient`, `StatusCodeClient` — `bal library client ballerina/http` |
| Classes | 91, too many to name here — `bal library class ballerina/http` |
| Module functions | 7 — `bal library funcs ballerina/http` |
| Errors | 65, too many to name here — read one with `bal library type ballerina/http <Name>` |
| Types | 470 declarations (135 records, 32 type aliases, 5 enums, 144 constants, 93 classes and object types, 61 module-level variables), not listed here — read one with `type` |
| Guide | 109 lines — `bal library guide ballerina/http` |

Guide chunks (2): 1. `Security`  2. `Listener` — `bal library guide ballerina/http <n>`

## Next

- `bal library client ballerina/http` — 10 clients, called with `->`
- `bal library class ballerina/http` — 91 classes and object types, called with `.`
- `bal library funcs ballerina/http` — 7 functions callable without a client
- `bal library overview ballerina/http -s "<what you need>"` — search every kind at once when you do not know which verb holds it
- `bal library type ballerina/http <Name> [-r]` — a declaration whole, with the types it names

## Clients — 10

- `ClientObject` — 7 resource, 15 remote · `bal library client ballerina/http ClientObject`
- `StatusCodeClientObject` — 7 resource, 15 remote · `bal library client ballerina/http StatusCodeClientObject`
- `Caller` — 5 remote, 1 normal · `bal library client ballerina/http Caller`
- `Client` — 7 resource, 15 remote, 4 normal · `bal library client ballerina/http Client`
- `ClientOAuth2Handler` — 1 remote, 2 normal · `bal library client ballerina/http ClientOAuth2Handler`
- `FailoverClient` — 7 resource, 15 remote, 1 normal · `bal library client ballerina/http FailoverClient`
- `ListenerLdapUserStoreBasicAuthHandler` — 2 remote · `bal library client ballerina/http ListenerLdapUserStoreBasicAuthHandler`
- `ListenerOAuth2Handler` — 1 remote · `bal library client ballerina/http ListenerOAuth2Handler`
- `LoadBalanceClient` — 7 resource, 15 remote · `bal library client ballerina/http LoadBalanceClient`
- `StatusCodeClient` — 7 resource, 15 remote, 4 normal · `bal library client ballerina/http StatusCodeClient`

## Classes and object types — 91

- `ClientBasicAuthHandler` — 3 normal · `bal library class ballerina/http ClientBasicAuthHandler`
- `ClientBearerTokenAuthHandler` — 3 normal · `bal library class ballerina/http ClientBearerTokenAuthHandler`
- `ClientSelfSignedJwtAuthHandler` — 3 normal · `bal library class ballerina/http ClientSelfSignedJwtAuthHandler`
- `Cookie` — 3 normal · `bal library class ballerina/http Cookie`
- `CookieStore` — 10 normal · `bal library class ballerina/http CookieStore`
- `CsvPersistentCookieHandler` — 4 normal · `bal library class ballerina/http CsvPersistentCookieHandler`
- `DefaultStatus` — nothing callable · `bal library class ballerina/http DefaultStatus`
- `Headers` — 4 normal · `bal library class ballerina/http Headers`
- `HttpCache` — nothing callable · `bal library class ballerina/http HttpCache`
- `HttpFuture` — nothing callable · `bal library class ballerina/http HttpFuture`
- `ListenerFileUserStoreBasicAuthHandler` — 2 normal · `bal library class ballerina/http ListenerFileUserStoreBasicAuthHandler`
- `ListenerJwtAuthHandler` — 2 normal · `bal library class ballerina/http ListenerJwtAuthHandler`
- `LoadBalancerRoundRobinRule` — 1 normal · `bal library class ballerina/http LoadBalancerRoundRobinRule`
- `PushPromise` — 8 normal · `bal library class ballerina/http PushPromise`
- `Request` — 34 normal · `bal library class ballerina/http Request`
- `RequestCacheControl` — 1 normal · `bal library class ballerina/http RequestCacheControl`
- `RequestContext` — 7 normal · `bal library class ballerina/http RequestContext`
- `Response` — 35 normal · `bal library class ballerina/http Response`
- `ResponseCacheControl` — 2 normal · `bal library class ballerina/http ResponseCacheControl`
- `StatusAccepted` — nothing callable · `bal library class ballerina/http StatusAccepted`

71 more, not listed — `bal library class ballerina/http -s "<what it does>"` searches all of them at once.

## Module-level functions — 7, call with `.`

```
authenticateResource     createHttpCachingClient  createHttpSecureClient   getDefaultListener
getHeaderMap             getQueryMap              parseHeader
```

## Quickstart

*Every Ballerina block in the package's own readme, quoted verbatim and in its order. It is Central's text and can be out of date; the signatures the container verbs generate win wherever the two disagree.*

<!-- guide: begin ballerina/http readme usage -->

```ballerina
http:Client clientEndpoint = check new("https://my-simple-backend.com");
```

```ballerina
// Send a GET request to the specified endpoint.
http:Response response = check clientEndpoint->get("/get?id=123");
```

```ballerina
// Retrieve payload as json.
json payload = check clientEndpoint->post("/backend/Json", "foo");
```

```ballerina
// Attributes associated with the `Listener` endpoint are defined here.
listener http:Listener helloWorldEP = new(9090);
```

```ballerina
// By default, Ballerina assumes that the service is to be exposed via HTTP/1.1.
service /helloWorld on helloWorldEP {

   resource function post [string name](@http:Payload string message) returns string {
       // Sends the response back to the client along with a string payload.
       return "Hello, World! I’m " + name + ". " + message;
   }
}
```

<!-- guide: end ballerina/http readme usage -->
