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
 * OpenAPI 3.x parser. Turns the raw YAML/JSON string into a flat,
 * UI-friendly model that the React tree can render without knowing
 * anything about $ref lookups, anyOf flattening, or schema recursion.
 *
 * Intentionally permissive — many of the generated specs in this app
 * are partial drafts. Missing fields default to sensible empty values;
 * unrecognised JSON-schema constructs degrade to `{ type: 'any' }`.
 */

import yaml from 'js-yaml';

export type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE' | 'HEAD' | 'OPTIONS';

/**
 * What a caller must present to reach an operation, read from the document's
 * and the operation's `security` blocks.
 *
 * - `public` — no token at all. The gateway applies no policy to the operation.
 * - `signedIn` — any valid token for this API's audience, no permission named.
 * - `scope` — the one permission handle the gateway checks, e.g. `claims:read`.
 *
 * There is no fourth state: the platform's gate admits AT MOST ONE scope on an
 * operation, so this view never has to render a conjunction.
 */
export type Protection =
  | { kind: 'public' }
  | { kind: 'signedIn' }
  | { kind: 'scope'; scope: string };

export interface ParsedInfo {
  title: string;
  version: string;
  description: string;
}

export interface Param {
  name: string;
  type: string;
  required: boolean;
  /** "query" | "path" | "header" | "cookie" | "body" — kept as the raw `in` value. */
  in: string;
  desc: string;
}

export interface SchemaField {
  name: string;
  type: string;
  required: boolean;
  desc: string;
  enumValues?: string[];
  children?: SchemaField[];
}

export interface Schema {
  /** Display label — `"object"`, `"array<Charge>"`, `"string"`, … */
  type: string;
  fields: SchemaField[];
}

export interface Response {
  code: string;
  description: string;
  /** Display name when the response body resolves to a named schema. */
  schemaName?: string;
  schema?: Schema;
  example?: unknown;
}

export interface Operation {
  /** Stable id derived from method + path, used for anchors and React keys. */
  id: string;
  method: Method;
  path: string;
  /** One-line summary (used as the right-aligned label in the row). */
  name: string;
  /** Longer description shown inside the body. */
  summary: string;
  params: Param[];
  responses: Response[];
  /** What the caller must present — the operation's own `security`, else the document default. */
  protection: Protection;
}

export interface TagSection {
  id: string;
  title: string;
  blurb: string;
  endpoints: Operation[];
}

export interface ParsedOpenApi {
  info: ParsedInfo;
  sections: TagSection[];
  schemas: Record<string, Schema>;
}

export interface ParseError {
  kind: 'parse-error';
  message: string;
}

export type ParseResult = ParsedOpenApi | ParseError;

const METHOD_SET = new Set<Method>(['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']);

// Crude JSON-pointer resolver — only needed for the local `#/components/...`
// shape that `$ref` always uses in our generated specs.
function resolveRef(root: unknown, ref: string): unknown {
  if (!ref.startsWith('#/')) return undefined;
  const parts = ref.slice(2).split('/');
  let cur: unknown = root;
  for (const p of parts) {
    if (!cur || typeof cur !== 'object') return undefined;
    cur = (cur as Record<string, unknown>)[decodeURIComponent(p.replace(/~1/g, '/').replace(/~0/g, '~'))];
  }
  return cur;
}

function asObject(v: unknown): Record<string, unknown> | undefined {
  return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : undefined;
}

function asString(v: unknown, fallback = ''): string {
  return typeof v === 'string' ? v : fallback;
}

function asArray(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}

