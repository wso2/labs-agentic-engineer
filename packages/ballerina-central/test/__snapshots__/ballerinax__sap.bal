// ============================================================
// Library: ballerinax/sap
// [SAP](https://www.sap.com/index.html) is a global leader in enterprise resource planning (ERP) software. Beyond ERP, SAP offers a diverse range of solutions including human capital management (HCM), customer relationship management (CRM), enterprise performance management (EPM), product lifecycle management (PLM), supplier relationship management (SRM), supply chain management (SCM), and business technology platform (BTP).
// ============================================================
import ballerinax/sap;

// --- Types ---

# Defines the possible client error types.
type ClientError error;

// Unknown type: RequestMessage

# The `sap` client return type for the HTTP client actions.
type TargetType http:Response|anydata;

// Unknown type: ClientError

// Unknown type: CSRFTokenFetchFailure

// --- Client ---

# The `sap` client provides the capability for initiating contact with a remote HTTP service provided by any SAP products. The API it
# provides includes the functions for the standard HTTP methods.
client class Client {
    function init(string url, http:ClientConfiguration config) returns ClientError?; // Special Agent Note: ClientConfiguration FROM ballerina/http package

    # The client resource function to send HTTP POST requests to SAP HTTP endpoints.
    resource function post [http:PathParamType... path](http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

    # The `Client.post()` function can be used to send HTTP POST requests to SAP HTTP endpoints.
    remote function post(string path, http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http package

    # The client resource function to send HTTP PUT requests to SAP HTTP endpoints.
    resource function put [http:PathParamType... path](http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

    # The `Client.put()` function can be used to send HTTP PUT requests to SAP HTTP endpoints.
    remote function put(string path, http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http package

    # The client resource function to send HTTP PATCH requests to SAP HTTP endpoints.
    resource function patch [http:PathParamType... path](http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

    # The `Client.patch()` function can be used to send HTTP PATCH requests to SAP HTTP endpoints.
    remote function patch(string path, http:RequestMessage message, map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http package

    # The client resource function to send HTTP DELETE requests to SAP HTTP endpoints.
    resource function delete [http:PathParamType... path](http:RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: RequestMessage, QueryParams FROM ballerina/http package

    # The `Client.delete()` function can be used to send HTTP DELETE requests to SAP HTTP endpoints.
    remote function delete(string path, http:RequestMessage message = (), map<string|string[]> headers = (), string? mediaType = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError; // Special Agent Note: RequestMessage FROM ballerina/http package

    # The client resource function to send HTTP HEAD requests to SAP HTTP endpoints.
    resource function head [http:PathParamType... path](map<string|string[]> headers = (), http:QueryParams params) returns http:Response|ClientError; // Special Agent Note: QueryParams, Response FROM ballerina/http package

    # The `Client.head()` function can be used to send HTTP HEAD requests to SAP HTTP endpoints.
    remote function head(string path, map<string|string[]> headers = ()) returns http:Response|ClientError; // Special Agent Note: Response FROM ballerina/http package

    # The client resource function to send HTTP GET requests to SAP HTTP endpoints.
    resource function get [http:PathParamType... path](map<string|string[]> headers = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: QueryParams FROM ballerina/http package

    # The `Client.get()` function can be used to send HTTP GET requests to SAP HTTP endpoints.
    remote function get(string path, map<string|string[]> headers = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError;

    # The client resource function to send HTTP OPTIONS requests to SAP HTTP endpoints.
    resource function options [http:PathParamType... path](map<string|string[]> headers = (), typedesc<TargetType> targetType = <>, http:QueryParams params) returns targetType|ClientError; // Special Agent Note: QueryParams FROM ballerina/http package

    # The `Client.options()` function can be used to send HTTP OPTIONS requests to SAP HTTP endpoints.
    remote function options(string path, map<string|string[]> headers = (), typedesc<TargetType> targetType = <>) returns targetType|ClientError;
}
