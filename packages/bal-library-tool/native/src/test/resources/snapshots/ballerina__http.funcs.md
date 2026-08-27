<!-- bal library funcs v1 -->
# Module functions — ballerina/http

| | |
|---|---|
| Showing | 7 signatures |

## Next

- one call and every type it needs: `bal library funcs ballerina/http authenticateResource -r`

## Module-level functions — 7, call with `.`

```ballerina
# Uses for declarative auth design, where the authentication/authorization decision is taken
# by reading the auth annotations provided in service/resource and the `Authorization` header of request.
# + serviceRef - The service reference where the resource locates
# + methodName - The name of the subjected resource
# + resourcePath - The relative path
public isolated function authenticateResource(Service serviceRef, string methodName, string[] resourcePath);

# Creates an HTTP client capable of caching HTTP responses.
# + url - The URL of the HTTP endpoint to connect
# + config - The configurations for the client endpoint associated with the caching client
# + cacheConfig - The configurations for the HTTP cache to be used with the caching client
# + return - An `http:HttpCachingClient` instance, which wraps the base `http:Client` with a caching layer 
# or else an `http:ClientError`
public isolated function createHttpCachingClient(string url, ClientConfiguration config, CacheConfig cacheConfig) returns object {}|ClientError;

# Creates an HTTP client capable of securing HTTP requests with authentication.
# + url - Base URL
# + config - Client endpoint configurations
# + return - Created secure HTTP client
public isolated function createHttpSecureClient(string url, ClientConfiguration config) returns object {}|ClientError;

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
public isolated function getDefaultListener() returns Listener|ListenerError;

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
public isolated function getHeaderMap(map<anydata> headers) returns map<string|string[]>;

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
public isolated function getQueryMap(map<anydata> queries) returns map<anydata>;

# Parses the header value which contains multiple values or parameters.
# ```ballerina
#  http:HeaderValue[] values = check http:parseHeader("text/plain;level=1;q=0.6, application/xml;level=2");
# ```
# + headerValue - The header value
# + return - An array of `http:HeaderValue` typed record containing the value and its parameter map
# or else an `http:ClientError` if the header parsing fails
public isolated function parseHeader(string headerValue) returns HeaderValue[]|ClientError;
```
