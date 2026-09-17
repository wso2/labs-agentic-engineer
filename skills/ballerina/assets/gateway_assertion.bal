// Verification of the API gateway's signed assertion, and the caller it names.
//
// Copied VERBATIM from the `ballerina` skill — there is no line to edit.
//
// WHAT THIS IS FOR, AND WHAT IT IS NOT
//
// The gateway in front of this service has already done the authorization. It
// validated the caller's token against the environment's identity provider and
// checked the scope every operation declares in openapi.yaml; a request that
// failed either never reached this process. This file does NOT repeat that
// work. There is no operation -> scope table here, and a service that keeps one
// is keeping a second copy of the contract that nothing keeps in sync.
//
// What a gateway cannot do for you is prove to your own code that a request
// came through it: any pod that can open a socket to this service can send
// whatever headers it likes. So the gateway signs a JWT of the caller it
// authenticated -- the assertion -- and this file verifies it against ONE
// certificate the platform published for this environment. That signature is
// the whole trust anchor: no identity-provider JWKS, no introspection, no
// network call.
//
// So: the gateway decides WHETHER a request may happen. This file decides WHO
// it is from, in a way a forged header cannot fake.
//
// THE THREE ENVIRONMENT VARIABLES
//
// The platform sets all three on the container when the environment's gateway
// publishes a keypair (see the `api-configuration` trait):
//
//   GATEWAY_ASSERTION_CERTIFICATE  PEM X.509 certificate carrying the public key
//   GATEWAY_ASSERTION_ISSUER       the `iss` every assertion carries
//   GATEWAY_ASSERTION_HEADER       the header it arrives in
//
// A missing one PANICS the interceptor's init, which stops the service from
// starting. That is deliberate: a service that starts without them cannot tell
// a real caller from a forged one.
//
// WIRING: the generated `service / on ep0` becomes
//   service http:InterceptableService / on ep0 {
//       public function createInterceptors() returns AssertionInterceptor => new;
//       ...
//   }
// and a resource that needs the caller takes `http:RequestContext ctx` and
// calls `gatewayCaller(ctx)`.

import ballerina/crypto;
import ballerina/http;
import ballerina/jwt;
import ballerina/lang.regexp;
import ballerina/os;

# The key the verified caller is stored under in the `http:RequestContext`.
const string CALLER_CTX_KEY = "aep_gateway_caller";

# The authenticated caller, as the gateway's assertion names them.
#
# + userId - the assertion's `sub`: a directory id for an end user, a client id
#            for a service-to-service caller
# + username - the assertion's `username`: the login name the person signs in
#              with. "" when the assertion carries no such claim. See
#              `requireCallerUsername` before you reach for this.
# + scopes - the whole handles the caller holds
# + orgHandle - the assertion's `ouHandle`; "" when the IdP sent none
public type GatewayCaller record {|
    string userId;
    string username;
    string[] scopes;
    string orgHandle;
|};

# Whether the caller holds this exact handle.
#
# Whole-string equality, never `string:includes`: `claims:read` is a prefix of
# `claims:read-all`, and the gateway compares them as whole strings too.
# The parameter is `scopeHandle`, not `handle`: `handle` is a Ballerina type
# keyword and does not parse as an identifier.
#
# + caller - the verified caller
# + scopeHandle - the whole handle to look for
# + return - true when the caller holds exactly that handle
public isolated function hasScope(GatewayCaller caller, string scopeHandle) returns boolean {
    return caller.scopes.indexOf(scopeHandle) !is ();
}

# The verified caller, if this request carried an assertion.
#
# Absent is NOT an error: a `security: []` operation is served by the gateway
# with no assertion at all, and its resource must read no identity. A resource
# that needs one calls `requireGatewayCaller`.
#
# + ctx - the request context the interceptor wrote to
# + return - the verified caller, or () when the request carried no assertion
public isolated function gatewayCaller(http:RequestContext ctx) returns GatewayCaller? {
    if !ctx.hasKey(CALLER_CTX_KEY) {
        return ();
    }
    http:ReqCtxMember stored = ctx.get(CALLER_CTX_KEY);
    return stored is GatewayCaller ? stored : ();
}

# The verified caller, or a 401 for a resource that cannot serve an anonymous
# request. Never fall back to a caller-supplied id.
#
# + ctx - the request context the interceptor wrote to
# + return - the verified caller, or a 401 payload to return as-is
public isolated function requireGatewayCaller(http:RequestContext ctx)
        returns GatewayCaller|http:Unauthorized {
    GatewayCaller? caller = gatewayCaller(ctx);
    if caller is () {
        return <http:Unauthorized>{body: {message: "no verified caller on this request"}};
    }
    return caller;
}

