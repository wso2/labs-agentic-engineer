// ============================================================
// Library: ballerina/http
// This module provides APIs for connecting and interacting with HTTP and HTTP2 endpoints. It facilitates two types of network entry points as the `Client` and `Listener`.
// ============================================================
import ballerina/http;

// --- Types ---

# The status code response record of `Accepted`.
public type Accepted record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusAccepted status = STATUS_ACCEPTED_OBJ; // Special Agent Note: the default STATUS_ACCEPTED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents HTTP access log configuration.
public type AccessLogConfiguration record {|
    # Enable or disable console access logs
    boolean console = false;
    # The format of access logs to be printed (either `flat` or `json`)
    string format = "flat";
    # The list of attributes of access logs to be printed
    string[] attributes?;
    # File path to store access logs
    @deprecated
    string path?;
    # Log file configuration to store access logs
    LogFileConfig file?;
|};

# The status code response record of `AlreadyReported`.
public type AlreadyReported record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusAlreadyReported status = STATUS_ALREADY_REPORTED_OBJ; // Special Agent Note: the default STATUS_ALREADY_REPORTED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `BadGateway`.
public type BadGateway record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusBadGateway status = STATUS_BAD_GATEWAY_OBJ; // Special Agent Note: the default STATUS_BAD_GATEWAY_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `BadRequest`.
public type BadRequest record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusBadRequest status = STATUS_BAD_REQUEST_OBJ; // Special Agent Note: the default STATUS_BAD_REQUEST_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents token for Bearer token authentication.
public type BearerTokenConfig record {|
    # Bearer token for authentication
    string token;
|};

# Represents a discrete sub-part of the time window (Bucket).
public type Bucket record {|
    # Total number of requests received during the sub-window time frame
    int totalCount = 0;
    # Number of failed requests during the sub-window time frame
    int failureCount = 0;
    # Number of rejected requests during the sub-window time frame
    int rejectedCount = 0;
    # The time that the `Bucket` is last updated.
    time:Utc lastUpdatedTime?; // Special Agent Note: Utc FROM ballerina/time module
|};

# Provides a set of configurations for controlling the caching behaviour of the endpoint.
public type CacheConfig record {|
    # Specifies whether HTTP caching is enabled. Caching is enabled by default.
    boolean enabled = true;
    # Specifies whether the HTTP caching layer should behave as a public cache or a private cache
    boolean isShared = false;
    # The capacity of the cache
    int capacity = 16;
    # The fraction of entries to be removed when the cache is full. The value should be
    # between 0 (exclusive) and 1 (inclusive).
    float evictionFactor = 0.2;
    # Gives the user some control over the caching behaviour. By default, this is set to
    # `CACHE_CONTROL_AND_VALIDATORS`. The default behaviour is to allow caching only when the `cache-control`
    # header and either the `etag` or `last-modified` header are present.
    CachingPolicy policy = CACHE_CONTROL_AND_VALIDATORS;
|};

# Represents combination of certificate, private key and private key password if encrypted.
public type CertKey record {|
    # A file containing the certificate
    string certFile;
    # A file containing the private key in PKCS8 format
    string keyFile;
    # Password of the private key if it is encrypted
    string keyPassword?;
|};

# Provides a set of configurations for controlling the behaviour of the Circuit Breaker.
public type CircuitBreakerConfig record {|
    # The `http:RollingWindow` options of the `CircuitBreaker`
    RollingWindow rollingWindow = {};
    # The threshold for request failures. When this threshold exceeds, the circuit trips. The threshold should be a
    # value between 0 and 1
    float failureThreshold = 0.0;
    # The time period (in seconds) to wait before attempting to make another request to the upstream service
    decimal resetTime = 0;
    # Array of HTTP response status codes which are considered as failures
    int[] statusCodes = [];
|};

# Derived set of configurations from the `CircuitBreakerConfig`.
public type CircuitBreakerInferredConfig record {|
    # The threshold for request failures. When this threshold exceeds, the circuit trips.
    # The threshold should be a value between 0 and 1
    float failureThreshold = 0.0;
    # The time period (in seconds) to wait before attempting to make another request to
    # the upstream service
    decimal resetTime = 0;
    # Array of HTTP response status codes which are considered as failures
    int[] statusCodes = [];
    # Number of buckets derived from the `RollingWindow`
    int noOfBuckets = 0;
    # The `http:RollingWindow` options provided in the `http:CircuitBreakerConfig`
    RollingWindow rollingWindow = {};
|};

# Maintains the health of the Circuit Breaker.
public type CircuitHealth record {|
    # Whether last request is success or not
    boolean lastRequestSuccess = false;
    # Total request count received within the `RollingWindow`
    int totalRequestCount = 0;
    # ID of the last bucket used in Circuit Breaker calculations
    int lastUsedBucketId = 0;
    # Circuit Breaker start time
    time:Utc startTime = time:utcNow(); // Special Agent Note: Utc FROM ballerina/time module
    # The time that the last request received
    time:Utc lastRequestTime?; // Special Agent Note: Utc FROM ballerina/time module
    # The time that the last error occurred
    time:Utc lastErrorTime?; // Special Agent Note: Utc FROM ballerina/time module
    # The time that circuit forcefully opened at last
    time:Utc lastForcedOpenTime?; // Special Agent Note: Utc FROM ballerina/time module
    # The discrete time buckets into which the time window is divided
    Bucket?[] totalBuckets = [];
|};

# Provides a set of configurations for controlling the behaviours when communicating with a remote HTTP endpoint.
# The following fields are inherited from the other configuration records in addition to the `Client`-specific
# configs.
public type ClientConfiguration record {|
    *CommonClientConfiguration;
    # SSL/TLS security settings for HTTPS connections
    ClientSecureSocket? secureSocket = ();
|};

# Provides settings related to HTTP/1.x protocol.
public type ClientHttp1Settings record {|
    # Specifies whether to reuse a connection for multiple requests
    KeepAlive keepAlive = KEEPALIVE_AUTO;
    # The chunking behaviour of the request
    Chunking chunking = CHUNKING_AUTO;
    # Proxy server related options
    ProxyConfig? proxy = ();
|};

# Provides settings related to HTTP/2 protocol.
public type ClientHttp2Settings record {|
    # Configuration to enable HTTP/2 prior knowledge
    boolean http2PriorKnowledge = false;
    # Configuration to change the initial window size
    int http2InitialWindowSize = 65535;
|};

