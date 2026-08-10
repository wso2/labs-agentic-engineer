// ============================================================
// Library: ballerina/http
// This module provides APIs for connecting and interacting with HTTP and HTTP2 endpoints. It facilitates two types of network entry points as the `Client` and `Listener`.
// ============================================================
import ballerina/http;

// --- Types ---

# The status code response record of `Accepted`.

type Accepted record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusAccepted status = STATUS_ACCEPTED_OBJ;
};

# Represents HTTP access log configuration.

type AccessLogConfiguration record {
    # Enable or disable console access logs
    boolean console = false;
    # The format of access logs to be printed (either `flat` or `json`)
    string format = "flat";
    # The list of attributes of access logs to be printed
    string[] attributes?;
    # File path to store access logs
    string path?;
    # Log file configuration to store access logs
    LogFileConfig file?;
};

# The status code response record of `AlreadyReported`.

type AlreadyReported record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusAlreadyReported status = STATUS_ALREADY_REPORTED_OBJ;
};

# The status code response record of `BadGateway`.

type BadGateway record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusBadGateway status = STATUS_BAD_GATEWAY_OBJ;
};

# The status code response record of `BadRequest`.

type BadRequest record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusBadRequest status = STATUS_BAD_REQUEST_OBJ;
};

# Represents token for Bearer token authentication.

type BearerTokenConfig record {
    # Bearer token for authentication
    string token;
};

# Represents a discrete sub-part of the time window (Bucket).

type Bucket record {
    # Total number of requests received during the sub-window time frame
    int totalCount = 0;
    # Number of failed requests during the sub-window time frame
    int failureCount = 0;
    # Number of rejected requests during the sub-window time frame
    int rejectedCount = 0;
    # The time that the `Bucket` is last updated.
    time:Utc lastUpdatedTime?; // Special Agent Note: Utc FROM ballerina/time package
};

# Provides a set of configurations for controlling the caching behaviour of the endpoint.

type CacheConfig record {
    # Specifies whether HTTP caching is enabled. Caching is enabled by default.
    boolean enabled = true;
    # Specifies whether the HTTP caching layer should behave as a public cache or a private cache
    boolean isShared = false;
    # The capacity of the cache
    int capacity = 16;
    # The fraction of entries to be removed when the cache is full. The value should be
between 0 (exclusive) and 1 (inclusive).
    float evictionFactor = 0.2;
    # Gives the user some control over the caching behaviour. By default, this is set to
`CACHE_CONTROL_AND_VALIDATORS`. The default behaviour is to allow caching only when the `cache-control`
header and either the `etag` or `last-modified` header are present.
    CachingPolicy policy = CACHE_CONTROL_AND_VALIDATORS;
};

# Represents combination of certificate, private key and private key password if encrypted.

type CertKey record {
    # A file containing the certificate
    string certFile;
    # A file containing the private key in PKCS8 format
    string keyFile;
    # Password of the private key if it is encrypted
    string keyPassword?;
};

# Provides a set of configurations for controlling the behaviour of the Circuit Breaker.

type CircuitBreakerConfig record {
    # The `http:RollingWindow` options of the `CircuitBreaker`
    RollingWindow rollingWindow = {};
    # The threshold for request failures. When this threshold exceeds, the circuit trips. The threshold should be a
value between 0 and 1
    float failureThreshold = 0.0;
    # The time period (in seconds) to wait before attempting to make another request to the upstream service
    decimal resetTime = 0;
    # Array of HTTP response status codes which are considered as failures
    int[] statusCodes = [];
};

# Derived set of configurations from the `CircuitBreakerConfig`.

type CircuitBreakerInferredConfig record {
    # The threshold for request failures. When this threshold exceeds, the circuit trips.
The threshold should be a value between 0 and 1
    float failureThreshold = 0.0;
    # The time period (in seconds) to wait before attempting to make another request to
the upstream service
    decimal resetTime = 0;
    # Array of HTTP response status codes which are considered as failures
    int[] statusCodes = [];
    # Number of buckets derived from the `RollingWindow`
    int noOfBuckets = 0;
    # The `http:RollingWindow` options provided in the `http:CircuitBreakerConfig`
    RollingWindow rollingWindow = {};
};

# Maintains the health of the Circuit Breaker.

type CircuitHealth record {
    # Whether last request is success or not
    boolean lastRequestSuccess = false;
    # Total request count received within the `RollingWindow`
    int totalRequestCount = 0;
    # ID of the last bucket used in Circuit Breaker calculations
    int lastUsedBucketId = 0;
    # Circuit Breaker start time
    time:Utc startTime = time:utcNow(); // Special Agent Note: Utc FROM ballerina/time package
    # The time that the last request received
    time:Utc lastRequestTime?; // Special Agent Note: Utc FROM ballerina/time package
    # The time that the last error occurred
    time:Utc lastErrorTime?; // Special Agent Note: Utc FROM ballerina/time package
    # The time that circuit forcefully opened at last
    time:Utc lastForcedOpenTime?; // Special Agent Note: Utc FROM ballerina/time package
    # The discrete time buckets into which the time window is divided
    Bucket?[] totalBuckets = [];
};

# Provides a set of configurations for controlling the behaviours when communicating with a remote HTTP endpoint.
# The following fields are inherited from the other configuration records in addition to the `Client`-specific
# configs.

type ClientConfiguration record {
    HttpVersion httpVersion;
    ClientHttp1Settings http1Settings;
    ClientHttp2Settings http2Settings;
    decimal timeout;
    string forwarded;
    FollowRedirects|() followRedirects;
    PoolConfiguration|() poolConfig;
    CacheConfig cache;
    Compression compression;
    CredentialsConfig|BearerTokenConfig|JwtIssuerConfig|OAuth2ClientCredentialsGrantConfig|OAuth2PasswordGrantConfig|OAuth2RefreshTokenGrantConfig|OAuth2JwtBearerGrantConfig|() auth;
    CircuitBreakerConfig|() circuitBreaker;
    RetryConfig|() retryConfig;
    CookieConfig|() cookieConfig;
    ResponseLimitConfigs responseLimits;
    ProxyConfig|() proxy;
    boolean validation;
    ClientSocketConfig socketConfig;
    boolean laxDataBinding;
    # SSL/TLS security settings for HTTPS connections
    ClientSecureSocket? secureSocket = ();
};

# Provides settings related to HTTP/1.x protocol.

type ClientHttp1Settings record {
    # Specifies whether to reuse a connection for multiple requests
    KeepAlive keepAlive = KEEPALIVE_AUTO;
    # The chunking behaviour of the request
    Chunking chunking = CHUNKING_AUTO;
    # Proxy server related options
    ProxyConfig? proxy = ();
};

# Provides settings related to HTTP/2 protocol.

type ClientHttp2Settings record {
    # Configuration to enable HTTP/2 prior knowledge
    boolean http2PriorKnowledge = false;
    # Configuration to change the initial window size
    int http2InitialWindowSize = 65535;
};

# Provides configurations for facilitating secure communication with a remote HTTP endpoint.

type ClientSecureSocket record {
    # Enable SSL validation
    boolean enable = true;
    # Configurations associated with `crypto:TrustStore` or single certificate file that the client trusts
    crypto:TrustStore|string cert?; // Special Agent Note: TrustStore FROM ballerina/crypto package
    # Configurations associated with `crypto:KeyStore` or combination of certificate and private key of the client
    crypto:KeyStore|CertKey key?; // Special Agent Note: KeyStore FROM ballerina/crypto package
    # SSL/TLS protocol related options
    record {Protocol name; string[] versions; } protocol?;
    # Certificate validation against OCSP_CRL, OCSP_STAPLING related options
    record {CertValidationType 'type; int cacheSize; int cacheValidityPeriod; } certValidation?;
    # List of ciphers to be used
eg: TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA
    string[] ciphers?;
    # Enable/disable host name verification
    boolean verifyHostName = true;
    # Enable/disable new SSL session creation
    boolean shareSession = true;
    # SSL handshake time out
    decimal handshakeTimeout?;
    # SSL session time out
    decimal sessionTimeout?;
    # Server name indication(SNI) to be used. If this is not present, hostname from the target URL will be used
    string serverName?;
};

# Provides settings related to client socket configuration.

type ClientSocketConfig record {
    # Connect timeout of the channel in seconds. If the Channel does not support connect operation,
this property is not used at all, and therefore will be ignored.
    decimal connectTimeOut = 15;
    # Sets the SO_RCVBUF option to the specified value for this Socket.
    int receiveBufferSize = 1048576;
    # Sets the SO_SNDBUF option to the specified value for this Socket.
    int sendBufferSize = 1048576;
    # Enable/disable TCP_NODELAY (disable/enable Nagle's algorithm).
    boolean tcpNoDelay = true;
    # Enable/disable the SO_REUSEADDR socket option.
    boolean socketReuse = true;
    # Enable/disable SO_KEEPALIVE.
    boolean keepAlive = false;
};

# Common client configurations for the next level clients.

type CommonClientConfiguration record {
    # HTTP protocol version supported by the client
    HttpVersion httpVersion = HTTP_2_0;
    # HTTP/1.x specific settings
    ClientHttp1Settings http1Settings = {};
    # HTTP/2 specific settings
    ClientHttp2Settings http2Settings = {};
    # Maximum time(in seconds) to wait for a response before the request times out
    decimal timeout = 30;
    # The choice of setting `Forwarded`/`X-Forwarded-For` header, when acting as a proxy
    string forwarded = "disable";
    # HTTP redirect handling configurations (with 3xx status codes)
    FollowRedirects? followRedirects = ();
    # Configurations associated with the request connection pool
    PoolConfiguration? poolConfig = ();
    # HTTP response caching related configurations
    CacheConfig cache = {};
    # Enable request/response compression (using `accept-encoding` header)
    Compression compression = COMPRESSION_AUTO;
    # Client authentication options (Basic, Bearer token, OAuth, etc.)
    ClientAuthConfig? auth = ();
    # Circuit breaker configurations to prevent cascading failures
    CircuitBreakerConfig? circuitBreaker = ();
    # Automatic retry settings for failed requests
    RetryConfig? retryConfig = ();
    # Cookie handling settings for session management
    CookieConfig? cookieConfig = ();
    # Limits for response size and headers (to prevent memory issues)
    ResponseLimitConfigs responseLimits = {};
    # Proxy server settings if requests need to go through a proxy
    ProxyConfig? proxy = ();
    # Enable automatic payload validation for request/response data against constraints
    boolean validation = true;
    # Low-level socket settings (timeouts, buffer sizes, etc.)
    ClientSocketConfig socketConfig = {};
    # Enable relaxed data binding on the client side.
When enabled:
- `null` values in JSON are allowed to be mapped to optional fields
- missing fields in JSON are allowed to be mapped as `null` values
    boolean laxDataBinding = false;
};

# The common attributed of response status code record type.

type CommonResponse record {
    # The value of response `Content-type` header
    string mediaType?;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers?;
    # The response payload
    anydata body?;
};

# A record for providing configurations for content compression.

type CompressionConfig record {
    # The status of compression
    Compression enable = COMPRESSION_AUTO;
    # Content types which are allowed for compression
    string[] contentTypes = [];
};

# The status code response record of `Conflict`.

type Conflict record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusConflict status = STATUS_CONFLICT_OBJ;
};

# The status code response record of `Continue`.

type Continue record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusContinue status = STATUS_CONTINUE_OBJ;
};

# Client configuration for cookies.

