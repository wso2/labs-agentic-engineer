// ============================================================
// Library: ballerinax/sap
// [SAP](https://www.sap.com/index.html) is a global leader in enterprise resource planning (ERP) software. Beyond ERP, SAP offers a diverse range of solutions including human capital management (HCM), customer relationship management (CRM), enterprise performance management (EPM), product lifecycle management (PLM), supplier relationship management (SRM), supply chain management (SCM), and business technology platform (BTP).
// ============================================================
import ballerinax/sap;

// --- Types ---

# The `sap` client return type for the HTTP client actions.
public type TargetType http:Response|anydata; // Special Agent Note: Response FROM ballerina/http module

# Defines the possible client error types.
public type ClientError http:ClientError; // Special Agent Note: ClientError FROM ballerina/http module

# Represents an error, which occured due to a CSRF token fetch failure.
public type CSRFTokenFetchFailure http:ClientError; // Special Agent Note: ClientError FROM ballerina/http module

// --- Client ---

# The `sap` client provides the capability for initiating contact with a remote HTTP service provided by any SAP products. The API it
# provides includes the functions for the standard HTTP methods.
public isolated client class Client {
    # Gets invoked to initialize the `client`. During initialization, the configurations provided through the `config`
    # record is used to determine which type of additional behaviours are added to the endpoint (e.g.
    # security, circuit breaking). Caching is enabled always.
    # + url - URL of the target service
    # + config - The configurations to be used when initializing the `client`
    # + return - The `client` or an `sap:ClientError` if the initialization failed
    isolated function init(string url, http:ClientConfiguration config) returns ClientError?; // Special Agent Note: ClientConfiguration FROM ballerina/http module

    # The client resource function to send HTTP POST requests to SAP HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function post [http:PathParamType... path](http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http module

    # The `Client.post()` function can be used to send HTTP POST requests to SAP HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function post(string path, http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http module

    # The client resource function to send HTTP PUT requests to SAP HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function put [http:PathParamType... path](http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http module

    # The `Client.put()` function can be used to send HTTP PUT requests to SAP HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function put(string path, http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http module

    # The client resource function to send HTTP PATCH requests to SAP HTTP endpoints.
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function patch [http:PathParamType... path](http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http module

    # The `Client.patch()` function can be used to send HTTP PATCH requests to SAP HTTP endpoints.
    # + path - Resource path
    # + message - An HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function patch(string path, http:RequestMessage message, map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http module

    # The client resource function to send HTTP DELETE requests to SAP HTTP endpoints.
    # + message - An optional HTTP outbound request or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function delete [http:PathParamType... path](http:RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http module

    # The `Client.delete()` function can be used to send HTTP DELETE requests to SAP HTTP endpoints.
    # + path - Resource path
    # + message - An optional HTTP outbound request message or any allowed payload
    # + headers - The entity headers
    # + mediaType - The MIME type header of the request entity
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function delete(string path, http:RequestMessage message = (), map<string|string[]>? headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http module

    # The client resource function to send HTTP HEAD requests to SAP HTTP endpoints.
    # + headers - The entity headers
    # + params - The query parameters
    # + return - The response or an `sap:ClientError` if failed to establish the communication with the upstream server
    isolated resource function head [http:PathParamType... path](map<string|string[]>? headers = (), *http:QueryParams params) returns http:Response|ClientError; // Special Agent Note: QueryParams, Response FROM ballerina/http module

    # The `Client.head()` function can be used to send HTTP HEAD requests to SAP HTTP endpoints.
    # + path - Resource path
    # + headers - The entity headers
    # + return - The response or an `sap:ClientError` if failed to establish the communication with the upstream server
    isolated remote function head(string path, map<string|string[]>? headers = ()) returns http:Response|ClientError; // Special Agent Note: Response FROM ballerina/http module

    # The client resource function to send HTTP GET requests to SAP HTTP endpoints.
    # + headers - The entity headers
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function get [http:PathParamType... path](map<string|string[]>? headers = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: QueryParams FROM ballerina/http module

    # The `Client.get()` function can be used to send HTTP GET requests to SAP HTTP endpoints.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function get(string path, map<string|string[]>? headers = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP OPTIONS requests to SAP HTTP endpoints.
    # + headers - The entity headers
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + params - The query parameters
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated resource function options [http:PathParamType... path](map<string|string[]>? headers = (), typedesc<TargetType> targetType = <>, *http:QueryParams params) returns targetType|ClientError; // Special Agent Note: QueryParams FROM ballerina/http module

    # The `Client.options()` function can be used to send HTTP OPTIONS requests to SAP HTTP endpoints.
    # + path - Request path
    # + headers - The entity headers
    # + targetType - HTTP response or `anydata`, which is expected to be returned after data binding
    # + return - The response or the payload (if the `targetType` is configured) or an `sap:ClientError` if failed to
    # establish the communication with the upstream server or a data binding failure
    isolated remote function options(string path, map<string|string[]>? headers = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError;
}