# Provides configurations for facilitating secure communication with a remote HTTP endpoint.
public type ClientSecureSocket record {|
    # Enable SSL validation
    boolean enable = true;
    # Configurations associated with `crypto:TrustStore` or single certificate file that the client trusts
    crypto:TrustStore|string cert?; // Special Agent Note: TrustStore FROM ballerina/crypto module
    # Configurations associated with `crypto:KeyStore` or combination of certificate and private key of the client
    crypto:KeyStore|CertKey key?; // Special Agent Note: KeyStore FROM ballerina/crypto module
    # SSL/TLS protocol related options
    record {Protocol name; string[] versions; } protocol?;
    # Certificate validation against OCSP_CRL, OCSP_STAPLING related options
    record {CertValidationType 'type; int cacheSize; int cacheValidityPeriod; } certValidation?;
    # List of ciphers to be used
    # eg: TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA
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
|};

# Provides settings related to client socket configuration.
public type ClientSocketConfig record {|
    # Connect timeout of the channel in seconds. If the Channel does not support connect operation,
    # this property is not used at all, and therefore will be ignored.
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
|};

# Common client configurations for the next level clients.
public type CommonClientConfiguration record {|
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
    # When enabled:
    # - `null` values in JSON are allowed to be mapped to optional fields
    # - missing fields in JSON are allowed to be mapped as `null` values
    boolean laxDataBinding = false;
|};

# The common attributed of response status code record type.
public type CommonResponse record {|
    # The value of response `Content-type` header
    string mediaType?;
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers?;
    # The response payload
    anydata body?;
|};

# A record for providing configurations for content compression.
public type CompressionConfig record {|
    # The status of compression
    Compression enable = COMPRESSION_AUTO;
    # Content types which are allowed for compression
    string[] contentTypes = [];
|};

# The status code response record of `Conflict`.
public type Conflict record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusConflict status = STATUS_CONFLICT_OBJ; // Special Agent Note: the default STATUS_CONFLICT_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `Continue`.
public type Continue record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusContinue status = STATUS_CONTINUE_OBJ; // Special Agent Note: the default STATUS_CONTINUE_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Client configuration for cookies.
public type CookieConfig record {|
    # User agents provide users with a mechanism for disabling or enabling cookies
    boolean enabled = false;
    # Maximum number of cookies per domain, which is 50
    int maxCookiesPerDomain = 50;
    # Maximum number of total cookies allowed to be stored in cookie store, which is 3000
    int maxTotalCookieCount = 3000;
    # User can block cookies from third party responses and refuse to send cookies for third party requests, if needed
    boolean blockThirdPartyCookies = true;
    # To manage persistent cookies, users are provided with a mechanism for specifying a persistent cookie store with their own mechanism
    # which references the persistent cookie handler or specifying the CSV persistent cookie handler. If not specified any, only the session cookies are used
    PersistentCookieHandler persistentCookieHandler?;
|};

# The options to be used when initializing the `http:Cookie`.
public type CookieOptions record {|
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
    time:Utc createdTime = time:utcNow(); // Special Agent Note: Utc FROM ballerina/time module
    # Last-accessed time of the cookie
    time:Utc lastAccessedTime = time:utcNow(); // Special Agent Note: Utc FROM ballerina/time module
    # Cookie is sent only to the requested host
    boolean hostOnly = false;
|};

# Configurations for CORS support.
public type CorsConfig record {|
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
|};

# The status code response record of `Created`.
public type Created record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusCreated status = STATUS_CREATED_OBJ; // Special Agent Note: the default STATUS_CREATED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents credentials for Basic Auth authentication.
public type CredentialsConfig record {|
    *auth:CredentialsConfig; // Special Agent Note: CredentialsConfig FROM ballerina/auth module
|};

# The default status code response record.
public type DefaultStatusCodeResponse record {|
    *CommonResponse;
    # The response status code object
    readonly DefaultStatus status;
|};

# Represents the details of an HTTP error.
public type Detail record {
    # The inbound error response status code
    int statusCode;
    # The inbound error response headers
    map<string[]> headers;
    # The inbound error response body
    anydata body;
};

# The status code response record of `EarlyHints`.
public type EarlyHints record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusEarlyHints status = STATUS_EARLY_HINTS_OBJ; // Special Agent Note: the default STATUS_EARLY_HINTS_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents the structure of the HTTP error payload.
public type ErrorPayload record {
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
public type ExpectationFailed record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusExpectationFailed status = STATUS_EXPECTATION_FAILED_OBJ; // Special Agent Note: the default STATUS_EXPECTATION_FAILED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `FailedDependency`.
public type FailedDependency record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusFailedDependency status = STATUS_FAILED_DEPENDENCY_OBJ; // Special Agent Note: the default STATUS_FAILED_DEPENDENCY_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Provides a set of HTTP related configurations and failover related configurations.
# The following fields are inherited from the other configuration records in addition to the failover client-specific
# configs.
public type FailoverClientConfiguration record {|
    *CommonClientConfiguration;
    # The upstream HTTP endpoints among which the incoming HTTP traffic load should be sent on failover
    TargetService[] targets = [];
    # Array of HTTP response status codes for which the failover behaviour should be triggered
    int[] failoverCodes = [501, 502, 503, 504];
    # Failover delay interval in seconds
    decimal interval = 0;
|};

# Represents file user store configurations for Basic Auth authentication.
public type FileUserStoreConfig record {|
|};

# Represents the auth annotation for file user store configurations with scopes.
public type FileUserStoreConfigWithScopes record {|
    # File user store configurations for Basic Auth authentication
    FileUserStoreConfig fileUserStoreConfig;
    # Scopes allowed for authorization
    string|string[] scopes?;
|};

# Provides configurations for controlling the endpoint's behaviour in response to HTTP redirect related responses.
# The response status codes of 301, 302, and 303 are redirected using a GET request while 300, 305, 307, and 308
# status codes use the original request HTTP method during redirection.
public type FollowRedirects record {|
    # Enable/disable redirection
    boolean enabled = false;
    # Maximum number of redirects to follow
    int maxCount = 5;
    # By default Authorization and Proxy-Authorization headers are removed from the redirect requests.
    # Set it to true if Auth headers are needed to be sent during the redirection
    boolean allowAuthHeaders = false;
|};

# The status code response record of `Forbidden`.
public type Forbidden record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusForbidden status = STATUS_FORBIDDEN_OBJ; // Special Agent Note: the default STATUS_FORBIDDEN_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `Found`.
public type Found record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusFound status = STATUS_FOUND_OBJ; // Special Agent Note: the default STATUS_FOUND_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `GatewayTimeout`.
public type GatewayTimeout record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusGatewayTimeout status = STATUS_GATEWAY_TIMEOUT_OBJ; // Special Agent Note: the default STATUS_GATEWAY_TIMEOUT_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `Gone`.
public type Gone record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusGone status = STATUS_GONE_OBJ; // Special Agent Note: the default STATUS_GONE_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents the parsed header value details
public type HeaderValue record {|
    # The header value
    string value;
    # Map of header parameters
    map<string> params;
|};

# Defines the HTTP response cache configuration. By default the `no-cache` directive is setted to the `cache-control`
# header. In addition to that `etag` and `last-modified` headers are also added for cache validation.
public type HttpCacheConfig record {|
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
    # must be validated with the origin server
    string[] noCacheFields = [];
    # Optional fields for the `private` directive. A cache can omit the fields specified and store
    # the rest of the response
    string[] privateFields = [];
    # Sets the `etag` header for the given payload
    boolean setETag = true;
    # Sets the current time as the `last-modified` header
    boolean setLastModified = true;
|};

# Configures the typing details type of the Caller resource signature parameter.
public type HttpCallerInfo record {|
    # Specifies the type of response
    typedesc<ResponseMessage|StatusCodeResponse|Error> respondType?;
|};

# Defines the Header resource signature parameter.
public type HttpHeader record {|
    # Specifies the name of the required header
    string name?;
|};

# Defines the Payload resource signature parameter and return parameter.
public type HttpPayload record {|
    # Specifies the allowed media types of the corresponding payload type
    string|string[] mediaType?;
|};

# Defines the query resource signature parameter.
public type HttpQuery record {|
    # Specifies the name of the query parameter
    string name?;
|};

# Configuration for an HTTP resource.
public type HttpResourceConfig record {|
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
|};

# Contains the configurations for an HTTP service.
public type HttpServiceConfig record {|
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
    # When enabled, the JSON data will be projected to the Ballerina record type and during the projection,
    # nil values will be considered as optional fields and absent fields will be considered for nilable types
    boolean laxDataBinding = false;
|};

# The status code response record of `HttpVersionNotSupported`.
public type HttpVersionNotSupported record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusHttpVersionNotSupported status = STATUS_HTTP_VERSION_NOT_SUPPORTED_OBJ; // Special Agent Note: the default STATUS_HTTP_VERSION_NOT_SUPPORTED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `IMUsed`.
public type IMUsed record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusIMUsed status = STATUS_IM_USED_OBJ; // Special Agent Note: the default STATUS_IM_USED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Provides a set of cloneable configurations for HTTP listener.
public type InferredListenerConfiguration record {|
    # The host name/IP of the endpoint
    string host;
    # Configurations related to HTTP/1.x protocol
    ListenerHttp1Settings http1Settings;
    # The SSL configurations for the service endpoint. This needs to be configured in order to
    # communicate through HTTPS.
    ListenerSecureSocket? secureSocket;
    # Highest HTTP version supported by the endpoint
    HttpVersion httpVersion;
    # Period of time in seconds that a connection waits for a read/write operation. Use value 0 to
    # disable timeout
    decimal timeout;
    # The server name which should appear as a response header
    string? server;
    # Configurations associated with inbound request size limits
    RequestLimitConfigs requestLimits;
|};

# The status code response record of `InsufficientStorage`.
public type InsufficientStorage record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusInsufficientStorage status = STATUS_INSUFFICIENT_STORAGE_OBJ; // Special Agent Note: the default STATUS_INSUFFICIENT_STORAGE_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `InternalServerError`.
public type InternalServerError record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusInternalServerError status = STATUS_INTERNAL_SERVER_ERROR_OBJ; // Special Agent Note: the default STATUS_INTERNAL_SERVER_ERROR_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents JWT issuer configurations for JWT authentication.
public type JwtIssuerConfig record {|
    *jwt:IssuerConfig; // Special Agent Note: IssuerConfig FROM ballerina/jwt module
|};

# Represents JWT validator configurations for JWT authentication.
public type JwtValidatorConfig record {|
    *jwt:ValidatorConfig; // Special Agent Note: ValidatorConfig FROM ballerina/jwt module
    # The key used to fetch the scopes
    string scopeKey = "scope";
|};

# Represents the auth annotation for JWT validator configurations with scopes.
public type JwtValidatorConfigWithScopes record {|
    # JWT validator configurations for JWT authentication
    JwtValidatorConfig jwtValidatorConfig;
    # Scopes allowed for authorization
    string|string[] scopes?;
|};

# Represents LDAP user store configurations for Basic Auth authentication.
public type LdapUserStoreConfig record {|
    *auth:LdapUserStoreConfig; // Special Agent Note: LdapUserStoreConfig FROM ballerina/auth module
|};

# Represents the auth annotation for LDAP user store configurations with scopes.
public type LdapUserStoreConfigWithScopes record {|
    # LDAP user store configurations for Basic Auth authentication
    LdapUserStoreConfig ldapUserStoreConfig;
    # Scopes allowed for authorization
    string|string[] scopes?;
|};

# The status code response record of `LengthRequired`.
public type LengthRequired record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusLengthRequired status = STATUS_LENGTH_REQUIRED_OBJ; // Special Agent Note: the default STATUS_LENGTH_REQUIRED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents a server-provided hyperlink
public type Link record {
    # Names the relationship of the linked target to the current representation
    string rel?;
    # Target URL
    string href;
    # Expected resource representation media types
    string[] types?;
    # Allowed resource methods
    Method[] methods?;
};

public type LinkedTo record {|
    # Name of the linked resource
    string name;
    # Name of the relationship between the linked resource and the current resource.
    # Defaulted to the IANA link relation `self`
    string relation = "self";
    # Method allowed in the linked resource
    string method?;
|};

# Represents available server-provided links
public type Links record {|
    # Map of available links
    map<Link> _links;
|};

# Provides a set of configurations for HTTP service endpoints.
public type ListenerConfiguration record {|
    # The host name/IP of the endpoint
    string host = "0.0.0.0";
    # Configurations related to HTTP/1.x protocol
    ListenerHttp1Settings http1Settings = {};
    # The SSL configurations for the service endpoint. This needs to be configured in order to
    # communicate through HTTPS.
    ListenerSecureSocket? secureSocket = ();
    # Highest HTTP version supported by the endpoint
    HttpVersion httpVersion = HTTP_2_0;
    # Period of time in seconds that a connection waits for a read/write operation. Use value 0 to
    # disable timeout
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
    # This only applies for HTTP/2. Default value is 5 minutes. If the value is set to -1,
    # the connection will be closed after all in-flight streams are completed
    decimal minIdleTimeInStaleState = 300;
    # Time between the connection stale eviction runs in seconds. This only applies for HTTP/2.
    # Default value is 30 seconds
    decimal timeBetweenStaleEviction = 30;
|};

# Provides settings related to HTTP/1.x protocol.
public type ListenerHttp1Settings record {|
    # Can be set to either `KEEPALIVE_AUTO`, which respects the `connection` header, or `KEEPALIVE_ALWAYS`,
    # which always keeps the connection alive, or `KEEPALIVE_NEVER`, which always closes the connection
    KeepAlive keepAlive = KEEPALIVE_AUTO;
    # Defines the maximum number of requests that can be processed at a given time on a single
    # connection. By default 10 requests can be pipelined on a single connection and user can
    # change this limit appropriately.
    int maxPipelinedRequests = MAX_PIPELINED_REQUESTS; // Special Agent Note: the default MAX_PIPELINED_REQUESTS is not exported by this package; omit the argument rather than repeating it
|};

# Configures the SSL/TLS options to be used for HTTP service.
public type ListenerSecureSocket record {|
    # Configurations associated with `crypto:KeyStore` or combination of certificate and (PKCS8) private key of the server
    crypto:KeyStore|CertKey key; // Special Agent Note: KeyStore FROM ballerina/crypto module
    # Configures associated with mutual SSL operations
    record {VerifyClient verifyClient; crypto:TrustStore|string cert; } mutualSsl?;
    # SSL/TLS protocol related options
    record {Protocol name; string[] versions; } protocol?;
    # Certificate validation against OCSP_CRL, OCSP_STAPLING related options
    record {CertValidationType 'type; int cacheSize; int cacheValidityPeriod; } certValidation?;
    # List of ciphers to be used
    # eg: TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA
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
|};

# Represents the details of the `LoadBalanceActionError`.
public type LoadBalanceActionErrorData record {
    # Array of errors occurred at each endpoint
    error[] httpActionErr?;
};

# The configurations related to the load balancing client endpoint. The following fields are inherited from the other
# configuration records in addition to the load balancing client specific configs.
public type LoadBalanceClientConfiguration record {|
    *CommonClientConfiguration;
    # The upstream HTTP endpoints among which the incoming HTTP traffic load should be distributed
    TargetService[] targets = [];
    # The `LoadBalancing` rule
    LoadBalancerRule? lbRule = ();
    # Configuration for the load balancer whether to fail over a failure
    boolean failover = true;
|};

# Presents a read-only view of the local address.
public type Local record {|
    # The local host name
    string host;
    # The local port
    int port;
    # The local IP address
    string ip;
|};

# The status code response record of `Locked`.
public type Locked record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusLocked status = STATUS_LOCKED_OBJ; // Special Agent Note: the default STATUS_LOCKED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents HTTP access log file configuration.
public type LogFileConfig record {|
    # The file path to store access logs
    string path;
    # The log rotation configuration for file destinations
    log:RotationConfig rotation?; // Special Agent Note: RotationConfig FROM ballerina/log module
|};

# The status code response record of `LoopDetected`.
public type LoopDetected record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusLoopDetected status = STATUS_LOOP_DETECTED_OBJ; // Special Agent Note: the default STATUS_LOOP_DETECTED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `MethodNotAllowed`.
public type MethodNotAllowed record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusMethodNotAllowed status = STATUS_METHOD_NOT_ALLOWED_OBJ; // Special Agent Note: the default STATUS_METHOD_NOT_ALLOWED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `MisdirectedRequest`.
public type MisdirectedRequest record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusMisdirectedRequest status = STATUS_MISDIRECTED_REQUEST_OBJ; // Special Agent Note: the default STATUS_MISDIRECTED_REQUEST_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `MovedPermanently`.
public type MovedPermanently record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusMovedPermanently status = STATUS_MOVED_PERMANENTLY_OBJ; // Special Agent Note: the default STATUS_MOVED_PERMANENTLY_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `MultipleChoices`.
public type MultipleChoices record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusMultipleChoices status = STATUS_MULTIPLE_CHOICES_OBJ; // Special Agent Note: the default STATUS_MULTIPLE_CHOICES_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `MultiStatus`.
public type MultiStatus record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusMultiStatus status = STATUS_MULTI_STATUS_OBJ; // Special Agent Note: the default STATUS_MULTI_STATUS_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# A record for providing mutual SSL handshake results.
public type MutualSslHandshake record {|
    # Status of the handshake.
    MutualSslStatus status = ();
    # Base64 encoded certificate.
    string? base64EncodedCert = ();
|};

# The status code response record of `NetworkAuthenticationRequired`.
public type NetworkAuthenticationRequired record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusNetworkAuthenticationRequired status = STATUS_NETWORK_AUTHENTICATION_REQUIRED_OBJ; // Special Agent Note: the default STATUS_NETWORK_AUTHENTICATION_REQUIRED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `NetworkAuthorizationRequired`.
@deprecated
public type NetworkAuthorizationRequired record {|
    *CommonResponse;
    # The response status code obj
    # # Deprecated
    # This record is deprecated. Please use `NetworkAuthenticationRequired` instead.
    readonly StatusNetworkAuthenticationRequired status = STATUS_NETWORK_AUTHENTICATION_REQUIRED_OBJ; // Special Agent Note: the default STATUS_NETWORK_AUTHENTICATION_REQUIRED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `NoContent`.
public type NoContent record {|
    # The response headers
    map<string|int|boolean|string[]|int[]|boolean[]> headers?;
    # The response status code obj
    readonly StatusNoContent status = STATUS_NO_CONTENT_OBJ; // Special Agent Note: the default STATUS_NO_CONTENT_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `NonAuthoritativeInformation`.
public type NonAuthoritativeInformation record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusNonAuthoritativeInformation status = STATUS_NON_AUTHORITATIVE_INFORMATION_OBJ; // Special Agent Note: the default STATUS_NON_AUTHORITATIVE_INFORMATION_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `NotAcceptable`.
public type NotAcceptable record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusNotAcceptable status = STATUS_NOT_ACCEPTABLE_OBJ; // Special Agent Note: the default STATUS_NOT_ACCEPTABLE_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `NotExtended`.
public type NotExtended record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusNotExtended status = STATUS_NOT_EXTENDED_OBJ; // Special Agent Note: the default STATUS_NOT_EXTENDED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `NotFound`.
public type NotFound record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusNotFound status = STATUS_NOT_FOUND_OBJ; // Special Agent Note: the default STATUS_NOT_FOUND_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `NotImplemented`.
public type NotImplemented record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusNotImplemented status = STATUS_NOT_IMPLEMENTED_OBJ; // Special Agent Note: the default STATUS_NOT_IMPLEMENTED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `NotModified`.
public type NotModified record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusNotModified status = STATUS_NOT_MODIFIED_OBJ; // Special Agent Note: the default STATUS_NOT_MODIFIED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents OAuth2 client credentials grant configurations for OAuth2 authentication.
public type OAuth2ClientCredentialsGrantConfig record {|
    *oauth2:ClientCredentialsGrantConfig; // Special Agent Note: ClientCredentialsGrantConfig FROM ballerina/oauth2 module
|};

# Represents OAuth2 introspection server configurations for OAuth2 authentication.
public type OAuth2IntrospectionConfig record {|
    *oauth2:IntrospectionConfig; // Special Agent Note: IntrospectionConfig FROM ballerina/oauth2 module
    # The key used to fetch the scopes
    string scopeKey = "scope";
|};

# Represents the auth annotation for OAuth2 introspection server configurations with scopes.
public type OAuth2IntrospectionConfigWithScopes record {|
    # OAuth2 introspection server configurations for OAuth2 authentication
    OAuth2IntrospectionConfig oauth2IntrospectionConfig;
    # Scopes allowed for authorization
    string|string[] scopes?;
|};

# Represents OAuth2 JWT bearer grant configurations for OAuth2 authentication.
public type OAuth2JwtBearerGrantConfig record {|
    *oauth2:JwtBearerGrantConfig; // Special Agent Note: JwtBearerGrantConfig FROM ballerina/oauth2 module
|};

# Represents OAuth2 password grant configurations for OAuth2 authentication.
public type OAuth2PasswordGrantConfig record {|
    *oauth2:PasswordGrantConfig; // Special Agent Note: PasswordGrantConfig FROM ballerina/oauth2 module
|};

# Represents OAuth2 refresh token grant configurations for OAuth2 authentication.
public type OAuth2RefreshTokenGrantConfig record {|
    *oauth2:RefreshTokenGrantConfig; // Special Agent Note: RefreshTokenGrantConfig FROM ballerina/oauth2 module
|};

# The status code response record of `Ok`.
public type Ok record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusOK status = STATUS_OK_OBJ; // Special Agent Note: the default STATUS_OK_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `PartialContent`.
public type PartialContent record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusPartialContent status = STATUS_PARTIAL_CONTENT_OBJ; // Special Agent Note: the default STATUS_PARTIAL_CONTENT_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `PayloadTooLarge`.
public type PayloadTooLarge record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusPayloadTooLarge status = STATUS_PAYLOAD_TOO_LARGE_OBJ; // Special Agent Note: the default STATUS_PAYLOAD_TOO_LARGE_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `PaymentRequired`.
public type PaymentRequired record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusPaymentRequired status = STATUS_PAYMENT_REQUIRED_OBJ; // Special Agent Note: the default STATUS_PAYMENT_REQUIRED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `PermanentRedirect`.
public type PermanentRedirect record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusPermanentRedirect status = STATUS_PERMANENT_REDIRECT_OBJ; // Special Agent Note: the default STATUS_PERMANENT_REDIRECT_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Configurations for managing HTTP client connection pool.
public type PoolConfiguration record {|
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
    # This only applies for HTTP/2. Default value is 5 minutes. If the value is set to -1,
    # the connection will be closed after all in-flight streams are completed
    decimal minIdleTimeInStaleState = minIdleTimeInStaleState;
    # Time between the connection stale eviction runs in seconds. This only applies for HTTP/2.
    # Default value is 30 seconds
    decimal timeBetweenStaleEviction = timeBetweenStaleEviction;
|};

# The status code response record of `PreconditionFailed`.
public type PreconditionFailed record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusPreconditionFailed status = STATUS_PRECONDITION_FAILED_OBJ; // Special Agent Note: the default STATUS_PRECONDITION_FAILED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `PreconditionRequired`.
public type PreconditionRequired record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusPreconditionRequired status = STATUS_PRECONDITION_REQUIRED_OBJ; // Special Agent Note: the default STATUS_PRECONDITION_REQUIRED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `Processing`.
public type Processing record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusProcessing status = STATUS_PROCESSING_OBJ; // Special Agent Note: the default STATUS_PROCESSING_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `ProxyAuthenticationRequired`.
public type ProxyAuthenticationRequired record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusProxyAuthenticationRequired status = STATUS_PROXY_AUTHENTICATION_REQUIRED_OBJ; // Special Agent Note: the default STATUS_PROXY_AUTHENTICATION_REQUIRED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Proxy server configurations to be used with the HTTP client endpoint.
public type ProxyConfig record {|
    # Host name of the proxy server
    string host = "";
    # Proxy server port
    int port = 0;
    # Proxy server username
    string userName = "";
    # proxy server password
    string password = "";
|};

# Defines the record type of query parameters supported with client resource methods.
public type QueryParams record {|
    # headers which cannot be used as a query field
    never headers?;
    # targetType which cannot be used as a query field
    never targetType?;
    # message which cannot be used as a query field
    never message?;
    # mediaType which cannot be used as a query field
    never mediaType?;
    QueryParamType...;
|};

# The status code response record of `RangeNotSatisfiable`.
public type RangeNotSatisfiable record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusRangeNotSatisfiable status = STATUS_RANGE_NOT_SATISFIABLE_OBJ; // Special Agent Note: the default STATUS_RANGE_NOT_SATISFIABLE_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Presents a read-only view of the remote address.
public type Remote record {|
    # The remote host name
    string host;
    # The remote port
    int port;
    # The remote IP address
    string ip;
|};

# The status code response record of `RequestHeaderFieldsTooLarge`.
public type RequestHeaderFieldsTooLarge record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusRequestHeaderFieldsTooLarge status = STATUS_REQUEST_HEADER_FIELDS_TOO_LARGE_OBJ; // Special Agent Note: the default STATUS_REQUEST_HEADER_FIELDS_TOO_LARGE_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Provides inbound request URI, total header and entity body size threshold configurations.
public type RequestLimitConfigs record {|
    # Maximum allowed length for a URI. Exceeding this limit will result in a `414 - URI Too Long`
    # response. For HTTP/2, this limit will not be applicable as it already has a `:path`
    # pseudo-header which will be validated by `maxHeaderSize`
    int maxUriLength = 4096;
    # Maximum allowed size for headers. Exceeding this limit will result in a
    # `431 - Request Header Fields Too Large` response
    int maxHeaderSize = 8192;
    # Maximum allowed size for the entity body. By default it is set to -1 which means there
    # is no restriction `maxEntityBodySize`, On the Exceeding this limit will result in a
    # `413 - Payload Too Large` response
    int maxEntityBodySize = -1;
|};

# The status code response record of `RequestTimeout`.
public type RequestTimeout record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusRequestTimeout status = STATUS_REQUEST_TIMEOUT_OBJ; // Special Agent Note: the default STATUS_REQUEST_TIMEOUT_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `ResetContent`.
public type ResetContent record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusResetContent status = STATUS_RESET_CONTENT_OBJ; // Special Agent Note: the default STATUS_RESET_CONTENT_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Provides inbound response status line, total header and entity body size threshold configurations.
public type ResponseLimitConfigs record {|
    # Maximum allowed length for response status line(`HTTP/1.1 200 OK`). Exceeding this limit will
    # result in a `ClientError`
    int maxStatusLineLength = 4096;
    # Maximum allowed size for headers. Exceeding this limit will result in a `ClientError`
    int maxHeaderSize = 8192;
    # Maximum allowed size for the entity body. By default it is set to -1 which means there is no
    # restriction `maxEntityBodySize`, On the Exceeding this limit will result in a `ClientError`
    int maxEntityBodySize = -1;
|};

# Provides configurations for controlling the retrying behavior in failure scenarios.
public type RetryConfig record {|
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
|};

# Represents a rolling window in the Circuit Breaker.
public type RollingWindow record {|
    # Minimum number of requests in a `RollingWindow` that will trip the circuit.
    int requestVolumeThreshold = 10;
    # Time period in seconds for which the failure threshold is calculated
    decimal timeWindow = 60;
    # The granularity at which the time window slides. This is measured in seconds.
    decimal bucketSize = 10;
|};

# Represents the annotation used for authorization.
public type Scopes record {|
    # Scopes allowed for authorization
    string|string[] scopes;
|};

# The status code response record of `SeeOther`.
public type SeeOther record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusSeeOther status = STATUS_SEE_OTHER_OBJ; // Special Agent Note: the default STATUS_SEE_OTHER_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Provides settings related to server socket configuration.
public type ServerSocketConfig record {|
    *ClientSocketConfig;
    # Requested maximum length of the queue of incoming connections.
    int soBackLog = 100;
|};

# The status code response record of `ServiceUnavailable`.
public type ServiceUnavailable record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusServiceUnavailable status = STATUS_SERVICE_UNAVAILABLE_OBJ; // Special Agent Note: the default STATUS_SERVICE_UNAVAILABLE_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents a Server Sent Event emitted from a service.
public type SseEvent record {|
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
|};

# Represents the details of an HTTP status code binding client error.
public type StatusCodeBindingErrorDetail record {
    *Detail;
    # Indicates whether the error orginates from default status code response mapping
    boolean fromDefaultStatusCodeMapping;
};

# Defines a status code response record type
public type StatusCodeRecord record {|
    # The status code
    int status;
    # The headers of the response
    map<string|int|boolean|string[]|int[]|boolean[]> headers?;
    # The response body
    anydata body?;
|};

# The status code response record of `SwitchingProtocols`.
public type SwitchingProtocols record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusSwitchingProtocols status = STATUS_SWITCHING_PROTOCOLS_OBJ; // Special Agent Note: the default STATUS_SWITCHING_PROTOCOLS_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents a single service and its related configurations.
public type TargetService record {|
    # URL of the target service
    string url = "";
    # Configurations for secure communication with the remote HTTP endpoint
    ClientSecureSocket? secureSocket = ();
|};

# The status code response record of `TemporaryRedirect`.
public type TemporaryRedirect record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusTemporaryRedirect status = STATUS_TEMPORARY_REDIRECT_OBJ; // Special Agent Note: the default STATUS_TEMPORARY_REDIRECT_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `TooEarly`.
public type TooEarly record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusTooEarly status = STATUS_TOO_EARLY_OBJ; // Special Agent Note: the default STATUS_TOO_EARLY_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `TooManyRequests`.
public type TooManyRequests record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusTooManyRequests status = STATUS_TOO_MANY_REQUESTS_OBJ; // Special Agent Note: the default STATUS_TOO_MANY_REQUESTS_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents HTTP trace log configuration.
public type TraceLogAdvancedConfiguration record {|
    # Enable or disable console trace logs
    boolean console = false;
    # File path to store trace logs
    @deprecated
    string path?;
    # Socket hostname to publish the trace logs
    string host?;
    # Socket port to publish the trace logs
    int port?;
    # Log file configuration to store trace logs
    LogFileConfig file?;
|};

# The status code response record of `Unauthorized`.
public type Unauthorized record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusUnauthorized status = STATUS_UNAUTHORIZED_OBJ; // Special Agent Note: the default STATUS_UNAUTHORIZED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `UnavailableDueToLegalReasons`.
public type UnavailableDueToLegalReasons record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusUnavailableDueToLegalReasons status = STATUS_UNAVAILABLE_DUE_TO_LEGAL_REASONS_OBJ; // Special Agent Note: the default STATUS_UNAVAILABLE_DUE_TO_LEGAL_REASONS_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `UnprocessableEntity`.
public type UnprocessableEntity record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusUnprocessableEntity status = STATUS_UNPROCESSABLE_ENTITY_OBJ; // Special Agent Note: the default STATUS_UNPROCESSABLE_ENTITY_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `UnsupportedMediaType`.
public type UnsupportedMediaType record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusUnsupportedMediaType status = STATUS_UNSUPPORTED_MEDIA_TYPE_OBJ; // Special Agent Note: the default STATUS_UNSUPPORTED_MEDIA_TYPE_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `UpgradeRequired`.
public type UpgradeRequired record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusUpgradeRequired status = STATUS_UPGRADE_REQUIRED_OBJ; // Special Agent Note: the default STATUS_UPGRADE_REQUIRED_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `UriTooLong`.
public type UriTooLong record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusUriTooLong status = STATUS_URI_TOO_LONG_OBJ; // Special Agent Note: the default STATUS_URI_TOO_LONG_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `UseProxy`.
public type UseProxy record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusUseProxy status = STATUS_USE_PROXY_OBJ; // Special Agent Note: the default STATUS_USE_PROXY_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# The status code response record of `VariantAlsoNegotiates`.
public type VariantAlsoNegotiates record {|
    *CommonResponse;
    # The response status code obj
    readonly StatusVariantAlsoNegotiates status = STATUS_VARIANT_ALSO_NEGOTIATES_OBJ; // Special Agent Note: the default STATUS_VARIANT_ALSO_NEGOTIATES_OBJ is not exported by this package; omit the argument rather than repeating it
|};

# Represents a client error that occurred due to all the load balance endpoint failure.
public type AllLoadBalanceEndpointsFailedError distinct ResiliencyError;

# Represents a client error that occurred due to all the the retry attempts failure.
public type AllRetryAttemptsFailed distinct ResiliencyError;

# Represents both 4XX and 5XX application response client error.
public type ApplicationResponseError distinct (ClientError & error<Detail>);

# Represents a client error that occurred due to circuit breaker configuration error.
public type CircuitBreakerConfigError distinct ResiliencyError;

# Defines the Auth error types that returned from client.
public type ClientAuthError distinct ClientError;

# Represents a client connector error that occurred.
public type ClientConnectorError distinct ClientError;

# Defines the possible client error types.
public type ClientError distinct Error;

# Represents an error, which occurred due to bad syntax or incomplete info in the client request(4xx HTTP response).
public type ClientRequestError distinct (ApplicationResponseError & error<Detail>);

# Represents a cookie error that occurred when using the cookies.
public type CookieHandlingError distinct GenericClientError;

# Defines the common error type for the module.
public type Error distinct error;

# Represents a client error that occurred due to failover action failure.
public type FailoverActionFailedError distinct ResiliencyError;

# Represents a client error that occurred due to all the failover endpoint failure.
public type FailoverAllEndpointsFailedError distinct ResiliencyError;

# Represents a generic client error.
public type GenericClientError distinct ClientError;

# Represents a generic listener error.
public type GenericListenerError distinct ListenerError;

# Represents an error, which occurred due to header binding.
public type HeaderBindingError distinct Error;

# Represents a header not found error when retrieving headers.
public type HeaderNotFoundError distinct Error;

# Represents an error, which occurred due to a header constraint validation.
public type HeaderValidationError distinct HeaderBindingError;

# Represents an HTTP/2 client generic error.
public type Http2ClientError distinct ClientError;

# Represents the error that triggered upon a request/response idle timeout.
public type IdleTimeoutError distinct ResiliencyError;

# Defines the listener error types that returned while receiving inbound request.
public type InboundRequestError distinct ListenerError;

# Defines the client error types that returned while receiving inbound response.
public type InboundResponseError distinct ClientError;

# Represents a listener error that occurred due to inbound request initialization failure.
public type InitializingInboundRequestError distinct InboundRequestError;

# Represents a client error that occurred due to inbound response initialization failure.
public type InitializingInboundResponseError distinct InboundResponseError;

# Represents a client error that occurred due to outbound request initialization failure.
public type InitializingOutboundRequestError distinct OutboundRequestError;

# Represents a listener error that occurred due to outbound response initialization failure.
public type InitializingOutboundResponseError distinct OutboundResponseError;

# Represents an error that occurred due to 100 continue response initialization failure.
public type Initiating100ContinueResponseError distinct OutboundResponseError;

# Represents a cookie error that occurred when sending cookies in the response.
public type InvalidCookieError distinct OutboundResponseError;

# Defines the auth error types that returned from listener.
public type ListenerAuthError distinct ListenerError;

# Defines the possible listener error types.
public type ListenerError distinct Error;

# Represents a client error that occurred exceeding maximum wait time.
public type MaximumWaitTimeExceededError distinct GenericClientError;

# Represents an error, which occurred due to media-type binding.
public type MediaTypeBindingError distinct Error;

# Represents an error, which occurred due to media type validation.
public type MediaTypeValidationError distinct MediaTypeBindingError;

# Represents an error, which occurred due to the absence of the payload.
public type NoContentError distinct ClientError;

# Defines the client error types that returned while sending outbound request.
public type OutboundRequestError distinct ClientError;

# Defines the listener error types that returned while sending outbound response.
public type OutboundResponseError distinct ListenerError;

# Represents an error, which occurred due to payload binding.
public type PayloadBindingError distinct Error;

# Represents an error, which occurred due to payload constraint validation.
public type PayloadValidationError distinct PayloadBindingError;

# Represents an error, which occurred due to a query parameter constraint validation.
public type QueryParameterValidationError distinct QueryParameterBindingError;

# Represents a listener error that occurred while writing the inbound request entity body.
public type ReadingInboundRequestBodyError distinct InboundRequestError;

# Represents a listener error that occurred while reading inbound request headers.
public type ReadingInboundRequestHeadersError distinct InboundRequestError;

# Represents a client error that occurred while reading inbound response entity body.
public type ReadingInboundResponseBodyError distinct InboundResponseError;

# Represents a client error that occurred while reading inbound response headers.
public type ReadingInboundResponseHeadersError distinct InboundResponseError;

# Represents an error, which occurred due to a failure of the remote server(5xx HTTP response).
public type RemoteServerError distinct (ApplicationResponseError & error<Detail>);

# Represents an error, which occurred during the request dispatching.
public type RequestDispatchingError distinct ListenerError;

# Defines the resiliency error types that returned from client.
public type ResiliencyError distinct ClientError;

# Represents an error, which occurred during the resource dispatching.
public type ResourceDispatchingError distinct RequestDispatchingError;

# Represents an error, which occurred during the service dispatching.
public type ServiceDispatchingError distinct RequestDispatchingError;

# Represents a client error that occurred due to SSL failure.
public type SslError distinct ClientError;

# Represents the client status code response data binding error
public type StatusCodeResponseDataBindingError MediaTypeBindingStatusCodeClientError|PayloadBindingStatusCodeClientError|HeaderBindingStatusCodeClientError;

# Represents a client error that occurred due to unsupported action invocation.
public type UnsupportedActionError distinct GenericClientError;

# Represents a client error that occurred due to upstream service unavailability.
public type UpstreamServiceUnavailableError distinct ResiliencyError;

# Represents an error that occurred while writing 100 continue response.
public type Writing100ContinueResponseError distinct OutboundResponseError;

# Represents a client error that occurred while writing outbound request entity body.
public type WritingOutboundRequestBodyError distinct OutboundRequestError;

# Represents a client error that occurred while writing outbound request headers.
public type WritingOutboundRequestHeadersError distinct OutboundRequestError;

# Represents a listener error that occurred while writing outbound response entity body.
public type WritingOutboundResponseBodyError distinct OutboundResponseError;

# Represents a listener error that occurred while writing outbound response headers.
public type WritingOutboundResponseHeadersError distinct OutboundResponseError;

# HTTP header key `age`. Gives the current age of a cached HTTP response.
public const string AGE = "age";

# Represents the Authorization header name.
public const string AUTH_HEADER = "Authorization";

# The prefix used to denote the Basic authentication scheme.
public const string AUTH_SCHEME_BASIC = "Basic";

# The prefix used to denote the Bearer authentication scheme.
public const string AUTH_SCHEME_BEARER = "Bearer";

# HTTP header key `authorization`
public const string AUTHORIZATION = "authorization";

# HTTP header key `cache-control`. Specifies the cache control directives required for the function of HTTP caches.
public const string CACHE_CONTROL = "cache-control";

# This is a more restricted mode of RFC 7234. Setting this as the caching policy restricts caching to instances
# where the `cache-control` header and either the `etag` or `last-modified` header are present.
public const string CACHE_CONTROL_AND_VALIDATORS = "CACHE_CONTROL_AND_VALIDATORS";

# Represents the closed state of the circuit. When the Circuit Breaker is in `CLOSED` state, all requests will be
# allowed to go through to the upstream service. If the failures exceed the configured threhold values, the circuit
# will trip and move to the `OPEN` state.
public const string CB_CLOSED_STATE = "CLOSED";

# Represents the half-open state of the circuit. When the Circuit Breaker is in `HALF_OPEN` state, a trial request
# will be sent to the upstream service. If it fails, the circuit will trip again and move to the `OPEN` state. If not,
# it will move to the `CLOSED` state.
public const string CB_HALF_OPEN_STATE = "HALF_OPEN";

# Represents the open state of the circuit. When the Circuit Breaker is in `OPEN` state, requests will fail
# immediately.
public const string CB_OPEN_STATE = "OPEN";

# Always set chunking header in the response.
public const string CHUNKING_ALWAYS = "ALWAYS";

# If the payload is less than 8KB, content-length header is set in the outbound request/response,
# otherwise chunking header is set in the outbound request/response.}
public const string CHUNKING_AUTO = "AUTO";

# Never set the chunking header even if the payload is larger than 8KB in the outbound request/response.
public const string CHUNKING_NEVER = "NEVER";

# Always set accept-encoding/content-encoding in outbound request/response.
public const string COMPRESSION_ALWAYS = "ALWAYS";

# When service behaves as a HTTP gateway inbound request/response accept-encoding option is set as the
# outbound request/response accept-encoding/content-encoding option.
public const string COMPRESSION_AUTO = "AUTO";

# Never set accept-encoding/content-encoding header in outbound request/response.
public const string COMPRESSION_NEVER = "NEVER";

# HTTP header key `connection`. Allows the sender to specify options that are desired for that particular connection.
public const string CONNECTION = "connection";

# HTTP header key `content-length`. Specifies the size of the response body in bytes.
public const string CONTENT_LENGTH = "content-length";

# HTTP header key `content-type`. Specifies the type of the message payload.
public const string CONTENT_TYPE = "content-type";

# HTTP header key `date`. The timestamp at the time the response was generated/received.
public const string DATE = "date";

# Constant for the default listener gracefulStop timeout in seconds
public const decimal DEFAULT_GRACEFULSTOP_TIMEOUT = 0;

# Constant for the default listener endpoint timeout in seconds
public const decimal DEFAULT_LISTENER_TIMEOUT = 60;

# HTTP header key `etag`. A finger print for a resource which is used by HTTP caches to identify whether a
# resource representation has changed.
public const string ETAG = "etag";

# HTTP header key `expect`. Specifies expectations to be fulfilled by the server.
public const string EXPECT = "expect";

# HTTP header key `expires`. Specifies the time at which the response becomes stale.
public const string EXPIRES = "expires";

# Mutual SSL handshake has failed.
public const string FAILED = "failed";

# Constant for the HTTP DELETE method
public const string HTTP_DELETE = "DELETE";

# Constant for the HTTP FORWARD method
public const string HTTP_FORWARD = "FORWARD";

# Constant for the HTTP GET method
public const string HTTP_GET = "GET";

# Constant for the HTTP HEAD method
public const string HTTP_HEAD = "HEAD";

# Constant for the identify not an HTTP Operation
public const string HTTP_NONE = "NONE";

# Constant for the HTTP OPTIONS method
public const string HTTP_OPTIONS = "OPTIONS";

# Constant for the HTTP PATCH method
public const string HTTP_PATCH = "PATCH";

# Constant for the HTTP POST method
public const string HTTP_POST = "POST";

# Constant for the HTTP PUT method
public const string HTTP_PUT = "PUT";

# constant for the HTTP SUBMIT method
public const string HTTP_SUBMIT = "SUBMIT";

# HTTP header key `if-match`
public const string IF_MATCH = "if-match";

# HTTP header key `if-modified-since`. Used when validating (with the origin server) whether a cached response
# is still valid. If the representation of the resource has modified since the timestamp in this field, a
# 304 response is returned.
public const string IF_MODIFIED_SINCE = "if-modified-since";

# HTTP header key `if-none-match`. Used when validating (with the origin server) whether a cached response is
# still valid. If the ETag provided in this field matches the representation of the requested resource, a
# 304 response is returned.
public const string IF_NONE_MATCH = "if-none-match";

# HTTP header key `if-range`
public const string IF_RANGE = "if-range";

# HTTP header key `if-unmodified-since`
public const string IF_UNMODIFIED_SINCE = "if-unmodified-since";

# Constant to get the jwt information from the request context.
public const string JWT_INFORMATION = "JWT_INFORMATION";

# Keeps the connection alive irrespective of the `connection` header value }
public const string KEEPALIVE_ALWAYS = "ALWAYS";

# Decides to keep the connection alive or not based on the `connection` header of the client request }
public const string KEEPALIVE_AUTO = "AUTO";

# Closes the connection irrespective of the `connection` header value }
public const string KEEPALIVE_NEVER = "NEVER";

# HTTP header key `last-modified`. The time at which the resource was last modified.
public const string LAST_MODIFIED = "last-modified";

# Header is placed before the payload of the request/response.
public const string LEADING = "leading";

# HTTP header key `location`. Indicates the URL to redirect a request to.
public const string LOCATION = "location";

# When used in requests, `max-age` implies that clients are not willing to accept responses whose age is greater
# than `max-age`. When used in responses, the response is to be considered stale after the specified
# number of seconds.
public const string MAX_AGE = "max-age";

# Indicates that the client is willing to accept responses which have exceeded their freshness lifetime by no more
# than the specified number of seconds.
public const string MAX_STALE = "max-stale";

# Setting this as the `max-stale` directives indicates that the `max-stale` directive does not specify a limit.
public const decimal MAX_STALE_ANY_AGE = 9223372036854775807;

# Indicates that the client is only accepting responses whose freshness lifetime >= current age + min-fresh.
public const string MIN_FRESH = "min-fresh";

# Represents multipart primary type
public const string MULTIPART_AS_PRIMARY_TYPE = "multipart/";

# Indicates that once the response has become stale, it should not be reused for subsequent requests without
# validating with the origin server.
public const string MUST_REVALIDATE = "must-revalidate";

# Forces the cache to validate a cached response with the origin server before serving.
public const string NO_CACHE = "no-cache";

# Instructs the cache to not store a response in non-volatile storage.
public const string NO_STORE = "no-store";

# Instructs intermediaries not to transform the payload.
public const string NO_TRANSFORM = "no-transform";

# Not a mutual ssl connection.
public const NONE = ();

# Indicates that the client is only willing to accept a cached response. A cached response is served subject to
# other constraints posed by the request.
public const string ONLY_IF_CACHED = "only-if-cached";

# Mutual SSL handshake is successful.
public const string PASSED = "passed";

# HTTP header key `pragma`. Used in dealing with HTTP 1.0 caches which do not understand the `cache-control` header.
public const string PRAGMA = "pragma";

# Indicates that the response is intended for a single user and should not be stored by shared caches.
public const string PRIVATE = "private";

# HTTP header key `proxy-authorization`. Contains the credentials to authenticate a user agent to a proxy serve.
public const string PROXY_AUTHORIZATION = "proxy-authorization";

# Has the same semantics as `must-revalidate`, except that this does not apply to private caches.
public const string PROXY_REVALIDATE = "proxy-revalidate";

# Indicates that any cache may store the response.
public const string PUBLIC = "public";

# Represents the HTTP redirect status code `302 - Found`.
public const int REDIRECT_FOUND_302 = 302;

# Represents the HTTP redirect status code `301 - Moved Permanently`.
public const int REDIRECT_MOVED_PERMANENTLY_301 = 301;

# Represents the HTTP redirect status code `300 - Multiple Choices`.
public const int REDIRECT_MULTIPLE_CHOICES_300 = 300;

# Represents the HTTP redirect status code `304 - Not Modified`.
public const int REDIRECT_NOT_MODIFIED_304 = 304;

# Represents the HTTP redirect status code `308 - Permanent Redirect`.
public const int REDIRECT_PERMANENT_REDIRECT_308 = 308;

# Represents the HTTP redirect status code `303 - See Other`.
public const int REDIRECT_SEE_OTHER_303 = 303;

# Represents the HTTP redirect status code `307 - Temporary Redirect`.
public const int REDIRECT_TEMPORARY_REDIRECT_307 = 307;

# Represents the HTTP redirect status code `305 - Use Proxy`.
public const int REDIRECT_USE_PROXY_305 = 305;

# Constant for the request method reference.
public const string REQUEST_METHOD = "REQUEST_METHOD";

# Constant for the resource name reference.
public const string RESOURCE_NAME = "RESOURCE_NAME";

# Caching behaviour is as specified by the RFC 7234 specification.
public const string RFC_7234 = "RFC_7234";

# In shared caches, `s-maxage` overrides the `max-age` or `expires` header field.
public const string S_MAX_AGE = "s-maxage";

# HTTP header key `server`. Specifies the details of the origin server.
public const string SERVER = "server";

# Constant for the service name reference.
public const string SERVICE_NAME = "SERVICE_NAME";

# The HTTP response status code: 202 Accepted
public const int STATUS_ACCEPTED = 202;

# The HTTP response status code: 208 Already Reported
public const int STATUS_ALREADY_REPORTED = 208;

# The HTTP response status code: 502 Bad Gateway
public const int STATUS_BAD_GATEWAY = 502;

# The HTTP response status code: 400 Bad Request
public const int STATUS_BAD_REQUEST = 400;

# The HTTP response status code: 409 Conflict
public const int STATUS_CONFLICT = 409;

# The HTTP response status code: 100 Continue
public const int STATUS_CONTINUE = 100;

# The HTTP response status code: 201 Created
public const int STATUS_CREATED = 201;

# The HTTP response status code: 103 Early Hints
public const int STATUS_EARLY_HINTS = 103;

# The HTTP response status code: 417 Expectation Failed
public const int STATUS_EXPECTATION_FAILED = 417;

# The HTTP response status code: 424 Failed Dependency
public const int STATUS_FAILED_DEPENDENCY = 424;

# The HTTP response status code: 403 Forbidden
public const int STATUS_FORBIDDEN = 403;

# The HTTP response status code: 302 Found
public const int STATUS_FOUND = 302;

# The HTTP response status code: 504 Gateway Timeout
public const int STATUS_GATEWAY_TIMEOUT = 504;

# The HTTP response status code: 410 Gone
public const int STATUS_GONE = 410;

# The HTTP response status code: 505 HTTP Version Not Supported
public const int STATUS_HTTP_VERSION_NOT_SUPPORTED = 505;

# The HTTP response status code: 226 IM Used
public const int STATUS_IM_USED = 226;

# The HTTP response status code: 507 Insufficient Storage
public const int STATUS_INSUFFICIENT_STORAGE = 507;

# The HTTP response status code: 500 Internal Server Error
public const int STATUS_INTERNAL_SERVER_ERROR = 500;

# The HTTP response status code: 411 Length Required
public const int STATUS_LENGTH_REQUIRED = 411;

# The HTTP response status code: 423 Locked
public const int STATUS_LOCKED = 423;

# The HTTP response status code: 508 Loop Detected
public const int STATUS_LOOP_DETECTED = 508;

# The HTTP response status code: 405 Method Not Allowed
public const int STATUS_METHOD_NOT_ALLOWED = 405;

# The HTTP response status code: 421 Misdirected Request
public const int STATUS_MISDIRECTED_REQUEST = 421;

# The HTTP response status code: 301 Moved Permanently
public const int STATUS_MOVED_PERMANENTLY = 301;

# The HTTP response status code: 207 Multi-Status
public const int STATUS_MULTI_STATUS = 207;

# The HTTP response status code: 300 Multiple Choices
public const int STATUS_MULTIPLE_CHOICES = 300;

# The HTTP response status code: 511 Network Authorization Required
public const int STATUS_NETWORK_AUTHENTICATION_REQUIRED = 511;

# The HTTP response status code: 204 No Content
public const int STATUS_NO_CONTENT = 204;

# The HTTP response status code: 203 Non Authoritative Information
public const int STATUS_NON_AUTHORITATIVE_INFORMATION = 203;

# The HTTP response status code: 406 Not Acceptable
public const int STATUS_NOT_ACCEPTABLE = 406;

# The HTTP response status code: 510 Not Extended
public const int STATUS_NOT_EXTENDED = 510;

# The HTTP response status code: 404 Not Found
public const int STATUS_NOT_FOUND = 404;

# The HTTP response status code: 501 Not Implemented
public const int STATUS_NOT_IMPLEMENTED = 501;

# The HTTP response status code: 304 Not Modified
public const int STATUS_NOT_MODIFIED = 304;

# The HTTP response status code: 200 OK
public const int STATUS_OK = 200;

# The HTTP response status code: 206 Partial Content
public const int STATUS_PARTIAL_CONTENT = 206;

# The HTTP response status code: 413 Payload Too Large
public const int STATUS_PAYLOAD_TOO_LARGE = 413;

# The HTTP response status code: 402 Payment Required
public const int STATUS_PAYMENT_REQUIRED = 402;

# The HTTP response status code: 308 Permanent Redirect
public const int STATUS_PERMANENT_REDIRECT = 308;

# The HTTP response status code: 412 Precondition Failed
public const int STATUS_PRECONDITION_FAILED = 412;

# The HTTP response status code: 428 Precondition Required
public const int STATUS_PRECONDITION_REQUIRED = 428;

# The HTTP response status code: 102 Processing
public const int STATUS_PROCESSING = 102;

# The HTTP response status code: 407 Proxy Authentication Required
public const int STATUS_PROXY_AUTHENTICATION_REQUIRED = 407;

# The HTTP response status code: 416 Range Not Satisfiable
public const int STATUS_RANGE_NOT_SATISFIABLE = 416;

# The HTTP response status code: 431 Request Header Fields Too Large
public const int STATUS_REQUEST_HEADER_FIELDS_TOO_LARGE = 431;

# The HTTP response status code: 408 Request Timeout
public const int STATUS_REQUEST_TIMEOUT = 408;

# The HTTP response status code: 205 Reset Content
public const int STATUS_RESET_CONTENT = 205;

# The HTTP response status code: 303 See Other
public const int STATUS_SEE_OTHER = 303;

# The HTTP response status code: 503 Service Unavailable
public const int STATUS_SERVICE_UNAVAILABLE = 503;

# The HTTP response status code: 101 Switching Protocols
public const int STATUS_SWITCHING_PROTOCOLS = 101;

# The HTTP response status code: 307 Temporary Redirect
public const int STATUS_TEMPORARY_REDIRECT = 307;

# The HTTP response status code: 425 Too Early
public const int STATUS_TOO_EARLY = 425;

# The HTTP response status code: 429 Too Many Requests
public const int STATUS_TOO_MANY_REQUESTS = 429;

# The HTTP response status code: 401 Unauthorized
public const int STATUS_UNAUTHORIZED = 401;

# The HTTP response status code: 451 Unavailable Due To Legal Reasons
public const int STATUS_UNAVAILABLE_DUE_TO_LEGAL_REASONS = 451;

# The HTTP response status code: 422 Unprocessable Entity
public const int STATUS_UNPROCESSABLE_ENTITY = 422;

# The HTTP response status code: 415 Unsupported Media Type
public const int STATUS_UNSUPPORTED_MEDIA_TYPE = 415;

# The HTTP response status code: 426 Upgrade Required
public const int STATUS_UPGRADE_REQUIRED = 426;

# The HTTP response status code: 414 URI Too Long
public const int STATUS_URI_TOO_LONG = 414;

# The HTTP response status code: 305 Use Proxy
public const int STATUS_USE_PROXY = 305;

# The HTTP response status code: 506 Variant Also Negotiates
public const int STATUS_VARIANT_ALSO_NEGOTIATES = 506;

# Header is placed after the payload of the request/response.
public const string TRAILING = "trailing";

# HTTP header key `transfer-encoding`. Specifies what type of transformation has been applied to entity body.
public const string TRANSFER_ENCODING = "transfer-encoding";

# HTTP header key `upgrade`. Allows the client to specify what additional communication protocols it supports and
# would like to use, if the server finds it appropriate to switch protocols.
public const string UPGRADE = "upgrade";

# HTTP header key `warning`. Specifies warnings generated when serving stale responses from HTTP caches.
public const string WARNING = "warning";

# Represents certification validation type options.
public enum CertValidationType {
    OCSP_CRL,
    OCSP_STAPLING
}

# Defines the supported HTTP protocols.
public enum HttpVersion {
    # Represents HTTP/1.0 protocol
    HTTP_1_0,
    # Represents HTTP/1.1 protocol
    HTTP_1_1,
    # Represents HTTP/2.0 protocol
    HTTP_2_0
}

# Represents HTTP methods.
public enum Method {
    GET,
    POST,
    PUT,
    DELETE,
    PATCH,
    HEAD,
    OPTIONS
}

# Represents protocol options.
public enum Protocol {
    SSL,
    TLS,
    DTLS
}

# Represents client verify options.
public enum VerifyClient {
    REQUIRE,
    OPTIONAL
}

# Defines the Basic Auth handler for client authentication.
public isolated class ClientBasicAuthHandler {
    # Initializes the `http:ClientBasicAuthHandler` object.
    # + config - The `http:CredentialsConfig` instance
    isolated function init(CredentialsConfig config);

    # Enrich the request with the relevant authentication requirements.
    # + req - The `http:Request` instance
    # + return - The updated `http:Request` instance or else an `http:ClientAuthError` in case of an error
    isolated function enrich(Request req) returns Request|ClientAuthError;

    # Enrich the headers map with the relevant authentication requirements.
    # + headers - The headers map
    # + return - The updated headers map or else an `http:ClientAuthError` in case of an error
    isolated function enrichHeaders(map<string|string[]> headers) returns map<string|string[]>|ClientAuthError;

    # Returns the headers map with the relevant authentication requirements.
    # + return - The updated headers map or else an `http:ClientAuthError` in case of an error
    isolated function getSecurityHeaders() returns map<string|string[]>|ClientAuthError;
}

# Defines the Bearer token auth handler for client authentication.
public isolated class ClientBearerTokenAuthHandler {
    # Initializes the `http:ClientBearerTokenAuthHandler` object.
    # + config - The `http:BearerTokenConfig` instance
    isolated function init(BearerTokenConfig config);

    # Enrich the request with the relevant authentication requirements.
    # + req - The `http:Request` instance
    # + return - The updated `http:Request` instance or else an `http:ClientAuthError` in case of an error
    isolated function enrich(Request req) returns Request|ClientAuthError;

    # Enrich the headers map with the relevant authentication requirements.
    # + headers - The headers map
    # + return - The updated headers map or else an `http:ClientAuthError` in case of an error
    isolated function enrichHeaders(map<string|string[]> headers) returns map<string|string[]>|ClientAuthError;

    # Returns the headers map with the relevant authentication requirements.
    # + return - The updated headers map or else an `http:ClientAuthError` in case of an error
    isolated function getSecurityHeaders() returns map<string|string[]>|ClientAuthError;
}

# Defines the self signed JWT handler for client authentication.
public isolated class ClientSelfSignedJwtAuthHandler {
    # Initializes the `http:ClientSelfSignedJwtAuthProvider` object.
    # + config - The `http:JwtIssuerConfig` instance
    isolated function init(JwtIssuerConfig config);

    # Enrich the request with the relevant authentication requirements.
    # + req - The `http:Request` instance
    # + return - The updated `http:Request` instance or else an `http:ClientAuthError` in case of an error
    isolated function enrich(Request req) returns Request|ClientAuthError;

    # Enrich the headers map with the relevant authentication requirements.
    # + headers - The headers map
    # + return - The updated headers map or else an `http:ClientAuthError` in case of an error
    isolated function enrichHeaders(map<string|string[]> headers) returns map<string|string[]>|ClientAuthError;

    # Returns the headers map with the relevant authentication requirements.
    # + return - The updated headers map or else an `http:ClientAuthError` in case of an error
    isolated function getSecurityHeaders() returns map<string|string[]>|ClientAuthError;
}

# Represents a Cookie.
public readonly class Cookie {
    # Name of the cookie
    public string name;
    # Value of the cookie
    public string value;
    # URI path to which the cookie belongs
    public string? path;
    # Host to which the cookie will be sent
    public string? domain;
    # Maximum lifetime of the cookie represented as the date and time at which the cookie expires
    public string? expires;
    # Maximum lifetime of the cookie represented as the number of seconds until the cookie expires
    public int maxAge;
    # Cookie is sent only to HTTP requests
    public boolean httpOnly;
    # Cookie is sent only to secure channels
    public boolean secure;
    # At what time the cookie was created
    public time:Utc createdTime; // Special Agent Note: Utc FROM ballerina/time module
    # Last-accessed time of the cookie
    public time:Utc lastAccessedTime; // Special Agent Note: Utc FROM ballerina/time module
    # Cookie is sent only to the requested host
    public boolean hostOnly;

    # Initializes the `http:Cookie` object.
    # + name - Name of the `http:Cookie`
    # + value - Value of the `http:Cookie`
    # + options - The options to be used when initializing the `http:Cookie`
    isolated function init(string name, string value, *CookieOptions options);

    # Checks the persistence of the cookie.
    # + return - `false` if the cookie will be discarded at the end of the "session" or else `true`.
    isolated function isPersistent() returns boolean;

    # Checks the validity of the attributes of the cookie.
    # + return - `true` if the attributes of the cookie are in the correct format or else an `http:InvalidCookieError`
    isolated function isValid() returns boolean|InvalidCookieError;

    # Gets the Cookie object in its string representation to be used in the ‘Set-Cookie’ header of the response.
    # + return - The string value of the `http:Cookie`
    isolated function toStringValue() returns string;
}

# Represents the cookie store.
public isolated class CookieStore {
    isolated function init(PersistentCookieHandler? persistentCookieHandler = ());

    # Adds a cookie to the cookie store according to the rules in [RFC-6265](https://tools.ietf.org/html/rfc6265#section-5.3).
    # + cookie - Cookie to be added
    # + cookieConfig - Configurations associated with the cookies
    # + url - Target service URL
    # + requestPath - Resource path
    # + return - An `http:CookieHandlingError` if there is any error occurred when adding a cookie or else `()`
    isolated function addCookie(Cookie cookie, CookieConfig cookieConfig, string url, string requestPath) returns CookieHandlingError?;

    # Adds an array of cookies.
    # + cookiesInResponse - Cookies to be added
    # + cookieConfig - Configurations associated with the cookies
    # + url - Target service URL
    # + requestPath - Resource path
    isolated function addCookies(Cookie[] cookiesInResponse, CookieConfig cookieConfig, string url, string requestPath);

    # Gets the relevant cookies for the given URL and the path according to the rules in [RFC-6265](https://tools.ietf.org/html/rfc6265#section-5.4).
    # + url - URL of the request URI
    # + requestPath - Path of the request URI
    # + return - Array of the matched cookies stored in the cookie store
    isolated function getCookies(string url, string requestPath) returns Cookie[];

    # Gets all the cookies in the cookie store.
    # + return - Array of all the cookie objects
    isolated function getAllCookies() returns Cookie[];

    # Gets all the cookies, which have the given name as the name of the cookie.
    # + cookieName - Name of the cookie
    # + return - Array of all the matched cookie objects
    isolated function getCookiesByName(string cookieName) returns Cookie[];

    # Gets all the cookies, which have the given name as the domain of the cookie.
    # + domain - Name of the domain
    # + return - Array of all the matched cookie objects
    isolated function getCookiesByDomain(string domain) returns Cookie[];

    # Removes a specific cookie.
    # + name - Name of the cookie to be removed
    # + domain - Domain of the cookie to be removed
    # + path - Path of the cookie to be removed
    # + return - An `http:CookieHandlingError` if there is any error occurred during the removal of the cookie or else `()`
    isolated function removeCookie(string name, string domain, string path) returns CookieHandlingError?;

    # Removes cookies, which match with the given domain.
    # + domain - Domain of the cookie to be removed
    # + return - An `http:CookieHandlingError` if there is any error occurred during the removal of cookies by domain or else `()`
    isolated function removeCookiesByDomain(string domain) returns CookieHandlingError?;

    # Removes all expired cookies.
    # + return - An `http:CookieHandlingError` if there is any error occurred during the removal of expired cookies or else `()`
    isolated function removeExpiredCookies() returns CookieHandlingError?;

    # Removes all the cookies.
    # + return - An `http:CookieHandlingError` if there is any error occurred during the removal of all the cookies or else `()`
    isolated function removeAllCookies() returns CookieHandlingError?;
}

# Represents a default persistent cookie handler, which stores persistent cookies in a CSV file.
public isolated class CsvPersistentCookieHandler {
    isolated function init(string fileName);

    # Adds a persistent cookie to the cookie store.
    # + cookie - Cookie to be added
    # + return - An error will be returned if there is any error occurred during the storing process of the cookie or else nil is returned
    isolated function storeCookie(Cookie cookie) returns CookieHandlingError?;

    # Gets all the persistent cookies.
    # + return - Array of persistent cookies stored in the cookie store or else an error is returned if one occurred during the retrieval of the cookies
    isolated function getAllCookies() returns Cookie[]|CookieHandlingError;

    # Removes a specific persistent cookie.
    # + name - Name of the persistent cookie to be removed
    # + domain - Domain of the persistent cookie to be removed
    # + path - Path of the persistent cookie to be removed
    # + return - An error will be returned if there is any error occurred during the removal of the cookie or else nil is returned
    isolated function removeCookie(string name, string domain, string path) returns CookieHandlingError?;

    # Removes all persistent cookies.
    # + return - An error will be returned if there is any error occurred during the removal of all the cookies or else nil is returned
    isolated function removeAllCookies() returns CookieHandlingError?;
}

# The default status code class.
public readonly class DefaultStatus {
    *Status;

    isolated function init(int code);
}

# Represents the headers of the inbound request.
public class Headers {
    # Checks whether the requested header key exists in the header map.
    # + headerName - The header name
    # + return - `true` if the specified header key exists
    isolated function hasHeader(string headerName) returns boolean;

    # Returns the value of the specified header. If the specified header key maps to multiple values, the first of
    # these values is returned.
    # + headerName - The header name
    # + return - The first header value for the specified header name or the `HeaderNotFoundError` if the header is not
    # found.
    isolated function getHeader(string headerName) returns string|HeaderNotFoundError;

    # Gets all the header values to which the specified header key maps to.
    # + headerName - The header name
    # + return - The header values the specified header key maps to or the `HeaderNotFoundError` if the header is not
    # found.
    isolated function getHeaders(string headerName) returns string[]|HeaderNotFoundError;

    # Gets all the names of the headers of the request.
    # + return - An array of all the header names
    isolated function getHeaderNames() returns string[];
}

# Implements a cache for storing HTTP responses. This cache complies with the caching policy set when configuring
# HTTP caching in the HTTP client endpoint.
public isolated class HttpCache {
    # Creates the HTTP cache.
    # + cacheConfig - The configurations for the HTTP cache
    isolated function init(CacheConfig cacheConfig);
}

# Represents a 'future' that returns as a result of an asynchronous HTTP request submission.
# This can be used as a reference to fetch the results of the submission.
public class HttpFuture {
}

# Defines the file store Basic Auth handler for listener authentication.
public isolated class ListenerFileUserStoreBasicAuthHandler {
    # Initializes the `http:ListenerFileUserStoreBasicAuthHandler` object.
    # + config - The `http:FileUserStoreConfig` instance
    isolated function init(FileUserStoreConfig config = {});

    # Authenticates with the relevant authentication requirements.
    # + data - The `http:Request` instance or `http:Headers` instance or `string` Authorization header
    # + return - The `auth:UserDetails` instance or else `Unauthorized` type in case of an error
    isolated function authenticate(Request|Headers|string data) returns auth:UserDetails|Unauthorized; // Special Agent Note: UserDetails FROM ballerina/auth module

    # Authorizes with the relevant authorization requirements.
    # + userDetails - The `auth:UserDetails` instance which is received from authentication results
    # + expectedScopes - The expected scopes as `string` or `string[]`
    # + return - `()`, if it is successful or else `Forbidden` type in case of an error
    isolated function authorize(auth:UserDetails userDetails, string|string[] expectedScopes) returns Forbidden?; // Special Agent Note: UserDetails FROM ballerina/auth module
}

# Defines the JWT auth handler for listener authentication.
public isolated class ListenerJwtAuthHandler {
    # Initializes the `http:ListenerJwtAuthHandler` object.
    # + config - The `http:JwtValidatorConfig` instance
    isolated function init(JwtValidatorConfig config);

    # Authenticates with the relevant authentication requirements.
    # + data - The `http:Request` instance or `http:Headers` instance or `string` Authorization header
    # + return - The `jwt:Payload` instance or else `Unauthorized` type in case of an error
    isolated function authenticate(Request|Headers|string data) returns jwt:Payload|Unauthorized; // Special Agent Note: Payload FROM ballerina/jwt module

    # Authorizes with the relevant authorization requirements.
    # + jwtPayload - The `jwt:Payload` instance which is received from authentication results
    # + expectedScopes - The expected scopes as `string` or `string[]`
    # + return - `()`, if it is successful or else `Forbidden` type in case of an error
    isolated function authorize(jwt:Payload jwtPayload, string|string[] expectedScopes) returns Forbidden?; // Special Agent Note: Payload FROM ballerina/jwt module
}

# Implementation of round robin load balancing strategy.
public isolated class LoadBalancerRoundRobinRule {
    # Provides an HTTP client, which is chosen according to the round robin algorithm.
    # + loadBalanceCallerActionsArray - Array of HTTP clients, which needs to be load balanced
    # + return - Chosen `http:Client` from the algorithm or else an `http:ClientError` for a failure in
    # the algorithm implementation
    isolated function getNextClient(Client?[] loadBalanceCallerActionsArray) returns Client|ClientError;
}

# Represents an HTTP/2 `PUSH_PROMISE` frame.
public class PushPromise {
    # The resource path
    public string path;
    # The HTTP method
    public string method;

    # Constructs an `http:PushPromise` from a given path and a method.
    # + path - The resource path
    # + method - The HTTP method
    isolated function init(string path = "/", string method = "GET");

    # Checks whether the requested header exists.
    # + headerName - The header name
    # + return - A `boolean` representing the existence of a given header
    isolated function hasHeader(string headerName) returns boolean;

    # Returns the header value with the specified header name.
    # If there are more than one header value for the specified header name, the first value is returned.
    # + headerName - The header name
    # + return - The header value or `()` if there is no such header
    isolated function getHeader(string headerName) returns string;

    # Gets transport headers from the `PushPromise`.
    # + headerName - The header name
    # + return - The array of header values
    isolated function getHeaders(string headerName) returns string[];

    # Adds the specified key/value pair as an HTTP header to the `http:PushPromise`. In the case of the `Content-Type`
    # header, the existing value is replaced with the specified value.
    # + headerName - The header name
    # + headerValue - The header value
    isolated function addHeader(string headerName, string headerValue);

    # Sets the value of a transport header in the `http:PushPromise`.
    # + headerName - The header name
    # + headerValue - The header value
    isolated function setHeader(string headerName, string headerValue);

    # Removes a transport header from the `http:PushPromise`.
    # + headerName - The header name
    isolated function removeHeader(string headerName);

    # Removes all transport headers from the `http:PushPromise`.
    isolated function removeAllHeaders();

    # Gets all transport header names from the `http:PushPromise`.
    # + return - An array of all transport header names
    isolated function getHeaderNames() returns string[];
}

# Represents an HTTP request.
public class Request {
    # Resource path of the request URL
    public string rawPath = "";
    # The HTTP request method
    public string method = "";
    # The HTTP version supported by the client
    public string httpVersion = "";
    # The user-agent. This value is used when setting the `user-agent` header
    public string userAgent = "";
    # The part of the URL, which matched to '*' if the request is dispatched to a wildcard resource
    public string extraPathInfo = "";
    # The cache-control directives for the request. This needs to be explicitly initialized if intending
    # on utilizing HTTP caching.
    public RequestCacheControl? cacheControl = ();
    # A record providing mutual ssl handshake results.
    public MutualSslHandshake? mutualSslHandshake = ();

    isolated function init();

    # Sets the provided `Entity` to the request.
    # + e - The `Entity` to be set to the request
    isolated function setEntity(mime:Entity e); // Special Agent Note: Entity FROM ballerina/mime module

    # Gets the query parameters of the request as a map consisting of a string array.
    # + return - String array map of the query params
    isolated function getQueryParams() returns map<string[]>;

    # Gets the query param value associated with the given key.
    # + key - Represents the query param key
    # + return - The query param value associated with the given key as a string. If multiple param values are
    # present, then the first value is returned. `()` is returned if no key is found.
    isolated function getQueryParamValue(string key) returns string?;

    # Gets all the query param values associated with the given key.
    # + key - Represents the query param key
    # + return - All the query param values associated with the given key as a `string[]`. `()` is returned if no key
    # is found.
    isolated function getQueryParamValues(string key) returns string[]?;

    # Gets the matrix parameters of the request.
    # + path - Path to the location of matrix parameters
    # + return - A map of matrix parameters which can be found for the given path
    isolated function getMatrixParams(string path) returns map<any>;

    # Gets the `Entity` associated with the request.
    # + return - The `Entity` of the request. An `http:ClientError` is returned, if entity construction fails
    isolated function getEntity() returns mime:Entity|ClientError; // Special Agent Note: Entity FROM ballerina/mime module

    # Checks whether the requested header key exists in the header map.
    # + headerName - The header name
    # + return - `true` if the specified header key exists
    isolated function hasHeader(string headerName) returns boolean;

    # Returns the value of the specified header. If the specified header key maps to multiple values, the first of
    # these values is returned.
    # + headerName - The header name
    # + return - The first header value for the specified header name or the `HeaderNotFoundError` if the header is not
    # found.
    isolated function getHeader(string headerName) returns string|HeaderNotFoundError;

    # Gets all the header values to which the specified header key maps to.
    # + headerName - The header name
    # + return - The header values the specified header key maps to or the `HeaderNotFoundError` if the header is not
    # found.
    isolated function getHeaders(string headerName) returns string[]|HeaderNotFoundError;

    # Sets the specified header to the request. If a mapping already exists for the specified header key, the existing
    # header value is replaced with the specified header value. Panic if an illegal header is passed.
    # + headerName - The header name
    # + headerValue - The header value
    isolated function setHeader(string headerName, string headerValue);

    # Adds the specified header to the request. Existing header values are not replaced, except for the `Content-Type`
    # header. In the case of the `Content-Type` header, the existing value is replaced with the specified value.
    # Panic if an illegal header is passed.
    # + headerName - The header name
    # + headerValue - The header value
    isolated function addHeader(string headerName, string headerValue);

    # Removes the specified header from the request.
    # + headerName - The header name
    isolated function removeHeader(string headerName);

    # Removes all the headers from the request.
    isolated function removeAllHeaders();

    # Gets all the names of the headers of the request.
    # + return - An array of all the header names
    isolated function getHeaderNames() returns string[];

    # Checks whether the client expects a `100-continue` response.
    # + return - `true` if the client expects a `100-continue` response
    isolated function expects100Continue() returns boolean;

    # Sets the `content-type` header to the request.
    # + contentType - Content type value to be set as the `content-type` header
    # + return - Nil if successful, error in case of invalid content-type
    isolated function setContentType(string contentType) returns error?;

    # Gets the type of the payload of the request (i.e: the `content-type` header value).
    # + return - The `content-type` header value as a string
    isolated function getContentType() returns string;

    # Extract `json` payload from the request. For an empty payload, `http:NoContentError` is returned.
    # 
    # If the content type is not JSON, an `http:ClientError` is returned.
    # + return - The `json` payload or `http:ClientError` in case of errors
    isolated function getJsonPayload() returns json|ClientError;

    # Extracts `xml` payload from the request. For an empty payload, `http:NoContentError` is returned.
    # 
    # If the content type is not XML, an `http:ClientError` is returned.
    # + return - The `xml` payload or `http:ClientError` in case of errors
    isolated function getXmlPayload() returns xml|ClientError;

    # Extracts `text` payload from the request. For an empty payload, `http:NoContentError` is returned.
    # 
    # If the content type is not of type text, an `http:ClientError` is returned.
    # + return - The `text` payload or `http:ClientError` in case of errors
    isolated function getTextPayload() returns string|ClientError;

    # Gets the request payload as  a stream of byte[], except in the case of multiparts. To retrieve multiparts, use
    # `Request.getBodyParts()`.
    # + arraySize - A defaultable parameter to state the size of the byte array. Default size is 8KB
    # + return - A byte stream from which the message payload can be read or `http:ClientError` in case of errors
    isolated function getByteStream(int arraySize = 8192) returns stream<byte[], io:Error?>|ClientError; // Special Agent Note: Error FROM ballerina/io module

    # Gets the request payload as a `byte[]`.
    # + return - The byte[] representation of the message payload or `http:ClientError` in case of errors
    isolated function getBinaryPayload() returns byte[]|ClientError;

    # Gets the form parameters from the HTTP request as a `map` when content type is application/x-www-form-urlencoded.
    # + return - The map of form params or `http:ClientError` in case of errors
    isolated function getFormParams() returns map<string>|ClientError;

    # Extracts body parts from the request. If the content type is not a composite media type, an error
    # is returned.
    # + return - The body parts as an array of entities or else an `http:ClientError` if there were any errors
    # constructing the body parts from the request
    isolated function getBodyParts() returns mime:Entity[]|ClientError; // Special Agent Note: Entity FROM ballerina/mime module

    # Sets a `json` as the payload. If the content-type header is not set then this method set content-type
    # headers with the default content-type, which is `application/json`. Any existing content-type can be
    # overridden by passing the content-type as an optional parameter. If the given payload is a record type with 
    # the `@jsondata:Name` annotation, the `jsondata:toJson` function internally converts the record to JSON
    # + payload - The `json` payload
    # + contentType - The content type of the payload. This is an optional parameter.
    # The `application/json` is the default value
    isolated function setJsonPayload(json payload, string? contentType = ());

    # Sets an `xml` as the payload. If the content-type header is not set then this method set content-type
    # headers with the default content-type, which is `application/xml`. Any existing content-type can be
    # overridden by passing the content-type as an optional parameter.
    # + payload - The `xml` payload
    # + contentType - The content type of the payload. This is an optional parameter.
    # The `application/xml` is the default value
    isolated function setXmlPayload(xml payload, string? contentType = ());

    # Sets a `string` as the payload. If the content-type header is not set then this method set
    # content-type headers with the default content-type, which is `text/plain`. Any
    # existing content-type can be overridden by passing the content-type as an optional parameter.
    # + payload - The `string` payload
    # + contentType - The content type of the payload. This is an optional parameter.
    # The `text/plain` is the default value
    isolated function setTextPayload(string payload, string? contentType = ());

    # Sets a `byte[]` as the payload. If the content-type header is not set then this method set content-type
    # headers with the default content-type, which is `application/octet-stream`. Any existing content-type
    # can be overridden by passing the content-type as an optional parameter.
    # + payload - The `byte[]` payload
    # + contentType - The content type of the payload. This is an optional parameter.
    # The `application/octet-stream` is the default value
    isolated function setBinaryPayload(byte[] payload, string? contentType = ());

    # Set multiparts as the payload. If the content-type header is not set then this method
    # set content-type headers with the default content-type, which is `multipart/form-data`.
    # Any existing content-type can be overridden by passing the content-type as an optional parameter.
    # + bodyParts - The entities which make up the message body
    # + contentType - The content type of the top level message. This is an optional parameter.
    # The `multipart/form-data` is the default value
    isolated function setBodyParts(mime:Entity[] bodyParts, string? contentType = ()); // Special Agent Note: Entity FROM ballerina/mime module

    # Sets the content of the specified file as the entity body of the request. If the content-type header
    # is not set then this method set content-type headers with the default content-type, which is
    # `application/octet-stream`. Any existing content-type can be overridden by passing the content-type
    # as an optional parameter.
    # + filePath - Path to the file to be set as the payload
    # + contentType - The content type of the specified file. This is an optional parameter.
    # The `application/octet-stream` is the default value
    isolated function setFileAsPayload(string filePath, string? contentType = ());

    # Sets a `Stream` as the payload. If the content-type header is not set then this method set content-type
    # headers with the default content-type, which is `application/octet-stream`. Any existing content-type can
    # be overridden by passing the content-type as an optional parameter.
    # + byteStream - Byte stream, which needs to be set to the request
    # + contentType - Content-type to be used with the payload. This is an optional parameter.
    # The `application/octet-stream` is the default value
    isolated function setByteStream(stream<byte[], io:Error?> byteStream, string? contentType = ()); // Special Agent Note: Error FROM ballerina/io module

    # Sets the request payload. This method overrides any existing content-type by passing the content-type
    # as an optional parameter. If the content type parameter is not provided then the default value derived
    # from the payload will be used as content-type only when there are no existing content-type header.
    # + payload - Payload can be of type `string`, `xml`, `byte[]`, `json`, `stream<byte[], io:Error?>`,
    # `Entity[]` (i.e., a set of body parts) or any other value of type `anydata` which will
    # be converted to `json` using the `toJson` method.
    # + contentType - Content-type to be used with the payload. This is an optional parameter
    isolated function setPayload(anydata|mime:Entity[]|stream<byte[], io:Error?> payload, string? contentType = ()); // Special Agent Note: Entity FROM ballerina/mime module, Error FROM ballerina/io module

    # Adds cookies to the request.
    # + cookiesToAdd - Represents the cookies to be added
    isolated function addCookies(Cookie[] cookiesToAdd);

    # Gets cookies from the request.
    # + return - An array of cookie objects, which are included in the request
    isolated function getCookies() returns Cookie[];
}

# Configures the cache control directives for an `http:Request`.
public class RequestCacheControl {
    # Sets the `no-cache` directive
    public boolean noCache = false;
    # Sets the `no-store` directive
    public boolean noStore = false;
    # Sets the `no-transform` directive
    public boolean noTransform = false;
    # Sets the `only-if-cached` directive
    public boolean onlyIfCached = false;
    # Sets the `max-age` directive
    public decimal maxAge = -1;
    # Sets the `max-stale` directive
    public decimal maxStale = -1;
    # Sets the `min-fresh` directive
    public decimal minFresh = -1;

    # Builds the cache control directives string from the current `http:RequestCacheControl` configurations.
    # + return - The cache control directives string to be used in the `cache-control` header
    isolated function buildCacheControlDirectives() returns string;
}

# Represents an HTTP Context that allows user to pass data between interceptors.
public isolated class RequestContext {
    # Sets a member to the request context object.
    # + key - Represents the member key
    # + value - Represents the member value
    isolated function set(string key, ReqCtxMember value);

    # Gets a member value from the request context object. It panics if there is no such member.
    # + key - Represents the member key
    # + return - Member value
    isolated function get(string key) returns ReqCtxMember;

    # Checks whether the request context object has a member corresponds to the key.
    # + key - Represents the member key
    # + return - true if the member exists, else false
    isolated function hasKey(string key) returns boolean;

    # Returns the member keys of the request context object.
    # + return - Array of member keys
    isolated function keys() returns string[];

    # Gets a member value with type from the request context object.
    # + key - Represents the member key
    # + targetType - Represents the expected type of the member value
    # + return - Attribute value or an error. The error is returned if the member does not exist or
    # if the member value is not of the expected type
    isolated function getWithType(string key, ReqCtxMemberType targetType = <>) returns targetType|ListenerError;

    # Removes a member from the request context object. It panics if there is no such member.
    # + key - Represents the member key
    isolated function remove(string key);

    # Calls the next service in the interceptor pipeline.
    # + return - The next service object in the pipeline. An error is returned, if the call fails
    isolated function next() returns NextService|error?;
}

# Represents an HTTP response.
public class Response {
    # The response status code
    public int statusCode = 200;
    # The status code reason phrase
    public string reasonPhrase = "";
    # The server header
    public string server = "";
    # The ultimate request URI that was made to receive the response when redirect is on
    public string resolvedRequestedURI = "";
    # The cache-control directives for the response. This needs to be explicitly initialized if
    # intending on utilizing HTTP caching. For incoming responses, this will already be populated
    # if the response was sent with cache-control directives
    public ResponseCacheControl? cacheControl = ();

    isolated function init();

    # Gets the `Entity` associated with the response.
    # + return - The `Entity` of the response. An `http:ClientError` is returned, if entity construction fails
    isolated function getEntity() returns mime:Entity|ClientError; // Special Agent Note: Entity FROM ballerina/mime module

    # Sets the provided `Entity` to the response.
    # + e - The `Entity` to be set to the response
    isolated function setEntity(mime:Entity e); // Special Agent Note: Entity FROM ballerina/mime module

    # Checks whether the requested header key exists in the header map.
    # + headerName - The header name
    # + position - Represents the position of the header as an optional parameter
    # + return - `true` if the specified header key exists
    isolated function hasHeader(string headerName, HeaderPosition position = LEADING) returns boolean;

    # Returns the value of the specified header. If the specified header key maps to multiple values, the first of
    # these values is returned.
    # + headerName - The header name
    # + position - Represents the position of the header as an optional parameter. If the position is `http:TRAILING`,
    # the entity-body of the `Response` must be accessed initially.
    # + return - The first header value for the specified header name or the `HeaderNotFoundError` if the header is not
    # found.
    isolated function getHeader(string headerName, HeaderPosition position = LEADING) returns string|HeaderNotFoundError;

    # Adds the specified header to the response. Existing header values are not replaced, except for the `Content-Type`
    # header. In the case of the `Content-Type` header, the existing value is replaced with the specified value.
    # . Panic if an illegal header is passed.
    # + headerName - The header name
    # + headerValue - The header value
    # + position - Represents the position of the header as an optional parameter. If the position is `http:TRAILING`,
    # the entity-body of the `Response` must be accessed initially.
    isolated function addHeader(string headerName, string headerValue, HeaderPosition position = LEADING);

    # Gets all the header values to which the specified header key maps to.
    # + headerName - The header name
    # + position - Represents the position of the header as an optional parameter. If the position is `http:TRAILING`,
    # the entity-body of the `Response` must be accessed initially.
    # + return - The header values the specified header key maps to or the `HeaderNotFoundError` if the header is not
    # found.
    isolated function getHeaders(string headerName, HeaderPosition position = LEADING) returns string[]|HeaderNotFoundError;

    # Sets the specified header to the response. If a mapping already exists for the specified header key, the
    # existing header value is replaced with the specified header value. Panic if an illegal header is passed.
    # + headerName - The header name
    # + headerValue - The header value
    # + position - Represents the position of the header as an optional parameter. If the position is `http:TRAILING`,
    # the entity-body of the `Response` must be accessed initially.
    isolated function setHeader(string headerName, string headerValue, HeaderPosition position = LEADING);

    # Removes the specified header from the response.
    # + headerName - The header name
    # + position - Represents the position of the header as an optional parameter. If the position is `http:TRAILING`,
    # the entity-body of the `Response` must be accessed initially.
    isolated function removeHeader(string headerName, HeaderPosition position = LEADING);

    # Removes all the headers from the response.
    # + position - Represents the position of the header as an optional parameter. If the position is `http:TRAILING`,
    # the entity-body of the `Response` must be accessed initially.
    isolated function removeAllHeaders(HeaderPosition position = LEADING);

    # Gets all the names of the headers of the response.
    # + position - Represents the position of the header as an optional parameter. If the position is `http:TRAILING`,
    # the entity-body of the `Response` must be accessed initially.
    # + return - An array of all the header names
    isolated function getHeaderNames(HeaderPosition position = LEADING) returns string[];

    # Sets the `content-type` header to the response.
    # + contentType - Content type value to be set as the `content-type` header
    # + return - Nil if successful, error in case of invalid content-type
    isolated function setContentType(string contentType) returns error?;

    # Gets the type of the payload of the response (i.e., the `content-type` header value).
    # + return - The `content-type` header value as a string
    isolated function getContentType() returns string;

    # Extract `json` payload from the response. For an empty payload, `http:NoContentError` is returned.
    # 
    # If the content type is not JSON, an `http:ClientError` is returned.
    # + return - The `json` payload or `http:ClientError` in case of errors
    isolated function getJsonPayload() returns json|ClientError;

    # Extracts `xml` payload from the response. For an empty payload, `http:NoContentError` is returned.
    # 
    # If the content type is not XML, an `http:ClientError` is returned.
    # + return - The `xml` payload or `http:ClientError` in case of errors
    isolated function getXmlPayload() returns xml|ClientError;

    # Extracts `text` payload from the response. For an empty payload, `http:NoContentError` is returned.
    # 
    # If the content type is not of type text, an `http:ClientError` is returned.
    # + return - The string representation of the message payload or `http:ClientError` in case of errors
    isolated function getTextPayload() returns string|ClientError;

    # Gets the response payload as  a stream of byte[], except in the case of multiparts. To retrieve multiparts, use
    # `Response.getBodyParts()`.
    # + arraySize - A defaultable parameter to state the size of the byte array. Default size is 8KB
    # + return - A byte stream from which the message payload can be read or `http:ClientError` in case of errors
    isolated function getByteStream(int arraySize = 8192) returns stream<byte[], io:Error?>|ClientError; // Special Agent Note: Error FROM ballerina/io module

    # Gets the response payload as a `byte[]`.
    # + return - The byte[] representation of the message payload or `http:ClientError` in case of errors
    isolated function getBinaryPayload() returns byte[]|ClientError;

    # Gets the response payload as a `stream` of SseEvent.
    # + return - A SseEvent stream from which the `http:SseEvent` can be read or `http:ClientError` in case of errors
    isolated function getSseEventStream() returns stream<SseEvent, error?>|ClientError;

    # Extracts body parts from the response. If the content type is not a composite media type, an error is returned.
    # + return - The body parts as an array of entities or else an `http:ClientError` if there were any errors in
    # constructing the body parts from the response
    isolated function getBodyParts() returns mime:Entity[]|ClientError; // Special Agent Note: Entity FROM ballerina/mime module

    # Sets the `etag` header for the given payload. The ETag is generated using a CRC32 hash isolated function.
    # + payload - The payload for which the ETag should be set
    isolated function setETag(json|xml|string|byte[] payload);

    # Sets the current time as the `last-modified` header.
    isolated function setLastModified();

    # Sets a `json` as the payload. If the content-type header is not set then this method set content-type
    # headers with the default content-type, which is `application/json`. Any existing content-type can be
    # overridden by passing the content-type as an optional parameter.
    # + payload - The `json` payload
    # + contentType - The content type of the payload. This is an optional parameter.
    # The `application/json` is the default value
    isolated function setJsonPayload(json payload, string? contentType = ());

    # Sets a `anydata` payaload, as a `json` payload. If the content-type header is not set then this method set content-type
    # headers with the default content-type, which is `application/json`. Any existing content-type can be
    # overridden by passing the content-type as an optional parameter.
    # + payload - The `json` payload
    # + contentType - The content type of the payload. This is an optional parameter.
    # The `application/json` is the default value
    isolated function setAnydataAsJsonPayload(anydata payload, string? contentType = ());

    # Sets an `xml` as the payload. If the content-type header is not set then this method set content-type
    # headers with the default content-type, which is `application/xml`. Any existing content-type can be
    # overridden by passing the content-type as an optional parameter.
    # + payload - The `xml` payload
    # + contentType - The content type of the payload. This is an optional parameter.
    # The `application/xml` is the default value
    isolated function setXmlPayload(xml payload, string? contentType = ());

    # Sets a `string` as the payload. If the content-type header is not set then this method set
    # content-type headers with the default content-type, which is `text/plain`. Any
    # existing content-type can be overridden by passing the content-type as an optional parameter.
    # + payload - The `string` payload
    # + contentType - The content type of the payload. This is an optional parameter.
    # The `text/plain` is the default value
    isolated function setTextPayload(string payload, string? contentType = ());

    # Sets a `byte[]` as the payload. If the content-type header is not set then this method set content-type
    # headers with the default content-type, which is `application/octet-stream`. Any existing content-type
    # can be overridden by passing the content-type as an optional parameter.
    # + payload - The `byte[]` payload
    # + contentType - The content type of the payload. This is an optional parameter.
    # The `application/octet-stream` is the default value
    isolated function setBinaryPayload(byte[] payload, string? contentType = ());

    # Set multiparts as the payload. If the content-type header is not set then this method
    # set content-type headers with the default content-type, which is `multipart/form-data`.
    # Any existing content-type can be overridden by passing the content-type as an optional parameter.
    # + bodyParts - The entities which make up the message body
    # + contentType - The content type of the top level message. This is an optional parameter.
    # The `multipart/form-data` is the default value
    isolated function setBodyParts(mime:Entity[] bodyParts, string? contentType = ()); // Special Agent Note: Entity FROM ballerina/mime module

    # Sets the content of the specified file as the entity body of the response. If the content-type header
    # is not set then this method set content-type headers with the default content-type, which is
    # `application/octet-stream`. Any existing content-type can be overridden by passing the content-type
    # as an optional parameter.
    # + filePath - Path to the file to be set as the payload
    # + contentType - The content type of the specified file. This is an optional parameter.
    # The `application/octet-stream` is the default value
    isolated function setFileAsPayload(string filePath, string? contentType = ());

    # Sets a `Stream` as the payload. If the content-type header is not set then this method set content-type
    # headers with the default content-type, which is `application/octet-stream`. Any existing content-type can
    # be overridden by passing the content-type as an optional parameter.
    # + byteStream - Byte stream, which needs to be set to the response
    # + contentType - Content-type to be used with the payload. This is an optional parameter.
    # The `application/octet-stream` is the default value
    isolated function setByteStream(stream<byte[], io:Error?> byteStream, string? contentType = ()); // Special Agent Note: Error FROM ballerina/io module

    # Sets an `http:SseEvent` stream as the payload, along with the Content-Type and Cache-Control 
    # headers set to 'text/event-stream' and 'no-cache', respectively.
    # + eventStream - SseEvent stream, which needs to be set to the response
    isolated function setSseEventStream(stream<SseEvent, error?>|stream<SseEvent, error> eventStream);

    # Sets the response payload. This method overrides any existing content-type by passing the content-type
    # as an optional parameter. If the content type parameter is not provided then the default value derived
    # from the payload will be used as content-type only when there are no existing content-type header. If
    # the payload is non-json typed value then the value is converted to json using the `toJson` method.
    # + payload - Payload can be of type `string`, `xml`, `byte[]`, `json`, `stream<byte[], io:Error?>`,
    # stream<SseEvent, error?>(represents Server-Sent events), `Entity[]` (i.e., a set of body
    # parts) or any other value of type `anydata` which will be converted to `json` using the
    # `toJson` method.
    # + contentType - Content-type to be used with the payload. This is an optional parameter
    isolated function setPayload(anydata|mime:Entity[]|stream<byte[], io:Error?>|stream<SseEvent, error?> payload, string? contentType = ()); // Special Agent Note: Entity FROM ballerina/mime module, Error FROM ballerina/io module

    # Adds the cookie to response.
    # + cookie - The cookie, which is added to response
    isolated function addCookie(Cookie cookie);

    # Deletes the cookies in the client's cookie store.
    # + cookiesToRemove - Cookies to be deleted
    isolated function removeCookiesFromRemoteStore(Cookie... cookiesToRemove);

    # Gets cookies from the response.
    # + return - An array of cookie objects, which are included in the response
    isolated function getCookies() returns Cookie[];

    # Gets the status code response record from the response.
    # + return - The status code response record
    isolated function getStatusCodeRecord() returns StatusCodeRecord|error;
}

# Configures cache control directives for an `http:Response`.
public class ResponseCacheControl {
    # Sets the `must-revalidate` directive
    public boolean mustRevalidate = false;
    # Sets the `no-cache` directive
    public boolean noCache = false;
    # Sets the `no-store` directive
    public boolean noStore = false;
    # Sets the `no-transform` directive
    public boolean noTransform = false;
    # Sets the `private` and `public` directives
    public boolean isPrivate = false;
    # Sets the `proxy-revalidate` directive
    public boolean proxyRevalidate = false;
    # Sets the `max-age` directive
    public decimal maxAge = -1;
    # Sets the `s-maxage` directive
    public decimal sMaxAge = -1;
    # Optional fields for the `no-cache` directive. Before sending a listed field in a response, it
    # must be validated with the origin server.
    public string[] noCacheFields = [];
    # Optional fields for the `private` directive. A cache can omit the fields specified and store
    # the rest of the response.
    public string[] privateFields = [];

    isolated function populateFields(HttpCacheConfig cacheConfig);

    # Builds the cache control directives string from the current `http:ResponseCacheControl` configurations.
    # + return - The cache control directives string to be used in the `cache-control` header
    isolated function buildCacheControlDirectives() returns string;
}

# Represents the status code of `STATUS_ACCEPTED`.
public readonly class StatusAccepted {
    *Status;
    # The response status code
    public STATUS_ACCEPTED code = STATUS_ACCEPTED;
}

# Represents the status code of `STATUS_ALREADY_REPORTED`.
public readonly class StatusAlreadyReported {
    *Status;
    # The response status code
    public STATUS_ALREADY_REPORTED code = STATUS_ALREADY_REPORTED;
}

# Represents the status code of `STATUS_BAD_GATEWAY`.
public readonly class StatusBadGateway {
    *Status;
    # The response status code
    public STATUS_BAD_GATEWAY code = STATUS_BAD_GATEWAY;
}

# Represents the status code of `STATUS_BAD_REQUEST`.
public readonly class StatusBadRequest {
    *Status;
    # The response status code
    public STATUS_BAD_REQUEST code = STATUS_BAD_REQUEST;
}

# Represents the status code of `STATUS_CONFLICT`.
public readonly class StatusConflict {
    *Status;
    # The response status code
    public STATUS_CONFLICT code = STATUS_CONFLICT;
}

# Represents the status code of `STATUS_CONTINUE`.
public readonly class StatusContinue {
    *Status;
    # The response status code
    public STATUS_CONTINUE code = STATUS_CONTINUE;
}

# Represents the status code of `STATUS_CREATED`.
public readonly class StatusCreated {
    *Status;
    # The response status code
    public STATUS_CREATED code = STATUS_CREATED;
}

# Represents the status code of `STATUS_EARLY_HINTS`.
public readonly class StatusEarlyHints {
    *Status;
    # The response status code
    public STATUS_EARLY_HINTS code = STATUS_EARLY_HINTS;
}

# Represents the status code of `STATUS_EXPECTATION_FAILED`.
public readonly class StatusExpectationFailed {
    *Status;
    # The response status code
    public STATUS_EXPECTATION_FAILED code = STATUS_EXPECTATION_FAILED;
}

# Represents the status code of `STATUS_FAILED_DEPENDENCY`.
public readonly class StatusFailedDependency {
    *Status;
    # The response status code
    public STATUS_FAILED_DEPENDENCY code = STATUS_FAILED_DEPENDENCY;
}

# Represents the status code of `STATUS_FORBIDDEN`.
public readonly class StatusForbidden {
    *Status;
    # The response status code
    public STATUS_FORBIDDEN code = STATUS_FORBIDDEN;
}

# Represents the status code of `STATUS_FOUND`.
public readonly class StatusFound {
    *Status;
    # The response status code
    public STATUS_FOUND code = STATUS_FOUND;
}

# Represents the status code of `STATUS_GATEWAY_TIMEOUT`.
public readonly class StatusGatewayTimeout {
    *Status;
    # The response status code
    public STATUS_GATEWAY_TIMEOUT code = STATUS_GATEWAY_TIMEOUT;
}

# Represents the status code of `STATUS_GONE`.
public readonly class StatusGone {
    *Status;
    # The response status code
    public STATUS_GONE code = STATUS_GONE;
}

# Represents the status code of `STATUS_HTTP_VERSION_NOT_SUPPORTED`.
public readonly class StatusHttpVersionNotSupported {
    *Status;
    # The response status code
    public STATUS_HTTP_VERSION_NOT_SUPPORTED code = STATUS_HTTP_VERSION_NOT_SUPPORTED;
}

# Represents the status code of `STATUS_IM_USED`.
public readonly class StatusIMUsed {
    *Status;
    # The response status code
    public STATUS_IM_USED code = STATUS_IM_USED;
}

# Represents the status code of `STATUS_INSUFFICIENT_STORAGE`.
public readonly class StatusInsufficientStorage {
    *Status;
    # The response status code
    public STATUS_INSUFFICIENT_STORAGE code = STATUS_INSUFFICIENT_STORAGE;
}

# Represents the status code of `STATUS_INTERNAL_SERVER_ERROR`.
public readonly class StatusInternalServerError {
    *Status;
    # The response status code
    public STATUS_INTERNAL_SERVER_ERROR code = STATUS_INTERNAL_SERVER_ERROR;
}

# Represents the status code of `STATUS_LENGTH_REQUIRED`.
public readonly class StatusLengthRequired {
    *Status;
    # The response status code
    public STATUS_LENGTH_REQUIRED code = STATUS_LENGTH_REQUIRED;
}

# Represents the status code of `STATUS_LOCKED`.
public readonly class StatusLocked {
    *Status;
    # The response status code
    public STATUS_LOCKED code = STATUS_LOCKED;
}

# Represents the status code of `STATUS_LOOP_DETECTED`.
public readonly class StatusLoopDetected {
    *Status;
    # The response status code
    public STATUS_LOOP_DETECTED code = STATUS_LOOP_DETECTED;
}

# Represents the status code of `STATUS_METHOD_NOT_ALLOWED`.
public readonly class StatusMethodNotAllowed {
    *Status;
    # The response status code
    public STATUS_METHOD_NOT_ALLOWED code = STATUS_METHOD_NOT_ALLOWED;
}

# Represents the status code of `STATUS_MISDIRECTED_REQUEST`.
public readonly class StatusMisdirectedRequest {
    *Status;
    # The response status code
    public STATUS_MISDIRECTED_REQUEST code = STATUS_MISDIRECTED_REQUEST;
}

# Represents the status code of `STATUS_MOVED_PERMANENTLY`.
public readonly class StatusMovedPermanently {
    *Status;
    # The response status code
    public STATUS_MOVED_PERMANENTLY code = STATUS_MOVED_PERMANENTLY;
}

# Represents the status code of `STATUS_MULTIPLE_CHOICES`.
public readonly class StatusMultipleChoices {
    *Status;
    # The response status code
    public STATUS_MULTIPLE_CHOICES code = STATUS_MULTIPLE_CHOICES;
}

# Represents the status code of `STATUS_MULTI_STATUS`.
public readonly class StatusMultiStatus {
    *Status;
    # The response status code
    public STATUS_MULTI_STATUS code = STATUS_MULTI_STATUS;
}

# Represents the status code of `STATUS_NETWORK_AUTHENTICATION_REQUIRED`.
public readonly class StatusNetworkAuthenticationRequired {
    *Status;
    # The response status code
    public STATUS_NETWORK_AUTHENTICATION_REQUIRED code = STATUS_NETWORK_AUTHENTICATION_REQUIRED;
}

# Represents the status code of `STATUS_NO_CONTENT`.
public readonly class StatusNoContent {
    *Status;
    # The response status code
    public STATUS_NO_CONTENT code = STATUS_NO_CONTENT;
}

# Represents the status code of `STATUS_NON_AUTHORITATIVE_INFORMATION`.
public readonly class StatusNonAuthoritativeInformation {
    *Status;
    # The response status code
    public STATUS_NON_AUTHORITATIVE_INFORMATION code = STATUS_NON_AUTHORITATIVE_INFORMATION;
}

# Represents the status code of `STATUS_NOT_ACCEPTABLE`.
public readonly class StatusNotAcceptable {
    *Status;
    # The response status code
    public STATUS_NOT_ACCEPTABLE code = STATUS_NOT_ACCEPTABLE;
}

# Represents the status code of `STATUS_NOT_EXTENDED`.
public readonly class StatusNotExtended {
    *Status;
    # The response status code
    public STATUS_NOT_EXTENDED code = STATUS_NOT_EXTENDED;
}

# Represents the status code of `STATUS_NOT_FOUND`.
public readonly class StatusNotFound {
    *Status;
    # The response status code
    public STATUS_NOT_FOUND code = STATUS_NOT_FOUND;
}

# Represents the status code of `STATUS_NOT_IMPLEMENTED`.
public readonly class StatusNotImplemented {
    *Status;
    # The response status code
    public STATUS_NOT_IMPLEMENTED code = STATUS_NOT_IMPLEMENTED;
}

# Represents the status code of `STATUS_NOT_MODIFIED`.
public readonly class StatusNotModified {
    *Status;
    # The response status code
    public STATUS_NOT_MODIFIED code = STATUS_NOT_MODIFIED;
}

# Represents the status code of `STATUS_OK`.
public readonly class StatusOK {
    *Status;
    # The response status code
    public STATUS_OK code = STATUS_OK;
}

# Represents the status code of `STATUS_PARTIAL_CONTENT`.
public readonly class StatusPartialContent {
    *Status;
    # The response status code
    public STATUS_PARTIAL_CONTENT code = STATUS_PARTIAL_CONTENT;
}

# Represents the status code of `STATUS_PAYLOAD_TOO_LARGE`.
public readonly class StatusPayloadTooLarge {
    *Status;
    # The response status code
    public STATUS_PAYLOAD_TOO_LARGE code = STATUS_PAYLOAD_TOO_LARGE;
}

# Represents the status code of `STATUS_PAYMENT_REQUIRED`.
public readonly class StatusPaymentRequired {
    *Status;
    # The response status code
    public STATUS_PAYMENT_REQUIRED code = STATUS_PAYMENT_REQUIRED;
}

# Represents the status code of `STATUS_PERMANENT_REDIRECT`.
public readonly class StatusPermanentRedirect {
    *Status;
    # The response status code
    public STATUS_PERMANENT_REDIRECT code = STATUS_PERMANENT_REDIRECT;
}

# Represents the status code of `STATUS_PRECONDITION_FAILED`.
public readonly class StatusPreconditionFailed {
    *Status;
    # The response status code
    public STATUS_PRECONDITION_FAILED code = STATUS_PRECONDITION_FAILED;
}

# Represents the status code of `STATUS_PRECONDITION_REQUIRED`.
public readonly class StatusPreconditionRequired {
    *Status;
    # The response status code
    public STATUS_PRECONDITION_REQUIRED code = STATUS_PRECONDITION_REQUIRED;
}

# Represents the status code of `STATUS_PROCESSING`.
public readonly class StatusProcessing {
    *Status;
    # The response status code
    public STATUS_PROCESSING code = STATUS_PROCESSING;
}

# Represents the status code of `STATUS_PROXY_AUTHENTICATION_REQUIRED`.
public readonly class StatusProxyAuthenticationRequired {
    *Status;
    # The response status code
    public STATUS_PROXY_AUTHENTICATION_REQUIRED code = STATUS_PROXY_AUTHENTICATION_REQUIRED;
}

# Represents the status code of `STATUS_RANGE_NOT_SATISFIABLE`.
public readonly class StatusRangeNotSatisfiable {
    *Status;
    # The response status code
    public STATUS_RANGE_NOT_SATISFIABLE code = STATUS_RANGE_NOT_SATISFIABLE;
}

# Represents the status code of `STATUS_REQUEST_HEADER_FIELDS_TOO_LARGE`.
public readonly class StatusRequestHeaderFieldsTooLarge {
    *Status;
    # The response status code
    public STATUS_REQUEST_HEADER_FIELDS_TOO_LARGE code = STATUS_REQUEST_HEADER_FIELDS_TOO_LARGE;
}

# Represents the status code of `STATUS_REQUEST_TIMEOUT`.
public readonly class StatusRequestTimeout {
    *Status;
    # The response status code
    public STATUS_REQUEST_TIMEOUT code = STATUS_REQUEST_TIMEOUT;
}

# Represents the status code of `STATUS_RESET_CONTENT`.
public readonly class StatusResetContent {
    *Status;
    # The response status code
    public STATUS_RESET_CONTENT code = STATUS_RESET_CONTENT;
}

# Represents the status code of `STATUS_SEE_OTHER`.
public readonly class StatusSeeOther {
    *Status;
    # The response status code
    public STATUS_SEE_OTHER code = STATUS_SEE_OTHER;
}

# Represents the status code of `STATUS_SERVICE_UNAVAILABLE`.
public readonly class StatusServiceUnavailable {
    *Status;
    # The response status code
    public STATUS_SERVICE_UNAVAILABLE code = STATUS_SERVICE_UNAVAILABLE;
}

# Represents the status code of `STATUS_SWITCHING_PROTOCOLS`.
public readonly class StatusSwitchingProtocols {
    *Status;
    # The response status code
    public STATUS_SWITCHING_PROTOCOLS code = STATUS_SWITCHING_PROTOCOLS;
}

# Represents the status code of `STATUS_TEMPORARY_REDIRECT`.
public readonly class StatusTemporaryRedirect {
    *Status;
    # The response status code
    public STATUS_TEMPORARY_REDIRECT code = STATUS_TEMPORARY_REDIRECT;
}

# Represents the status code of `STATUS_TOO_EARLY`.
public readonly class StatusTooEarly {
    *Status;
    # The response status code
    public STATUS_TOO_EARLY code = STATUS_TOO_EARLY;
}

# Represents the status code of `STATUS_TOO_MANY_REQUESTS`.
public readonly class StatusTooManyRequests {
    *Status;
    # The response status code
    public STATUS_TOO_MANY_REQUESTS code = STATUS_TOO_MANY_REQUESTS;
}

# Represents the status code of `STATUS_UNAUTHORIZED`.
public readonly class StatusUnauthorized {
    *Status;
    # The response status code
    public STATUS_UNAUTHORIZED code = STATUS_UNAUTHORIZED;
}

# Represents the status code of `STATUS_UNAVAILABLE_DUE_TO_LEGAL_REASONS`.
public readonly class StatusUnavailableDueToLegalReasons {
    *Status;
    # The response status code
    public STATUS_UNAVAILABLE_DUE_TO_LEGAL_REASONS code = STATUS_UNAVAILABLE_DUE_TO_LEGAL_REASONS;
}

# Represents the status code of `STATUS_UNPROCESSABLE_ENTITY`.
public readonly class StatusUnprocessableEntity {
    *Status;
    # The response status code
    public STATUS_UNPROCESSABLE_ENTITY code = STATUS_UNPROCESSABLE_ENTITY;
}

# Represents the status code of `STATUS_UNSUPPORTED_MEDIA_TYPE`.
public readonly class StatusUnsupportedMediaType {
    *Status;
    # The response status code
    public STATUS_UNSUPPORTED_MEDIA_TYPE code = STATUS_UNSUPPORTED_MEDIA_TYPE;
}

# Represents the status code of `STATUS_UPGRADE_REQUIRED`.
public readonly class StatusUpgradeRequired {
    *Status;
    # The response status code
    public STATUS_UPGRADE_REQUIRED code = STATUS_UPGRADE_REQUIRED;
}

# Represents the status code of `STATUS_URI_TOO_LONG`.
public readonly class StatusUriTooLong {
    *Status;
    # The response status code
    public STATUS_URI_TOO_LONG code = STATUS_URI_TOO_LONG;
}

# Represents the status code of `STATUS_USE_PROXY`.
public readonly class StatusUseProxy {
    *Status;
    # The response status code
    public STATUS_USE_PROXY code = STATUS_USE_PROXY;
}

# Represents the status code of `STATUS_VARIANT_ALSO_NEGOTIATES`.
public readonly class StatusVariantAlsoNegotiates {
    *Status;
    # The response status code
    public STATUS_VARIANT_ALSO_NEGOTIATES code = STATUS_VARIANT_ALSO_NEGOTIATES;
}

# The representation of the http Client object type for managing resilient clients.
public type ClientObject client object {
    # The client resource function to send HTTP POST requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function post [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The client resource function to send HTTP PUT requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function put [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The client resource function to send HTTP PATCH requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The client resource function to send HTTP DELETE requests to HTTP endpoints.
    # + message - An optional HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The client resource function to send HTTP HEAD requests to HTTP endpoints.
    # + headers - The entity headers
    # + params - The query parameters
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated resource function head [PathParamType ...path](map<string|string[]>? headers = (), *QueryParams params) returns Response|ClientError;

    # The client resource function to send HTTP GET requests to HTTP endpoints.
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function get [PathParamType ...path](map<string|string[]>? headers = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The client resource function to send HTTP OPTIONS requests to HTTP endpoints.
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function options [PathParamType ...path](map<string|string[]>? headers = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The `Client.post()` function can be used to send HTTP POST requests to HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function post(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The `Client.put()` function can be used to send HTTP PUT requests to HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function put(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The `Client.patch()` function can be used to send HTTP PATCH requests to HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function patch(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The `Client.delete()` function can be used to send HTTP DELETE requests to HTTP endpoints.
    # + path - Resource path
    # + message - An optional HTTP outbound request message or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function delete(string path, RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The `Client.head()` function can be used to send HTTP HEAD requests to HTTP endpoints.
    # + path - Resource path
    # + headers - The entity headers
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated remote function head(string path, map<string|string[]>? headers = ()) returns Response|ClientError;

    # The `Client.get()` function can be used to send HTTP GET requests to HTTP endpoints.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function get(string path, map<string|string[]>? headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # The `Client.options()` function can be used to send HTTP OPTIONS requests to HTTP endpoints.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function options(string path, map<string|string[]>? headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # Invokes an HTTP call with the specified HTTP verb.
    # + httpVerb - HTTP verb value
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function execute(string httpVerb, string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The `Client.forward()` function can be used to invoke an HTTP call with inbound request's HTTP verb
    # + path - Request path
    # + request - An HTTP inbound request message
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function forward(string path, Request request, TargetType targetType = <>) returns targetType|ClientError;

    # Submits an HTTP request to a service with the specified HTTP verb.
    # The `Client->submit()` function does not give out a `http:Response` as the result.
    # Rather it returns an `http:HttpFuture` which can be used to do further interactions with the endpoint.
    # + httpVerb - The HTTP verb value. The HTTP verb is case-sensitive. Use the `http:Method` type to specify the
    # the standard HTTP methods.
    # + path - The resource path
    # + message - An HTTP outbound request message or any payload of type `string`, `xml`, `json`, `byte[]`
    # or `mime:Entity[]`
    # + return - An `http:HttpFuture` that represents an asynchronous service invocation or else an `http:ClientError` if the submission fails
    isolated remote function submit(string httpVerb, string path, RequestMessage message) returns HttpFuture|ClientError;

    # This just pass the request to actual network call.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:Response` message or else an `http: ClientError` if the invocation fails
    isolated remote function getResponse(HttpFuture httpFuture) returns Response|ClientError;

    # This just pass the request to actual network call.
    # + httpFuture - The `http:HttpFuture` relates to a previous asynchronous invocation
    # + return - A `boolean`, which represents whether an `http:PushPromise` exists
    isolated remote function hasPromise(HttpFuture httpFuture) returns boolean;

    # This just pass the request to actual network call.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:PushPromise` message or else an `http:ClientError` if the invocation fails
    isolated remote function getNextPromise(HttpFuture httpFuture) returns PushPromise|ClientError;

    # Passes the request to an actual network call.
    # + promise - The related `http:PushPromise`
    # + return - A promised `http:Response` message or else an `http:ClientError` if the invocation fails
    isolated remote function getPromisedResponse(PushPromise promise) returns Response|ClientError;

    # This just pass the request to actual network call.
    # + promise - The Push Promise to be rejected
    isolated remote function rejectPromise(PushPromise promise);
};

# LoadBalancerRule object type provides a required abstraction to implement different algorithms.
public type LoadBalancerRule object {
    # Provides an HTTP client which is chosen according to the algorithm.
    # + loadBalanceCallerActionsArray - Array of HTTP clients which needs to be load balanced
    # + return - Chosen `Client` from the algorithm or an `http:ClientError`
    # for the failure in the algorithm implementation
    isolated function getNextClient(Client?[] loadBalanceCallerActionsArray) returns Client|ClientError;
};

# The representation of a persistent cookie handler object type for managing persistent cookies.
public type PersistentCookieHandler object {
    # Adds a persistent cookie to the cookie store.
    # + cookie - Cookie to be added
    # + return - An `http:CookieHandlingError` if there is any error occurred during the storing process of the cookie or else `()`
    isolated function storeCookie(Cookie cookie) returns CookieHandlingError?;

    # Gets all persistent cookies.
    # + return - Array of persistent cookies stored in the cookie store or else an `http:CookieHandlingError` if one occurred during the retrieval of the cookies
    isolated function getAllCookies() returns Cookie[]|CookieHandlingError;

    # Removes a specific persistent cookie.
    # + name - Name of the persistent cookie to be removed
    # + domain - Domain of the persistent cookie to be removed
    # + path - Path of the persistent cookie to be removed
    # + return - An `http:CookieHandlingError` if there is one occurred during the removal of the cookie or else `()`
    isolated function removeCookie(string name, string domain, string path) returns CookieHandlingError?;

    # Removes all persistent cookies.
    # + return - An `http:CookieHandlingError` if there is one occurred during the removal of all the cookies or else `()`
    isolated function removeAllCookies() returns CookieHandlingError?;
};

# The `Status` object creates the distinction for the different response status code types.
public type Status distinct object {
    # The response status code
    public int code;
};

# The representation of the http Status Code Client object type for managing resilient clients.
public type StatusCodeClientObject client object {
    # The client resource function to send HTTP POST requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function post [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The client resource function to send HTTP PUT requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function put [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The client resource function to send HTTP PATCH requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The client resource function to send HTTP DELETE requests to HTTP endpoints.
    # + message - An optional HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The client resource function to send HTTP HEAD requests to HTTP endpoints.
    # + headers - The entity headers
    # + params - The query parameters
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated resource function head [PathParamType ...path](map<string|string[]>? headers = (), *QueryParams params) returns Response|ClientError;

    # The client resource function to send HTTP GET requests to HTTP endpoints.
    # + headers - The entity headers
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function get [PathParamType ...path](map<string|string[]>? headers = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The client resource function to send HTTP OPTIONS requests to HTTP endpoints.
    # + headers - The entity headers
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function options [PathParamType ...path](map<string|string[]>? headers = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The `Client.post()` function can be used to send HTTP POST requests to HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function post(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The `Client.put()` function can be used to send HTTP PUT requests to HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function put(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The `Client.patch()` function can be used to send HTTP PATCH requests to HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function patch(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The `Client.delete()` function can be used to send HTTP DELETE requests to HTTP endpoints.
    # + path - Resource path
    # + message - An optional HTTP outbound request message or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function delete(string path, RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The `Client.head()` function can be used to send HTTP HEAD requests to HTTP endpoints.
    # + path - Resource path
    # + headers - The entity headers
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated remote function head(string path, map<string|string[]>? headers = ()) returns Response|ClientError;

    # The `Client.get()` function can be used to send HTTP GET requests to HTTP endpoints.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function get(string path, map<string|string[]>? headers = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The `Client.options()` function can be used to send HTTP OPTIONS requests to HTTP endpoints.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function options(string path, map<string|string[]>? headers = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # Invokes an HTTP call with the specified HTTP verb.
    # + httpVerb - HTTP verb value
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function execute(string httpVerb, string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The `Client.forward()` function can be used to invoke an HTTP call with inbound request's HTTP verb
    # + path - Request path
    # + request - An HTTP inbound request message
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function forward(string path, Request request, typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # Submits an HTTP request to a service with the specified HTTP verb.
    # The `Client->submit()` function does not give out a `http:Response` as the result.
    # Rather it returns an `http:HttpFuture` which can be used to do further interactions with the endpoint.
    # + httpVerb - The HTTP verb value. The HTTP verb is case-sensitive. Use the `http:Method` type to specify the
    # the standard HTTP methods.
    # + path - The resource path
    # + message - An HTTP outbound request message or any payload of type `string`, `xml`, `json`, `byte[]`
    # or `mime:Entity[]`
    # + return - An `http:HttpFuture` that represents an asynchronous service invocation or else an `http:ClientError` if the submission fails
    isolated remote function submit(string httpVerb, string path, RequestMessage message) returns HttpFuture|ClientError;

    # This just pass the request to actual network call.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:Response` message or else an `http: ClientError` if the invocation fails
    isolated remote function getResponse(HttpFuture httpFuture) returns Response|ClientError;

    # This just pass the request to actual network call.
    # + httpFuture - The `http:HttpFuture` relates to a previous asynchronous invocation
    # + return - A `boolean`, which represents whether an `http:PushPromise` exists
    isolated remote function hasPromise(HttpFuture httpFuture) returns boolean;

    # This just pass the request to actual network call.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:PushPromise` message or else an `http:ClientError` if the invocation fails
    isolated remote function getNextPromise(HttpFuture httpFuture) returns PushPromise|ClientError;

    # Passes the request to an actual network call.
    # + promise - The related `http:PushPromise`
    # + return - A promised `http:Response` message or else an `http:ClientError` if the invocation fails
    isolated remote function getPromisedResponse(PushPromise promise) returns Response|ClientError;

    # This just pass the request to actual network call.
    # + promise - The Push Promise to be rejected
    isolated remote function rejectPromise(PushPromise promise);
};

# The HTTP service type.
public type Service distinct service object {
};

# The HTTP service contract type.
public type ServiceContract distinct service object {
};

# The HTTP request interceptor service object type
public type RequestInterceptor distinct service object {
};

# The HTTP response interceptor service object type
public type ResponseInterceptor distinct service object {
};

# The HTTP request error interceptor service object type
public type RequestErrorInterceptor distinct service object {
};

# The HTTP response error interceptor service object type
public type ResponseErrorInterceptor distinct service object {
};

# The service type to be used when engaging interceptors at the service level
public type InterceptableService distinct service object {
    # Function to define interceptor pipeline
    # + return - The `http:Interceptor|http:Interceptor[]`
    function createInterceptors() returns Interceptor|Interceptor[];
};

# Used for configuring the caching behaviour. Setting the `policy` field in the `CacheConfig` record allows
# the user to control the caching behaviour.
public type CachingPolicy CACHE_CONTROL_AND_VALIDATORS|RFC_7234;

# The types of messages that are accepted by HTTP `client` when sending out the outbound request.
public type RequestMessage anydata|Request|mime:Entity[]|stream<byte[], io:Error?>; // Special Agent Note: Entity FROM ballerina/mime module, Error FROM ballerina/io module

# The types of messages that are accepted by HTTP `listener` when sending out the outbound response.
public type ResponseMessage anydata|Response|mime:Entity[]|stream<byte[], io:Error?>|stream<SseEvent, error?>|stream<SseEvent, error>; // Special Agent Note: Entity FROM ballerina/mime module, Error FROM ballerina/io module

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
public type HttpOperation HTTP_FORWARD|HTTP_GET|HTTP_POST|HTTP_DELETE|HTTP_OPTIONS|HTTP_PUT|HTTP_PATCH|HTTP_HEAD|HTTP_SUBMIT|HTTP_NONE;

# Defines the possible values for the keep-alive configuration in service and client endpoints.
public type KeepAlive KEEPALIVE_AUTO|KEEPALIVE_ALWAYS|KEEPALIVE_NEVER;

# Defines the possible values for the mutual ssl status.
# 
# `passed`: Mutual SSL handshake is successful.
# `failed`: Mutual SSL handshake has failed.
public type MutualSslStatus PASSED|FAILED|();

# Defines the HTTP redirect codes as a type.
public type RedirectCode REDIRECT_MULTIPLE_CHOICES_300|REDIRECT_MOVED_PERMANENTLY_301|REDIRECT_FOUND_302|REDIRECT_SEE_OTHER_303|REDIRECT_NOT_MODIFIED_304|REDIRECT_USE_PROXY_305|REDIRECT_TEMPORARY_REDIRECT_307|REDIRECT_PERMANENT_REDIRECT_308;

# A finite type for modeling the states of the Circuit Breaker. The Circuit Breaker starts in the `CLOSED` state.
# If any failure thresholds are exceeded during execution, the circuit trips and goes to the `OPEN` state. After
# the specified timeout period expires, the circuit goes to the `HALF_OPEN` state. If the trial request sent while
# in the `HALF_OPEN` state succeeds, the circuit goes back to the `CLOSED` state.
public type CircuitState CB_OPEN_STATE|CB_HALF_OPEN_STATE|CB_CLOSED_STATE;

# Defines the possible values for the chunking configuration in HTTP services and clients.
# 
# `AUTO`: If the payload is less than 8KB, content-length header is set in the outbound request/response,
# otherwise chunking header is set in the outbound request/response
# `ALWAYS`: Always set chunking header in the response
# `NEVER`: Never set the chunking header even if the payload is larger than 8KB in the outbound request/response
public type Chunking CHUNKING_AUTO|CHUNKING_ALWAYS|CHUNKING_NEVER;

# Options to compress using gzip or deflate.
# 
# `AUTO`: When service behaves as a HTTP gateway inbound request/response accept-encoding option is set as the
# outbound request/response accept-encoding/content-encoding option
# `ALWAYS`: Always set accept-encoding/content-encoding in outbound request/response
# `NEVER`: Never set accept-encoding/content-encoding header in outbound request/response
public type Compression COMPRESSION_AUTO|COMPRESSION_ALWAYS|COMPRESSION_NEVER;

# Defines the position of the headers in the request/response.
# 
# `LEADING`: Header is placed before the payload of the request/response
# `TRAILING`: Header is placed after the payload of the request/response
public type HeaderPosition LEADING|TRAILING;

# Represents OAuth2 grant configurations for OAuth2 authentication.
public type OAuth2GrantConfig OAuth2ClientCredentialsGrantConfig|OAuth2PasswordGrantConfig|OAuth2RefreshTokenGrantConfig|OAuth2JwtBearerGrantConfig;

# Defines the authentication configurations for the HTTP client.
public type ClientAuthConfig CredentialsConfig|BearerTokenConfig|JwtIssuerConfig|OAuth2GrantConfig;

# Defines the authentication configurations for the HTTP listener.
public type ListenerAuthConfig FileUserStoreConfigWithScopes|LdapUserStoreConfigWithScopes|JwtValidatorConfigWithScopes|OAuth2IntrospectionConfigWithScopes;

# Defines the possible status code response record types.
public type StatusCodeResponse Continue|SwitchingProtocols|Processing|EarlyHints|Ok|Created|Accepted|NonAuthoritativeInformation|NoContent|ResetContent|PartialContent|MultiStatus|AlreadyReported|IMUsed|MultipleChoices|MovedPermanently|Found|SeeOther|NotModified|UseProxy|TemporaryRedirect|PermanentRedirect|BadRequest|Unauthorized|PaymentRequired|Forbidden|NotFound|MethodNotAllowed|NotAcceptable|ProxyAuthenticationRequired|RequestTimeout|Conflict|Gone|LengthRequired|PreconditionFailed|PayloadTooLarge|UriTooLong|UnsupportedMediaType|RangeNotSatisfiable|ExpectationFailed|MisdirectedRequest|UnprocessableEntity|Locked|FailedDependency|TooEarly|PreconditionRequired|UnavailableDueToLegalReasons|UpgradeRequired|TooManyRequests|RequestHeaderFieldsTooLarge|InternalServerError|NotImplemented|BadGateway|ServiceUnavailable|GatewayTimeout|HttpVersionNotSupported|VariantAlsoNegotiates|InsufficientStorage|LoopDetected|NotExtended|NetworkAuthenticationRequired|DefaultStatusCodeResponse;

# Represents a non-error type that can be cloned.
public type Cloneable (any & readonly)|xml|Cloneable[]|map<Cloneable>|table<map<Cloneable>>;

# Request context member type.
public type ReqCtxMember Cloneable|isolated object {};

# The return type of an interceptor service function
public type NextService RequestInterceptor|ResponseInterceptor|Service;

# Types of HTTP interceptor services
public type Interceptor RequestInterceptor|ResponseInterceptor|RequestErrorInterceptor|ResponseErrorInterceptor;

# Defines the path parameter types.
public type PathParamType boolean|int|float|decimal|string;

# Defines the possible simple query parameter types.
public type SimpleQueryParamType boolean|int|float|decimal|string;

# Defines the query parameter type supported with client resource methods.
public type QueryParamType SimpleQueryParamType[]|SimpleQueryParamType;

# Represents an error, which occurred due to a failure in interceptor return.
public type InterceptorReturnError distinct ListenerError & httpscerr:InternalServerErrorError; // Special Agent Note: InternalServerErrorError FROM ballerina/http.httpscerr module

# Represents an error, which occurred due to a query parameter binding.
public type QueryParameterBindingError distinct ListenerError & httpscerr:BadRequestError; // Special Agent Note: BadRequestError FROM ballerina/http.httpscerr module

# Represents an error, which occurred due to a path parameter binding.
public type PathParameterBindingError distinct ListenerError & httpscerr:BadRequestError; // Special Agent Note: BadRequestError FROM ballerina/http.httpscerr module

# Defines the authentication error types that returned from listener.
public type ListenerAuthnError distinct httpscerr:UnauthorizedError & ListenerAuthError;

# Defines the authorization error types that returned from listener.
public type ListenerAuthzError distinct httpscerr:ForbiddenError & ListenerAuthError;

# Represents an error occurred in an remote function of the Load Balance connector.
public type LoadBalanceActionError distinct ResiliencyError & error<LoadBalanceActionErrorData>;

# Represents Service Not Found error.
public type ServiceNotFoundError httpscerr:NotFoundError & ServiceDispatchingError; // Special Agent Note: NotFoundError FROM ballerina/http.httpscerr module

# Represents Bad Matrix Parameter in the request error.
public type BadMatrixParamError httpscerr:BadRequestError & ServiceDispatchingError; // Special Agent Note: BadRequestError FROM ballerina/http.httpscerr module

# Represents an error, which occurred when the resource is not found during dispatching.
public type ResourceNotFoundError httpscerr:NotFoundError & ResourceDispatchingError; // Special Agent Note: NotFoundError FROM ballerina/http.httpscerr module

# Represents an error, which occurred due to a path parameter constraint validation.
public type ResourcePathValidationError httpscerr:BadRequestError & ResourceDispatchingError; // Special Agent Note: BadRequestError FROM ballerina/http.httpscerr module

# Represents an error, which occurred when the resource method is not allowed during dispatching.
public type ResourceMethodNotAllowedError httpscerr:MethodNotAllowedError & ResourceDispatchingError; // Special Agent Note: MethodNotAllowedError FROM ballerina/http.httpscerr module

# Represents an error, which occurred when the media type is not supported during dispatching.
public type UnsupportedRequestMediaTypeError httpscerr:UnsupportedMediaTypeError & ResourceDispatchingError; // Special Agent Note: UnsupportedMediaTypeError FROM ballerina/http.httpscerr module

# Represents an error, which occurred when the payload is not acceptable during dispatching.
public type RequestNotAcceptableError httpscerr:NotAcceptableError & ResourceDispatchingError; // Special Agent Note: NotAcceptableError FROM ballerina/http.httpscerr module

# Represents other internal server errors during dispatching.
public type ResourceDispatchingServerError httpscerr:InternalServerErrorError & ResourceDispatchingError; // Special Agent Note: InternalServerErrorError FROM ballerina/http.httpscerr module

# Represents the client status code binding error
public type StatusCodeResponseBindingError distinct ClientError & error<StatusCodeBindingErrorDetail>;

# Represents the status code binding error that occurred due to 4XX status code response binding
public type StatusCodeBindingClientRequestError distinct StatusCodeResponseBindingError & ClientRequestError;

# Represents the status code binding error that occurred due to 5XX status code response binding
public type StatusCodeBindingRemoteServerError distinct StatusCodeResponseBindingError & RemoteServerError;

# The types of data values that are expected by the HTTP `client` to return after the data binding operation.
public type TargetType typedesc<Response|anydata|stream<SseEvent, error?>>;

# Request context member type descriptor.
public type ReqCtxMemberType typedesc<ReqCtxMember>;

# The common status code response constant of `Continue`.
public final readonly & Continue CONTINUE = {};

# The common status code response constant of `SwitchingProtocols`.
public final readonly & SwitchingProtocols SWITCHING_PROTOCOLS = {};

# The common status code response constant of `Processing`.
public final readonly & Processing PROCESSING = {};

# The common status code response constant of `EarlyHints`.
public final readonly & EarlyHints EARLY_HINTS = {};

# The common status code response constant of `Ok`.
public final readonly & Ok OK = {};

# The common status code response constant of `Created`.
public final readonly & Created CREATED = {};

# The common status code response constant of `Accepted`.
public final readonly & Accepted ACCEPTED = {};

# The common status code response constant of `NonAuthoritativeInformation`.
public final readonly & NonAuthoritativeInformation NON_AUTHORITATIVE_INFORMATION = {};

# The common status code response constant of `NoContent`.
public final readonly & NoContent NO_CONTENT = {};

# The common status code response constant of `ResetContent`.
public final readonly & ResetContent RESET_CONTENT = {};

# The common status code response constant of `PartialContent`.
public final readonly & PartialContent PARTIAL_CONTENT = {};

# The common status code response constant of `MultiStatus`.
public final readonly & MultiStatus MULTI_STATUS = {};

# The common status code response constant of `AlreadyReported`.
public final readonly & AlreadyReported ALREADY_REPORTED = {};

# The common status code response constant of `IMUsed`.
public final readonly & IMUsed IM_USED = {};

# The common status code response constant of `MultipleChoices`.
public final readonly & MultipleChoices MULTIPLE_CHOICES = {};

# The common status code response constant of `MovedPermanently`.
public final readonly & MovedPermanently MOVED_PERMANENTLY = {};

# The common status code response constant of `Found`.
public final readonly & Found FOUND = {};

# The common status code response constant of `SeeOther`.
public final readonly & SeeOther SEE_OTHER = {};

# The common status code response constant of `NotModified`.
public final readonly & NotModified NOT_MODIFIED = {};

# The common status code response constant of `UseProxy`.
public final readonly & UseProxy USE_PROXY = {};

# The common status code response constant of `TemporaryRedirect`.
public final readonly & TemporaryRedirect TEMPORARY_REDIRECT = {};

# The common status code response constant of `PermanentRedirect`.
public final readonly & PermanentRedirect PERMANENT_REDIRECT = {};

# The common status code response constant of `BadRequest`.
public final readonly & BadRequest BAD_REQUEST = {};

# The common status code response constant of `Unauthorized`.
public final readonly & Unauthorized UNAUTHORIZED = {};

# The common status code response constant of `PaymentRequired`.
public final readonly & PaymentRequired PAYMENT_REQUIRED = {};

# The common status code response constant of `Forbidden`.
public final readonly & Forbidden FORBIDDEN = {};

# The common status code response constant of `NotFound`.
public final readonly & NotFound NOT_FOUND = {};

# The common status code response constant of `MethodNotAllowed`.
public final readonly & MethodNotAllowed METHOD_NOT_ALLOWED = {};

# The common status code response constant of `NotAcceptable`.
public final readonly & NotAcceptable NOT_ACCEPTABLE = {};

# The common status code response constant of `ProxyAuthenticationRequired`.
public final readonly & ProxyAuthenticationRequired PROXY_AUTHENTICATION_REQUIRED = {};

# The common status code response constant of `RequestTimeout`.
public final readonly & RequestTimeout REQUEST_TIMEOUT = {};

# The common status code response constant of `Conflict`.
public final readonly & Conflict CONFLICT = {};

# The common status code response constant of `Gone`.
public final readonly & Gone GONE = {};

# The common status code response constant of `LengthRequired`.
public final readonly & LengthRequired LENGTH_REQUIRED = {};

# The common status code response constant of `PreconditionFailed`.
public final readonly & PreconditionFailed PRECONDITION_FAILED = {};

# The common status code response constant of `PayloadTooLarge`.
public final readonly & PayloadTooLarge PAYLOAD_TOO_LARGE = {};

# The common status code response constant of `UriTooLong`.
public final readonly & UriTooLong URI_TOO_LONG = {};

# The common status code response constant of `UnsupportedMediaType`.
public final readonly & UnsupportedMediaType UNSUPPORTED_MEDIA_TYPE = {};

# The common status code response constant of `RangeNotSatisfiable`.
public final readonly & RangeNotSatisfiable RANGE_NOT_SATISFIABLE = {};

# The common status code response constant of `ExpectationFailed`.
public final readonly & ExpectationFailed EXPECTATION_FAILED = {};

# The common status code response constant of `MisdirectedRequest`.
public final readonly & MisdirectedRequest MISDIRECTED_REQUEST = {};

# The common status code response constant of `UnprocessableEntity`.
public final readonly & UnprocessableEntity UNPROCESSABLE_ENTITY = {};

# The common status code response constant of `Locked`.
public final readonly & Locked LOCKED = {};

# The common status code response constant of `FailedDependency`.
public final readonly & FailedDependency FAILED_DEPENDENCY = {};

# The common status code response constant of `TooEarly`.
public final readonly & TooEarly TOO_EARLY = {};

# The common status code response constant of `PreconditionRequired`.
public final readonly & PreconditionRequired PREDICTION_REQUIRED = {};

# The common status code response constant of `UnavailableDueToLegalReasons`.
public final readonly & UnavailableDueToLegalReasons UNAVAILABLE_DUE_TO_LEGAL_REASONS = {};

# The common status code response constant of `UpgradeRequired`.
public final readonly & UpgradeRequired UPGRADE_REQUIRED = {};

# The common status code response constant of `TooManyRequests`.
public final readonly & TooManyRequests TOO_MANY_REQUESTS = {};

# The common status code response constant of `RequestHeaderFieldsTooLarge`.
public final readonly & RequestHeaderFieldsTooLarge REQUEST_HEADER_FIELDS_TOO_LARGE = {};

# The common status code response constant of `InternalServerError`.
public final readonly & InternalServerError INTERNAL_SERVER_ERROR = {};

# The common status code response constant of `NotImplemented`.
public final readonly & NotImplemented NOT_IMPLEMENTED = {};

# The common status code response constant of `BadGateway`.
public final readonly & BadGateway BAD_GATEWAY = {};

# The common status code response constant of `ServiceUnavailable`.
public final readonly & ServiceUnavailable SERVICE_UNAVAILABLE = {};

# The common status code response constant of `GatewayTimeout`.
public final readonly & GatewayTimeout GATEWAY_TIMEOUT = {};

# The common status code response constant of `HttpVersionNotSupported`.
public final readonly & HttpVersionNotSupported HTTP_VERSION_NOT_SUPPORTED = {};

# The common status code response constant of `VariantAlsoNegotiates`.
public final readonly & VariantAlsoNegotiates VARIANT_ALSO_NEGOTIATES = {};

# The common status code response constant of `InsufficientStorage`.
public final readonly & InsufficientStorage INSUFFICIENT_STORAGE = {};

# The common status code response constant of `LoopDetected`.
public final readonly & LoopDetected LOOP_DETECTED = {};

# The common status code response constant of `NotExtended`.
public final readonly & NotExtended NOT_EXTENDED = {};

# The common status code response constant of `NetworkAuthenticationRequired`.
public final readonly & NetworkAuthenticationRequired NETWORK_AUTHENTICATION_REQUIRED = {};

// --- Client ---

# The caller actions for responding to client requests.
public isolated client class Caller {
    # Sends the outbound response to the caller.
    # + message - The outbound response or status code response or error or any allowed payload
    # + return - An `http:ListenerError` if failed to respond or else `()`
    isolated remote function respond(ResponseMessage|StatusCodeResponse|error message = ()) returns ListenerError?;

    # Pushes a promise to the caller.
    # + promise - Push promise message
    # + return - An `http:ListenerError` in case of failures
    isolated remote function promise(PushPromise promise) returns ListenerError?;

    # Sends a promised push response to the caller.
    # + promise - Push promise message
    # + response - The outbound response
    # + return - An `http:ListenerError` in case of failures while responding with the promised response
    isolated remote function pushPromisedResponse(PushPromise promise, Response response) returns ListenerError?;

    # Sends a `100-continue` response to the caller.
    # + return - An `http:ListenerError` if failed to send the `100-continue` response or else `()`
    isolated remote function 'continue() returns ListenerError?;

    # Sends a redirect response to the user with the specified redirection status code.
    # + response - Response to be sent to the caller
    # + code - The redirect status code to be sent
    # + locations - An array of URLs to which the caller can redirect to
    # + return - An `http:ListenerError` if failed to send the redirect response or else `()`
    isolated remote function redirect(Response response, RedirectCode code, string[] locations) returns ListenerError?;

    # Gets the hostname from the remote address. This method may trigger a DNS reverse lookup if the address was created
    # with a literal IP address.
    # ```ballerina
    # string? remoteHost = caller.getRemoteHostName();
    # ```
    # + return - The hostname of the address or else `()` if it is unresolved
    isolated function getRemoteHostName() returns string?;
}

# The HTTP client provides functionality to connect to remote HTTP services and perform requests using standard HTTP methods like GET, POST, PUT, DELETE, etc.
public isolated client class Client {
    # Gets invoked to initialize the `client`. During initialization, the configurations provided through the `config`
    # record is used to determine which type of additional behaviours are added to the endpoint (e.g., caching,
    # security, circuit breaking).
    # + url - URL of the target service
    # + config - The configurations to be used when initializing the `client`
    # + return - The `client` or an `http:ClientError` if the initialization failed
    isolated function init(string url, *ClientConfiguration config) returns ClientError?;

    # The client resource function to send HTTP GET requests to HTTP endpoints.
    # + headers - The entity headers
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function get [PathParamType ...path](map<string|string[]>? headers = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # Retrieve a representation of a specified resource from an HTTP endpoint.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function get(string path, map<string|string[]>? headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP POST requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function post [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # Create a new resource or submit data to a resource for processing.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function post(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP PUT requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function put [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # Create a new resource or replace a representation of a specified resource.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function put(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP DELETE requests to HTTP endpoints.
    # + message - An optional HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # Remove a specified resource from an HTTP endpoint.
    # + path - Resource path
    # + message - An optional HTTP outbound request message or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function delete(string path, RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP PATCH requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # Partially update an existing resource in an HTTP endpoint.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function patch(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP HEAD requests to HTTP endpoints.
    # + headers - The entity headers
    # + params - The query parameters
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated resource function head [PathParamType ...path](map<string|string[]>? headers = (), *QueryParams params) returns Response|ClientError;

    # Get the metadata of a resource in the form of headers without the body. Often used for testing the resource existence or finding recent modifications.
    # + path - Resource path
    # + headers - The entity headers
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated remote function head(string path, map<string|string[]>? headers = ()) returns Response|ClientError;

    # The client resource function to send HTTP OPTIONS requests to HTTP endpoints.
    # + headers - The entity headers
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function options [PathParamType ...path](map<string|string[]>? headers = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # Get the communication options for a specified resource.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function options(string path, map<string|string[]>? headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # Send a request using any HTTP method. Can be used to invoke the endpoint with a custom or less common HTTP method.
    # + httpVerb - HTTP verb value
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function execute(string httpVerb, string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # Forward an incoming request to another endpoint using the same HTTP method. Can be used in proxy or gateway scenarios.
    # + path - Request path
    # + request - An HTTP inbound request message
    # + targetType - Expected return type (to be used for automatic data binding).
    # Supported types:
    # - Built-in subtypes of `anydata` (`string`, `byte[]`, `json|xml`, etc.)
    # - Custom types (e.g., `User`, `Student?`, `Person[]`, etc.)
    # - Full HTTP response with headers and status (`http:Response`)
    # - Stream of Server-Sent Events (`stream<http:SseEvent, error?>`)
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function forward(string path, Request request, TargetType targetType = <>) returns targetType|ClientError;

    # Send an asynchronous HTTP request that does not wait for the response immediately. Can be used for non-blocking operations.
    # + httpVerb - The HTTP verb value. The HTTP verb is case-sensitive. Use the `http:Method` type to specify the
    # the standard HTTP methods.
    # + path - The resource path
    # + message - An HTTP outbound request or any allowed payload
    # + return - An `http:HttpFuture` that represents an asynchronous service invocation or else an `http:ClientError` if the submission fails
    isolated remote function submit(string httpVerb, string path, RequestMessage message) returns HttpFuture|ClientError;

    # Get the response from a previously submitted asynchronous request. Can be used after calling `submit()` action to retrieve the actual response.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:Response` message or else an `http: ClientError` if the invocation fails
    isolated remote function getResponse(HttpFuture httpFuture) returns Response|ClientError;

    # Check if the server has sent a push promise for additional resources. Should be used with HTTP/2 server push functionality.
    # + httpFuture - The `http:HttpFuture` relates to a previous asynchronous invocation
    # + return - A `boolean`, which represents whether an `http:PushPromise` exists
    isolated remote function hasPromise(HttpFuture httpFuture) returns boolean;

    # Get the next server push promise that contains information about additional resources the server wants to send.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:PushPromise` message or else an `http:ClientError` if the invocation fails
    isolated remote function getNextPromise(HttpFuture httpFuture) returns PushPromise|ClientError;

    # Get the actual response data from a server push promise. Can be used to receive resources that the server proactively sends.
    # + promise - The related `http:PushPromise`
    # + return - A promised `http:Response` message or else an `http:ClientError` if the invocation fails
    isolated remote function getPromisedResponse(PushPromise promise) returns Response|ClientError;

    # Reject a server push promise to decline receiving the additional resource.
    # + promise - The Push Promise to be rejected
    isolated remote function rejectPromise(PushPromise promise);

    # Get the cookie storage associated with this HTTP client. Can be used to access stored cookies for session management.
    # + return - The cookie store related to the client
    isolated function getCookieStore() returns CookieStore?;

    # Force the circuit breaker to allow all requests through, ignoring current error rates. Can be used to manually
    # restore service after fixing issues.
    isolated function circuitBreakerForceClose();

    # Force the circuit breaker to block all requests until the reset time expires. Can be used to manually stop
    # requests during maintenance or known issues.
    isolated function circuitBreakerForceOpen();

    # Check the current state of the circuit breaker. Can be used to monitor the health status of your HTTP connections.
    # + return - The current `http:CircuitState` of the circuit breaker
    isolated function getCircuitBreakerCurrentState() returns CircuitState;
}

# Defines the OAuth2 handler for client authentication.
public isolated client class ClientOAuth2Handler {
    # Initializes the `http:ClientOAuth2Handler` object.
    # + config - The `http:OAuth2GrantConfig` instance
    isolated function init(OAuth2GrantConfig config);

    # Enrich the request with the relevant authentication requirements.
    # + req - The `http:Request` instance
    # + return - The updated `http:Request` instance or else an `http:ClientAuthError` in case of an error
    isolated remote function enrich(Request req) returns Request|ClientAuthError;

    # Enrich the headers map with the relevant authentication requirements.
    # + headers - The headers map
    # + return - The updated headers map or else an `http:ClientAuthError` in case of an error
    isolated function enrichHeaders(map<string|string[]> headers) returns map<string|string[]>|ClientAuthError;

    # Returns the headers map with the relevant authentication requirements.
    # + return - The updated headers map or else an `http:ClientAuthError` in case of an error
    isolated function getSecurityHeaders() returns map<string|string[]>|ClientAuthError;
}

# An HTTP client endpoint which provides failover support over multiple HTTP clients.
public isolated client class FailoverClient {
    # Failover caller actions which provides failover capabilities to an HTTP client endpoint.
    # + failoverClientConfig - The configurations of the client endpoint associated with this `Failover` instance
    # + return - The `client` or an `http:ClientError` if the initialization failed
    isolated function init(*FailoverClientConfiguration failoverClientConfig) returns ClientError?;

    # The POST resource function implementation of the Failover Connector.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function post [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The POST remote function implementation of the Failover Connector.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function post(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The PUT resource function implementation of the Failover Connector.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function put [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The PUT remote function  implementation of the Failover Connector.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function put(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The PATCH resource function implementation of the Failover Connector.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The PATCH remote function implementation of the Failover Connector.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function patch(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The DELETE resource function implementation of the Failover Connector.
    # + message - An optional HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The DELETE remote function implementation of the Failover Connector.
    # + path - Resource path
    # + message - An optional HTTP outbound request message or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function delete(string path, RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The HEAD resource function implementation of the Failover Connector.
    # + headers - The entity headers
    # + params - The query parameters
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated resource function head [PathParamType ...path](map<string|string[]>? headers = (), *QueryParams params) returns Response|ClientError;

    # The HEAD remote function implementation of the Failover Connector.
    # + path - Resource path
    # + headers - The entity headers
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated remote function head(string path, map<string|string[]>? headers = ()) returns Response|ClientError;

    # The GET resource function implementation of the Failover Connector.
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function get [PathParamType ...path](map<string|string[]>? headers = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The GET remote function implementation of the Failover Connector.
    # + path - Resource path
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function get(string path, map<string|string[]>? headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # The OPTIONS resource function implementation of the Failover Connector.
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function options [PathParamType ...path](map<string|string[]>? headers = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The OPTIONS remote function implementation of the Failover Connector.
    # + path - Resource path
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function options(string path, map<string|string[]>? headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # Invokes an HTTP call with the specified HTTP method.
    # + httpVerb - HTTP verb value
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function execute(string httpVerb, string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # Invokes an HTTP call using the incoming request's HTTP method.
    # + path - Resource path
    # + request - An HTTP request
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function forward(string path, Request request, TargetType targetType = <>) returns targetType|ClientError;

    # Submits an HTTP request to a service with the specified HTTP verb. The `FailoverClient.submit()` function does not
    # return an `http:Response` as the result. Rather it returns an `http:HttpFuture` which can be used for subsequent interactions
    # with the HTTP endpoint.
    # + httpVerb - The HTTP verb value. The HTTP verb is case-sensitive. Use the `http:Method` type to specify the
    # the standard HTTP methods.
    # + path - The resource path
    # + message - An HTTP outbound request or any allowed payload
    # + return - An `http:HttpFuture` that represents an asynchronous service invocation or else an `http:ClientError` if the submission
    # fails
    isolated remote function submit(string httpVerb, string path, RequestMessage message) returns HttpFuture|ClientError;

    # Retrieves the `http:Response` for a previously-submitted request.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:Response` message or else an `http:ClientError` if the invocation fails
    isolated remote function getResponse(HttpFuture httpFuture) returns Response|ClientError;

    # Checks whether an `http:PushPromise` exists for a previously-submitted request.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - A `boolean`, which represents whether an `http:PushPromise` exists
    isolated remote function hasPromise(HttpFuture httpFuture) returns boolean;

    # Retrieves the next available `http:PushPromise` for a previously-submitted request.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:PushPromise` message or else an `http:ClientError` if the invocation fails
    isolated remote function getNextPromise(HttpFuture httpFuture) returns PushPromise|ClientError;

    # Retrieves the promised server push `http:Response` message.
    # + promise - The related `http:PushPromise`
    # + return - A promised `http:Response` message or else an `http:ClientError` if the invocation fails
    isolated remote function getPromisedResponse(PushPromise promise) returns Response|ClientError;

    # Rejects an `http:PushPromise`. When an `http:PushPromise` is rejected, there is no chance of fetching a promised
    # response using the rejected promise.
    # + promise - The Push Promise to be rejected
    isolated remote function rejectPromise(PushPromise promise);

    # Gets the index of the `TargetService[]` array which given a successful response.
    # + return - The successful target endpoint index
    isolated function getSucceededEndpointIndex() returns int;
}

# Defines the LDAP store Basic Auth handler for listener authentication.
public isolated client class ListenerLdapUserStoreBasicAuthHandler {
    # Initializes the `http:ListenerLdapUserStoreBasicAuthProvider` object.
    # + config - The `http:LdapUserStoreConfig` instance
    isolated function init(LdapUserStoreConfig config);

    # Authenticates with the relevant authentication requirements.
    # + data - The `http:Request` instance or `http:Headers` instance or `string` Authorization header
    # + return - The `auth:UserDetails` instance or else `Unauthorized` type in case of an error
    isolated remote function authenticate(Request|Headers|string data) returns auth:UserDetails|Unauthorized; // Special Agent Note: UserDetails FROM ballerina/auth module

    # Authorizes with the relevant authorization requirements.
    # + userDetails - The `auth:UserDetails` instance which is received from authentication results
    # + expectedScopes - The expected scopes as `string` or `string[]`
    # + return - `()`, if it is successful or else `Forbidden` type in case of an error
    isolated remote function authorize(auth:UserDetails userDetails, string|string[] expectedScopes) returns Forbidden?; // Special Agent Note: UserDetails FROM ballerina/auth module
}

# Defines the OAuth2 handler for listener authentication.
public isolated client class ListenerOAuth2Handler {
    # Initializes the `http:ListenerOAuth2Handler` object.
    # + config - The `http:OAuth2IntrospectionConfig` instance
    isolated function init(OAuth2IntrospectionConfig config);

    # Authorizes with the relevant authentication & authorization requirements.
    # + data - The `http:Request` instance or `http:Headers` instance or `string` Authorization header
    # + expectedScopes - The expected scopes as `string` or `string[]`
    # + optionalParams - Map of optional parameters that need to be sent to introspection endpoint
    # + return - The `oauth2:IntrospectionResponse` instance or else `Unauthorized` or `Forbidden` type in case of an error
    isolated remote function authorize(Request|Headers|string data, string|string[]? expectedScopes = (), map<string>? optionalParams = ()) returns oauth2:IntrospectionResponse|Unauthorized|Forbidden; // Special Agent Note: IntrospectionResponse FROM ballerina/oauth2 module
}

# LoadBalanceClient endpoint provides load balancing functionality over multiple HTTP clients.
public isolated client class LoadBalanceClient {
    # Load Balancer adds an additional layer to the HTTP client to make network interactions more resilient.
    # + loadBalanceClientConfig - The configurations for the load balance client endpoint
    # + return - The `client` or an `http:ClientError` if the initialization failed
    isolated function init(*LoadBalanceClientConfiguration loadBalanceClientConfig) returns ClientError?;

    # The POST resource function implementation of the LoadBalancer Connector.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function post [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The POST remote function implementation of the LoadBalancer Connector.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function post(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The PUT resource function implementation of the LoadBalancer Connector.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function put [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The PUT remote function implementation of the Load Balance Connector.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function put(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The PATCH resource function implementation of the LoadBalancer Connector.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The PATCH remote function implementation of the LoadBalancer Connector.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function patch(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The DELETE resource function implementation of the LoadBalancer Connector.
    # + message - An optional HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The DELETE remote function implementation of the LoadBalancer Connector.
    # + path - Resource path
    # + message - An optional HTTP outbound request message or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function delete(string path, RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The HEAD resource function implementation of the LoadBalancer Connector.
    # + headers - The entity headers
    # + params - The query parameters
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated resource function head [PathParamType ...path](map<string|string[]>? headers = (), *QueryParams params) returns Response|ClientError;

    # The HEAD remote function implementation of the LoadBalancer Connector.
    # + path - Resource path
    # + headers - The entity headers
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated remote function head(string path, map<string|string[]>? headers = ()) returns Response|ClientError;

    # The GET resource function implementation of the LoadBalancer Connector.
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function get [PathParamType ...path](map<string|string[]>? headers = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The GET remote function implementation of the LoadBalancer Connector.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function get(string path, map<string|string[]>? headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # The OPTIONS resource function implementation of the LoadBalancer Connector.
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function options [PathParamType ...path](map<string|string[]>? headers = (), TargetType targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The OPTIONS remote function implementation of the LoadBalancer Connector.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function options(string path, map<string|string[]>? headers = (), TargetType targetType = <>) returns targetType|ClientError;

    # The EXECUTE remote function implementation of the LoadBalancer Connector.
    # + httpVerb - HTTP verb value
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function execute(string httpVerb, string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), TargetType targetType = <>) returns targetType|ClientError;

    # The FORWARD remote function implementation of the LoadBalancer Connector.
    # + path - Resource path
    # + request - An HTTP request
    # + targetType - HTTP response, `anydata` or stream of HTTP SSE, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function forward(string path, Request request, TargetType targetType = <>) returns targetType|ClientError;

    # The submit implementation of the LoadBalancer Connector.
    # + httpVerb - The HTTP verb value. The HTTP verb is case-sensitive. Use the `http:Method` type to specify the
    # the standard HTTP methods.
    # + path - The resource path
    # + message - An HTTP outbound request or any allowed payload
    # + return - An `http:HttpFuture` that represents an asynchronous service invocation or else an `http:ClientError` if the submission
    # fails
    isolated remote function submit(string httpVerb, string path, RequestMessage message) returns HttpFuture|ClientError;

    # The getResponse implementation of the LoadBalancer Connector.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:Response` message or else an `http:ClientError` if the invocation fails
    isolated remote function getResponse(HttpFuture httpFuture) returns Response|ClientError;

    # The hasPromise implementation of the LoadBalancer Connector.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - A `boolean`, which represents whether an `http:PushPromise` exists
    isolated remote function hasPromise(HttpFuture httpFuture) returns boolean;

    # The getNextPromise implementation of the LoadBalancer Connector.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:PushPromise` message or else an `http:ClientError` if the invocation fails
    isolated remote function getNextPromise(HttpFuture httpFuture) returns PushPromise|ClientError;

    # The getPromisedResponse implementation of the LoadBalancer Connector.
    # + promise - The related `http:PushPromise`
    # + return - A promised `http:Response` message or else an `http:ClientError` if the invocation fails
    isolated remote function getPromisedResponse(PushPromise promise) returns Response|ClientError;

    # The rejectPromise implementation of the LoadBalancer Connector.
    # + promise - The Push Promise to be rejected
    isolated remote function rejectPromise(PushPromise promise);
}

# The HTTP status code client provides the capability for initiating contact with a remote HTTP service. The API it
# provides includes the functions for the standard HTTP methods forwarding a received request and sending requests
# using custom HTTP verbs. The responses can be binded to `http:StatusCodeResponse` types
public isolated client class StatusCodeClient {
    # Gets invoked to initialize the `client`. During initialization, the configurations provided through the `config`
    # record is used to determine which type of additional behaviours are added to the endpoint (e.g., caching,
    # security, circuit breaking).
    # + url - URL of the target service
    # + config - The configurations to be used when initializing the `client`
    # + return - The `client` or an `http:ClientError` if the initialization failed
    isolated function init(string url, *ClientConfiguration config) returns ClientError?;

    # The client resource function to send HTTP POST requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function post [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The `Client.post()` function can be used to send HTTP POST requests to HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function post(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP PUT requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function put [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The `Client.put()` function can be used to send HTTP PUT requests to HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function put(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP PATCH requests to HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function patch [PathParamType ...path](RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The `Client.patch()` function can be used to send HTTP PATCH requests to HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function patch(string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP DELETE requests to HTTP endpoints.
    # + message - An optional HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function delete [PathParamType ...path](RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The `Client.delete()` function can be used to send HTTP DELETE requests to HTTP endpoints.
    # + path - Resource path
    # + message - An optional HTTP outbound request message or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function delete(string path, RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP HEAD requests to HTTP endpoints.
    # + headers - The entity headers
    # + params - The query parameters
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated resource function head [PathParamType ...path](map<string|string[]>? headers = (), *QueryParams params) returns Response|ClientError;

    # The `Client.head()` function can be used to send HTTP HEAD requests to HTTP endpoints.
    # + path - Resource path
    # + headers - The entity headers
    # + return - The response or an `http:ClientError` if failed to establish the communication with the upstream server
    isolated remote function head(string path, map<string|string[]>? headers = ()) returns Response|ClientError;

    # The client resource function to send HTTP GET requests to HTTP endpoints.
    # + headers - The entity headers
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function get [PathParamType ...path](map<string|string[]>? headers = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The `Client.get()` function can be used to send HTTP GET requests to HTTP endpoints.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function get(string path, map<string|string[]>? headers = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP OPTIONS requests to HTTP endpoints.
    # + headers - The entity headers
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function options [PathParamType ...path](map<string|string[]>? headers = (), typedesc<StatusCodeResponse> targetType = <>, *QueryParams params) returns targetType|ClientError;

    # The `Client.options()` function can be used to send HTTP OPTIONS requests to HTTP endpoints.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function options(string path, map<string|string[]>? headers = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # Invokes an HTTP call with the specified HTTP verb.
    # + httpVerb - HTTP verb value
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function execute(string httpVerb, string path, RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # The `Client.forward()` function can be used to invoke an HTTP call with inbound request's HTTP verb
    # + path - Request path
    # + request - An HTTP inbound request message
    # + targetType - HTTP status code response, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `http:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function forward(string path, Request request, typedesc<StatusCodeResponse> targetType = <>) returns targetType|ClientError;

    # Submits an HTTP request to a service with the specified HTTP verb.
    # The `Client->submit()` function does not give out a `http:Response` as the result.
    # Rather it returns an `http:HttpFuture` which can be used to do further interactions with the endpoint.
    # + httpVerb - The HTTP verb value. The HTTP verb is case-sensitive. Use the `http:Method` type to specify the
    # the standard HTTP methods.
    # + path - The resource path
    # + message - An HTTP outbound request or any allowed payload
    # + return - An `http:HttpFuture` that represents an asynchronous service invocation or else an `http:ClientError` if the submission fails
    isolated remote function submit(string httpVerb, string path, RequestMessage message) returns HttpFuture|ClientError;

    # This just pass the request to actual network call.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:Response` message or else an `http: ClientError` if the invocation fails
    isolated remote function getResponse(HttpFuture httpFuture) returns Response|ClientError;

    # This just pass the request to actual network call.
    # + httpFuture - The `http:HttpFuture` relates to a previous asynchronous invocation
    # + return - A `boolean`, which represents whether an `http:PushPromise` exists
    isolated remote function hasPromise(HttpFuture httpFuture) returns boolean;

    # This just pass the request to actual network call.
    # + httpFuture - The `http:HttpFuture` related to a previous asynchronous invocation
    # + return - An `http:PushPromise` message or else an `http:ClientError` if the invocation fails
    isolated remote function getNextPromise(HttpFuture httpFuture) returns PushPromise|ClientError;

    # Passes the request to an actual network call.
    # + promise - The related `http:PushPromise`
    # + return - A promised `http:Response` message or else an `http:ClientError` if the invocation fails
    isolated remote function getPromisedResponse(PushPromise promise) returns Response|ClientError;

    # This just pass the request to actual network call.
    # + promise - The Push Promise to be rejected
    isolated remote function rejectPromise(PushPromise promise);

    # Retrieves the cookie store of the client.
    # + return - The cookie store related to the client
    isolated function getCookieStore() returns CookieStore?;

    # The circuit breaker client related method to force the circuit into a closed state in which it will allow
    # requests regardless of the error percentage until the failure threshold exceeds.
    isolated function circuitBreakerForceClose();

    # The circuit breaker client related method to force the circuit into a open state in which it will suspend all
    # requests until `resetTime` interval exceeds.
    isolated function circuitBreakerForceOpen();

    # The circuit breaker client related method to provides the `http:CircuitState` of the circuit breaker.
    # + return - The current `http:CircuitState` of the circuit breaker
    isolated function getCircuitBreakerCurrentState() returns CircuitState;
}

// --- Functions ---

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

// --- Listeners ---

# This is used for creating HTTP server endpoints. An HTTP server endpoint is capable of responding to
# remote callers. The `Listener` is responsible for initializing the endpoint using the provided configurations.
public isolated class Listener {
    # Gets invoked during module initialization to initialize the listener.
    # + port - Listening port of the HTTP service listener
    # + config - Configurations for the HTTP service listener
    # + return - A `ListenerError` if an error occurred during the listener initialization
    isolated function init(int port, *ListenerConfiguration config) returns ListenerError?;

    # Starts the registered service programmatically.
    # + return - An `error` if an error occurred during the listener starting process
    isolated function 'start() returns error?;

    # Stops the service listener gracefully. Already-accepted requests will be served before connection closure.
    # + return - An `error` if an error occurred during the listener stopping process
    isolated function gracefulStop() returns error?;

    # Stops the service listener immediately. It is not implemented yet.
    # + return - An `error` if an error occurred during the listener stop process
    isolated function immediateStop() returns error?;

    # Attaches a service to the listener.
    # + httpService - The service that needs to be attached
    # + name - Name of the service
    # + return - An `error` if an error occurred during the service attachment process or else `()`
    isolated function attach(Service httpService, string[]|string? name = ()) returns error?;

    # Detaches an HTTP service from the listener.
    # + httpService - The service to be detached
    # + return - An `error` if one occurred during detaching of a service or else `()`
    isolated function detach(Service httpService) returns error?;

    # Retrieves the port of the HTTP listener.
    # + return - The HTTP listener port
    isolated function getPort() returns int;

    # Retrieves the `InferredListenerConfiguration` of the HTTP listener.
    # + return - The readonly HTTP listener inferred configuration
    isolated function getConfig() returns readonly & InferredListenerConfiguration;
}

// --- Service ---

service http:Service on new http:Listener(port, config) {
    // Central publishes no method contract for this service type. The listener may still require
    // one — add the resource or remote methods the package's guide shows; `bal library overview`
    // reproduces it.
}

// These service object types are declared above; this reader cannot confirm that http:Listener
// accepts them, so it writes no attachment template for them:
//   http:ServiceContract
//   http:RequestInterceptor
//   http:ResponseInterceptor
//   http:RequestErrorInterceptor
//   http:ResponseErrorInterceptor
//   http:InterceptableService
// http:Listener.attach takes one specific type. A `distinct service object` type reaches it only
// by INCLUDING that type, and Central publishes no inclusion for an object type — so some of these
// do attach and some do not, and the payload cannot say which. An interceptor type, for one, reaches
// the runtime as a `createInterceptors()` return rather than as an attachment. The package's own
// guide is where the usage of each is written; `bal library overview` reproduces it.

// --- Annotations ---

# The annotation which is used to define the response cache configuration. This annotation only supports `anydata` and
# Success(2XX) `StatusCodeResponses` return types. Default annotation adds `must-revalidate,public,max-age=3600` as
# `cache-control` header in addition to `etag` and `last-modified` headers.
public annotation HttpCacheConfig Cache on return;

# The annotation which is used to configure the type of the response.
public annotation HttpCallerInfo CallerInfo on parameter;

# The annotation which is used to define the Header parameter.
public annotation HttpHeader Header on parameter, record field;

# The annotation which is used to define the Payload resource signature parameter and return parameter.
public annotation HttpPayload Payload on parameter, return;

# The annotation which is used to define the query parameter.
public annotation HttpQuery Query on parameter, record field;

# The annotation which is used to configure an HTTP resource.
public annotation HttpResourceConfig ResourceConfig on object function;

# The annotation which is used to configure an HTTP service.
public annotation HttpServiceConfig ServiceConfig on service, type;

// --- Configurables ---

// Set in the CALLER's Config.toml, under a [ballerina.http] table. These are
// module-private, so they cannot be referenced from code — a default above that a signature
// also names is one you set here rather than pass.

// traceLogConsole = false    # boolean

// traceLogAdvancedConfig = {}    # TraceLogAdvancedConfiguration

// accessLogConfig = {}    # AccessLogConfiguration

// maxActiveConnections = -1    # int

// maxIdleConnections = 100    # int

// waitTime = 30    # decimal

// maxActiveStreamsPerConnection = 100    # int

// minEvictableIdleTime = 300    # decimal

// timeBetweenEvictionRuns = 30    # decimal

// minIdleTimeInStaleState = 300    # decimal

// timeBetweenStaleEviction = 30    # decimal

// defaultListenerPort = 9090    # int — Default HTTP listener port used by the HTTP Default Listener.

// defaultListenerConfig = {}    # ListenerConfiguration — Default HTTP listener configuration used by the HTTP Default Listener.