type CookieConfig record {
    # User agents provide users with a mechanism for disabling or enabling cookies
    boolean enabled = false;
    # Maximum number of cookies per domain, which is 50
    int maxCookiesPerDomain = 50;
    # Maximum number of total cookies allowed to be stored in cookie store, which is 3000
    int maxTotalCookieCount = 3000;
    # User can block cookies from third party responses and refuse to send cookies for third party requests, if needed
    boolean blockThirdPartyCookies = true;
    # To manage persistent cookies, users are provided with a mechanism for specifying a persistent cookie store with their own mechanism
which references the persistent cookie handler or specifying the CSV persistent cookie handler. If not specified any, only the session cookies are used
    PersistentCookieHandler persistentCookieHandler?;
};

# The options to be used when initializing the `http:Cookie`.

type CookieOptions record {
    # URI path to which the cookie belongs
    string path?;
    # Host to which the cookie will be sent
    string domain?;
    # Maximum lifetime of the cookie represented as the date and time at which the cookie expires
    string expires?;
    # Maximum lifetime of the cookie represented as the number of seconds until the cookie expires
    int maxAge = 0;
    # Cookie is sent only to HTTP requests
    boolean httpOnly = false;
    # Cookie is sent only to secure channels
    boolean secure = false;
    # At what time the cookie was created
    time:Utc createdTime = time:utcNow(); // Special Agent Note: Utc FROM ballerina/time package
    # Last-accessed time of the cookie
    time:Utc lastAccessedTime = time:utcNow(); // Special Agent Note: Utc FROM ballerina/time package
    # Cookie is sent only to the requested host
    boolean hostOnly = false;
};

# Configurations for CORS support.

type CorsConfig record {
    # The array of allowed headers by the service
    string[] allowHeaders = [];
    # The array of allowed methods by the service
    string[] allowMethods = [];
    # The array of origins with which the response is shared by the service
    string[] allowOrigins = [];
    # The allowlisted headers, which clients are allowed to access
    string[] exposeHeaders = [];
    # Specifies whether credentials are required to access the service
    boolean allowCredentials = false;
    # The maximum duration to cache the preflight from client side
    decimal maxAge = -1;
};

# The status code response record of `Created`.

type Created record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusCreated status = STATUS_CREATED_OBJ;
};

# Represents credentials for Basic Auth authentication.

type CredentialsConfig record {
    # Username for Basic Auth authentication
    string username;
    # Password for Basic Auth authentication
    string password;
};

# The default status code response record.

type DefaultStatusCodeResponse record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code object
    DefaultStatus status;
};

# Represents the details of an HTTP error.

type Detail record {
    # The inbound error response status code
    int statusCode;
    # The inbound error response headers
    map<string[]> headers;
    # The inbound error response body
    anydata body;
};

# The status code response record of `EarlyHints`.

type EarlyHints record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusEarlyHints status = STATUS_EARLY_HINTS_OBJ;
};

# Represents the structure of the HTTP error payload.

type ErrorPayload record {
    # Timestamp of the error
    string timestamp;
    # Relevant HTTP status code
    int status;
    # Reason phrase
    string reason;
    # Error message
    string message;
    # Request path
    string path;
    # Method type of the request
    string method;
};

# The status code response record of `ExpectationFailed`.

type ExpectationFailed record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusExpectationFailed status = STATUS_EXPECTATION_FAILED_OBJ;
};

# The status code response record of `FailedDependency`.

type FailedDependency record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusFailedDependency status = STATUS_FAILED_DEPENDENCY_OBJ;
};

# Provides a set of HTTP related configurations and failover related configurations.
# The following fields are inherited from the other configuration records in addition to the failover client-specific
# configs.

type FailoverClientConfiguration record {
    HttpVersion httpVersion;
    ClientHttp1Settings http1Settings;
    ClientHttp2Settings http2Settings;
    decimal timeout;
    string forwarded;
    FollowRedirects|() followRedirects;
    PoolConfiguration|() poolConfig;
    CacheConfig cache;
    Compression compression;
    CredentialsConfig|BearerTokenConfig|JwtIssuerConfig|OAuth2ClientCredentialsGrantConfig|OAuth2PasswordGrantConfig|OAuth2RefreshTokenGrantConfig|OAuth2JwtBearerGrantConfig|() auth;
    CircuitBreakerConfig|() circuitBreaker;
    RetryConfig|() retryConfig;
    CookieConfig|() cookieConfig;
    ResponseLimitConfigs responseLimits;
    ProxyConfig|() proxy;
    boolean validation;
    ClientSocketConfig socketConfig;
    boolean laxDataBinding;
    # The upstream HTTP endpoints among which the incoming HTTP traffic load should be sent on failover
    TargetService[] targets = [];
    # Array of HTTP response status codes for which the failover behaviour should be triggered
    int[] failoverCodes = [501, 502, 503, 504];
    # Failover delay interval in seconds
    decimal interval = 0;
};

# Represents file user store configurations for Basic Auth authentication.

type FileUserStoreConfig record {
};

# Represents the auth annotation for file user store configurations with scopes.

type FileUserStoreConfigWithScopes record {
    # File user store configurations for Basic Auth authentication
    FileUserStoreConfig fileUserStoreConfig;
    # Scopes allowed for authorization
    string|string[] scopes?;
};

# Provides configurations for controlling the endpoint's behaviour in response to HTTP redirect related responses.
# The response status codes of 301, 302, and 303 are redirected using a GET request while 300, 305, 307, and 308
# status codes use the original request HTTP method during redirection.

type FollowRedirects record {
    # Enable/disable redirection
    boolean enabled = false;
    # Maximum number of redirects to follow
    int maxCount = 5;
    # By default Authorization and Proxy-Authorization headers are removed from the redirect requests.
Set it to true if Auth headers are needed to be sent during the redirection
    boolean allowAuthHeaders = false;
};

# The status code response record of `Forbidden`.

type Forbidden record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusForbidden status = STATUS_FORBIDDEN_OBJ;
};

# The status code response record of `Found`.

type Found record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusFound status = STATUS_FOUND_OBJ;
};

# The status code response record of `GatewayTimeout`.

type GatewayTimeout record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusGatewayTimeout status = STATUS_GATEWAY_TIMEOUT_OBJ;
};

# The status code response record of `Gone`.

type Gone record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusGone status = STATUS_GONE_OBJ;
};

# Represents the parsed header value details

type HeaderValue record {
    # The header value
    string value;
    # Map of header parameters
    map<string> params;
};

# Defines the HTTP response cache configuration. By default the `no-cache` directive is setted to the `cache-control`
# header. In addition to that `etag` and `last-modified` headers are also added for cache validation.

type HttpCacheConfig record {
    # Sets the `must-revalidate` directive
    boolean mustRevalidate = true;
    # Sets the `no-cache` directive
    boolean noCache = false;
    # Sets the `no-store` directive
    boolean noStore = false;
    # Sets the `no-transform` directive
    boolean noTransform = false;
    # Sets the `private` and `public` directives
    boolean isPrivate = false;
    # Sets the `proxy-revalidate` directive
    boolean proxyRevalidate = false;
    # Sets the `max-age` directive. Default value is 3600 seconds
    decimal maxAge = 3600;
    # Sets the `s-maxage` directive
    decimal sMaxAge = -1;
    # Optional fields for the `no-cache` directive. Before sending a listed field in a response, it
must be validated with the origin server
    string[] noCacheFields = [];
    # Optional fields for the `private` directive. A cache can omit the fields specified and store
the rest of the response
    string[] privateFields = [];
    # Sets the `etag` header for the given payload
    boolean setETag = true;
    # Sets the current time as the `last-modified` header
    boolean setLastModified = true;
};

# Configures the typing details type of the Caller resource signature parameter.

type HttpCallerInfo record {
    # Specifies the type of response
    typedesc<ResponseMessage|StatusCodeResponse|Error> respondType?;
};

# Defines the Header resource signature parameter.

type HttpHeader record {
    # Specifies the name of the required header
    string name?;
};

# Defines the Payload resource signature parameter and return parameter.

type HttpPayload record {
    # Specifies the allowed media types of the corresponding payload type
    string|string[] mediaType?;
};

# Defines the query resource signature parameter.

type HttpQuery record {
    # Specifies the name of the query parameter
    string name?;
};

# Configuration for an HTTP resource.

type HttpResourceConfig record {
    # The name of the resource
    string name?;
    # The media types which are accepted by resource
    string[] consumes = [];
    # The media types which are produced by resource
    string[] produces = [];
    # The cross origin resource sharing configurations for the resource. If not set, the resource will inherit the CORS behaviour of the enclosing service.
    CorsConfig cors = {};
    # Allow to participate in the distributed transactions if value is true
    boolean transactionInfectable = true;
    # Resource auth configurations
    ListenerAuthConfig[]|Scopes auth?;
    # The array of linked resources
    LinkedTo[] linkedTo?;
};

# Contains the configurations for an HTTP service.

type HttpServiceConfig record {
    # Domain name of the service
    string host = "b7a.default";
    # The status of compression
    CompressionConfig compression = {};
    # Configures the chunking behaviour for the service
    Chunking chunking = CHUNKING_AUTO;
    # The cross origin resource sharing configurations for the service
    CorsConfig cors = {};
    # Service auth configurations
    ListenerAuthConfig[] auth?;
    # Service specific media-type subtype prefix
    string mediaTypeSubtypePrefix?;
    # Treat Nilable parameters as optional
    boolean treatNilableAsOptional = true;
    # The generated OpenAPI definition for the HTTP service. This is auto-generated at compile-time if OpenAPI doc auto generation is enabled
    byte[] openApiDefinition = [];
    # Enables the inbound payload validation functionality which provided by the constraint package. Enabled by default
    boolean validation = true;
    # The service object type which defines the service contract. This is auto-generated at compile-time
    typedesc<ServiceContract> serviceType?;
    # Base path to be used with the service implementation. This is only allowed on service contract types
    string basePath?;
    # Enables or disables relaxed data binding on the service side. Disabled by default. 
When enabled, the JSON data will be projected to the Ballerina record type and during the projection,
nil values will be considered as optional fields and absent fields will be considered for nilable types
    boolean laxDataBinding = false;
};

# The status code response record of `HttpVersionNotSupported`.

type HttpVersionNotSupported record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusHttpVersionNotSupported status = STATUS_HTTP_VERSION_NOT_SUPPORTED_OBJ;
};

# The status code response record of `IMUsed`.

type IMUsed record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusIMUsed status = STATUS_IM_USED_OBJ;
};

# Provides a set of cloneable configurations for HTTP listener.

type InferredListenerConfiguration record {
    # The host name/IP of the endpoint
    string host;
    # Configurations related to HTTP/1.x protocol
    ListenerHttp1Settings http1Settings;
    # The SSL configurations for the service endpoint. This needs to be configured in order to
communicate through HTTPS.
    ListenerSecureSocket? secureSocket;
    # Highest HTTP version supported by the endpoint
    HttpVersion httpVersion;
    # Period of time in seconds that a connection waits for a read/write operation. Use value 0 to
disable timeout
    decimal timeout;
    # The server name which should appear as a response header
    string? server;
    # Configurations associated with inbound request size limits
    RequestLimitConfigs requestLimits;
};

# The status code response record of `InsufficientStorage`.

type InsufficientStorage record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusInsufficientStorage status = STATUS_INSUFFICIENT_STORAGE_OBJ;
};

# The status code response record of `InternalServerError`.

type InternalServerError record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusInternalServerError status = STATUS_INTERNAL_SERVER_ERROR_OBJ;
};

# Represents JWT issuer configurations for JWT authentication.

