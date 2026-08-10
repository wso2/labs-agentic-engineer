// ============================================================
// Library: ballerina/graphql
// This module provides APIs for connecting and interacting with GraphQL endpoints.
// ============================================================
import ballerina/graphql;

// --- Types ---

# Represents a GraphQL enum value.

type __EnumValue record {
    # The name of the enum value
    string name;
    # The description of the enum value
    string? description = ();
    # Whether the enum value is deprecated
    boolean isDeprecated = false;
    # The reason for deprecation of the enum value
    string? deprecationReason = ();
};

# Represents a GraphQL field.

type __Field record {
    # The name of the field
    string name;
    # The description of the field
    string? description = ();
    # The arguments of the field
    __InputValue[] args;
    __Type 'type;
    # Whether the field is deprecated
    boolean isDeprecated = false;
    # The reason for deprecation of the field
    string? deprecationReason = ();
};

# Represents a GraphQL input value.

type __InputValue record {
    # The name of the input value
    string name;
    # The description of the input value
    string? description = ();
    __Type 'type;
    # The default value of the input value, if there is one
    string? defaultValue = ();
};

# Represents a GraphQL schema type.

type __Type record {
    # The `__TypeKind` of the type
    __TypeKind kind;
    # The name of the type. This can be nil if the type is `NON_NULL` or `LIST`
    string? name = ();
    # The description of the type
    string? description = ();
    # The fields of the type. This only applies if the `kind` is `OBJECT` or `INTERFACE`. Otherwise,
this will be nil.
    __Field[]? fields = ();
    # The interfaces of the type. This only applies if the `kind` is `OBJECT` or `INTERFACE`. Otherwise,
this will be nil.
    __Type[]? interfaces = ();
    # The possible types of the type. This only applies if the `kind` is `UNION` or `INTERFACE`.
Otherwise, this will be nil.
    __Type[]? possibleTypes = ();
    # The enum values of the type. This only applies if the `kind` is `ENUM`. Otherwise, this will be nil.
    __EnumValue[]? enumValues = ();
    # The input fields of the type. This only applies if the `kind` is `INPUT_OBJECT`. Otherwise,
this will be nil.
    __InputValue[]? inputFields = ();
    # The type of the type. This only applies if the `kind` is `NON_NULL` or `LIST`. Otherwise, this will be nil.
    __Type? ofType = ();
};

# Represents token for Bearer token authentication.

type BearerTokenConfig record {
};

# Provides a set of configurations for controlling the caching behaviour of the endpoint.

type CacheConfig record {
};

# Provides a set of configurations for controlling the behaviour of the Circuit Breaker.

type CircuitBreakerConfig record {
};

# Provides a set of configurations for controlling the behaviour of the GraphQL client when communicating with
# the GraphQL server that operates over HTTP.

type ClientConfiguration record {
    # Configurations related to HTTP/1.1 protocol
    ClientHttp1Settings http1Settings = {};
    # The maximum time to wait (in seconds) for a response before closing the connection
    decimal timeout = 60;
    # The choice of setting `forwarded`/`x-forwarded` header
    string forwarded = "disable";
    # Configurations associated with Redirection
    FollowRedirects? followRedirects = ();
    # Configurations associated with request pooling
    PoolConfiguration? poolConfig = ();
    # HTTP caching related configurations
    CacheConfig cache = {};
    # Specifies the way of handling compression (`accept-encoding`) header
    Compression compression = COMPRESSION_AUTO;
    # Configurations related to client authentication
    ClientAuthConfig? auth = ();
    # Configurations associated with the behaviour of the Circuit Breaker
    CircuitBreakerConfig? circuitBreaker = ();
    # Configurations associated with retrying
    RetryConfig? retryConfig = ();
    # Configurations associated with cookies
    CookieConfig? cookieConfig = ();
    # Configurations associated with inbound response size limits
    ResponseLimitConfigs responseLimits = {};
    # SSL/TLS-related options
    ClientSecureSocket? secureSocket = ();
    # Proxy server related options
    ProxyConfig? proxy = ();
    # Enables the inbound payload validation functionality which provided by the constraint package. Enabled by default
    boolean validation = true;
};

# Provides settings related to HTTP/1.x protocol.

type ClientHttp1Settings record {
};

# Provides configurations for facilitating secure communication with a remote GraphQL endpoint.

type ClientSecureSocket record {
};

# Client configuration for cookies.

type CookieConfig record {
};

# Represent CORS configurations for internal HTTP service

type CorsConfig record {
};

# Represents credentials for Basic Auth authentication.

type CredentialsConfig record {
};

# Represent the document cache configurations of GraphQL server.

type DocumentCacheConfig record {
    # State of the document caching
    boolean enabled = true;
    # Maximum number of cache entries
    int maxSize = 100;
};

# Represents an error in GraphQL.

