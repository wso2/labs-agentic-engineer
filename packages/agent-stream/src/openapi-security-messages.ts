/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * Every sentence the openapi.yaml SECURITY gate can say, keyed.
 *
 * Same contract as `security-design-messages.ts` next door, for the same
 * reason: two gates apply this rule set — the agent's FileBundle write-gate
 * (this package) and `securityspec` in the BFF (Go) — and the design's promise
 * is that "a document that passes one passes the other". The templates live
 * here once and are published beside this file as
 * `openapi-security-messages.json`, which Go vendors; `test/openapi-security-gate.test.ts`
 * asserts the artifact still matches this source, so an edit here that is not
 * republished fails the package's own tests rather than drifting quietly.
 *
 * A template is a flat string with `{placeholder}` slots. No nesting, no
 * pluralization, no conditionals: anything a template cannot say is said by
 * choosing a different key, so the Go formatter is a `strings.NewReplacer` and
 * nothing more.
 */

/**
 * key → template. The keys are the gate's stable vocabulary; renaming one is a
 * cross-language break, so add rather than rename.
 */
export const OPENAPI_SECURITY_MESSAGES = {
  // --- the scheme and the document default ---------------------------------
  missing_oauth2_scheme:
    "{component} depends on the sign-in client, so it must declare the oauth2 security scheme, but components.securitySchemes.oauth2 is absent. A component that depends on the sign-in client declares the oauth2 scheme; one that does not declares none.",
  scheme_wrong_type:
    "components.securitySchemes.oauth2 in {component} is declared as type: {type} — the scheme the platform's tokens are issued for is type: oauth2, and the gateway and the generated server are both rendered from it.",
  missing_document_security:
    "{component} declares the oauth2 scheme but not the document-level default. The default must be exactly `security: [{oauth2: []}]` at the root of the document, with an EMPTY scope list: it makes every operation signed-in unless the operation says otherwise, so an operation whose security block is forgotten fails closed instead of open. A default that names a scope is refused too — an operation that inherits it is enforced as signed-in only, and the permission it appears to require is silently lost.",

  // --- a component with no sign-in dependency declares nothing -------------
  scheme_without_dependency:
    "{component} does not depend on the sign-in client, so every one of its operations is public and it must declare no security scheme — remove components.securitySchemes. A component that depends on the sign-in client declares the oauth2 scheme; one that does not declares none. If this component is meant to be protected, add the sign-in dependency to its design.json first.",
  document_security_without_dependency:
    "{component} does not depend on the sign-in client, so it must declare no security — remove the document-level `security` block. A component that depends on the sign-in client declares the oauth2 scheme; one that does not declares none. If this component is meant to be protected, add the sign-in dependency to its design.json first.",
  security_without_dependency:
    "{component} does not depend on the sign-in client, so {method} {path} must declare no security — remove the operation's `security` block. A component that depends on the sign-in client declares the oauth2 scheme; one that does not declares none. If this component is meant to be protected, add the sign-in dependency to its design.json first.",

  // --- one requirement object, one scope (decision B1) ---------------------
  operation_security_not_a_list:
    "the `security` of {method} {path} is not a list. An operation's security is either absent (inheriting the document default), an empty list `[]` for a public operation, or a list holding one requirement object — `security: [ { oauth2: [<handle>] } ]`.",
  operation_multiple_requirements:
    "{method} {path} declares more than one security requirement object. An operation names at most one requirement object with at most one scope: several objects mean ANY of them to an OpenAPI tool, the gateway compares scopes as whole strings, and the generated server takes the last one — so the document, the gateway and the service middleware would disagree. Name the single handle this operation needs.",
  operation_multiple_scopes:
    "{method} {path} names more than one scope in its security requirement. An operation names at most one requirement object with at most one scope: several scopes inside one object mean ALL of them to an OpenAPI tool, and a permission handle is already the grain an operation is authorized at. Name the single handle this operation needs.",
  operation_unknown_scheme:
    "{method} {path} is secured with the scheme `{scheme}`, which this document does not declare. The only scheme a generated component declares is `oauth2` — write `security: [ { oauth2: [<handle>] } ]`.",

  // --- every scope is a catalog handle this component owns -----------------
  scope_not_in_catalog:
    "{method} {path} requires the scope `{scope}`, which the permission catalog in specs/design/security.json does not declare. OpenAPI operations reference catalog handles; they never define them. Add the action to its resource in security.json first, or name a handle the catalog already declares.",
  scope_not_owned:
    "{method} {path} requires the scope `{scope}`, whose resource the catalog assigns to component `{owner}`. Every scope an operation requires must be a catalog handle whose resource is owned by THIS component: one resource, one owning component, so the service that enforces a handle is the service that defines it. Call {owner} for that capability instead, or move the resource's ownership in specs/design/security.json.",
  flow_scope_not_in_catalog:
    "components.securitySchemes.oauth2 advertises the scope `{scope}` in flows, which the permission catalog in specs/design/security.json does not declare. The flows.*.scopes map lists only the handles this component's operations use, and every one of them is a catalog handle.",
  flow_scope_not_owned:
    "components.securitySchemes.oauth2 advertises the scope `{scope}` in flows, whose resource the catalog assigns to component `{owner}`. The flows.*.scopes map lists only the handles this component's own operations use, and this component does not own that resource.",

  // --- the OIDC scopes ride every access token -----------------------------
  reserved_oidc_scope:
    "`{scope}` is an OIDC scope, not a permission handle. It rides EVERY access token the identity provider issues, so an operation guarded on it admits every signed-in account in the organization while looking guarded — the gateway reports nothing and the operation fails wide open. Remove it from {where} and name a handle from the permission catalog in specs/design/security.json.",

  // --- the injected identity header ----------------------------------------
  identity_header_required:
    "{method} {path} declares the header parameter {header} as `required: true`. The generated server binds parameters before the authentication middleware runs, so a request that carries no verified identity is answered 400 by the parameter binder instead of 401 by the middleware. Declare it `required: false`; the gateway injects it on every protected operation.",
  public_operation_declares_identity_header:
    "{method} {path} is public (`security: []`) and declares the header parameter {header}. On an operation with no policy the gateway is a two-way pass-through: it does not strip inbound x-user-* headers, so anything the handler reads there is caller-supplied. Remove the parameter, or give the operation a scope so the gateway stamps the identity itself.",
} as const;

/** A key of this gate's message vocabulary. */
export type OpenapiSecurityMessageKey = keyof typeof OPENAPI_SECURITY_MESSAGES;

/** The values a template slot may be filled with. */
export type OpenapiSecurityMessageParams = Readonly<Record<string, string>>;

const SLOT_RE = /\{([a-zA-Z][a-zA-Z0-9]*)\}/g;

/**
 * Render one message. A slot with no parameter is left verbatim rather than
 * blanked: `missing_document_security` embeds the literal `{oauth2: []}` the
 * model is being asked to write, and a gate message that silently loses a name
 * is worse than one that shows a brace.
 */
export function openapiSecurityMessage(
  key: OpenapiSecurityMessageKey,
  params: OpenapiSecurityMessageParams = {},
): string {
  const template: string = OPENAPI_SECURITY_MESSAGES[key];
  return template.replace(SLOT_RE, (whole, slot: string) => {
    const value = params[slot];
    return value === undefined ? whole : value;
  });
}