type JwtIssuerConfig record {
    # JWT issuer, which is mapped to the `iss`
    string issuer;
    # JWT username, which is mapped to the `sub`
    string username;
    # JWT audience, which is mapped to the `aud`
    string|string[] audience;
    # JWT ID, which is mapped to the `jti`
    string jwtId;
    # JWT key ID, which is mapped the `kid`
    string keyId;
    # Map of custom claims
    map<json> customClaims;
    # Expiry time in seconds
    decimal expTime;
    # JWT signature configurations
    jwt:IssuerSignatureConfig signatureConfig; // Special Agent Note: IssuerSignatureConfig FROM ballerina/jwt package
};

# Represents JWT validator configurations for JWT authentication.

type JwtValidatorConfig record {
    # Expected issuer, which is mapped to the `iss`
    string issuer;
    # Expected username, which is mapped to the `sub`
    string username;
    # Expected audience, which is mapped to the `aud`
    string|string[] audience;
    # Expected JWT ID, which is mapped to the `jti`
    string jwtId;
    # Expected JWT key ID, which is mapped the `kid`
    string keyId;
    # Expected map of custom claims
    map<json> customClaims;
    # Clock skew (in seconds) that can be used to avoid token validation failures due to clock synchronization problems
    decimal clockSkew;
    # JWT signature configurations
    jwt:ValidatorSignatureConfig signatureConfig; // Special Agent Note: ValidatorSignatureConfig FROM ballerina/jwt package
    # Configurations related to the cache, which are used to store parsed JWT information
    cache:CacheConfig cacheConfig; // Special Agent Note: CacheConfig FROM ballerina/cache package
    # The key used to fetch the scopes
    string scopeKey = "scope";
};

# Represents the auth annotation for JWT validator configurations with scopes.

type JwtValidatorConfigWithScopes record {
    # JWT validator configurations for JWT authentication
    JwtValidatorConfig jwtValidatorConfig;
    # Scopes allowed for authorization
    string|string[] scopes?;
};

# Represents LDAP user store configurations for Basic Auth authentication.

type LdapUserStoreConfig record {
    # Unique name to identify the user store
    string domainName;
    # Connection URL of the LDAP server
    string connectionUrl;
    # The username used to connect to the LDAP server
    string connectionName;
    # The password used to connect to the LDAP server
    string connectionPassword;
    # DN of the context or object under which the user entries are stored in the LDAP server
    string userSearchBase;
    # Object class used to construct user entries
    string userEntryObjectClass;
    # The attribute used for uniquely identifying a user entry
    string userNameAttribute;
    # Filtering criteria used to search for a particular user entry
    string userNameSearchFilter;
    # Filtering criteria for searching user entries in the LDAP server
    string userNameListFilter;
    # DN of the context or object under which the group entries are stored in the LDAP server
    string[] groupSearchBase;
    # Object class used to construct group entries
    string groupEntryObjectClass;
    # The attribute used for uniquely identifying a group entry
    string groupNameAttribute;
    # Filtering criteria used to search for a particular group entry
    string groupNameSearchFilter;
    # Filtering criteria for searching group entries in the LDAP server
    string groupNameListFilter;
    # Define the attribute, which contains the distinguished names (DN) of user objects that are there in a group
    string membershipAttribute;
    # To indicate whether to cache the role list of a user
    boolean userRolesCacheEnabled;
    # Define whether LDAP connection pooling is enabled
    boolean connectionPoolingEnabled;
    # Connection timeout (in seconds) when making the initial LDAP connection
    decimal connectionTimeout;
    # Reading timeout (in seconds) for LDAP operations
    decimal readTimeout;
    # The SSL configurations for the LDAP client socket. This needs to be configured in order to communicate through LDAPs
    auth:SecureSocket secureSocket; // Special Agent Note: SecureSocket FROM ballerina/auth package
};

# Represents the auth annotation for LDAP user store configurations with scopes.

type LdapUserStoreConfigWithScopes record {
    # LDAP user store configurations for Basic Auth authentication
    LdapUserStoreConfig ldapUserStoreConfig;
    # Scopes allowed for authorization
    string|string[] scopes?;
};

# The status code response record of `LengthRequired`.

type LengthRequired record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusLengthRequired status = STATUS_LENGTH_REQUIRED_OBJ;
};

# Represents a server-provided hyperlink

type Link record {
    # Names the relationship of the linked target to the current representation
    string rel?;
    # Target URL
    string href;
    # Expected resource representation media types
    string[] types?;
    # Allowed resource methods
    Method[] methods?;
};


type LinkedTo record {
    # Name of the linked resource
    string name;
    # Name of the relationship between the linked resource and the current resource.
Defaulted to the IANA link relation `self`
    string relation = "self";
    # Method allowed in the linked resource
    string method?;
};

# Represents available server-provided links

type Links record {
    # Map of available links
    map<Link> _links;
};

# Provides a set of configurations for HTTP service endpoints.

type ListenerConfiguration record {
    # The host name/IP of the endpoint
    string host = "0.0.0.0";
    # Configurations related to HTTP/1.x protocol
    ListenerHttp1Settings http1Settings = {};
    # The SSL configurations for the service endpoint. This needs to be configured in order to
communicate through HTTPS.
    ListenerSecureSocket? secureSocket = ();
    # Highest HTTP version supported by the endpoint
    HttpVersion httpVersion = HTTP_2_0;
    # Period of time in seconds that a connection waits for a read/write operation. Use value 0 to
disable timeout
    decimal timeout = DEFAULT_LISTENER_TIMEOUT;
    # The server name which should appear as a response header
    string? server = ();
    # Configurations associated with inbound request size limits
    RequestLimitConfigs requestLimits = {};
    # Grace period of time in seconds for listener gracefulStop
    decimal gracefulStopTimeout = DEFAULT_GRACEFULSTOP_TIMEOUT;
    # Provides settings related to server socket configuration
    ServerSocketConfig socketConfig = {};
    # Configuration to change the initial window size in HTTP/2
    int http2InitialWindowSize = 65535;
    # Minimum time in seconds for a connection to be kept open which has received a GOAWAY.
This only applies for HTTP/2. Default value is 5 minutes. If the value is set to -1,
the connection will be closed after all in-flight streams are completed
    decimal minIdleTimeInStaleState = 300;
    # Time between the connection stale eviction runs in seconds. This only applies for HTTP/2.
Default value is 30 seconds
    decimal timeBetweenStaleEviction = 30;
};

# Provides settings related to HTTP/1.x protocol.

type ListenerHttp1Settings record {
    # Can be set to either `KEEPALIVE_AUTO`, which respects the `connection` header, or `KEEPALIVE_ALWAYS`,
which always keeps the connection alive, or `KEEPALIVE_NEVER`, which always closes the connection
    KeepAlive keepAlive = KEEPALIVE_AUTO;
    # Defines the maximum number of requests that can be processed at a given time on a single
connection. By default 10 requests can be pipelined on a single connection and user can
change this limit appropriately.
    int maxPipelinedRequests = MAX_PIPELINED_REQUESTS;
};

# Configures the SSL/TLS options to be used for HTTP service.

type ListenerSecureSocket record {
    # Configurations associated with `crypto:KeyStore` or combination of certificate and (PKCS8) private key of the server
    crypto:KeyStore|CertKey key; // Special Agent Note: KeyStore FROM ballerina/crypto package
    # Configures associated with mutual SSL operations
    record {VerifyClient verifyClient; crypto:TrustStore|string cert; } mutualSsl?;
    # SSL/TLS protocol related options
    record {Protocol name; string[] versions; } protocol?;
    # Certificate validation against OCSP_CRL, OCSP_STAPLING related options
    record {CertValidationType 'type; int cacheSize; int cacheValidityPeriod; } certValidation?;
    # List of ciphers to be used
eg: TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA
    string[] ciphers = ["TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256", "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256",
                        "TLS_DHE_RSA_WITH_AES_128_CBC_SHA256", "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA",
                        "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA", "TLS_DHE_RSA_WITH_AES_128_CBC_SHA",
                        "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
                        "TLS_DHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384",
                        "TLS_CHACHA20_POLY1305_SHA256", "TLS_AES_128_GCM_SHA256"];
    # Enable/Disable new SSL session creation
    boolean shareSession = true;
    # SSL handshake time out
    decimal handshakeTimeout?;
    # SSL session time out
    decimal sessionTimeout?;
};

# Represents the details of the `LoadBalanceActionError`.

type LoadBalanceActionErrorData record {
    # Array of errors occurred at each endpoint
    error[] httpActionErr?;
};

# The configurations related to the load balancing client endpoint. The following fields are inherited from the other
# configuration records in addition to the load balancing client specific configs.

type LoadBalanceClientConfiguration record {
    HttpVersion httpVersion;
    ClientHttp1Settings http1Settings;
    ClientHttp2Settings http2Settings;
    decimal timeout;
    string forwarded;
    FollowRedirects|() followRedirects;
    PoolConfiguration|() poolConfig;
    CacheConfig cache;
    Compression compression;
    CredentialsConfig|BearerTokenConfig|JwtIssuerConfig|OAuth2ClientCredentialsGrantConfig|OAuth2PasswordGrantConfig|OAuth2RefreshTokenGrantConfig|OAuth2JwtBearerGrantConfig|() auth;
    CircuitBreakerConfig|() circuitBreaker;
    RetryConfig|() retryConfig;
    CookieConfig|() cookieConfig;
    ResponseLimitConfigs responseLimits;
    ProxyConfig|() proxy;
    boolean validation;
    ClientSocketConfig socketConfig;
    boolean laxDataBinding;
    # The upstream HTTP endpoints among which the incoming HTTP traffic load should be distributed
    TargetService[] targets = [];
    # The `LoadBalancing` rule
    LoadBalancerRule? lbRule = ();
    # Configuration for the load balancer whether to fail over a failure
    boolean failover = true;
};

# Presents a read-only view of the local address.

type Local record {
    # The local host name
    string host;
    # The local port
    int port;
    # The local IP address
    string ip;
};

# The status code response record of `Locked`.

type Locked record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusLocked status = STATUS_LOCKED_OBJ;
};

# Represents HTTP access log file configuration.

type LogFileConfig record {
    # The file path to store access logs
    string path;
    # The log rotation configuration for file destinations
    log:RotationConfig rotation?; // Special Agent Note: RotationConfig FROM ballerina/log package
};

# The status code response record of `LoopDetected`.

type LoopDetected record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusLoopDetected status = STATUS_LOOP_DETECTED_OBJ;
};

# The status code response record of `MethodNotAllowed`.

type MethodNotAllowed record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusMethodNotAllowed status = STATUS_METHOD_NOT_ALLOWED_OBJ;
};

# The status code response record of `MisdirectedRequest`.

type MisdirectedRequest record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusMisdirectedRequest status = STATUS_MISDIRECTED_REQUEST_OBJ;
};

# The status code response record of `MovedPermanently`.

type MovedPermanently record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusMovedPermanently status = STATUS_MOVED_PERMANENTLY_OBJ;
};

# The status code response record of `MultipleChoices`.

type MultipleChoices record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusMultipleChoices status = STATUS_MULTIPLE_CHOICES_OBJ;
};

# The status code response record of `MultiStatus`.

type MultiStatus record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusMultiStatus status = STATUS_MULTI_STATUS_OBJ;
};

# A record for providing mutual SSL handshake results.

type MutualSslHandshake record {
    # Status of the handshake.
    MutualSslStatus status = ();
    # Base64 encoded certificate.
    string? base64EncodedCert = ();
};