type ErrorDetail record {
    # The error message describing the error
    string message;
    # The locations in the GraphQL document related to the error
    json[] locations;
    # The path of the field in the GraphQL document related to the error
    int|string[] path;
    # Additional information to error
    map<anydata> extensions;
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

type FollowRedirects record {
};

# Represents the target type binding record with data and extensions of a GraphQL response for `executeWithType` method.

type GenericResponse record {
    # Meta information on protocol extensions from the GraphQL server
    map<json?> extensions?;
    # The requested data from the GraphQL server
    record {}|map<json?> data?;
};

# Represents the target type binding record with data, extensions and errors of a GraphQL response for `execute` method.

type GenericResponseWithErrors record {
    # Meta information on protocol extensions from the GraphQL server
    map<json|()> extensions;
    # The requested data from the GraphQL server
    record {}|map<json|()> data;
    # The errors occurred (if present) while processing the GraphQL request.
    ErrorDetail[] errors?;
};

# Represent GraphiQL client configurations

type Graphiql record {
    # Status of the client
    boolean enabled = false;
    # Path for the client
    string path = "graphiql";
    # Enable/disable printing the GraphiQL client URL to the stdout
    boolean printUrl = true;
};

# Provides a set of configurations for the GraphQL interceptors.

type GraphqlInterceptorConfig record {
    # Scope of the interceptor. If `true`, the interceptor will be applied to all the resolvers.
    boolean global = true;
};

# Provides a set of configurations for the GraphQL resolvers.

type GraphqlResourceConfig record {
    # GraphQL field level interceptors
    (readonly&Interceptor)|(readonly&Interceptor)[] interceptors = [];
    # The name of the instance method to be used for prefetching
    string prefetchMethodName?;
    # The cache configurations for the fields
    ServerCacheConfig cacheConfig?;
    # The complexity value of the field
    int complexity?;
};

# Provides a set of configurations for the GraphQL service.

type GraphqlServiceConfig record {
    # The maximum depth allowed for a query
    int maxQueryDepth?;
    # Listener authentication configurations
    ListenerAuthConfig[] auth?;
    # Function to initialize the context. If not provided, an empty context will be created
    ContextInit contextInit = initDefaultContext;
    # The cross origin resource sharing configurations for the service
    CorsConfig cors?;
    # GraphiQL client configurations
    Graphiql graphiql = {};
    # The generated schema. This is auto-generated at the compile-time. Providing a value for this field will end up in
a compilation error.
    string schemaString = "";
    # GraphQL service level interceptors
    (readonly&Interceptor)|(readonly&Interceptor)[] interceptors = [];
    # Whether to enable or disable the introspection on the service
    boolean introspection = true;
    # Whether to enable or disable the constraint validation
    boolean validation = true;
    # The cache configurations for the operations
    ServerCacheConfig cacheConfig?;
    # The field cache config derived from the resource annotations. This is auto-generated at the compile time
    ServerCacheConfig? fieldCacheConfig = ();
    # The query complexity configuration for the service.
    QueryComplexityConfig queryComplexityConfig?;
    # The document cache configurations for the service
    DocumentCacheConfig documentCacheConfig?;
};

# Represents JWT issuer configurations for JWT authentication.

type JwtIssuerConfig record {
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

# Provides a set of configurations for configure the underlying HTTP listener of the GraphQL listener.

type ListenerConfiguration record {
};

# Provides settings related to HTTP/1.x protocol, when using HTTP 1.x as the underlying protocol for the GraphQL
# service.

type ListenerHttp1Settings record {
};

# Configures the SSL/TLS options to be used for the underlying HTTP service used in GraphQL service.

type ListenerSecureSocket record {
};

# Represents a location in a GraphQL document.

type Location record {
    # The line of the document
    int line;
    # The column of the document
    int column;
};

# Represents OAuth2 client credentials grant configurations for OAuth2 authentication.

type OAuth2ClientCredentialsGrantConfig record {
};

# Represents OAuth2 introspection server configurations for OAuth2 authentication.

type OAuth2IntrospectionConfig record {
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
};

# Represents OAuth2 password grant configurations for OAuth2 authentication.

type OAuth2PasswordGrantConfig record {
};

# Represents OAuth2 refresh token grant configurations for OAuth2 authentication.

type OAuth2RefreshTokenGrantConfig record {
};

# Configurations for managing GraphQL client connection pool.

type PoolConfiguration record {
};

# Proxy server configurations to be used with the GraphQL client endpoint.

type ProxyConfig record {
};

# Defines the query complexity configuration for the GraphQL service.

type QueryComplexityConfig record {
    # Maximum allowed query depth
    int maxComplexity = 100;
    # Default complexity for a field
    int defaultFieldComplexity = 1;
    # Whether to only log a warning or return an error when the complexity exceeds the limit
    boolean warnOnly = false;
};

# Provides inbound request URI, total header and entity body size threshold configurations.

type RequestLimitConfigs record {
};

# Provides inbound response status line, total header and entity body size threshold configurations.

type ResponseLimitConfigs record {
};

# Provides configurations for controlling the retrying behavior in failure scenarios.

type RetryConfig record {
};

# Represent the cache configurations of GraphQL server.

type ServerCacheConfig record {
    # State of the caching
    boolean enabled = true;
    # TTL of the cache in seconds
    decimal maxAge = 60;
    # Maximum number of cache entries
    int maxSize = 120;
};

# The input parameter type used for file uploads in GraphQL mutations.

type Upload record {
    # Name of the file
    string fileName;
    # File mime type according to the content
    string mimeType;
    # File stream encoding
    string encoding;
    # File content as a stream of `byte[]`
    stream<byte[],io:Error?> byteStream; // Special Agent Note: Error FROM ballerina/io package
};

# Represents the authentication error type.
type AuthnError distinct Error;

# Represents the authorization error type.
type AuthzError distinct Error;

# Represents GraphQL client related generic errors.
type ClientError distinct error;

# Represents any error related to the Ballerina GraphQL module.
type Error distinct error;

# Represents network level errors.
type HttpError distinct (RequestError&error);

# Represents GraphQL errors due to request validation.
type InvalidDocumentError distinct (RequestError&error);

# Represents client side data binding error.
type PayloadBindingError distinct (ClientError&error);

# Represents GraphQL client side or network level errors.
type RequestError distinct ClientError;

# Represents GraphQL API response during GraphQL API server side errors.
type ServerError distinct (ClientError&error);

# The prefix used to denote the Basic authentication scheme.
const string AUTH_SCHEME_BASIC = "Basic";

# The prefix used to denote the Bearer authentication scheme.
const string AUTH_SCHEME_BEARER = "Bearer";

# Always set accept-encoding/content-encoding in outbound request/response.
const string COMPRESSION_ALWAYS = "ALWAYS";

# When service behaves as a HTTP gateway inbound request/response accept-encoding option is set as the
# outbound request/response accept-encoding/content-encoding option.
const string COMPRESSION_AUTO = "AUTO";

# Never set accept-encoding/content-encoding header in outbound request/response.
const string COMPRESSION_NEVER = "NEVER";

# Represents a GraphQL type kind. This is used to represent the kind of a GraphQL type.
enum __TypeKind {
    SCALAR,
    OBJECT,
    ENUM,
    NON_NULL,
    LIST,
    UNION,
    INTERFACE,
    INPUT_OBJECT
}

class Context {
}

class Field {
}

# Defines the authentication configurations for the GraphQL listener.
type ListenerAuthConfig FileUserStoreConfigWithScopes|LdapUserStoreConfigWithScopes|JwtValidatorConfigWithScopes|OAuth2IntrospectionConfigWithScopes;

# Defines the authentication configurations for the GraphQL client.
type ClientAuthConfig CredentialsConfig|BearerTokenConfig|JwtIssuerConfig|OAuth2GrantConfig;

# Represents OAuth2 grant configurations for OAuth2 authentication.
type OAuth2GrantConfig OAuth2ClientCredentialsGrantConfig|OAuth2PasswordGrantConfig|OAuth2RefreshTokenGrantConfig|OAuth2JwtBearerGrantConfig;

# Represents the Scalar types supported by the Ballerina GraphQL module.
type Scalar boolean|int|float|string|decimal;

# Options to compress using gzip or deflate.
type Compression COMPRESSION_AUTO|COMPRESSION_ALWAYS|COMPRESSION_NEVER;

// --- Client ---

# The Ballerina GraphQL client that can be used to communicate with GraphQL APIs.
client class Client {
    function init(string serviceUrl, ClientConfiguration clientConfig) returns ClientError?;

    # Executes a GraphQL document and data binds the GraphQL response to a record with data and extensions
    # which is a subtype of GenericResponse.
    remote function executeWithType(string document, map<anydata> variables = (), string? operationName = (), map<string|string[]> headers = (), typedesc<GenericResponse|record {}|json> targetType = <>) returns targetType|ClientError;

    # Executes a GraphQL document and data binds the GraphQL response to a record with data, extensions and errors
    # which is a subtype of GenericResponseWithErrors.
    remote function execute(string document, map<anydata> variables = (), string? operationName = (), map<string|string[]> headers = (), typedesc<GenericResponseWithErrors|record {}|json> targetType = <>) returns targetType|ClientError;
}

// --- Functions ---

# Adds an error to the GraphQL response. Using this to add an error is not recommended.
# + context - The context of the GraphQL request.
# + errorDetail - The error to be added to the response.
function __addError(Context context, ErrorDetail errorDetail) returns nil;

# Obtains the schema representation of a federated subgraph, expressed in the SDL format.
# + encodedSchemaString - Compile time auto generated schema
# + return - Subgraph schema in SDL format as a string on success, or an error otherwise
function getSdlString(string encodedSchemaString) returns string|error;

// --- Service ---

// --- Service (generic) ---
// Listener: Listener(int listenTo)
// Instructions:

// --- Annotations ---

# The annotation to configure a GraphQL resolver.
public annotation ResourceConfig on service_function;

# The annotation to configure a GraphQL service.
public annotation ServiceConfig on service;