interface ResolveCtx {
  root: unknown;
  /** Set of refs currently being expanded — prevents infinite recursion on cyclic specs. */
  seen: Set<string>;
  /**
   * The schema nodes on the current path, by IDENTITY.
   *
   * `seen` only guards `$ref` cycles, and a `$ref` is not the only way a
   * document can be cyclic: js-yaml materialises a YAML anchor referred to
   * from inside itself as a genuinely circular object graph
   * (`Node: &N {type: object, properties: {self: *N}}`), so the walk below
   * would recurse on the very node it started from and blow the stack. A ref
   * cycle is a cycle in the DOCUMENT's text; this is a cycle in the parsed
   * OBJECT, and only object identity can see it.
   *
   * Path-scoped, not global — the same schema reached twice down two different
   * branches still expands twice, exactly as `seen` behaves for refs.
   */
  nodes: ReadonlySet<object>;
}

/** `ctx` with `node` marked as being on the current path. */
function entering(ctx: ResolveCtx, node: object): ResolveCtx {
  return { ...ctx, nodes: new Set(ctx.nodes).add(node) };
}

function describeSchemaType(node: Record<string, unknown>, ctx: ResolveCtx): string {
  const refStr = typeof node.$ref === 'string' ? node.$ref : undefined;
  if (refStr) {
    const lastSeg = refStr.split('/').pop() ?? 'ref';
    return lastSeg;
  }
  const type = asString(node.type);
  if (type === 'array') {
    const items = asObject(node.items);
    // `items: *self` — the element type IS the array. Name it `array` rather
    // than descending into a loop that has no bottom.
    if (items && ctx.nodes.has(items)) return 'array';
    if (items) return `array<${describeSchemaType(items, entering(ctx, items))}>`;
    return 'array';
  }
  if (Array.isArray(node.enum)) return 'enum';
  if (type) return type;
  if (node.properties) return 'object';
  if (node.allOf || node.oneOf || node.anyOf) return 'object';
  return 'any';
}

function buildField(
  name: string,
  node: Record<string, unknown>,
  required: boolean,
  ctx: ResolveCtx,
): SchemaField {
  // Resolve $ref to its target before reading the rest.
  let resolved = node;
  const refStr = typeof node.$ref === 'string' ? node.$ref : undefined;
  if (refStr && !ctx.seen.has(refStr)) {
    const target = asObject(resolveRef(ctx.root, refStr));
    if (target) {
      const nextSeen = new Set(ctx.seen);
      nextSeen.add(refStr);
      const lastSeg = refStr.split('/').pop() ?? 'ref';
      return {
        name,
        type: lastSeg,
        required,
        desc: asString(node.description, asString(target.description)),
        children: collectFields(target, { ...ctx, seen: nextSeen }),
      };
    }
  }

  const type = describeSchemaType(resolved, ctx);
  const desc = asString(resolved.description);
  const enumValues = Array.isArray(resolved.enum)
    ? resolved.enum.map((v) => String(v))
    : undefined;

  // Recurse for object-with-properties or array-with-object-items
  let children: SchemaField[] | undefined;
  if (resolved.properties) {
    children = collectFields(resolved, ctx);
  } else if (type.startsWith('array')) {
    const items = asObject(resolved.items);
    if (items?.properties) {
      children = collectFields(items, ctx);
    } else if (items && typeof items.$ref === 'string' && !ctx.seen.has(items.$ref)) {
      const target = asObject(resolveRef(ctx.root, items.$ref));
      if (target) {
        const nextSeen = new Set(ctx.seen);
        nextSeen.add(items.$ref);
        children = collectFields(target, { ...ctx, seen: nextSeen });
      }
    }
  }

  // Conditional spread rather than assigning `undefined`: SchemaField's
  // enumValues/children are optional, and exactOptionalPropertyTypes forbids
  // an explicit `undefined` on an optional property.
  return {
    name,
    type,
    required,
    desc,
    ...(enumValues !== undefined ? { enumValues } : {}),
    ...(children !== undefined ? { children } : {}),
  };
}