# The status code response record of `NetworkAuthenticationRequired`.

type NetworkAuthenticationRequired record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusNetworkAuthenticationRequired status = STATUS_NETWORK_AUTHENTICATION_REQUIRED_OBJ;
};

# The status code response record of `NetworkAuthorizationRequired`.

type NetworkAuthorizationRequired record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
# Deprecated
This record is deprecated. Please use `NetworkAuthenticationRequired` instead.
    StatusNetworkAuthenticationRequired status = STATUS_NETWORK_AUTHENTICATION_REQUIRED_OBJ;
};

# The status code response record of `NoContent`.

type NoContent record {
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers?;
    # The response status code obj
    StatusNoContent status = STATUS_NO_CONTENT_OBJ;
};

# The status code response record of `NonAuthoritativeInformation`.

type NonAuthoritativeInformation record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusNonAuthoritativeInformation status = STATUS_NON_AUTHORITATIVE_INFORMATION_OBJ;
};

# The status code response record of `NotAcceptable`.

type NotAcceptable record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusNotAcceptable status = STATUS_NOT_ACCEPTABLE_OBJ;
};

# The status code response record of `NotExtended`.

type NotExtended record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusNotExtended status = STATUS_NOT_EXTENDED_OBJ;
};

# The status code response record of `NotFound`.

type NotFound record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusNotFound status = STATUS_NOT_FOUND_OBJ;
};

# The status code response record of `NotImplemented`.

type NotImplemented record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusNotImplemented status = STATUS_NOT_IMPLEMENTED_OBJ;
};

# The status code response record of `NotModified`.

type NotModified record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusNotModified status = STATUS_NOT_MODIFIED_OBJ;
};

# Represents OAuth2 client credentials grant configurations for OAuth2 authentication.

type OAuth2ClientCredentialsGrantConfig record {
    # Token URL of the token endpoint
    string tokenUrl;
    # Client ID of the client authentication
    string clientId;
    # Client secret of the client authentication
    string clientSecret;
    # Scope(s) of the access request
    string|string[] scopes;
    # Expiration time (in seconds) of the tokens if the token endpoint response does not contain an `expires_in` field
    decimal defaultTokenExpTime;
    # Clock skew (in seconds) that can be used to avoid token validation failures due to clock synchronization problems
    decimal clockSkew;
    # Map of the optional parameters used for the token endpoint
    map<string> optionalParams;
    # Bearer of the authentication credentials, which is sent to the token endpoint
    oauth2:CredentialBearer credentialBearer; // Special Agent Note: CredentialBearer FROM ballerina/oauth2 package
    # HTTP client configurations, which are used to call the token endpoint
    oauth2:ClientConfiguration clientConfig; // Special Agent Note: ClientConfiguration FROM ballerina/oauth2 package
};

# Represents OAuth2 introspection server configurations for OAuth2 authentication.

type OAuth2IntrospectionConfig record {
    # URL of the introspection endpoint
    string url;
    # A hint about the type of the token submitted for introspection
    string tokenTypeHint;
    # Map of the optional parameters used for the introspection endpoint
    map<string> optionalParams;
    # Configurations for the cache used to store the OAuth2 access token and other related information
    cache:CacheConfig cacheConfig; // Special Agent Note: CacheConfig FROM ballerina/cache package
    # Expiration time (in seconds) of the tokens if the introspection response does not contain an `exp` field
    decimal defaultTokenExpTime;
    # HTTP client configurations, which call the introspection endpoint
    oauth2:ClientConfiguration clientConfig; // Special Agent Note: ClientConfiguration FROM ballerina/oauth2 package
    # The key used to fetch the scopes
    string scopeKey = "scope";
};

# Represents the auth annotation for OAuth2 introspection server configurations with scopes.

type OAuth2IntrospectionConfigWithScopes record {
    # OAuth2 introspection server configurations for OAuth2 authentication
    OAuth2IntrospectionConfig oauth2IntrospectionConfig;
    # Scopes allowed for authorization
    string|string[] scopes?;
};

# Represents OAuth2 JWT bearer grant configurations for OAuth2 authentication.

type OAuth2JwtBearerGrantConfig record {
    # Token URL of the token endpoint
    string tokenUrl;
    # A single JWT for the JWT bearer grant type
    string assertion;
    # Client ID of the client authentication
    string clientId;
    # Client secret of the client authentication
    string clientSecret;
    # Scope(s) of the access request
    string|string[] scopes;
    # Expiration time (in seconds) of the tokens if the token endpoint response does not contain an `expires_in` field
    decimal defaultTokenExpTime;
    # Clock skew (in seconds) that can be used to avoid token validation failures due to clock synchronization problems
    decimal clockSkew;
    # Map of the optional parameters used for the token endpoint
    map<string> optionalParams;
    # Bearer of the authentication credentials, which is sent to the token endpoint
    oauth2:CredentialBearer credentialBearer; // Special Agent Note: CredentialBearer FROM ballerina/oauth2 package
    # HTTP client configurations, which are used to call the token endpoint
    oauth2:ClientConfiguration clientConfig; // Special Agent Note: ClientConfiguration FROM ballerina/oauth2 package
};

# Represents OAuth2 password grant configurations for OAuth2 authentication.

type OAuth2PasswordGrantConfig record {
    # Token URL of the token endpoint
    string tokenUrl;
    # Username for the password grant type
    string username;
    # Password for the password grant type
    string password;
    # Client ID of the client authentication
    string clientId;
    # Client secret of the client authentication
    string clientSecret;
    # Scope(s) of the access request
    string|string[] scopes;
    # Configurations for refreshing the access token
    oauth2:RefreshConfig|"INFER_REFRESH_CONFIG" refreshConfig; // Special Agent Note: RefreshConfig FROM ballerina/oauth2 package
    # Expiration time (in seconds) of the tokens if the token endpoint response does not contain an `expires_in` field
    decimal defaultTokenExpTime;
    # Clock skew (in seconds) that can be used to avoid token validation failures due to clock synchronization problems
    decimal clockSkew;
    # Map of the optional parameters used for the token endpoint
    map<string> optionalParams;
    # Bearer of the authentication credentials, which is sent to the token endpoint
    oauth2:CredentialBearer credentialBearer; // Special Agent Note: CredentialBearer FROM ballerina/oauth2 package
    # HTTP client configurations, which are used to call the token endpoint
    oauth2:ClientConfiguration clientConfig; // Special Agent Note: ClientConfiguration FROM ballerina/oauth2 package
};

# Represents OAuth2 refresh token grant configurations for OAuth2 authentication.

type OAuth2RefreshTokenGrantConfig record {
    # Refresh token URL of the token endpoint
    string refreshUrl;
    # Refresh token for the token endpoint
    string refreshToken;
    # Client ID of the client authentication
    string clientId;
    # Client secret of the client authentication
    string clientSecret;
    # Scope(s) of the access request
    string|string[] scopes;
    # Expiration time (in seconds) of the tokens if the token endpoint response does not contain an `expires_in` field
    decimal defaultTokenExpTime;
    # Clock skew (in seconds) that can be used to avoid token validation failures due to clock synchronization problems
    decimal clockSkew;
    # Map of the optional parameters used for the token endpoint
    map<string> optionalParams;
    # Bearer of the authentication credentials, which is sent to the token endpoint
    oauth2:CredentialBearer credentialBearer; // Special Agent Note: CredentialBearer FROM ballerina/oauth2 package
    # HTTP client configurations, which are used to call the token endpoint
    oauth2:ClientConfiguration clientConfig; // Special Agent Note: ClientConfiguration FROM ballerina/oauth2 package
};

# The status code response record of `Ok`.

type Ok record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusOK status = STATUS_OK_OBJ;
};

# The status code response record of `PartialContent`.

type PartialContent record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusPartialContent status = STATUS_PARTIAL_CONTENT_OBJ;
};

# The status code response record of `PayloadTooLarge`.

type PayloadTooLarge record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusPayloadTooLarge status = STATUS_PAYLOAD_TOO_LARGE_OBJ;
};

# The status code response record of `PaymentRequired`.

type PaymentRequired record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusPaymentRequired status = STATUS_PAYMENT_REQUIRED_OBJ;
};

# The status code response record of `PermanentRedirect`.

type PermanentRedirect record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusPermanentRedirect status = STATUS_PERMANENT_REDIRECT_OBJ;
};

# Configurations for managing HTTP client connection pool.

type PoolConfiguration record {
    # Max active connections per route(host:port). Default value is -1 which indicates unlimited.
    int maxActiveConnections = maxActiveConnections;
    # Maximum number of idle connections allowed per pool.
    int maxIdleConnections = maxIdleConnections;
    # Maximum amount of time (in seconds), the client should wait for an idle connection before it sends an error when the pool is exhausted
    decimal waitTime = waitTime;
    # Maximum active streams per connection. This only applies to HTTP/2. Default value is 100
    int maxActiveStreamsPerConnection = maxActiveStreamsPerConnection;
    # Minimum evictable time for an idle connection in seconds. Default value is 5 minutes
    decimal minEvictableIdleTime = minEvictableIdleTime;
    # Time between eviction runs in seconds. Default value is 30 seconds
    decimal timeBetweenEvictionRuns = timeBetweenEvictionRuns;
    # Minimum time in seconds for a connection to be kept open which has received a GOAWAY.
This only applies for HTTP/2. Default value is 5 minutes. If the value is set to -1,
the connection will be closed after all in-flight streams are completed
    decimal minIdleTimeInStaleState = minIdleTimeInStaleState;
    # Time between the connection stale eviction runs in seconds. This only applies for HTTP/2.
Default value is 30 seconds
    decimal timeBetweenStaleEviction = timeBetweenStaleEviction;
};

# The status code response record of `PreconditionFailed`.

type PreconditionFailed record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusPreconditionFailed status = STATUS_PRECONDITION_FAILED_OBJ;
};

# The status code response record of `PreconditionRequired`.

type PreconditionRequired record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusPreconditionRequired status = STATUS_PRECONDITION_REQUIRED_OBJ;
};

# The status code response record of `Processing`.

type Processing record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusProcessing status = STATUS_PROCESSING_OBJ;
};

# The status code response record of `ProxyAuthenticationRequired`.

type ProxyAuthenticationRequired record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusProxyAuthenticationRequired status = STATUS_PROXY_AUTHENTICATION_REQUIRED_OBJ;
};

# Proxy server configurations to be used with the HTTP client endpoint.

type ProxyConfig record {
    # Host name of the proxy server
    string host = "";
    # Proxy server port
    int port = 0;
    # Proxy server username
    string userName = "";
    # proxy server password
    string password = "";
};

# Defines the record type of query parameters supported with client resource methods.

type QueryParams record {
    # headers which cannot be used as a query field
    never headers?;
    # targetType which cannot be used as a query field
    never targetType?;
    # message which cannot be used as a query field
    never message?;
    # mediaType which cannot be used as a query field
    never mediaType?;
    # Rest field
    QueryParamType ;
};

# The status code response record of `RangeNotSatisfiable`.

type RangeNotSatisfiable record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusRangeNotSatisfiable status = STATUS_RANGE_NOT_SATISFIABLE_OBJ;
};

# Presents a read-only view of the remote address.

type Remote record {
    # The remote host name
    string host;
    # The remote port
    int port;
    # The remote IP address
    string ip;
};

# The status code response record of `RequestHeaderFieldsTooLarge`.

type RequestHeaderFieldsTooLarge record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusRequestHeaderFieldsTooLarge status = STATUS_REQUEST_HEADER_FIELDS_TOO_LARGE_OBJ;
};

