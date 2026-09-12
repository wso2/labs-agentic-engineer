// Scope enforcement for a `bal openapi --mode service` generated service.
//
// `bal openapi` DROPS the contract's `security` block: the generated resource
// functions carry no scope information at all. So the operation -> scope table
// below is authored from the same openapi.yaml the service was generated from,
// and tests/scope_table_drift_test.bal (copied verbatim beside this file) fails
// if the two ever disagree. `bal build` does NOT run tests -- the verify step
// for a service with this file is `bal build && bal test`, or the guard never
// fires. Everything except the OPERATION_SCOPES rows is verbatim.
//
// WIRING: the generated `service / on ep0` becomes
//   service http:InterceptableService / on ep0 {
//       public function createInterceptors() returns ScopeInterceptor => new;
//       ...
//   }
//
// The deny bodies below cast to `Error` -- the error record `bal openapi`
// generated from the contract's error schema (fields `'error` and `message`).
// If your generated types name it differently, change the two casts and
// nothing else.

import ballerina/http;
import ballerina/lang.regexp;
import ballerina/log;

# Sentinel for an operation whose contract says `security: []`.
public const string PUBLIC = "#public";

# One row per operation in openapi.yaml.
# + method - upper-case HTTP method
# + segments - path segments; "*" matches one path parameter
# + scope - a handle, or () for the document-level `security: [{oauth2: []}]`
#           (signed-in, no scope), or PUBLIC
public type OperationScope record {|
    string method;
    string[] segments;
    string? scope;
|};

// --- authored from openapi.yaml: one row per operation -----------------------
// The ONLY section you write. One row per operation in the contract, in any
// order. `segments` is the path split on "/" with every {pathParam} as "*".
// Example rows for a contract with GET /me (inherited), GET|POST /claims,
// POST /claims/{claimId}/approve and a public GET /health:
final readonly & OperationScope[] OPERATION_SCOPES = [
    {method: "GET", segments: ["me"], scope: ()},
    {method: "GET", segments: ["claims"], scope: "claims:read"},
    {method: "POST", segments: ["claims"], scope: "claims:submit"},
    {method: "POST", segments: ["claims", "*", "approve"], scope: "claims:approve"},
    {method: "GET", segments: ["health"], scope: PUBLIC}
];
// --- end authored section ----------------------------------------------------

public isolated service class ScopeInterceptor {
    *http:RequestInterceptor;

    isolated resource function 'default [string... path](http:RequestContext ctx, http:Request req)
            returns http:NextService|http:Unauthorized|http:Forbidden|error? {
        OperationScope[] matched = from OperationScope op in OPERATION_SCOPES
            where op.method == req.method && matches(op.segments, path)
            select op;
        if matched.length() != 1 {
            // No row, or an ambiguous table: deny rather than pass through.
            log:printError("no unique scope row for request", method = req.method, segments = path);
            return unauthorized();
        }
        string? required = matched[0].scope;
        if required == PUBLIC {
            return ctx.next();
        }
        if header(req, "X-User-Id") == "" {
            return unauthorized();
        }
        if required is string && !hasScope(header(req, "X-User-Scopes"), required) {
            return forbidden(required);
        }
        return ctx.next();
    }
}

# Whether the caller holds `handle`. Use it inside a resource for widening
# (own rows vs every row); the operation's own scope is the interceptor's job.
# + grantedHeader - the X-User-Scopes value the generated resource already binds
# + scopeHandle - the catalog handle to test
# + return - true when the caller was granted it
public isolated function hasScope(string? grantedHeader, string scopeHandle) returns boolean =>
    regexp:split(re `\s+`, (grantedHeader ?: "").trim()).indexOf(scopeHandle) != ();

isolated function matches(string[] pattern, string[] actual) returns boolean {
    if pattern.length() != actual.length() {
        return false;
    }
    foreach int i in 0 ..< pattern.length() {
        if pattern[i] != "*" && pattern[i] != actual[i] {
            return false;
        }
    }
    return true;
}

isolated function header(http:Request req, string name) returns string {
    string|error value = req.getHeader(name);
    return value is string ? value.trim() : "";
}

isolated function unauthorized() returns http:Unauthorized =>
    {body: <Error>{'error: "unauthorized", message: "no signed-in user"}};

isolated function forbidden(string required) returns http:Forbidden => {
    headers: {"WWW-Authenticate": string `Bearer error="insufficient_scope", scope="${required}"`},
    body: <Error>{'error: "insufficient_scope", message: string `this action requires the ${required} permission`}
};