function collectFields(node: Record<string, unknown>, ctx: ResolveCtx): SchemaField[] {
  // Already expanding this exact node further up the path: a YAML anchor points
  // back at one of its own ancestors. Stop, and let the field that named it
  // render as a leaf.
  if (ctx.nodes.has(node)) return [];
  const props = asObject(node.properties);
  if (!props) return [];
  const requiredList = new Set(asArray(node.required).filter((v): v is string => typeof v === 'string'));
  const inner = entering(ctx, node);
  const fields: SchemaField[] = [];
  for (const [name, raw] of Object.entries(props)) {
    const propNode = asObject(raw);
    if (!propNode) continue;
    fields.push(buildField(name, propNode, requiredList.has(name), inner));
  }
  return fields;
}

function buildSchema(node: Record<string, unknown>, ctx: ResolveCtx): Schema {
  return {
    type: describeSchemaType(node, ctx),
    fields: collectFields(node, ctx),
  };
}

function bodyToSchemaAndName(
  body: Record<string, unknown> | undefined,
  ctx: ResolveCtx,
): { schema?: Schema; schemaName?: string; example?: unknown } {
  if (!body) return {};
  const content = asObject(body.content);
  if (!content) return {};
  // Pick the first JSON-ish media type we recognise.
  const mediaKey = Object.keys(content).find((k) => /json/i.test(k)) ?? Object.keys(content)[0];
  if (mediaKey === undefined) return {};
  const media = asObject(content[mediaKey]);
  if (!media) return {};
  const schemaNode = asObject(media.schema);
  if (!schemaNode) return { example: media.example };

  const ref = typeof schemaNode.$ref === 'string' ? schemaNode.$ref : undefined;
  if (ref) {
    const target = asObject(resolveRef(ctx.root, ref));
    const name = ref.split('/').pop();
    return {
      ...(name !== undefined ? { schemaName: name } : {}),
      ...(target ? { schema: buildSchema(target, ctx) } : {}),
      example: media.example,
    };
  }
  return {
    schema: buildSchema(schemaNode, ctx),
    example: media.example,
  };
}

function buildParam(node: Record<string, unknown>, ctx: ResolveCtx): Param {
  const schemaNode = asObject(node.schema);
  return {
    name: asString(node.name),
    type: schemaNode ? describeSchemaType(schemaNode, ctx) : asString(node.type, 'string'),
    required: node.required === true,
    in: asString(node.in, 'query'),
    desc: asString(node.description),
  };
}

// ── Protection (the `security` blocks) ───────────────────────────────────────
//
// The platform's build gate fixes the shape this reader sees
// (`packages/agent-stream/src/openapi-security.ts`): a protected component
// declares exactly one scheme, `oauth2`; the document default is exactly
// `security: [ { oauth2: [] } ]`; an operation's own block is absent, `[]`, or
// ONE requirement object naming that scheme with at most one scope. A component
// with no sign-in dependency declares no scheme and no `security` anywhere, so
// every one of its operations is public.
//
// Nothing here judges a document — a spec is read here while it is still being
// streamed, and a half-written or hand-edited one must render, not throw. Every
// shape the gate refuses degrades to the closest readable state instead. Two
// leniencies follow from that and are worth naming: the scheme is found by
// TYPE rather than by the name `oauth2`, and nothing here knows whether the
// component provisions sign-in at all — so a contract that declares a scheme
// while its component depends on no sign-in reads as protected here, and it is
// the build gate, which can see the architecture, that calls that a mistake.

/** The scheme name the platform uses, and the fallback when none is declared. */
const DEFAULT_SCHEME = 'oauth2';

interface SecurityCtx {
  /** The name this document gives its OAuth2 scheme. */
  scheme: string;
  /** Protection for an operation that declares no `security` of its own. */
  documentDefault: Protection;
}

/**
 * The name of the document's OAuth2 security scheme. Generated specs always
 * call it `oauth2`; a hand-written one may not, so the declared scheme wins.
 */
function oauth2SchemeName(root: Record<string, unknown>): string {
  const schemes = asObject(asObject(root.components)?.securitySchemes);
  if (schemes) {
    for (const [name, raw] of Object.entries(schemes)) {
      const node = asObject(raw);
      if (node && asString(node.type).toLowerCase() === 'oauth2') return name;
    }
  }
  return DEFAULT_SCHEME;
}