# Provides inbound request URI, total header and entity body size threshold configurations.

type RequestLimitConfigs record {
    # Maximum allowed length for a URI. Exceeding this limit will result in a `414 - URI Too Long`
response. For HTTP/2, this limit will not be applicable as it already has a `:path`
pseudo-header which will be validated by `maxHeaderSize`
    int maxUriLength = 4096;
    # Maximum allowed size for headers. Exceeding this limit will result in a
`431 - Request Header Fields Too Large` response
    int maxHeaderSize = 8192;
    # Maximum allowed size for the entity body. By default it is set to -1 which means there
is no restriction `maxEntityBodySize`, On the Exceeding this limit will result in a
`413 - Payload Too Large` response
    int maxEntityBodySize = -1;
};

# The status code response record of `RequestTimeout`.

type RequestTimeout record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusRequestTimeout status = STATUS_REQUEST_TIMEOUT_OBJ;
};

# The status code response record of `ResetContent`.

type ResetContent record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusResetContent status = STATUS_RESET_CONTENT_OBJ;
};

# Provides inbound response status line, total header and entity body size threshold configurations.

type ResponseLimitConfigs record {
    # Maximum allowed length for response status line(`HTTP/1.1 200 OK`). Exceeding this limit will
result in a `ClientError`
    int maxStatusLineLength = 4096;
    # Maximum allowed size for headers. Exceeding this limit will result in a `ClientError`
    int maxHeaderSize = 8192;
    # Maximum allowed size for the entity body. By default it is set to -1 which means there is no
restriction `maxEntityBodySize`, On the Exceeding this limit will result in a `ClientError`
    int maxEntityBodySize = -1;
};

# Provides configurations for controlling the retrying behavior in failure scenarios.

type RetryConfig record {
    # Number of retry attempts before giving up
    int count = 0;
    # Retry interval in seconds
    decimal interval = 0;
    # Multiplier, which increases the retry interval exponentially.
    float backOffFactor = 0.0;
    # Maximum time of the retry interval in seconds
    decimal maxWaitInterval = 0;
    # HTTP response status codes which are considered as failures
    int[] statusCodes = [];
};

# Represents a rolling window in the Circuit Breaker.

type RollingWindow record {
    # Minimum number of requests in a `RollingWindow` that will trip the circuit.
    int requestVolumeThreshold = 10;
    # Time period in seconds for which the failure threshold is calculated
    decimal timeWindow = 60;
    # The granularity at which the time window slides. This is measured in seconds.
    decimal bucketSize = 10;
};

# Represents the annotation used for authorization.

type Scopes record {
    # Scopes allowed for authorization
    string|string[] scopes;
};

# The status code response record of `SeeOther`.

type SeeOther record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusSeeOther status = STATUS_SEE_OTHER_OBJ;
};

# Provides settings related to server socket configuration.

type ServerSocketConfig record {
    # Connect timeout of the channel in seconds. If the Channel does not support connect operation,
this property is not used at all, and therefore will be ignored.
    decimal connectTimeOut;
    # Sets the SO_RCVBUF option to the specified value for this Socket.
    int receiveBufferSize;
    # Sets the SO_SNDBUF option to the specified value for this Socket.
    int sendBufferSize;
    # Enable/disable TCP_NODELAY (disable/enable Nagle's algorithm).
    boolean tcpNoDelay;
    # Enable/disable the SO_REUSEADDR socket option.
    boolean socketReuse;
    # Enable/disable SO_KEEPALIVE.
    boolean keepAlive;
    # Requested maximum length of the queue of incoming connections.
    int soBackLog = 100;
};

# The status code response record of `ServiceUnavailable`.

type ServiceUnavailable record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusServiceUnavailable status = STATUS_SERVICE_UNAVAILABLE_OBJ;
};

# Represents a Server Sent Event emitted from a service.

type SseEvent record {
    # Name of the event
    string event?;
    # Id of the event
    string id?;
    # Data part of the event
    string data?;
    # The reconnect time on failure in milliseconds.
    int 'retry?;
    # Comment added to the event
    string comment?;
};

# Represents the details of an HTTP status code binding client error.

type StatusCodeBindingErrorDetail record {
    # The inbound error response status code
    int statusCode;
    # The inbound error response headers
    map<string[]> headers;
    # The inbound error response body
    anydata body;
    # Indicates whether the error orginates from default status code response mapping
    boolean fromDefaultStatusCodeMapping;
};

# Defines a status code response record type

type StatusCodeRecord record {
    # The status code
    int status;
    # The headers of the response
    map<string|int|boolean|string[]|int[]|boolean[]> headers?;
    # The response body
    anydata body?;
};

# The status code response record of `SwitchingProtocols`.

type SwitchingProtocols record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusSwitchingProtocols status = STATUS_SWITCHING_PROTOCOLS_OBJ;
};

# Represents a single service and its related configurations.

type TargetService record {
    # URL of the target service
    string url = "";
    # Configurations for secure communication with the remote HTTP endpoint
    ClientSecureSocket? secureSocket = ();
};

# The status code response record of `TemporaryRedirect`.

type TemporaryRedirect record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusTemporaryRedirect status = STATUS_TEMPORARY_REDIRECT_OBJ;
};

# The status code response record of `TooEarly`.

type TooEarly record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusTooEarly status = STATUS_TOO_EARLY_OBJ;
};

# The status code response record of `TooManyRequests`.

type TooManyRequests record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusTooManyRequests status = STATUS_TOO_MANY_REQUESTS_OBJ;
};

# Represents HTTP trace log configuration.

type TraceLogAdvancedConfiguration record {
    # Enable or disable console trace logs
    boolean console = false;
    # File path to store trace logs
    string path?;
    # Socket hostname to publish the trace logs
    string host?;
    # Socket port to publish the trace logs
    int port?;
    # Log file configuration to store trace logs
    LogFileConfig file?;
};

# The status code response record of `Unauthorized`.

type Unauthorized record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusUnauthorized status = STATUS_UNAUTHORIZED_OBJ;
};

# The status code response record of `UnavailableDueToLegalReasons`.

type UnavailableDueToLegalReasons record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusUnavailableDueToLegalReasons status = STATUS_UNAVAILABLE_DUE_TO_LEGAL_REASONS_OBJ;
};

# The status code response record of `UnprocessableEntity`.

type UnprocessableEntity record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusUnprocessableEntity status = STATUS_UNPROCESSABLE_ENTITY_OBJ;
};

# The status code response record of `UnsupportedMediaType`.

type UnsupportedMediaType record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusUnsupportedMediaType status = STATUS_UNSUPPORTED_MEDIA_TYPE_OBJ;
};

# The status code response record of `UpgradeRequired`.

type UpgradeRequired record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusUpgradeRequired status = STATUS_UPGRADE_REQUIRED_OBJ;
};

# The status code response record of `UriTooLong`.

type UriTooLong record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusUriTooLong status = STATUS_URI_TOO_LONG_OBJ;
};

# The status code response record of `UseProxy`.

type UseProxy record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusUseProxy status = STATUS_USE_PROXY_OBJ;
};

# The status code response record of `VariantAlsoNegotiates`.

type VariantAlsoNegotiates record {
    # The value of response `Content-type` header
    string mediaType;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers;
    # The response payload
    anydata body;
    # The response status code obj
    StatusVariantAlsoNegotiates status = STATUS_VARIANT_ALSO_NEGOTIATES_OBJ;
};

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

# HTTP header key `age`. Gives the current age of a cached HTTP response.
const string AGE = ""age"";

# Represents the Authorization header name.
const string AUTH_HEADER = ""Authorization"";

# The prefix used to denote the Basic authentication scheme.
const string AUTH_SCHEME_BASIC = ""Basic"";

# The prefix used to denote the Bearer authentication scheme.
const string AUTH_SCHEME_BEARER = ""Bearer"";

# HTTP header key `authorization`
const string AUTHORIZATION = ""authorization"";

# HTTP header key `cache-control`. Specifies the cache control directives required for the function of HTTP caches.
const string CACHE_CONTROL = ""cache-control"";

# This is a more restricted mode of RFC 7234. Setting this as the caching policy restricts caching to instances
# where the `cache-control` header and either the `etag` or `last-modified` header are present.
const string CACHE_CONTROL_AND_VALIDATORS = ""CACHE_CONTROL_AND_VALIDATORS"";

# Represents the closed state of the circuit. When the Circuit Breaker is in `CLOSED` state, all requests will be
# allowed to go through to the upstream service. If the failures exceed the configured threhold values, the circuit
# will trip and move to the `OPEN` state.
const string CB_CLOSED_STATE = ""CLOSED"";

# Represents the half-open state of the circuit. When the Circuit Breaker is in `HALF_OPEN` state, a trial request
# will be sent to the upstream service. If it fails, the circuit will trip again and move to the `OPEN` state. If not,
# it will move to the `CLOSED` state.
const string CB_HALF_OPEN_STATE = ""HALF_OPEN"";

# Represents the open state of the circuit. When the Circuit Breaker is in `OPEN` state, requests will fail
# immediately.
const string CB_OPEN_STATE = ""OPEN"";

# Always set chunking header in the response.
const string CHUNKING_ALWAYS = ""ALWAYS"";

# If the payload is less than 8KB, content-length header is set in the outbound request/response,
# otherwise chunking header is set in the outbound request/response.}
const string CHUNKING_AUTO = ""AUTO"";

# Never set the chunking header even if the payload is larger than 8KB in the outbound request/response.
const string CHUNKING_NEVER = ""NEVER"";

# Always set accept-encoding/content-encoding in outbound request/response.
const string COMPRESSION_ALWAYS = ""ALWAYS"";

# When service behaves as a HTTP gateway inbound request/response accept-encoding option is set as the
# outbound request/response accept-encoding/content-encoding option.
const string COMPRESSION_AUTO = ""AUTO"";

# Never set accept-encoding/content-encoding header in outbound request/response.
const string COMPRESSION_NEVER = ""NEVER"";

# HTTP header key `connection`. Allows the sender to specify options that are desired for that particular connection.
const string CONNECTION = ""connection"";

# HTTP header key `content-length`. Specifies the size of the response body in bytes.
const string CONTENT_LENGTH = ""content-length"";

# HTTP header key `content-type`. Specifies the type of the message payload.
const string CONTENT_TYPE = ""content-type"";

# HTTP header key `date`. The timestamp at the time the response was generated/received.
const string DATE = ""date"";

# Constant for the default listener gracefulStop timeout in seconds
const decimal DEFAULT_GRACEFULSTOP_TIMEOUT = 0;

# Constant for the default listener endpoint timeout in seconds
const decimal DEFAULT_LISTENER_TIMEOUT = 60;

# HTTP header key `etag`. A finger print for a resource which is used by HTTP caches to identify whether a
# resource representation has changed.
const string ETAG = ""etag"";

# HTTP header key `expect`. Specifies expectations to be fulfilled by the server.
const string EXPECT = ""expect"";

# HTTP header key `expires`. Specifies the time at which the response becomes stale.
const string EXPIRES = ""expires"";

# Mutual SSL handshake has failed.
const string FAILED = ""failed"";

# Constant for the HTTP DELETE method
const string HTTP_DELETE = ""DELETE"";

# Constant for the HTTP FORWARD method
const string HTTP_FORWARD = ""FORWARD"";

# Constant for the HTTP GET method
const string HTTP_GET = ""GET"";

# Constant for the HTTP HEAD method
const string HTTP_HEAD = ""HEAD"";

