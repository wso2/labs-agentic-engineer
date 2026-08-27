// ============================================================
// Library: ballerina/graphql
// This module provides APIs for connecting and interacting with GraphQL endpoints.
// ============================================================
import ballerina/graphql;

// --- Types ---

# Represents a GraphQL enum value.
public type __EnumValue record {|
    # The name of the enum value
    string name;
    # The description of the enum value
    string? description = ();
    # Whether the enum value is deprecated
    boolean isDeprecated = false;
    # The reason for deprecation of the enum value
    string? deprecationReason = ();
|};

# Represents a GraphQL field.
public type __Field record {|
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
|};

# Represents a GraphQL input value.
public type __InputValue record {|
    # The name of the input value
    string name;
    # The description of the input value
    string? description = ();
    __Type 'type;
    # The default value of the input value, if there is one
    string? defaultValue = ();
|};

# Represents a GraphQL schema type.
public type __Type record {|
    # The `__TypeKind` of the type
    __TypeKind kind;
    # The name of the type. This can be nil if the type is `NON_NULL` or `LIST`
    string? name = ();
    # The description of the type
    string? description = ();
    # The fields of the type. This only applies if the `kind` is `OBJECT` or `INTERFACE`. Otherwise,
    # this will be nil.
    __Field[]? fields = ();
    # The interfaces of the type. This only applies if the `kind` is `OBJECT` or `INTERFACE`. Otherwise,
    # this will be nil.
    __Type[]? interfaces = ();
    # The possible types of the type. This only applies if the `kind` is `UNION` or `INTERFACE`.
    # Otherwise, this will be nil.
    __Type[]? possibleTypes = ();
    # The enum values of the type. This only applies if the `kind` is `ENUM`. Otherwise, this will be nil.
    __EnumValue[]? enumValues = ();
    # The input fields of the type. This only applies if the `kind` is `INPUT_OBJECT`. Otherwise,
    # this will be nil.
    __InputValue[]? inputFields = ();
    # The type of the type. This only applies if the `kind` is `NON_NULL` or `LIST`. Otherwise, this will be nil.
    __Type? ofType = ();
|};

# Represents token for Bearer token authentication.
public type BearerTokenConfig record {|
|};

# Provides a set of configurations for controlling the caching behaviour of the endpoint.
public type CacheConfig record {|
|};

# Provides a set of configurations for controlling the behaviour of the Circuit Breaker.
public type CircuitBreakerConfig record {|
|};

# Provides a set of configurations for controlling the behaviour of the GraphQL client when communicating with
# the GraphQL server that operates over HTTP.
public type ClientConfiguration record {|
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
|};

# Provides settings related to HTTP/1.x protocol.
public type ClientHttp1Settings record {|
|};

# Provides configurations for facilitating secure communication with a remote GraphQL endpoint.
public type ClientSecureSocket record {|
|};

# Client configuration for cookies.
public type CookieConfig record {|
|};

# Represent CORS configurations for internal HTTP service
public type CorsConfig record {|
|};

# Represents credentials for Basic Auth authentication.
public type CredentialsConfig record {|
|};

# Represent the document cache configurations of GraphQL server.
public type DocumentCacheConfig record {|
    # State of the document caching
    boolean enabled = true;
    # Maximum number of cache entries
    int maxSize = 100;
|};