/**
 * Read one `security` block into a protection, or `undefined` when the block is
 * absent or unreadable — which means "inherit the document default" for an
 * operation, and "no default" for the document itself.
 *
 * Degradations, none of which a gate-passing document can reach:
 * - several requirement objects (an "any of") → the first one is read;
 * - an object naming several schemes (an "all of") → the OAuth2 one is read;
 * - an object naming only some other scheme → `signedIn`, because this view
 *   cannot name a handle the gateway would not enforce;
 * - several scopes in one requirement → the first one;
 * - an empty requirement object `{}` → `public`, OpenAPI's own reading of it.
 */
function readSecurity(value: unknown, scheme: string): Protection | undefined {
  if (!Array.isArray(value)) return undefined;
  if (value.length === 0) return { kind: 'public' };
  const requirement = asObject(value[0]);
  if (!requirement) return undefined;
  const scopes = requirement[scheme];
  if (scopes === undefined) {
    return Object.keys(requirement).length > 0 ? { kind: 'signedIn' } : { kind: 'public' };
  }
  if (!Array.isArray(scopes) || scopes.length === 0) return { kind: 'signedIn' };
  const first = scopes[0];
  return typeof first === 'string' && first !== ''
    ? { kind: 'scope', scope: first }
    : { kind: 'signedIn' };
}

/**
 * The document-level default. A document with NO `security` key protects
 * nothing: that is OpenAPI's reading of an absent default, and it is exactly
 * the shape the gate requires of a component with no sign-in dependency.
 */
function buildSecurityCtx(root: Record<string, unknown>): SecurityCtx {
  const scheme = oauth2SchemeName(root);
  return {
    scheme,
    documentDefault: readSecurity(root.security, scheme) ?? { kind: 'public' },
  };
}

function buildOperation(
  method: Method,
  path: string,
  node: Record<string, unknown>,
  pathLevelParams: unknown[],
  ctx: ResolveCtx,
  sec: SecurityCtx,
): Operation {
  const params: Param[] = [];
  for (const raw of [...pathLevelParams, ...asArray(node.parameters)]) {
    const obj = asObject(raw);
    if (obj) params.push(buildParam(obj, ctx));
  }

  // Body params: collapse `requestBody.content[…]/schema` into a synthetic
  // `body` row that lists every top-level property — closer to how Swagger
  // surfaces them than a single opaque "body" entry.
  const requestBody = asObject(node.requestBody);
  if (requestBody) {
    const { schema, schemaName } = bodyToSchemaAndName(requestBody, ctx);
    if (schema && schema.fields.length) {
      for (const f of schema.fields) {
        params.push({
          name: f.name,
          type: f.type,
          required: f.required,
          in: 'body',
          desc: f.desc,
        });
      }
    } else if (schemaName) {
      params.push({
        name: 'body',
        type: schemaName,
        required: requestBody.required === true,
        in: 'body',
        desc: asString(requestBody.description),
      });
    }
  }

  const responses: Response[] = [];
  const respObj = asObject(node.responses);
  if (respObj) {
    for (const [code, raw] of Object.entries(respObj)) {
      const r = asObject(raw);
      if (!r) continue;
      const { schema, schemaName, example } = bodyToSchemaAndName(r, ctx);
      responses.push({
        code,
        description: asString(r.description, ''),
        ...(schemaName !== undefined ? { schemaName } : {}),
        ...(schema !== undefined ? { schema } : {}),
        example,
      });
    }
  }

  return {
    id: `${method.toLowerCase()}-${path.replace(/[^a-z0-9]+/gi, '-').replace(/^-+|-+$/g, '')}`,
    method,
    path,
    name: asString(node.summary, asString(node.operationId, path)),
    summary: asString(node.description),
    params,
    responses,
    protection: readSecurity(node.security, sec.scheme) ?? sec.documentDefault,
  };
}