# Constant for the identify not an HTTP Operation
const string HTTP_NONE = ""NONE"";

# Constant for the HTTP OPTIONS method
const string HTTP_OPTIONS = ""OPTIONS"";

# Constant for the HTTP PATCH method
const string HTTP_PATCH = ""PATCH"";

# Constant for the HTTP POST method
const string HTTP_POST = ""POST"";

# Constant for the HTTP PUT method
const string HTTP_PUT = ""PUT"";

# constant for the HTTP SUBMIT method
const string HTTP_SUBMIT = ""SUBMIT"";

# HTTP header key `if-match`
const string IF_MATCH = ""if-match"";

# HTTP header key `if-modified-since`. Used when validating (with the origin server) whether a cached response
# is still valid. If the representation of the resource has modified since the timestamp in this field, a
# 304 response is returned.
const string IF_MODIFIED_SINCE = ""if-modified-since"";

# HTTP header key `if-none-match`. Used when validating (with the origin server) whether a cached response is
# still valid. If the ETag provided in this field matches the representation of the requested resource, a
# 304 response is returned.
const string IF_NONE_MATCH = ""if-none-match"";

# HTTP header key `if-range`
const string IF_RANGE = ""if-range"";

# HTTP header key `if-unmodified-since`
const string IF_UNMODIFIED_SINCE = ""if-unmodified-since"";

# Constant to get the jwt information from the request context.
const string JWT_INFORMATION = ""JWT_INFORMATION"";

# Keeps the connection alive irrespective of the `connection` header value }
const string KEEPALIVE_ALWAYS = ""ALWAYS"";

# Decides to keep the connection alive or not based on the `connection` header of the client request }
const string KEEPALIVE_AUTO = ""AUTO"";

# Closes the connection irrespective of the `connection` header value }
const string KEEPALIVE_NEVER = ""NEVER"";

# HTTP header key `last-modified`. The time at which the resource was last modified.
const string LAST_MODIFIED = ""last-modified"";

# Header is placed before the payload of the request/response.
const string LEADING = ""leading"";

# HTTP header key `location`. Indicates the URL to redirect a request to.
const string LOCATION = ""location"";

# When used in requests, `max-age` implies that clients are not willing to accept responses whose age is greater
# than `max-age`. When used in responses, the response is to be considered stale after the specified
# number of seconds.
const string MAX_AGE = ""max-age"";

# Indicates that the client is willing to accept responses which have exceeded their freshness lifetime by no more
# than the specified number of seconds.
const string MAX_STALE = ""max-stale"";

# Setting this as the `max-stale` directives indicates that the `max-stale` directive does not specify a limit.
const decimal MAX_STALE_ANY_AGE = 9223372036854775807;

# Indicates that the client is only accepting responses whose freshness lifetime >= current age + min-fresh.
const string MIN_FRESH = ""min-fresh"";

# Represents multipart primary type
const string MULTIPART_AS_PRIMARY_TYPE = ""multipart/"";

# Indicates that once the response has become stale, it should not be reused for subsequent requests without
# validating with the origin server.
const string MUST_REVALIDATE = ""must-revalidate"";

# Forces the cache to validate a cached response with the origin server before serving.
const string NO_CACHE = ""no-cache"";

# Instructs the cache to not store a response in non-volatile storage.
const string NO_STORE = ""no-store"";

# Instructs intermediaries not to transform the payload.
const string NO_TRANSFORM = ""no-transform"";

# Not a mutual ssl connection.
const record {} NONE = ();

# Indicates that the client is only willing to accept a cached response. A cached response is served subject to
# other constraints posed by the request.
const string ONLY_IF_CACHED = ""only-if-cached"";

# Mutual SSL handshake is successful.
const string PASSED = ""passed"";

# HTTP header key `pragma`. Used in dealing with HTTP 1.0 caches which do not understand the `cache-control` header.
const string PRAGMA = ""pragma"";

# Indicates that the response is intended for a single user and should not be stored by shared caches.
const string PRIVATE = ""private"";

# HTTP header key `proxy-authorization`. Contains the credentials to authenticate a user agent to a proxy serve.
const string PROXY_AUTHORIZATION = ""proxy-authorization"";

# Has the same semantics as `must-revalidate`, except that this does not apply to private caches.
const string PROXY_REVALIDATE = ""proxy-revalidate"";

# Indicates that any cache may store the response.
const string PUBLIC = ""public"";

# Represents the HTTP redirect status code `302 - Found`.
const int REDIRECT_FOUND_302 = 302;

# Represents the HTTP redirect status code `301 - Moved Permanently`.
const int REDIRECT_MOVED_PERMANENTLY_301 = 301;

# Represents the HTTP redirect status code `300 - Multiple Choices`.
const int REDIRECT_MULTIPLE_CHOICES_300 = 300;

# Represents the HTTP redirect status code `304 - Not Modified`.
const int REDIRECT_NOT_MODIFIED_304 = 304;

# Represents the HTTP redirect status code `308 - Permanent Redirect`.
const int REDIRECT_PERMANENT_REDIRECT_308 = 308;

# Represents the HTTP redirect status code `303 - See Other`.
const int REDIRECT_SEE_OTHER_303 = 303;

# Represents the HTTP redirect status code `307 - Temporary Redirect`.
const int REDIRECT_TEMPORARY_REDIRECT_307 = 307;

# Represents the HTTP redirect status code `305 - Use Proxy`.
const int REDIRECT_USE_PROXY_305 = 305;

# Constant for the request method reference.
const string REQUEST_METHOD = ""REQUEST_METHOD"";

# Constant for the resource name reference.
const string RESOURCE_NAME = ""RESOURCE_NAME"";

# Caching behaviour is as specified by the RFC 7234 specification.
const string RFC_7234 = ""RFC_7234"";

# In shared caches, `s-maxage` overrides the `max-age` or `expires` header field.
const string S_MAX_AGE = ""s-maxage"";

# HTTP header key `server`. Specifies the details of the origin server.
const string SERVER = ""server"";

# Constant for the service name reference.
const string SERVICE_NAME = ""SERVICE_NAME"";

# The HTTP response status code: 202 Accepted
const int STATUS_ACCEPTED = 202;

# The HTTP response status code: 208 Already Reported
const int STATUS_ALREADY_REPORTED = 208;

# The HTTP response status code: 502 Bad Gateway
const int STATUS_BAD_GATEWAY = 502;

# The HTTP response status code: 400 Bad Request
const int STATUS_BAD_REQUEST = 400;

# The HTTP response status code: 409 Conflict
const int STATUS_CONFLICT = 409;

# The HTTP response status code: 100 Continue
const int STATUS_CONTINUE = 100;

# The HTTP response status code: 201 Created
const int STATUS_CREATED = 201;

# The HTTP response status code: 103 Early Hints
const int STATUS_EARLY_HINTS = 103;

# The HTTP response status code: 417 Expectation Failed
const int STATUS_EXPECTATION_FAILED = 417;

# The HTTP response status code: 424 Failed Dependency
const int STATUS_FAILED_DEPENDENCY = 424;

# The HTTP response status code: 403 Forbidden
const int STATUS_FORBIDDEN = 403;

# The HTTP response status code: 302 Found
const int STATUS_FOUND = 302;

# The HTTP response status code: 504 Gateway Timeout
const int STATUS_GATEWAY_TIMEOUT = 504;

# The HTTP response status code: 410 Gone
const int STATUS_GONE = 410;

# The HTTP response status code: 505 HTTP Version Not Supported
const int STATUS_HTTP_VERSION_NOT_SUPPORTED = 505;

# The HTTP response status code: 226 IM Used
const int STATUS_IM_USED = 226;

# The HTTP response status code: 507 Insufficient Storage
const int STATUS_INSUFFICIENT_STORAGE = 507;

# The HTTP response status code: 500 Internal Server Error
const int STATUS_INTERNAL_SERVER_ERROR = 500;

# The HTTP response status code: 411 Length Required
const int STATUS_LENGTH_REQUIRED = 411;

# The HTTP response status code: 423 Locked
const int STATUS_LOCKED = 423;

# The HTTP response status code: 508 Loop Detected
const int STATUS_LOOP_DETECTED = 508;

# The HTTP response status code: 405 Method Not Allowed
const int STATUS_METHOD_NOT_ALLOWED = 405;

# The HTTP response status code: 421 Misdirected Request
const int STATUS_MISDIRECTED_REQUEST = 421;

# The HTTP response status code: 301 Moved Permanently
const int STATUS_MOVED_PERMANENTLY = 301;

# The HTTP response status code: 207 Multi-Status
const int STATUS_MULTI_STATUS = 207;

# The HTTP response status code: 300 Multiple Choices
const int STATUS_MULTIPLE_CHOICES = 300;

# The HTTP response status code: 511 Network Authorization Required
const int STATUS_NETWORK_AUTHENTICATION_REQUIRED = 511;

# The HTTP response status code: 204 No Content
const int STATUS_NO_CONTENT = 204;

# The HTTP response status code: 203 Non Authoritative Information
const int STATUS_NON_AUTHORITATIVE_INFORMATION = 203;

# The HTTP response status code: 406 Not Acceptable
const int STATUS_NOT_ACCEPTABLE = 406;

# The HTTP response status code: 510 Not Extended
const int STATUS_NOT_EXTENDED = 510;

# The HTTP response status code: 404 Not Found
const int STATUS_NOT_FOUND = 404;

# The HTTP response status code: 501 Not Implemented
const int STATUS_NOT_IMPLEMENTED = 501;

# The HTTP response status code: 304 Not Modified
const int STATUS_NOT_MODIFIED = 304;

# The HTTP response status code: 200 OK
const int STATUS_OK = 200;

# The HTTP response status code: 206 Partial Content
const int STATUS_PARTIAL_CONTENT = 206;

# The HTTP response status code: 413 Payload Too Large
const int STATUS_PAYLOAD_TOO_LARGE = 413;

# The HTTP response status code: 402 Payment Required
const int STATUS_PAYMENT_REQUIRED = 402;

# The HTTP response status code: 308 Permanent Redirect
const int STATUS_PERMANENT_REDIRECT = 308;

# The HTTP response status code: 412 Precondition Failed
const int STATUS_PRECONDITION_FAILED = 412;

# The HTTP response status code: 428 Precondition Required
const int STATUS_PRECONDITION_REQUIRED = 428;

# The HTTP response status code: 102 Processing
const int STATUS_PROCESSING = 102;

# The HTTP response status code: 407 Proxy Authentication Required
const int STATUS_PROXY_AUTHENTICATION_REQUIRED = 407;

# The HTTP response status code: 416 Range Not Satisfiable
const int STATUS_RANGE_NOT_SATISFIABLE = 416;

# The HTTP response status code: 431 Request Header Fields Too Large
const int STATUS_REQUEST_HEADER_FIELDS_TOO_LARGE = 431;

# The HTTP response status code: 408 Request Timeout
const int STATUS_REQUEST_TIMEOUT = 408;

# The HTTP response status code: 205 Reset Content
const int STATUS_RESET_CONTENT = 205;

# The HTTP response status code: 303 See Other
const int STATUS_SEE_OTHER = 303;

# The HTTP response status code: 503 Service Unavailable
const int STATUS_SERVICE_UNAVAILABLE = 503;

# The HTTP response status code: 101 Switching Protocols
const int STATUS_SWITCHING_PROTOCOLS = 101;

# The HTTP response status code: 307 Temporary Redirect
const int STATUS_TEMPORARY_REDIRECT = 307;

# The HTTP response status code: 425 Too Early
const int STATUS_TOO_EARLY = 425;

