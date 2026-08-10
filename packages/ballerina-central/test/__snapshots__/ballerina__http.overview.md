<!-- bal-library overview v1 -->
# ballerina/http 0.0.0-fixture

| | |
|---|---|
| Source | central |
| Clients | `Caller`, `Client`, `ClientOAuth2Handler`, `FailoverClient`, `ListenerLdapUserStoreBasicAuthHandler`, `ListenerOAuth2Handler`, `LoadBalanceClient`, `StatusCodeClient` |
| Module functions | 7, listed below |
| Errors | 56, listed below |
| Types | 408 declarations (135 records, 39 unions, 234 other), not listed here — read one with `type` |

## Next

- `bal-library ops ballerina/http <path>` — navigate a client's operations
- `bal-library type ballerina/http <Name> [--deps]` — read a declaration whole
- `bal-library api ballerina/http` — every declaration, when nothing above answered

## Client `Caller`

The caller actions for responding to client requests.

### Remote functions — 5, call with `->`

```ballerina
# Sends the outbound response to the caller.
remote function respond(ResponseMessage|StatusCodeResponse|error message = ()) returns ListenerError?;

# Pushes a promise to the caller.
remote function promise(PushPromise promise) returns ListenerError?;

# Sends a promised push response to the caller.
remote function pushPromisedResponse(PushPromise promise, Response response) returns ListenerError?;

# Sends a `100-continue` response to the caller.
remote function 'continue() returns ListenerError?;

# Sends a redirect response to the user with the specified redirection status code.
remote function redirect(Response response, RedirectCode code, string[] locations) returns ListenerError?;
```

### Normal functions — 1, call with `.`

```ballerina
# Gets the hostname from the remote address. This method may trigger a DNS reverse lookup if the address was created
# with a literal IP address.
# ```ballerina
# string? remoteHost = caller.getRemoteHostName();
# ```
function getRemoteHostName() returns string?;
```

## Client `Client`

The HTTP client provides functionality to connect to remote HTTP services and perform requests using standard HTTP methods like GET, POST, PUT, DELETE, etc.

### Constructor

```ballerina
function init(string url, ClientConfiguration config) returns ClientError?;
```

### Remote functions — 15, call with `->`

```ballerina
# Retrieve a representation of a specified resource from an HTTP endpoint.
remote function get(string path, map<string|string[]> headers = (), TargetType targetType = <>) returns targetType|ClientError;