function buildSections(root: Record<string, unknown>, ctx: ResolveCtx, sec: SecurityCtx): TagSection[] {
  const paths = asObject(root.paths) ?? {};
  const tags = asArray(root.tags).map((t) => asObject(t)).filter((t): t is Record<string, unknown> => !!t);
  const tagBlurb = new Map<string, string>();
  const tagOrder: string[] = [];
  for (const t of tags) {
    const name = asString(t.name);
    if (!name) continue;
    tagBlurb.set(name, asString(t.description));
    tagOrder.push(name);
  }

  const byTag = new Map<string, Operation[]>();
  for (const [path, raw] of Object.entries(paths)) {
    const pathNode = asObject(raw);
    if (!pathNode) continue;
    const pathLevelParams = asArray(pathNode.parameters);
    for (const [methodKey, opRaw] of Object.entries(pathNode)) {
      const method = methodKey.toUpperCase() as Method;
      if (!METHOD_SET.has(method)) continue;
      const opNode = asObject(opRaw);
      if (!opNode) continue;
      const op = buildOperation(method, path, opNode, pathLevelParams, ctx, sec);
      const opTags = asArray(opNode.tags).filter((t): t is string => typeof t === 'string');
      const bucket = opTags[0] ?? 'Operations';
      if (!byTag.has(bucket)) byTag.set(bucket, []);
      byTag.get(bucket)!.push(op);
    }
  }

  // Preserve declared tag order; append any tags discovered only on operations.
  const seen = new Set<string>();
  const sections: TagSection[] = [];
  for (const t of tagOrder) {
    if (!byTag.has(t)) continue;
    seen.add(t);
    sections.push({
      id: slug(t),
      title: t,
      blurb: tagBlurb.get(t) ?? '',
      endpoints: byTag.get(t)!,
    });
  }
  for (const [t, ops] of byTag.entries()) {
    if (seen.has(t)) continue;
    sections.push({
      id: slug(t),
      title: t,
      blurb: tagBlurb.get(t) ?? '',
      endpoints: ops,
    });
  }
  return sections;
}

function slug(s: string): string {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'section';
}

function buildSchemas(root: Record<string, unknown>, ctx: ResolveCtx): Record<string, Schema> {
  const components = asObject(root.components);
  if (!components) return {};
  const schemas = asObject(components.schemas);
  if (!schemas) return {};
  const out: Record<string, Schema> = {};
  for (const [name, raw] of Object.entries(schemas)) {
    const node = asObject(raw);
    if (!node) continue;
    out[name] = buildSchema(node, ctx);
  }
  return out;
}

/**
 * Read a document into the view model, or say why it cannot be read.
 *
 * NEVER THROWS. The whole body is guarded, not just `yaml.load`: this parser
 * runs against half-streamed and hand-edited documents, and one caller
 * (`baselineOperations`, behind the console's Security page) parses every
 * owning component's contract during render — a fault escaping as an exception
 * there takes a page down rather than degrading one panel. A parser fault is a
 * document this reader could not read, which is exactly what `parse-error`
 * says, so the caller that already handles a malformed document handles this
 * too.
 */
export function parseOpenApi(text: string): ParseResult {
  try {
    const doc = yaml.load(text);
    const root = asObject(doc);
    if (!root) {
      return { kind: 'parse-error', message: 'OpenAPI document is not an object' };
    }

    const ctx: ResolveCtx = { root, seen: new Set(), nodes: new Set() };
    const info = asObject(root.info) ?? {};
    return {
      info: {
        title: asString(info.title, 'Untitled API'),
        version: asString(info.version, ''),
        description: asString(info.description),
      },
      sections: buildSections(root, ctx, buildSecurityCtx(root)),
      schemas: buildSchemas(root, ctx),
    };
  } catch (e) {
    return { kind: 'parse-error', message: e instanceof Error ? e.message : String(e) };
  }
}