# The HTTP response status code: 429 Too Many Requests
const int STATUS_TOO_MANY_REQUESTS = 429;

# The HTTP response status code: 401 Unauthorized
const int STATUS_UNAUTHORIZED = 401;

# The HTTP response status code: 451 Unavailable Due To Legal Reasons
const int STATUS_UNAVAILABLE_DUE_TO_LEGAL_REASONS = 451;

# The HTTP response status code: 422 Unprocessable Entity
const int STATUS_UNPROCESSABLE_ENTITY = 422;

# The HTTP response status code: 415 Unsupported Media Type
const int STATUS_UNSUPPORTED_MEDIA_TYPE = 415;

# The HTTP response status code: 426 Upgrade Required
const int STATUS_UPGRADE_REQUIRED = 426;

# The HTTP response status code: 414 URI Too Long
const int STATUS_URI_TOO_LONG = 414;

# The HTTP response status code: 305 Use Proxy
const int STATUS_USE_PROXY = 305;

# The HTTP response status code: 506 Variant Also Negotiates
const int STATUS_VARIANT_ALSO_NEGOTIATES = 506;

# Header is placed after the payload of the request/response.
const string TRAILING = ""trailing"";

# HTTP header key `transfer-encoding`. Specifies what type of transformation has been applied to entity body.
const string TRANSFER_ENCODING = ""transfer-encoding"";

# HTTP header key `upgrade`. Allows the client to specify what additional communication protocols it supports and
# would like to use, if the server finds it appropriate to switch protocols.
const string UPGRADE = ""upgrade"";

# HTTP header key `warning`. Specifies warnings generated when serving stale responses from HTTP caches.
const string WARNING = ""warning"";

# Represents certification validation type options.
enum CertValidationType {
    OCSP_CRL,
    OCSP_STAPLING
}

# Defines the supported HTTP protocols.
enum HttpVersion {
    HTTP_1_0,
    HTTP_1_1,
    HTTP_2_0
}

# Represents HTTP methods.
enum Method {
    GET,
    POST,
    PUT,
    DELETE,
    PATCH,
    HEAD,
    OPTIONS
}

# Represents protocol options.
enum Protocol {
    SSL,
    TLS,
    DTLS
}

# Represents client verify options.
enum VerifyClient {
    REQUIRE,
    OPTIONAL
}

class ClientBasicAuthHandler {
}

class ClientBearerTokenAuthHandler {
}

class ClientSelfSignedJwtAuthHandler {
}

class Cookie {
}

class CookieStore {
}

class CsvPersistentCookieHandler {
}

class DefaultStatus {
}

class Headers {
}

class HttpCache {
}

class HttpFuture {
}

class ListenerFileUserStoreBasicAuthHandler {
}

class ListenerJwtAuthHandler {
}

class LoadBalancerRoundRobinRule {
}

class PushPromise {
}

class Request {
}

class RequestCacheControl {
}

class RequestContext {
}

class Response {
}

class ResponseCacheControl {
}

class StatusAccepted {
}

class StatusAlreadyReported {
}

class StatusBadGateway {
}

class StatusBadRequest {
}

class StatusConflict {
}

class StatusContinue {
}

class StatusCreated {
}

class StatusEarlyHints {
}

class StatusExpectationFailed {
}

class StatusFailedDependency {
}

class StatusForbidden {
}

class StatusFound {
}

class StatusGatewayTimeout {
}

class StatusGone {
}

class StatusHttpVersionNotSupported {
}

class StatusIMUsed {
}

class StatusInsufficientStorage {
}

class StatusInternalServerError {
}

class StatusLengthRequired {
}

class StatusLocked {
}

class StatusLoopDetected {
}

class StatusMethodNotAllowed {
}

class StatusMisdirectedRequest {
}

class StatusMovedPermanently {
}

class StatusMultipleChoices {
}

class StatusMultiStatus {
}

class StatusNetworkAuthenticationRequired {
}

class StatusNoContent {
}

class StatusNonAuthoritativeInformation {
}

class StatusNotAcceptable {
}

class StatusNotExtended {
}

class StatusNotFound {
}

class StatusNotImplemented {
}

class StatusNotModified {
}

class StatusOK {
}

class StatusPartialContent {
}

class StatusPayloadTooLarge {
}

class StatusPaymentRequired {
}

class StatusPermanentRedirect {
}

class StatusPreconditionFailed {
}

class StatusPreconditionRequired {
}

class StatusProcessing {
}

class StatusProxyAuthenticationRequired {
}

class StatusRangeNotSatisfiable {
}

class StatusRequestHeaderFieldsTooLarge {
}

class StatusRequestTimeout {
}

class StatusResetContent {
}

class StatusSeeOther {
}

class StatusServiceUnavailable {
}

class StatusSwitchingProtocols {
}

class StatusTemporaryRedirect {
}

class StatusTooEarly {
}

class StatusTooManyRequests {
}

class StatusUnauthorized {
}

class StatusUnavailableDueToLegalReasons {
}

class StatusUnprocessableEntity {
}

class StatusUnsupportedMediaType {
}

class StatusUpgradeRequired {
}

class StatusUriTooLong {
}

class StatusUseProxy {
}

class StatusVariantAlsoNegotiates {
}

class ClientObject {
}

class LoadBalancerRule {
}

class PersistentCookieHandler {
}

class Status {
}

class StatusCodeClientObject {
}

# Used for configuring the caching behaviour. Setting the `policy` field in the `CacheConfig` record allows
# the user to control the caching behaviour.
type CachingPolicy CACHE_CONTROL_AND_VALIDATORS|RFC_7234;

# The types of messages that are accepted by HTTP `client` when sending out the outbound request.
type RequestMessage anydata|Request|mime:Entity[]|stream<byte[],io:Error?>;

# The types of messages that are accepted by HTTP `listener` when sending out the outbound response.
type ResponseMessage anydata|Response|mime:Entity[]|stream<byte[],io:Error?>|stream<SseEvent,error?>|stream<SseEvent,error>;

# Defines the HTTP operations related to circuit breaker, failover and load balancer.
# 
# `FORWARD`: Forward the specified payload
# `GET`: Request a resource
# `POST`: Create a new resource
# `DELETE`: Deletes the specified resource
# `OPTIONS`: Request communication options available
# `PUT`: Replace the target resource
# `PATCH`: Apply partial modification to the resource
# `HEAD`: Identical to `GET` but no resource body should be returned
# `SUBMIT`: Submits a http request and returns an HttpFuture object
# `NONE`: No operation should be performed
type HttpOperation HTTP_FORWARD|HTTP_GET|HTTP_POST|HTTP_DELETE|HTTP_OPTIONS|HTTP_PUT|HTTP_PATCH|HTTP_HEAD|HTTP_SUBMIT|HTTP_NONE;

# Defines the possible values for the keep-alive configuration in service and client endpoints.
type KeepAlive KEEPALIVE_AUTO|KEEPALIVE_ALWAYS|KEEPALIVE_NEVER;

# Defines the possible values for the mutual ssl status.
# 
# `passed`: Mutual SSL handshake is successful.
# `failed`: Mutual SSL handshake has failed.
type MutualSslStatus PASSED|FAILED|();

# Defines the HTTP redirect codes as a type.
type RedirectCode REDIRECT_MULTIPLE_CHOICES_300|REDIRECT_MOVED_PERMANENTLY_301|REDIRECT_FOUND_302|REDIRECT_SEE_OTHER_303|REDIRECT_NOT_MODIFIED_304|REDIRECT_USE_PROXY_305|REDIRECT_TEMPORARY_REDIRECT_307|REDIRECT_PERMANENT_REDIRECT_308;

# A finite type for modeling the states of the Circuit Breaker. The Circuit Breaker starts in the `CLOSED` state.
# If any failure thresholds are exceeded during execution, the circuit trips and goes to the `OPEN` state. After
# the specified timeout period expires, the circuit goes to the `HALF_OPEN` state. If the trial request sent while
# in the `HALF_OPEN` state succeeds, the circuit goes back to the `CLOSED` state.
type CircuitState CB_OPEN_STATE|CB_HALF_OPEN_STATE|CB_CLOSED_STATE;

# Defines the possible values for the chunking configuration in HTTP services and clients.
# 
# `AUTO`: If the payload is less than 8KB, content-length header is set in the outbound request/response,
# otherwise chunking header is set in the outbound request/response
# `ALWAYS`: Always set chunking header in the response
# `NEVER`: Never set the chunking header even if the payload is larger than 8KB in the outbound request/response
type Chunking CHUNKING_AUTO|CHUNKING_ALWAYS|CHUNKING_NEVER;

# Options to compress using gzip or deflate.
# 
# `AUTO`: When service behaves as a HTTP gateway inbound request/response accept-encoding option is set as the
# outbound request/response accept-encoding/content-encoding option
# `ALWAYS`: Always set accept-encoding/content-encoding in outbound request/response
# `NEVER`: Never set accept-encoding/content-encoding header in outbound request/response
type Compression COMPRESSION_AUTO|COMPRESSION_ALWAYS|COMPRESSION_NEVER;

# Defines the position of the headers in the request/response.
# 
# `LEADING`: Header is placed before the payload of the request/response
# `TRAILING`: Header is placed after the payload of the request/response
type HeaderPosition LEADING|TRAILING;

# Represents OAuth2 grant configurations for OAuth2 authentication.
type OAuth2GrantConfig OAuth2ClientCredentialsGrantConfig|OAuth2PasswordGrantConfig|OAuth2RefreshTokenGrantConfig|OAuth2JwtBearerGrantConfig;

# Defines the authentication configurations for the HTTP client.
type ClientAuthConfig CredentialsConfig|BearerTokenConfig|JwtIssuerConfig|OAuth2GrantConfig;

# Defines the authentication configurations for the HTTP listener.
type ListenerAuthConfig FileUserStoreConfigWithScopes|LdapUserStoreConfigWithScopes|JwtValidatorConfigWithScopes|OAuth2IntrospectionConfigWithScopes;

# Defines the possible status code response record types.
type StatusCodeResponse Continue|SwitchingProtocols|Processing|EarlyHints|Ok|Created|Accepted|NonAuthoritativeInformation|NoContent|ResetContent|PartialContent|MultiStatus|AlreadyReported|IMUsed|MultipleChoices|MovedPermanently|Found|SeeOther|NotModified|UseProxy|TemporaryRedirect|PermanentRedirect|BadRequest|Unauthorized|PaymentRequired|Forbidden|NotFound|MethodNotAllowed|NotAcceptable|ProxyAuthenticationRequired|RequestTimeout|Conflict|Gone|LengthRequired|PreconditionFailed|PayloadTooLarge|UriTooLong|UnsupportedMediaType|RangeNotSatisfiable|ExpectationFailed|MisdirectedRequest|UnprocessableEntity|Locked|FailedDependency|TooEarly|PreconditionRequired|UnavailableDueToLegalReasons|UpgradeRequired|TooManyRequests|RequestHeaderFieldsTooLarge|InternalServerError|NotImplemented|BadGateway|ServiceUnavailable|GatewayTimeout|HttpVersionNotSupported|VariantAlsoNegotiates|InsufficientStorage|LoopDetected|NotExtended|NetworkAuthenticationRequired|DefaultStatusCodeResponse;

# Represents a non-error type that can be cloned.
type Cloneable (any&readonly)|xml|Cloneable[]|map<Cloneable>|table;

# Request context member type.
type ReqCtxMember Cloneable|isolated object {};

# The return type of an interceptor service function
type NextService RequestInterceptor|ResponseInterceptor|Service;