# Create a new resource or submit data to a resource for processing.
remote function post(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# Create a new resource or replace a representation of a specified resource.
remote function put(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# Remove a specified resource from an HTTP endpoint.
remote function delete(string path, RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# Partially update an existing resource in an HTTP endpoint.
remote function patch(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# Get the metadata of a resource in the form of headers without the body. Often used for testing the resource existence or finding recent modifications.
remote function head(string path, map<string|string[]> headers = ()) returns Response|ClientError;

# Get the communication options for a specified resource.
remote function options(string path, map<string|string[]> headers = (), TargetType targetType = <>) returns targetType|ClientError;

# Send a request using any HTTP method. Can be used to invoke the endpoint with a custom or less common HTTP method.
remote function execute(string httpVerb, string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# Forward an incoming request to another endpoint using the same HTTP method. Can be used in proxy or gateway scenarios.
remote function forward(string path, Request request, TargetType targetType = <>) returns targetType|ClientError;

# Send an asynchronous HTTP request that does not wait for the response immediately. Can be used for non-blocking operations.
remote function submit(string httpVerb, string path, RequestMessage message) returns HttpFuture|ClientError;

# Get the response from a previously submitted asynchronous request. Can be used after calling `submit()` action to retrieve the actual response.
remote function getResponse(HttpFuture httpFuture) returns Response|ClientError;

# Check if the server has sent a push promise for additional resources. Should be used with HTTP/2 server push functionality.
remote function hasPromise(HttpFuture httpFuture) returns boolean;

# Get the next server push promise that contains information about additional resources the server wants to send.
remote function getNextPromise(HttpFuture httpFuture) returns PushPromise|ClientError;

# Get the actual response data from a server push promise. Can be used to receive resources that the server proactively sends.
remote function getPromisedResponse(PushPromise promise) returns Response|ClientError;

# Reject a server push promise to decline receiving the additional resource.
remote function rejectPromise(PushPromise promise) returns nil;
```

### Normal functions — 4, call with `.`

```ballerina
# Get the cookie storage associated with this HTTP client. Can be used to access stored cookies for session management.
function getCookieStore() returns CookieStore?;

# Force the circuit breaker to allow all requests through, ignoring current error rates. Can be used to manually
# restore service after fixing issues.
function circuitBreakerForceClose() returns nil;

# Force the circuit breaker to block all requests until the reset time expires. Can be used to manually stop
# requests during maintenance or known issues.
function circuitBreakerForceOpen() returns nil;

# Check the current state of the circuit breaker. Can be used to monitor the health status of your HTTP connections.
function getCircuitBreakerCurrentState() returns CircuitState;
```

### Resource functions — 7, call with `->` and a path

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

## Client `ClientOAuth2Handler`

Defines the OAuth2 handler for client authentication.

### Constructor

```ballerina
function init(OAuth2GrantConfig config) returns nil;
```

### Remote functions — 1, call with `->`

```ballerina
# Enrich the request with the relevant authentication requirements.
remote function enrich(Request req) returns Request|ClientAuthError;
```

### Normal functions — 2, call with `.`

```ballerina
# Enrich the headers map with the relevant authentication requirements.
function enrichHeaders(map<string|string[]> headers) returns map<string|string[]>|ClientAuthError;

# Returns the headers map with the relevant authentication requirements.
function getSecurityHeaders() returns map<string|string[]>|ClientAuthError;
```

## Client `FailoverClient`

An HTTP client endpoint which provides failover support over multiple HTTP clients.

### Constructor

```ballerina
function init(FailoverClientConfiguration failoverClientConfig) returns ClientError?;
```

### Remote functions — 15, call with `->`

```ballerina
# The POST remote function implementation of the Failover Connector.
remote function post(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# The PUT remote function  implementation of the Failover Connector.
remote function put(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# The PATCH remote function implementation of the Failover Connector.
remote function patch(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# The DELETE remote function implementation of the Failover Connector.
remote function delete(string path, RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# The HEAD remote function implementation of the Failover Connector.
remote function head(string path, map<string|string[]> headers = ()) returns Response|ClientError;

# The GET remote function implementation of the Failover Connector.
remote function get(string path, map<string|string[]> headers = (), TargetType targetType = <>) returns targetType|ClientError;

# The OPTIONS remote function implementation of the Failover Connector.
remote function options(string path, map<string|string[]> headers = (), TargetType targetType = <>) returns targetType|ClientError;

# Invokes an HTTP call with the specified HTTP method.
remote function execute(string httpVerb, string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# Invokes an HTTP call using the incoming request's HTTP method.
remote function forward(string path, Request request, TargetType targetType = <>) returns targetType|ClientError;

# Submits an HTTP request to a service with the specified HTTP verb. The `FailoverClient.submit()` function does not
# return an `http:Response` as the result. Rather it returns an `http:HttpFuture` which can be used for subsequent interactions
# with the HTTP endpoint.
remote function submit(string httpVerb, string path, RequestMessage message) returns HttpFuture|ClientError;

# Retrieves the `http:Response` for a previously-submitted request.
remote function getResponse(HttpFuture httpFuture) returns Response|ClientError;

# Checks whether an `http:PushPromise` exists for a previously-submitted request.
remote function hasPromise(HttpFuture httpFuture) returns boolean;

# Retrieves the next available `http:PushPromise` for a previously-submitted request.
remote function getNextPromise(HttpFuture httpFuture) returns PushPromise|ClientError;

# Retrieves the promised server push `http:Response` message.
remote function getPromisedResponse(PushPromise promise) returns Response|ClientError;

# Rejects an `http:PushPromise`. When an `http:PushPromise` is rejected, there is no chance of fetching a promised
# response using the rejected promise.
remote function rejectPromise(PushPromise promise) returns nil;
```

### Normal functions — 1, call with `.`

```ballerina
# Gets the index of the `TargetService[]` array which given a successful response.
function getSucceededEndpointIndex() returns int;
```

### Resource functions — 7, call with `->` and a path

```ballerina
# The POST resource function implementation of the Failover Connector.
resource function post [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The PUT resource function implementation of the Failover Connector.
resource function put [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The PATCH resource function implementation of the Failover Connector.
resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The DELETE resource function implementation of the Failover Connector.
resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The HEAD resource function implementation of the Failover Connector.
resource function head [PathParamType ...path](map<string|string[]> headers = (), QueryParams params) returns Response|ClientError;

# The GET resource function implementation of the Failover Connector.
resource function get [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The OPTIONS resource function implementation of the Failover Connector.
resource function options [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;
```

## Client `ListenerLdapUserStoreBasicAuthHandler`

Defines the LDAP store Basic Auth handler for listener authentication.

### Constructor

```ballerina
function init(LdapUserStoreConfig config) returns nil;
```

### Remote functions — 2, call with `->`

```ballerina
# Authenticates with the relevant authentication requirements.
remote function authenticate(Request|Headers|string data) returns auth:UserDetails|Unauthorized; // Special Agent Note: UserDetails FROM ballerina/auth package

# Authorizes with the relevant authorization requirements.
remote function authorize(auth:UserDetails userDetails, string|string[] expectedScopes) returns Forbidden?; // Special Agent Note: UserDetails FROM ballerina/auth package
```

## Client `ListenerOAuth2Handler`

Defines the OAuth2 handler for listener authentication.

### Constructor

```ballerina
function init(OAuth2IntrospectionConfig config) returns nil;
```

### Remote functions — 1, call with `->`

```ballerina
# Authorizes with the relevant authentication & authorization requirements.
remote function authorize(Request|Headers|string data, string|string[]? expectedScopes = (), map<string> optionalParams = ()) returns oauth2:IntrospectionResponse|Unauthorized|Forbidden; // Special Agent Note: IntrospectionResponse FROM ballerina/oauth2 package
```

## Client `LoadBalanceClient`

LoadBalanceClient endpoint provides load balancing functionality over multiple HTTP clients.

### Constructor

```ballerina
function init(LoadBalanceClientConfiguration loadBalanceClientConfig) returns ClientError?;
```

### Remote functions — 15, call with `->`

```ballerina
# The POST remote function implementation of the LoadBalancer Connector.
remote function post(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# The PUT remote function implementation of the Load Balance Connector.
remote function put(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# The PATCH remote function implementation of the LoadBalancer Connector.
remote function patch(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# The DELETE remote function implementation of the LoadBalancer Connector.
remote function delete(string path, RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# The HEAD remote function implementation of the LoadBalancer Connector.
remote function head(string path, map<string|string[]> headers = ()) returns Response|ClientError;

# The GET remote function implementation of the LoadBalancer Connector.
remote function get(string path, map<string|string[]> headers = (), TargetType targetType = <>) returns targetType|ClientError;

# The OPTIONS remote function implementation of the LoadBalancer Connector.
remote function options(string path, map<string|string[]> headers = (), TargetType targetType = <>) returns targetType|ClientError;

# The EXECUTE remote function implementation of the LoadBalancer Connector.
remote function execute(string httpVerb, string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

# The FORWARD remote function implementation of the LoadBalancer Connector.
remote function forward(string path, Request request, TargetType targetType = <>) returns targetType|ClientError;

# The submit implementation of the LoadBalancer Connector.
remote function submit(string httpVerb, string path, RequestMessage message) returns HttpFuture|ClientError;

# The getResponse implementation of the LoadBalancer Connector.
remote function getResponse(HttpFuture httpFuture) returns Response|ClientError;

# The hasPromise implementation of the LoadBalancer Connector.
remote function hasPromise(HttpFuture httpFuture) returns boolean;

# The getNextPromise implementation of the LoadBalancer Connector.
remote function getNextPromise(HttpFuture httpFuture) returns PushPromise|ClientError;

# The getPromisedResponse implementation of the LoadBalancer Connector.
remote function getPromisedResponse(PushPromise promise) returns Response|ClientError;

# The rejectPromise implementation of the LoadBalancer Connector.
remote function rejectPromise(PushPromise promise) returns nil;
```

### Resource functions — 7, call with `->` and a path

```ballerina
# The POST resource function implementation of the LoadBalancer Connector.
resource function post [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The PUT resource function implementation of the LoadBalancer Connector.
resource function put [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The PATCH resource function implementation of the LoadBalancer Connector.
resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The DELETE resource function implementation of the LoadBalancer Connector.
resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The HEAD resource function implementation of the LoadBalancer Connector.
resource function head [PathParamType ...path](map<string|string[]> headers = (), QueryParams params) returns Response|ClientError;

# The GET resource function implementation of the LoadBalancer Connector.
resource function get [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

# The OPTIONS resource function implementation of the LoadBalancer Connector.
resource function options [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;
```

## Client `StatusCodeClient`

The HTTP status code client provides the capability for initiating contact with a remote HTTP service. The API it

### Constructor

```ballerina
function init(string url, ClientConfiguration config) returns ClientError?;
```

### Remote functions — 15, call with `->`

```ballerina
# The `Client.post()` function can be used to send HTTP POST requests to HTTP endpoints.
remote function post(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

# The `Client.put()` function can be used to send HTTP PUT requests to HTTP endpoints.
remote function put(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

# The `Client.patch()` function can be used to send HTTP PATCH requests to HTTP endpoints.
remote function patch(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

# The `Client.delete()` function can be used to send HTTP DELETE requests to HTTP endpoints.
remote function delete(string path, RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

# The `Client.head()` function can be used to send HTTP HEAD requests to HTTP endpoints.
remote function head(string path, map<string|string[]> headers = ()) returns Response|ClientError;

# The `Client.get()` function can be used to send HTTP GET requests to HTTP endpoints.
remote function get(string path, map<string|string[]> headers = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

# The `Client.options()` function can be used to send HTTP OPTIONS requests to HTTP endpoints.
remote function options(string path, map<string|string[]> headers = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

# Invokes an HTTP call with the specified HTTP verb.
remote function execute(string httpVerb, string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

# The `Client.forward()` function can be used to invoke an HTTP call with inbound request's HTTP verb
remote function forward(string path, Request request, typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

# Submits an HTTP request to a service with the specified HTTP verb.
# The `Client->submit()` function does not give out a `http:Response` as the result.
# Rather it returns an `http:HttpFuture` which can be used to do further interactions with the endpoint.
remote function submit(string httpVerb, string path, RequestMessage message) returns HttpFuture|ClientError;

# This just pass the request to actual network call.
remote function getResponse(HttpFuture httpFuture) returns Response|ClientError;

# This just pass the request to actual network call.
remote function hasPromise(HttpFuture httpFuture) returns boolean;

# This just pass the request to actual network call.
remote function getNextPromise(HttpFuture httpFuture) returns PushPromise|ClientError;

# Passes the request to an actual network call.
remote function getPromisedResponse(PushPromise promise) returns Response|ClientError;

# This just pass the request to actual network call.
remote function rejectPromise(PushPromise promise) returns nil;
```

### Normal functions — 4, call with `.`

```ballerina
# Retrieves the cookie store of the client.
function getCookieStore() returns CookieStore?;

# The circuit breaker client related method to force the circuit into a closed state in which it will allow
# requests regardless of the error percentage until the failure threshold exceeds.
function circuitBreakerForceClose() returns nil;

# The circuit breaker client related method to force the circuit into a open state in which it will suspend all
# requests until `resetTime` interval exceeds.
function circuitBreakerForceOpen() returns nil;

# The circuit breaker client related method to provides the `http:CircuitState` of the circuit breaker.
function getCircuitBreakerCurrentState() returns CircuitState;
```

### Resource functions — 7, call with `->` and a path

```ballerina
# The client resource function to send HTTP POST requests to HTTP endpoints.
resource function post [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

# The client resource function to send HTTP PUT requests to HTTP endpoints.
resource function put [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

# The client resource function to send HTTP PATCH requests to HTTP endpoints.
resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

# The client resource function to send HTTP DELETE requests to HTTP endpoints.
resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

# The client resource function to send HTTP HEAD requests to HTTP endpoints.
resource function head [PathParamType ...path](map<string|string[]> headers = (), QueryParams params) returns Response|ClientError;

# The client resource function to send HTTP GET requests to HTTP endpoints.
resource function get [PathParamType ...path](map<string|string[]> headers = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

# The client resource function to send HTTP OPTIONS requests to HTTP endpoints.
resource function options [PathParamType ...path](map<string|string[]> headers = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;
```

## Module-level functions — 7, call with `.`

```ballerina
# Uses for declarative auth design, where the authentication/authorization decision is taken
# by reading the auth annotations provided in service/resource and the `Authorization` header of request.
# + serviceRef - The service reference where the resource locates
# + methodName - The name of the subjected resource
# + resourcePath - The relative path
function authenticateResource(Service serviceRef, string methodName, string[] resourcePath) returns nil;

# Creates an HTTP client capable of caching HTTP responses.
# + url - The URL of the HTTP endpoint to connect
# + config - The configurations for the client endpoint associated with the caching client
# + cacheConfig - The configurations for the HTTP cache to be used with the caching client
# + return - An `http:HttpCachingClient` instance, which wraps the base `http:Client` with a caching layer 
or else an `http:ClientError`
function createHttpCachingClient(string url, ClientConfiguration config, CacheConfig cacheConfig) returns record {}|ClientError;

# Creates an HTTP client capable of securing HTTP requests with authentication.
# + url - Base URL
# + config - Client endpoint configurations
# + return - Created secure HTTP client
function createHttpSecureClient(string url, ClientConfiguration config) returns record {}|ClientError;

# Returns the default HTTP listener. If the default listener is not already created, a new
# listener will be created with the port and configuration. An error will be returned if
# the listener creation fails.
# 
# The default listener configuration can be changed in the `Config.toml` file. Example:
# ```toml
# [ballerina.http]
# defaultListenerPort = 8080
# 
# [ballerina.http.defaultListenerConfig]
# httpVersion = "1.1"
# 
# [ballerina.http.defaultListenerConfig.secureSocket.key]
# path = "resources/certs/key.pem"
# password = "password"
# ```
# + return - The default HTTP listener or an error if the listener creation fails.
function getDefaultListener() returns Listener|ListenerError;

# Converts the headers represented as a map of `anydata` to a map of `string` or `string` array. The `value:toString`
# method will be used to convert the values to `string`. Additionally if the header name is specified by the
# `http:Header` annotation, then it will be used as the header name.
# ```ballerina
# type Headers record {
#   @http:Header {name: "X-API-VERSION"}
#   string apiVersion;
#   int id;
# };
# 
# Headers headers = {apiVersion: "v1", id: 1};
# map<string|string[]> headersMap = http:getHeaderMap(headers); // { "X-API-VERSION": "v1", "id": "1" }
# ```
# + headers - The headers represented as a map of anydata
# + return - A map of string or string array representing the headers
function getHeaderMap(map<anydata> headers) returns map<string|string[]>;

# If the query name is specified by the `http:Query` annotation, then this function will return the queries map
# with the specified query name. Otherwise, it will return the map as it is.
# ```ballerina
# type Queries record {
#   @http:Query {name: "filter_ids"}
#   string[] ids;
# };
# 
# Queries queries = {ids: ["1", "2"]};
# map<anydata> queriesMap = http:getQueryMap(queries); // { "filter_ids": ["1", "2"] }
# ```
# + queries - The queries represented as a map of anydata
# + return - The queries map with names specified by the `http:Query` annotation
function getQueryMap(map<anydata> queries) returns map<anydata>;

# Parses the header value which contains multiple values or parameters.
# ```ballerina
#  http:HeaderValue[] values = check http:parseHeader("text/plain;level=1;q=0.6, application/xml;level=2");
# ```
# + headerValue - The header value
# + return - An array of `http:HeaderValue` typed record containing the value and its parameter map
or else an `http:ClientError` if the header parsing fails
function parseHeader(string headerValue) returns HeaderValue[]|ClientError;
```

## Errors — 56

The subtype chain is what `is` tests against, so `e is <Name>` works off these lines directly.

```ballerina
# Represents a client error that occurred due to all the load balance endpoint failure.
type AllLoadBalanceEndpointsFailedError distinct ResiliencyError;

# Represents a client error that occurred due to all the the retry attempts failure.
type AllRetryAttemptsFailed distinct ResiliencyError;

# Represents both 4XX and 5XX application response client error.
type ApplicationResponseError distinct (ClientError&error<Detail>);

# Represents a client error that occurred due to circuit breaker configuration error.
type CircuitBreakerConfigError distinct ResiliencyError;

# Defines the Auth error types that returned from client.
type ClientAuthError distinct ClientError;

# Represents a client connector error that occurred.
type ClientConnectorError distinct ClientError;

# Defines the possible client error types.
type ClientError distinct Error;

# Represents an error, which occurred due to bad syntax or incomplete info in the client request(4xx HTTP response).
type ClientRequestError distinct (ApplicationResponseError&error<Detail>);

# Represents a cookie error that occurred when using the cookies.
type CookieHandlingError distinct GenericClientError;

# Defines the common error type for the module.
type Error distinct error;

# Represents a client error that occurred due to failover action failure.
type FailoverActionFailedError distinct ResiliencyError;

# Represents a client error that occurred due to all the failover endpoint failure.
type FailoverAllEndpointsFailedError distinct ResiliencyError;

# Represents a generic client error.
type GenericClientError distinct ClientError;

# Represents a generic listener error.
type GenericListenerError distinct ListenerError;

# Represents an error, which occurred due to header binding.
type HeaderBindingError distinct Error;

# Represents a header not found error when retrieving headers.
type HeaderNotFoundError distinct Error;

# Represents an error, which occurred due to a header constraint validation.
type HeaderValidationError distinct HeaderBindingError;

# Represents an HTTP/2 client generic error.
type Http2ClientError distinct ClientError;

# Represents the error that triggered upon a request/response idle timeout.
type IdleTimeoutError distinct ResiliencyError;

# Defines the listener error types that returned while receiving inbound request.
type InboundRequestError distinct ListenerError;

# Defines the client error types that returned while receiving inbound response.
type InboundResponseError distinct ClientError;

# Represents a listener error that occurred due to inbound request initialization failure.
type InitializingInboundRequestError distinct InboundRequestError;

# Represents a client error that occurred due to inbound response initialization failure.
type InitializingInboundResponseError distinct InboundResponseError;

# Represents a client error that occurred due to outbound request initialization failure.
type InitializingOutboundRequestError distinct OutboundRequestError;

# Represents a listener error that occurred due to outbound response initialization failure.
type InitializingOutboundResponseError distinct OutboundResponseError;

# Represents an error that occurred due to 100 continue response initialization failure.
type Initiating100ContinueResponseError distinct OutboundResponseError;

# Represents a cookie error that occurred when sending cookies in the response.
type InvalidCookieError distinct OutboundResponseError;

# Defines the auth error types that returned from listener.
type ListenerAuthError distinct ListenerError;

# Defines the possible listener error types.
type ListenerError distinct Error;

# Represents a client error that occurred exceeding maximum wait time.
type MaximumWaitTimeExceededError distinct GenericClientError;

# Represents an error, which occurred due to media-type binding.
type MediaTypeBindingError distinct Error;

# Represents an error, which occurred due to media type validation.
type MediaTypeValidationError distinct MediaTypeBindingError;

# Represents an error, which occurred due to the absence of the payload.
type NoContentError distinct ClientError;

# Defines the client error types that returned while sending outbound request.
type OutboundRequestError distinct ClientError;

# Defines the listener error types that returned while sending outbound response.
type OutboundResponseError distinct ListenerError;

# Represents an error, which occurred due to payload binding.
type PayloadBindingError distinct Error;

# Represents an error, which occurred due to payload constraint validation.
type PayloadValidationError distinct PayloadBindingError;

# Represents an error, which occurred due to a query parameter constraint validation.
type QueryParameterValidationError distinct QueryParameterBindingError;

# Represents a listener error that occurred while writing the inbound request entity body.
type ReadingInboundRequestBodyError distinct InboundRequestError;

# Represents a listener error that occurred while reading inbound request headers.
type ReadingInboundRequestHeadersError distinct InboundRequestError;

# Represents a client error that occurred while reading inbound response entity body.
type ReadingInboundResponseBodyError distinct InboundResponseError;

# Represents a client error that occurred while reading inbound response headers.
type ReadingInboundResponseHeadersError distinct InboundResponseError;

# Represents an error, which occurred due to a failure of the remote server(5xx HTTP response).
type RemoteServerError distinct (ApplicationResponseError&error<Detail>);

# Represents an error, which occurred during the request dispatching.
type RequestDispatchingError distinct ListenerError;

# Defines the resiliency error types that returned from client.
type ResiliencyError distinct ClientError;

# Represents an error, which occurred during the resource dispatching.
type ResourceDispatchingError distinct RequestDispatchingError;

# Represents an error, which occurred during the service dispatching.
type ServiceDispatchingError distinct RequestDispatchingError;

# Represents a client error that occurred due to SSL failure.
type SslError distinct ClientError;

# Represents the client status code response data binding error
type StatusCodeResponseDataBindingError MediaTypeBindingStatusCodeClientError|PayloadBindingStatusCodeClientError|HeaderBindingStatusCodeClientError;

# Represents a client error that occurred due to unsupported action invocation.
type UnsupportedActionError distinct GenericClientError;

# Represents a client error that occurred due to upstream service unavailability.
type UpstreamServiceUnavailableError distinct ResiliencyError;

# Represents an error that occurred while writing 100 continue response.
type Writing100ContinueResponseError distinct OutboundResponseError;

# Represents a client error that occurred while writing outbound request entity body.
type WritingOutboundRequestBodyError distinct OutboundRequestError;

# Represents a client error that occurred while writing outbound request headers.
type WritingOutboundRequestHeadersError distinct OutboundRequestError;

# Represents a listener error that occurred while writing outbound response entity body.
type WritingOutboundResponseBodyError distinct OutboundResponseError;

# Represents a listener error that occurred while writing outbound response headers.
type WritingOutboundResponseHeadersError distinct OutboundResponseError;
```

## Guide

*The package's own readme, verbatim, with its headings demoted two levels.*

#### Overview

This module provides APIs for connecting and interacting with HTTP and HTTP2 endpoints. It facilitates two types of network entry points as the `Client` and `Listener`.

##### Client

The `Client` is used to connect to and interact with HTTP endpoints. They support connection pooling and can be 
configured to have a maximum number of active connections that can be made with the remote endpoint. The `Client` 
activates connection eviction after a given idle period and also supports follow-redirects so that you do not 
have to manually handle 3xx HTTP status codes.

###### Resiliency

The `Client` handles resilience in multiple ways such as load balancing, circuit breaking, endpoint timeouts, and via a 
retry mechanism.

Load balancing is used in the round-robin or failover manner.

When a failure occurs in the remote service, the client connections might wait for some time before a timeout occurs. 
Awaiting requests consume resources in the system. Circuit Breakers are used to trip after a certain number of failed 
requests to the remote service. Once a circuit breaker trips, it does not allow the client to send requests to the 
remote service for a period of time.

The Ballerina circuit breaker supports tripping on HTTP error status codes and I/O errors. Failure thresholds can be 
configured based on a sliding window (e.g., 5 failures within 10 seconds). The `Client` also supports a retry 
mechanism that allows it to resend failed requests periodically for a given number of times.

###### Security

The `Client` supports Server Name Indication (SNI), Certificate Revocation List (CRL), Online Certificate Status 
Protocol (OCSP), and OCSP Stapling for SSL/TLS connections.
Also, the `Client` can be configured to send authentication information to the endpoint being invoked. Ballerina has 
built-in support for Basic authentication, JWT authentication, and OAuth2 authentication.

In addition to that, it supports both HTTP/1.1 and HTTP2 protocols and connection keep-alive, content 
chunking, HTTP caching, data compression/decompression, response payload binding, and authorization can be highlighted as the features of the `Clients`.

A `Client` can be defined using the URL of the remote service that it needs to connect with as shown below:

```ballerina
http:Client clientEndpoint = check new("https://my-simple-backend.com");
```
The defined `Client` endpoint can be used to call a remote service as follows:

```ballerina
// Send a GET request to the specified endpoint.
http:Response response = check clientEndpoint->get("/get?id=123");
```
The payload can be retrieved as the return value from the remote function as follows:

```ballerina
// Retrieve payload as json.
json payload = check clientEndpoint->post("/backend/Json", "foo");
```

##### Listener

The `Listener` is the underneath server connector that binds the given IP/Port to the network and it's behavior can 
be changed using the `http:ListenerConfiguration`. In HTTP, the `http:Service`-typed services can be attached to 
the `Listener`. The service type precisely describes the syntax for both the service and resource.

A `Service` represents a collection of network-accessible entry points and can be exposed via a `Listener` endpoint. 
A resource represents one such entry point and can have its own path, HTTP methods, body format, `consumes` and 
`produces` content types, CORS headers, etc. In resources, the HTTP method and resource path are mandatory parameters and
the String literal and path parameters can be stated as the path. The resource function accepts the `http:Caller`, `http:Request`, 
`http:Headers`, query parameters, header parameters, and payload parameters as arguments. However, they are optional.

When a `Service` receives a request, it is dispatched to the best-matched resource.

A `Listener` endpoint can be defined as follows:

```ballerina
// Attributes associated with the `Listener` endpoint are defined here.
listener http:Listener helloWorldEP = new(9090);
```

Then a `Service` can be defined and attached to the above `Listener` endpoint as shown below:

```ballerina
// By default, Ballerina assumes that the service is to be exposed via HTTP/1.1.
service /helloWorld on helloWorldEP {

   resource function post [string name](@http:Payload string message) returns string {
       // Sends the response back to the client along with a string payload.
       return "Hello, World! I’m " + name + ". " + message;
   }
}
```

###### Security

`Listener` endpoints can be exposed via SSL. They support Mutual SSL, Hostname Verification, and Application Layer 
Protocol Negotiation (ALPN) for HTTP2. `Listener` endpoints also support Certificate Revocation List (CRL), Online 
Certificate Status Protocol (OCSP), and OCSP Stapling.
Also, The `listener` can be configured to authenticate and authorize the inbound requests. Ballerina has 
built-in support for basic authentication, JWT authentication, and OAuth2 authentication.

In addition to that, supports both the HTTP/1.1 and HTTP2 protocols and connection keep-alive, content 
chunking, HTTP caching, data compression/decompression, payload binding, and authorization can be highlighted as the features of a `Service`.

###### HTTP/2 Stream Concurrency

Each HTTP/2 connection can carry multiple requests simultaneously using independent streams. To prevent a single
connection from opening an unbounded number of streams and exhausting server memory, the listener enforces a limit
of **100 concurrent streams per connection** by default. This is the value recommended by RFC 7540 and consistent
with other widely-used HTTP/2 server implementations such as Nginx and Envoy.

When a client reaches this limit on a connection, it will automatically open an additional connection rather than
stalling, so normal traffic is unaffected for typical workloads.