# Represents an error in GraphQL.
public type ErrorDetail record {|
    *parser:ErrorDetail; // Special Agent Note: ErrorDetail FROM ballerina/graphql.parser module
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
public type FollowRedirects record {|
|};

# Represents the target type binding record with data and extensions of a GraphQL response for `executeWithType` method.
public type GenericResponse record {|
    # Meta information on protocol extensions from the GraphQL server
    map<json?> extensions?;
    # The requested data from the GraphQL server
    record {}|map<json?> data?;
|};

# Represents the target type binding record with data, extensions and errors of a GraphQL response for `execute` method.
public type GenericResponseWithErrors record {|
    *GenericResponse;
    # The errors occurred (if present) while processing the GraphQL request.
    ErrorDetail[] errors?;
|};

# Represent GraphiQL client configurations
public type Graphiql record {|
    # Status of the client
    boolean enabled = false;
    # Path for the client
    string path = "graphiql";
    # Enable/disable printing the GraphiQL client URL to the stdout
    boolean printUrl = true;
|};

# Provides a set of configurations for the GraphQL interceptors.
public type GraphqlInterceptorConfig record {|
    # Scope of the interceptor. If `true`, the interceptor will be applied to all the resolvers.
    boolean global = true;
|};

# Provides a set of configurations for the GraphQL resolvers.
public type GraphqlResourceConfig record {|
    # GraphQL field level interceptors
    readonly (readonly & Interceptor)|(readonly & Interceptor)[] interceptors = [];
    # The name of the instance method to be used for prefetching
    string prefetchMethodName?;
    # The cache configurations for the fields
    ServerCacheConfig cacheConfig?;
    # The complexity value of the field
    int complexity?;
|};

# Provides a set of configurations for the GraphQL service.
public type GraphqlServiceConfig record {|
    # The maximum depth allowed for a query
    int maxQueryDepth?;
    # Listener authentication configurations
    ListenerAuthConfig[] auth?;
    # Function to initialize the context. If not provided, an empty context will be created
    ContextInit contextInit = initDefaultContext; // Special Agent Note: the default initDefaultContext is not exported by this package; omit the argument rather than repeating it
    # The cross origin resource sharing configurations for the service
    CorsConfig cors?;
    # GraphiQL client configurations
    Graphiql graphiql = {};
    # The generated schema. This is auto-generated at the compile-time. Providing a value for this field will end up in
    # a compilation error.
    readonly string schemaString = "";
    # GraphQL service level interceptors
    readonly (readonly & Interceptor)|(readonly & Interceptor)[] interceptors = [];
    # Whether to enable or disable the introspection on the service
    boolean introspection = true;
    # Whether to enable or disable the constraint validation
    boolean validation = true;
    # The cache configurations for the operations
    ServerCacheConfig cacheConfig?;
    # The field cache config derived from the resource annotations. This is auto-generated at the compile time
    readonly ServerCacheConfig? fieldCacheConfig = ();
    # The query complexity configuration for the service.
    QueryComplexityConfig queryComplexityConfig?;
    # The document cache configurations for the service
    DocumentCacheConfig documentCacheConfig?;
|};

# Represents JWT issuer configurations for JWT authentication.
public type JwtIssuerConfig record {|
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

# Provides a set of configurations for configure the underlying HTTP listener of the GraphQL listener.
public type ListenerConfiguration record {|
|};

# Provides settings related to HTTP/1.x protocol, when using HTTP 1.x as the underlying protocol for the GraphQL
# service.
public type ListenerHttp1Settings record {|
|};

# Configures the SSL/TLS options to be used for the underlying HTTP service used in GraphQL service.
public type ListenerSecureSocket record {|
|};

# Represents a location in a GraphQL document.
public type Location record {|
    *parser:Location; // Special Agent Note: Location FROM ballerina/graphql.parser module
|};

# Represents OAuth2 client credentials grant configurations for OAuth2 authentication.
public type OAuth2ClientCredentialsGrantConfig record {|
|};

# Represents OAuth2 introspection server configurations for OAuth2 authentication.
public type OAuth2IntrospectionConfig record {|
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
|};

# Represents OAuth2 password grant configurations for OAuth2 authentication.
public type OAuth2PasswordGrantConfig record {|
|};

# Represents OAuth2 refresh token grant configurations for OAuth2 authentication.
public type OAuth2RefreshTokenGrantConfig record {|
|};

# Configurations for managing GraphQL client connection pool.
public type PoolConfiguration record {|
|};

# Proxy server configurations to be used with the GraphQL client endpoint.
public type ProxyConfig record {|
|};

# Defines the query complexity configuration for the GraphQL service.
public type QueryComplexityConfig record {|
    # Maximum allowed query depth
    int maxComplexity = 100;
    # Default complexity for a field
    int defaultFieldComplexity = 1;
    # Whether to only log a warning or return an error when the complexity exceeds the limit
    boolean warnOnly = false;
|};

# Provides inbound request URI, total header and entity body size threshold configurations.
public type RequestLimitConfigs record {|
|};

# Provides inbound response status line, total header and entity body size threshold configurations.
public type ResponseLimitConfigs record {|
|};

# Provides configurations for controlling the retrying behavior in failure scenarios.
public type RetryConfig record {|
|};

# Represent the cache configurations of GraphQL server.
public type ServerCacheConfig record {|
    # State of the caching
    boolean enabled = true;
    # TTL of the cache in seconds
    decimal maxAge = 60;
    # Maximum number of cache entries
    int maxSize = 120;
|};

# The input parameter type used for file uploads in GraphQL mutations.
public type Upload record {|
    # Name of the file
    string fileName;
    # File mime type according to the content
    string mimeType;
    # File stream encoding
    string encoding;
    # File content as a stream of `byte[]`
    stream<byte[], io:Error?> byteStream; // Special Agent Note: Error FROM ballerina/io module
|};

# Represents the authentication error type.
public type AuthnError distinct Error;

# Represents the authorization error type.
public type AuthzError distinct Error;

# Represents GraphQL client related generic errors.
public type ClientError distinct error;

# Represents any error related to the Ballerina GraphQL module.
public type Error distinct error;

# Represents network level errors.
public type HttpError distinct (RequestError & error<record {| anydata body; |}>);

# Represents GraphQL errors due to request validation.
public type InvalidDocumentError distinct (RequestError & error<record {| ErrorDetail[]? errors; |}>);

# Represents client side data binding error.
public type PayloadBindingError distinct (ClientError & error<record {| ErrorDetail[]? errors; |}>);

# Represents GraphQL client side or network level errors.
public type RequestError distinct ClientError;

# Represents GraphQL API response during GraphQL API server side errors.
public type ServerError distinct (ClientError & error<record {| json? data?; ErrorDetail[] errors; map<json>? extensions?; |}>);

# The prefix used to denote the Basic authentication scheme.
public const string AUTH_SCHEME_BASIC = "Basic";

# The prefix used to denote the Bearer authentication scheme.
public const string AUTH_SCHEME_BEARER = "Bearer";

# Always set accept-encoding/content-encoding in outbound request/response.
public const string COMPRESSION_ALWAYS = "ALWAYS";

# When service behaves as a HTTP gateway inbound request/response accept-encoding option is set as the
# outbound request/response accept-encoding/content-encoding option.
public const string COMPRESSION_AUTO = "AUTO";

# Never set accept-encoding/content-encoding header in outbound request/response.
public const string COMPRESSION_NEVER = "NEVER";

# Represents a GraphQL type kind. This is used to represent the kind of a GraphQL type.
public enum __TypeKind {
    # Represents a GraphQL scalar type
    SCALAR,
    # Represents a GraphQL (output) object type
    OBJECT,
    # Represents a GraphQL enum type
    ENUM,
    # Represents a GraphQL non-null type. If a field is of this type, it is guaranteed to be non-null
    NON_NULL,
    # Represents a GraphQL list type
    LIST,
    # Represents a GraphQL union type
    UNION,
    # Represents a GraphQL interface type
    INTERFACE,
    # Represents a GraphQL input object type
    INPUT_OBJECT
}

# The GraphQL context object used to pass the meta information between resolvers.
public isolated class Context {
    isolated function init();

    # Sets a given value for a given key in the GraphQL context.
    # + value - Value to be set
    isolated function set(string 'key, value:Cloneable|isolated object {} value); // Special Agent Note: Cloneable FROM ballerina/lang.value module

    # Retrieves a value using the given key from the GraphQL context.
    # ```ballerina
    # string userId = check context.get("userId").ensureType();  
    # ```
    # + return - The value if the key is present in the context, a `graphql:Error` otherwise
    isolated function get(string 'key) returns value:Cloneable|isolated object {}|Error; // Special Agent Note: Cloneable FROM ballerina/lang.value module

    # Removes a value using the given key from the GraphQL context.
    # ```ballerina
    # string userId = check context.remove("userId").ensureType();  
    # ```
    # + return - The value if the key is present in the context, a `graphql:Error` otherwise
    isolated function remove(string 'key) returns value:Cloneable|isolated object {}|Error; // Special Agent Note: Cloneable FROM ballerina/lang.value module

    # Register a given DataLoader instance for a given key in the GraphQL context.
    # ```ballerina
    # dataloader:DataLoader userDataLoader = new dataloader:DefaultDataLoader(batchUsers);
    # check context.registerDataLoader("user", userDataLoader);
    # ```
    # + key - The key for the DataLoader to be registered
    # + dataloader - The DataLoader instance to be registered
    isolated function registerDataLoader(string key, dataloader:DataLoader dataloader); // Special Agent Note: DataLoader FROM ballerina/graphql.dataloader module

    # Retrieves a DataLoader instance using the given key from the GraphQL context.
    # ```ballerina
    # dataloader:DataLoader userDataLoader = check context.getDataLoader("user");
    # ```
    # + key - The key corresponding to the required DataLoader instance
    # + return - The DataLoader instance if the key is present in the context otherwise panics
    isolated function getDataLoader(string key) returns dataloader:DataLoader; // Special Agent Note: DataLoader FROM ballerina/graphql.dataloader module

    # Remove cache entries related to the given path.
    # + path - The path corresponding to the cache entries to be removed (Ex: "person.address.city")
    # + return - The error if the cache invalidateion fails or nil otherwise
    isolated function invalidate(string path) returns error?;

    # Remove all cache entries.
    # + return - The error if the cache invalidateion fails or nil otherwise
    isolated function invalidateAll() returns error?;

    isolated function resolve(Field 'field) returns anydata;
}

# Represents the information about a particular field of a GraphQL document.
public class Field {
    # Returns the name of the field.
    # + return - The name of the field
    isolated function getName() returns string;

    # Returns the effective alias of the field.
    # + return - The alias of the field. If an alias is not present, the field name will be returned
    isolated function getAlias() returns string;

    # Returns the current path of the field. If the field returns an array, the path will include the index of the
    # element.
    # + return - The path of the field
    isolated function getPath() returns readonly & (string|int)[];

    # Returns the subfields of this field as a `Field` object array.
    # + return - The subfield objects of this field
    isolated function getSubfields() returns Field[]?;

    # Returns the names of the subfields of this field as a string array.
    # + return - The names of the subfields of this field
    isolated function getSubfieldNames() returns string[];

    # Returns the type of the field.
    # + return - The type of the field
    isolated function getType() returns __Type;

    # Returns the location of the field in the GraphQL document.
    # + return - The location of the field
    isolated function getLocation() returns Location;
}

# Represents a GraphQL service.
public type Service distinct service object {
};

# Represent a GraphQL interceptor
public type Interceptor distinct service object {
    isolated remote function execute(Context context, Field 'field) returns anydata|error;
};

# Defines the authentication configurations for the GraphQL listener.
public type ListenerAuthConfig FileUserStoreConfigWithScopes|LdapUserStoreConfigWithScopes|JwtValidatorConfigWithScopes|OAuth2IntrospectionConfigWithScopes;

# Defines the authentication configurations for the GraphQL client.
public type ClientAuthConfig CredentialsConfig|BearerTokenConfig|JwtIssuerConfig|OAuth2GrantConfig;

# Represents OAuth2 grant configurations for OAuth2 authentication.
public type OAuth2GrantConfig OAuth2ClientCredentialsGrantConfig|OAuth2PasswordGrantConfig|OAuth2RefreshTokenGrantConfig|OAuth2JwtBearerGrantConfig;

# Represents the Scalar types supported by the Ballerina GraphQL module.
public type Scalar boolean|int|float|string|decimal;

# Options to compress using gzip or deflate.
public type Compression COMPRESSION_AUTO|COMPRESSION_ALWAYS|COMPRESSION_NEVER;

# Function type for initializing the `graphql:Context` object.
# This function will be called with the `http:Request` and the `http:RequestContext` objects from the original request
# received to the GraphQL endpoint.
public type ContextInit isolated function (RequestContext, Request) returns Context|error;

public final readonly & ConnectionInitialisationTimeout CONNECTION_INITIALISATION_TIMEOUT = {};

public final readonly & TooManyInitializationRequests TOO_MANY_INITIALIZATION_REQUESTS = {};

public final readonly & Unauthorized UNAUTHORIZED = {};

// --- Client ---

# The Ballerina GraphQL client that can be used to communicate with GraphQL APIs.
public isolated client class Client {
    # Gets invoked to initialize the `connector`.
    # + serviceUrl - URL of the target service
    # + clientConfig - The configurations to be used when initializing the `connector`
    # + return - An error at the failure of client initialization
    isolated function init(string serviceUrl, *ClientConfiguration clientConfig) returns ClientError?;

    # Executes a GraphQL document and data binds the GraphQL response to a record with data and extensions
    # which is a subtype of GenericResponse.
    # + document - The GraphQL document. It can include queries & mutations.
    # For example `query OperationName($code:ID!) {country(code:$code) {name}}`.
    # + variables - The GraphQL variables. For example `{"code": "<variable_value>"}`.
    # + operationName - The GraphQL operation name. If a request has two or more operations, then each operation must have a name.
    # A request can only execute one operation, so you must also include the operation name to execute.
    # + headers - The GraphQL API headers to execute each query
    # + targetType - The payload, which is expected to be returned after data binding. For example
    # `type CountryByCodeResponse record {| map<json?> extensions?; record {| record{|string name;|}? country; |} data;`
    # + return - The GraphQL response or a `graphql:ClientError` if failed to execute the query
    # # Deprecated
    # This method is now deprecated. Use the `client->execute()` API instead
    @deprecated
    isolated remote function executeWithType(string document, map<anydata>? variables = (), string? operationName = (), map<string|string[]>? headers = (), typedesc<GenericResponse|record {}|json> targetType = <>) returns targetType|ClientError;

    # Executes a GraphQL document and data binds the GraphQL response to a record with data, extensions and errors
    # which is a subtype of GenericResponseWithErrors.
    # + document - The GraphQL document. It can include queries & mutations.
    # For example `query countryByCode($code:ID!) {country(code:$code) {name}}`.
    # + variables - The GraphQL variables. For example `{"code": "<variable_value>"}`.
    # + operationName - The GraphQL operation name. If a request has two or more operations, then each operation must have a name.
    # A request can only execute one operation, so you must also include the operation name to execute.
    # + headers - The GraphQL API headers to execute each query
    # + targetType - The payload (`GenericResponseWithErrors`), which is expected to be returned after data binding. For example
    # `type CountryByCodeResponse record {| map<json?> extensions?; record {| record{|string name;|}? country; |} data; ErrorDetail[] errors?; |};`
    # + return - The GraphQL response or a `graphql:ClientError` if failed to execute the query
    isolated remote function execute(string document, map<anydata>? variables = (), string? operationName = (), map<string|string[]>? headers = (), typedesc<GenericResponseWithErrors|record {}|json> targetType = <>) returns targetType|ClientError;
}

// --- Functions ---

# Adds an error to the GraphQL response. Using this to add an error is not recommended.
# + context - The context of the GraphQL request.
# + errorDetail - The error to be added to the response.
public isolated function __addError(Context context, ErrorDetail errorDetail);

# Obtains the schema representation of a federated subgraph, expressed in the SDL format.
# + encodedSchemaString - Compile time auto generated schema
# + return - Subgraph schema in SDL format as a string on success, or an error otherwise
public isolated function getSdlString(string encodedSchemaString) returns string|error;

// --- Listeners ---

# Represents a Graphql listener endpoint.
public class Listener {
    # Invoked during the initialization of a `graphql:Listener`. Either an `http:Listener` or a port number must be
    # provided to initialize the listener.
    # + listenTo - An `http:Listener` or a port number to listen to the GraphQL service endpoint
    # + configuration - The additional configurations for the GraphQL listener
    # + return - A `graphql:Error` if the listener initialization is failed or else `()`
    isolated function init(int|Listener listenTo, *ListenerConfiguration configuration) returns Error?;

    # Attaches the provided service to the Listener.
    # + s - The `graphql:Service` object to attach to the listener
    # + name - The path of the service to be hosted
    # + return - A `graphql:Error` if an error occurred during the service-attaching process or the schema
    # generation process or else `()`
    isolated function attach(Service s, string[]|string? name = ()) returns Error?;

    # Detaches the provided service from the Listener.
    # + s - The service to be detached from the listener
    # + return - A `graphql:Error` if an error occurred during the service detaching process or else `()`
    isolated function detach(Service s) returns Error?;

    # Starts the attached service.
    # + return - A `graphql:Error`, if an error occurred during the service starting process, otherwise nil
    isolated function 'start() returns Error?;

    # Gracefully stops the graphql listener. Already accepted requests will be served before the connection closure.
    # + return - A `graphql:Error`, if an error occurred during the service stopping process, otherwise nil
    isolated function gracefulStop() returns Error?;

    # Stops the service listener immediately.
    # + return - A `graphql:Error` if an error occurred during the service stopping process or else `()`
    isolated function immediateStop() returns Error?;
}

// --- Service ---

service graphql:Service on new graphql:Listener(listenTo, configuration) {
    // Central publishes no method contract for this service type. The listener may still require
    // one — add the resource or remote methods the package's guide shows; `bal library overview`
    // reproduces it.
}

// These service object types are declared above; this reader cannot confirm that graphql:Listener
// accepts them, so it writes no attachment template for them:
//   graphql:Interceptor
// graphql:Listener.attach takes one specific type. A `distinct service object` type reaches it only
// by INCLUDING that type, and Central publishes no inclusion for an object type — so some of these
// do attach and some do not, and the payload cannot say which. An interceptor type, for one, reaches
// the runtime as a `createInterceptors()` return rather than as an attachment. The package's own
// guide is where the usage of each is written; `bal library overview` reproduces it.

// --- Annotations ---

# Represents the annotation of the ID type.
public annotation ID on record field, parameter, return;

# The annotation to configure a GraphQL interceptor.
public annotation GraphqlInterceptorConfig InterceptorConfig on class;

# The annotation to configure a GraphQL resolver.
public annotation GraphqlResourceConfig ResourceConfig on object function;

# The annotation to configure a GraphQL service.
public annotation GraphqlServiceConfig ServiceConfig on service;

// --- Configurables ---

// Set in the CALLER's Config.toml, under a [ballerina.graphql] table. These are
// module-private, so they cannot be referenced from code — a default above that a signature
// also names is one you set here rather than pass.

// MESSAGE_SCHEDULE_INITIAL_DELAY = 5    # decimal