# Types of HTTP interceptor services
type Interceptor RequestInterceptor|ResponseInterceptor|RequestErrorInterceptor|ResponseErrorInterceptor;

# Defines the path parameter types.
type PathParamType boolean|int|float|decimal|string;

# Defines the possible simple query parameter types.
type SimpleQueryParamType boolean|int|float|decimal|string;

# Defines the query parameter type supported with client resource methods.
type QueryParamType SimpleQueryParamType[]|SimpleQueryParamType;

# Represents an error, which occurred due to a failure in interceptor return.
type InterceptorReturnError distinct ListenerError |httpscerr:InternalServerErrorError;

# Represents an error, which occurred due to a query parameter binding.
type QueryParameterBindingError distinct ListenerError |httpscerr:BadRequestError;

# Represents an error, which occurred due to a path parameter binding.
type PathParameterBindingError distinct ListenerError |httpscerr:BadRequestError;

# Defines the authentication error types that returned from listener.
type ListenerAuthnError distinct httpscerr:UnauthorizedError |ListenerAuthError;

# Defines the authorization error types that returned from listener.
type ListenerAuthzError distinct httpscerr:ForbiddenError |ListenerAuthError;

# Represents an error occurred in an remote function of the Load Balance connector.
type LoadBalanceActionError distinct ResiliencyError |error;

# Represents Service Not Found error.
type ServiceNotFoundError httpscerr:NotFoundError|ServiceDispatchingError;

# Represents Bad Matrix Parameter in the request error.
type BadMatrixParamError httpscerr:BadRequestError|ServiceDispatchingError;

# Represents an error, which occurred when the resource is not found during dispatching.
type ResourceNotFoundError httpscerr:NotFoundError|ResourceDispatchingError;

# Represents an error, which occurred due to a path parameter constraint validation.
type ResourcePathValidationError httpscerr:BadRequestError|ResourceDispatchingError;

# Represents an error, which occurred when the resource method is not allowed during dispatching.
type ResourceMethodNotAllowedError httpscerr:MethodNotAllowedError|ResourceDispatchingError;

# Represents an error, which occurred when the media type is not supported during dispatching.
type UnsupportedRequestMediaTypeError httpscerr:UnsupportedMediaTypeError|ResourceDispatchingError;

# Represents an error, which occurred when the payload is not acceptable during dispatching.
type RequestNotAcceptableError httpscerr:NotAcceptableError|ResourceDispatchingError;

# Represents other internal server errors during dispatching.
type ResourceDispatchingServerError httpscerr:InternalServerErrorError|ResourceDispatchingError;

# Represents the client status code binding error
type StatusCodeResponseBindingError distinct ClientError |error;

# Represents the status code binding error that occurred due to 4XX status code response binding
type StatusCodeBindingClientRequestError distinct StatusCodeResponseBindingError |ClientRequestError;

# Represents the status code binding error that occurred due to 5XX status code response binding
type StatusCodeBindingRemoteServerError distinct StatusCodeResponseBindingError |RemoteServerError;

// --- Client ---

# The caller actions for responding to client requests.
client class Caller {

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

    # Gets the hostname from the remote address. This method may trigger a DNS reverse lookup if the address was created
    # with a literal IP address.
    # ```ballerina
    # string? remoteHost = caller.getRemoteHostName();
    # ```
    function getRemoteHostName() returns string?;
}

# The HTTP client provides functionality to connect to remote HTTP services and perform requests using standard HTTP methods like GET, POST, PUT, DELETE, etc.
client class Client {
    function init(string url, ClientConfiguration config) returns ClientError?;

    # The client resource function to send HTTP GET requests to HTTP endpoints.
    resource function get [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # Retrieve a representation of a specified resource from an HTTP endpoint.
    remote function get(string path, map<string|string[]> headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP POST requests to HTTP endpoints.
    resource function post [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # Create a new resource or submit data to a resource for processing.
    remote function post(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP PUT requests to HTTP endpoints.
    resource function put [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # Create a new resource or replace a representation of a specified resource.
    remote function put(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP DELETE requests to HTTP endpoints.
    resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # Remove a specified resource from an HTTP endpoint.
    remote function delete(string path, RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP PATCH requests to HTTP endpoints.
    resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # Partially update an existing resource in an HTTP endpoint.
    remote function patch(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP HEAD requests to HTTP endpoints.
    resource function head [PathParamType ...path](map<string|string[]> headers = (), QueryParams params) returns Response|ClientError;

    # Get the metadata of a resource in the form of headers without the body. Often used for testing the resource existence or finding recent modifications.
    remote function head(string path, map<string|string[]> headers = ()) returns Response|ClientError;

    # The client resource function to send HTTP OPTIONS requests to HTTP endpoints.
    resource function options [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

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
}

# Defines the OAuth2 handler for client authentication.
client class ClientOAuth2Handler {
    function init(OAuth2GrantConfig config) returns nil;

    # Enrich the request with the relevant authentication requirements.
    remote function enrich(Request req) returns Request|ClientAuthError;

    # Enrich the headers map with the relevant authentication requirements.
    function enrichHeaders(map<string|string[]> headers) returns map<string|string[]>|ClientAuthError;

    # Returns the headers map with the relevant authentication requirements.
    function getSecurityHeaders() returns map<string|string[]>|ClientAuthError;
}

# An HTTP client endpoint which provides failover support over multiple HTTP clients.
client class FailoverClient {
    function init(FailoverClientConfiguration failoverClientConfig) returns ClientError?;

    # The POST resource function implementation of the Failover Connector.
    resource function post [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # The POST remote function implementation of the Failover Connector.
    remote function post(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The PUT resource function implementation of the Failover Connector.
    resource function put [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # The PUT remote function  implementation of the Failover Connector.
    remote function put(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The PATCH resource function implementation of the Failover Connector.
    resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # The PATCH remote function implementation of the Failover Connector.
    remote function patch(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The DELETE resource function implementation of the Failover Connector.
    resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # The DELETE remote function implementation of the Failover Connector.
    remote function delete(string path, RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The HEAD resource function implementation of the Failover Connector.
    resource function head [PathParamType ...path](map<string|string[]> headers = (), QueryParams params) returns Response|ClientError;

    # The HEAD remote function implementation of the Failover Connector.
    remote function head(string path, map<string|string[]> headers = ()) returns Response|ClientError;

    # The GET resource function implementation of the Failover Connector.
    resource function get [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # The GET remote function implementation of the Failover Connector.
    remote function get(string path, map<string|string[]> headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # The OPTIONS resource function implementation of the Failover Connector.
    resource function options [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

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

    # Gets the index of the `TargetService[]` array which given a successful response.
    function getSucceededEndpointIndex() returns int;
}

# Defines the LDAP store Basic Auth handler for listener authentication.
client class ListenerLdapUserStoreBasicAuthHandler {
    function init(LdapUserStoreConfig config) returns nil;

    # Authenticates with the relevant authentication requirements.
    remote function authenticate(Request|Headers|string data) returns auth:UserDetails|Unauthorized; // Special Agent Note: UserDetails FROM ballerina/auth package

    # Authorizes with the relevant authorization requirements.
    remote function authorize(auth:UserDetails userDetails, string|string[] expectedScopes) returns Forbidden?; // Special Agent Note: UserDetails FROM ballerina/auth package
}

# Defines the OAuth2 handler for listener authentication.
client class ListenerOAuth2Handler {
    function init(OAuth2IntrospectionConfig config) returns nil;

    # Authorizes with the relevant authentication & authorization requirements.
    remote function authorize(Request|Headers|string data, string|string[]? expectedScopes = (), map<string> optionalParams = ()) returns oauth2:IntrospectionResponse|Unauthorized|Forbidden; // Special Agent Note: IntrospectionResponse FROM ballerina/oauth2 package
}

# LoadBalanceClient endpoint provides load balancing functionality over multiple HTTP clients.
client class LoadBalanceClient {
    function init(LoadBalanceClientConfiguration loadBalanceClientConfig) returns ClientError?;

    # The POST resource function implementation of the LoadBalancer Connector.
    resource function post [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # The POST remote function implementation of the LoadBalancer Connector.
    remote function post(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The PUT resource function implementation of the LoadBalancer Connector.
    resource function put [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # The PUT remote function implementation of the Load Balance Connector.
    remote function put(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The PATCH resource function implementation of the LoadBalancer Connector.
    resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # The PATCH remote function implementation of the LoadBalancer Connector.
    remote function patch(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The DELETE resource function implementation of the LoadBalancer Connector.
    resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # The DELETE remote function implementation of the LoadBalancer Connector.
    remote function delete(string path, RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The HEAD resource function implementation of the LoadBalancer Connector.
    resource function head [PathParamType ...path](map<string|string[]> headers = (), QueryParams params) returns Response|ClientError;

    # The HEAD remote function implementation of the LoadBalancer Connector.
    remote function head(string path, map<string|string[]> headers = ()) returns Response|ClientError;

    # The GET resource function implementation of the LoadBalancer Connector.
    resource function get [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

    # The GET remote function implementation of the LoadBalancer Connector.
    remote function get(string path, map<string|string[]> headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # The OPTIONS resource function implementation of the LoadBalancer Connector.
    resource function options [PathParamType ...path](map<string|string[]> headers = (), TargetType targetType = <>, QueryParams params) returns targetType|ClientError;

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
}

# The HTTP status code client provides the capability for initiating contact with a remote HTTP service. The API it
# provides includes the functions for the standard HTTP methods forwarding a received request and sending requests
# using custom HTTP verbs. The responses can be binded to `http:StatusCodeResponse` types
client class StatusCodeClient {
    function init(string url, ClientConfiguration config) returns ClientError?;

    # The client resource function to send HTTP POST requests to HTTP endpoints.
    resource function post [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

    # The `Client.post()` function can be used to send HTTP POST requests to HTTP endpoints.
    remote function post(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP PUT requests to HTTP endpoints.
    resource function put [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

    # The `Client.put()` function can be used to send HTTP PUT requests to HTTP endpoints.
    remote function put(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP PATCH requests to HTTP endpoints.
    resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

    # The `Client.patch()` function can be used to send HTTP PATCH requests to HTTP endpoints.
    remote function patch(string path, RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP DELETE requests to HTTP endpoints.
    resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

    # The `Client.delete()` function can be used to send HTTP DELETE requests to HTTP endpoints.
    remote function delete(string path, RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP HEAD requests to HTTP endpoints.
    resource function head [PathParamType ...path](map<string|string[]> headers = (), QueryParams params) returns Response|ClientError;

    # The `Client.head()` function can be used to send HTTP HEAD requests to HTTP endpoints.
    remote function head(string path, map<string|string[]> headers = ()) returns Response|ClientError;

    # The client resource function to send HTTP GET requests to HTTP endpoints.
    resource function get [PathParamType ...path](map<string|string[]> headers = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

    # The `Client.get()` function can be used to send HTTP GET requests to HTTP endpoints.
    remote function get(string path, map<string|string[]> headers = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP OPTIONS requests to HTTP endpoints.
    resource function options [PathParamType ...path](map<string|string[]> headers = (), typedesc<StatusCodeResponse> targetType = <>, QueryParams params) returns targetType|ClientError;

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
}

// --- Functions ---

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

// --- Service ---

// --- Service (generic) ---
// Listener: Listener(int port)
// Instructions:

// --- Annotations ---

# The annotation which is used to configure an HTTP resource.
public annotation ResourceConfig on service_function;

# The annotation which is used to configure an HTTP service.
public annotation ServiceConfig on service;