# The caller's login name, for a model that identifies people by name.
#
# `userId` is a directory UUID. When a row names its person by login name —
# `ownerUsername`, an email local part — that is the side to match on, and
# comparing the UUID instead matches nothing for every caller, silently: the
# endpoint answers `200 []`, which reads as "you own nothing" rather than "this
# service cannot tell who you are". An identity that will not resolve is an
# error, never an empty result.
#
# Not the `x-user-name` header: the gateway sends one, but it is unsigned and
# anything reaching this pod can forge it. Only the assertion is evidence.
#
# + caller - the verified caller
# + return - the caller's login name, or a 500 to return as-is when the
#            assertion carried none
public isolated function requireCallerUsername(GatewayCaller caller)
        returns string|http:InternalServerError {
    if caller.username.trim() == "" {
        return <http:InternalServerError>{body: {
            message: "the gateway assertion carries no username, so this service "
                + "cannot resolve the caller's own records"
        }};
    }
    return caller.username;
}

# Verifies the assertion, if the request carries one, and puts the caller it
# names on the request context.
#
# Three outcomes, and the middle one is the point:
#
# - no assertion  -> continue with no caller. The gateway serves a
#   `security: []` operation without one.
# - present but not verifiable -> 401, immediately. A forged or tampered
#   assertion is never downgraded to "anonymous": that would make forging one
#   strictly better for an attacker than sending none.
# - verified -> continue with the caller on the context.
# The three fields are the PEM text, the issuer and the header — all strings,
# and that is what keeps this class `isolated` so the listener serves requests
# concurrently. It does NOT cache the decoded `crypto:PublicKey`, for a reason
# worth knowing:
#
#   A `crypto:PublicKey` is not a readonly value, so an isolated object cannot
#   hold one in a final field. a readonly clone COMPILES and then fails at
#   runtime — MEASURED: the clone loses the native key material and every
#   assertion comes back "SHA256 signature verification failed", which reads
#   exactly like a wrong key. Do not reach for it.
#
# The cost of decoding per request was measured on this stack at ~73µs against
# ~422µs for the validation itself — about a sixth, for full concurrency.
# Dropping `isolated` to cache the key costs far more: Ballerina then serves
# this interceptor one request at a time.
public isolated service class AssertionInterceptor {
    *http:RequestInterceptor;

    private final string certificate;
    private final string issuer;
    private final string header;

    public isolated function init() {
        string cert = os:getEnv("GATEWAY_ASSERTION_CERTIFICATE");
        string iss = os:getEnv("GATEWAY_ASSERTION_ISSUER");
        string hdr = os:getEnv("GATEWAY_ASSERTION_HEADER");
        if cert == "" || iss == "" || hdr == "" {
            panic error("GATEWAY_ASSERTION_CERTIFICATE / _ISSUER / _HEADER must all be set; "
                + "this service cannot tell a real caller from a forged one without them");
        }
        // Decoded once here only to fail FAST: a certificate this service
        // cannot read must stop it starting, not 401 every caller later.
        crypto:PublicKey|crypto:Error decoded = crypto:decodeRsaPublicKeyFromContent(cert.toBytes());
        if decoded is crypto:Error {
            panic error("GATEWAY_ASSERTION_CERTIFICATE is not a readable PEM certificate", decoded);
        }
        self.certificate = cert;
        self.issuer = iss;
        self.header = hdr;
    }

    isolated resource function 'default [string... path](http:RequestContext ctx, http:Request req)
            returns http:NextService|http:Unauthorized|error? {
        string|http:HeaderNotFoundError raw = req.getHeader(self.header);
        if raw is http:HeaderNotFoundError || raw.trim() == "" {
            return ctx.next();
        }
        crypto:PublicKey|crypto:Error key = crypto:decodeRsaPublicKeyFromContent(self.certificate.toBytes());
        if key is crypto:Error {
            return error("gateway assertion certificate became unreadable", key);
        }
        // `jwt:validate` checks the signature, the `iss` and the `exp` in one
        // call. clockSkew absorbs drift between the gateway and this pod; it is
        // small on purpose, because the assertion is minted per request.
        jwt:ValidatorConfig config = {
            issuer: self.issuer,
            clockSkew: 60,
            signatureConfig: {certFile: key}
        };
        jwt:Payload|jwt:Error payload = jwt:validate(raw, config);
        if payload is jwt:Error {
            return <http:Unauthorized>{body: {message: "invalid gateway assertion"}};
        }
        string? subject = payload.sub;
        if subject is () || subject.trim() == "" {
            return <http:Unauthorized>{body: {message: "gateway assertion names no subject"}};
        }
        anydata orgHandle = payload["ouHandle"];
        anydata username = payload["username"];
        GatewayCaller caller = {
            userId: subject,
            username: username is string ? username : "",
            scopes: splitScopes(payload["scope"]),
            orgHandle: orgHandle is string ? orgHandle : ""
        };
        ctx.set(CALLER_CTX_KEY, caller);
        return ctx.next();
    }
}

# Splits the assertion's `scope` claim, which is space-separated as OAuth 2.0
# spells a scope list. Split on RUNS of whitespace so a double space is not a
# scope, and drop the empty strings a leading or trailing space leaves.
#
# + raw - the `scope` claim as the assertion carried it
# + return - the whole handles, in the order the claim listed them
isolated function splitScopes(anydata raw) returns string[] {
    if raw !is string || raw.trim() == "" {
        return [];
    }
    return from string s in regexp:split(re `\s+`, raw.trim())
        where s != ""
        select s;
}
