/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * The ONLY description of what Ballerina Central sends.
 *
 * Everything downstream of `parseCentralDocs` takes typed values; this module
 * is the one place that touches `unknown`. That matters because the payload is
 * a deeply nested, undocumented JSON tree: hand-walking it — which is what the
 * upstream reader does, across 500 lines — turns a Central field being renamed
 * into subtly wrong signatures that nobody notices until an agent writes
 * Ballerina that will not compile. A schema turns the same event into a located
 * parse error.
 *
 * The strictness split is deliberate:
 *
 *   fields we read are REQUIRED — a rename, a removal or a type change is a
 *   loud `schema-drift` failure, because those are the changes that would make
 *   us render the wrong thing;
 *
 *   unknown keys are STRIPPED rather than rejected — Central adding a field is
 *   harmless to a reader that does not read it, and failing the command over
 *   one would take the capability away for a cosmetic upstream change.
 *
 * Additions are still visible: `test/keyspace.test.ts` snapshots the payload's
 * whole key space per fixture, so a new or vanished Central field shows up as a
 * reviewable diff without costing anything at run time.
 */

import { z } from "zod";

/**
 * A type expression, as Central models it: a recursive node whose meaning comes
 * from which combination of flags is set. Every field is optional because the
 * same node type stands in for a builtin, an external record reference, an
 * inline record's field, a union member and an array element.
 */
export interface CentralType {
  readonly name?: string | undefined;
  readonly category?: string | undefined;
  readonly orgName?: string | undefined;
  readonly moduleName?: string | undefined;
  readonly description?: string | undefined;
  readonly isArrayType?: boolean | undefined;
  readonly isNullable?: boolean | undefined;
  readonly isOptional?: boolean | undefined;
  readonly isAnonymousUnionType?: boolean | undefined;
  readonly isIntersectionType?: boolean | undefined;
  readonly isParenthesisedType?: boolean | undefined;
  readonly isTypeDesc?: boolean | undefined;
  readonly constraint?: CentralType | undefined;
  readonly elementType?: CentralType | undefined;
  readonly memberTypes?: readonly CentralType[] | undefined;
}

export const centralTypeSchema: z.ZodType<CentralType> = z.lazy(() =>
  z.object({
    name: z.string().optional(),
    category: z.string().optional(),
    orgName: z.string().optional(),
    moduleName: z.string().optional(),
    description: z.string().optional(),
    isArrayType: z.boolean().optional(),
    isNullable: z.boolean().optional(),
    isOptional: z.boolean().optional(),
    isAnonymousUnionType: z.boolean().optional(),
    isIntersectionType: z.boolean().optional(),
    isParenthesisedType: z.boolean().optional(),
    isTypeDesc: z.boolean().optional(),
    constraint: centralTypeSchema.optional(),
    elementType: centralTypeSchema.optional(),
    memberTypes: z.array(centralTypeSchema).optional(),
  }),
);

const parameterSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  defaultValue: z.string().optional(),
  type: centralTypeSchema,
});

const returnParameterSchema = z.object({
  description: z.string().optional(),
  type: centralTypeSchema,
});

/**
 * A function, in every position Central uses one: a client method, a module
 * function, a listener's `init`, a service type's remote contract. Which of the
 * four it is comes from `name === "init"`, `isResource` and `isRemote` — the
 * same discrimination `fromCentral` performs once, so the rest of the codebase
 * never sees these flags.
 */
const methodSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  isRemote: z.boolean().optional(),
  isResource: z.boolean().optional(),
  isDeprecated: z.boolean().optional(),
  accessor: z.string().optional(),
  resourcePath: z.string().optional(),
  parameters: z.array(parameterSchema).optional(),
  returnParameters: z.array(returnParameterSchema).optional(),
});

/**
 * A record field is one of two things and they have nothing in common: an
 * inclusion (`*OtherRecord;`, whose members get spliced in) or a declared
 * field. Modelling it as a union means `fromCentral` cannot read a declared
 * field's `type` off an inclusion by accident.
 */
const recordFieldSchema = z.union([
  z.object({ inclusionType: centralTypeSchema }),
  z.object({
    name: z.string(),
    description: z.string().optional(),
    defaultValue: z.string().optional(),
    type: centralTypeSchema,
  }),
]);

const namedSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
});

/**
 * An error declaration, which carries two fields beyond a name.
 *
 * `isDistinct` is required under this module's usual rule — verified present on
 * all 74 error declarations across the nine fixtures, so its absence means the
 * payload changed shape rather than that a package is unusual.
 *
 * `detailType` is optional because six of the nine module roots genuinely
 * publish none: an error at the top of its own hierarchy (`http:Error`,
 * `kafka:Error`) narrows nothing. Despite the name it holds the distinct
 * SUPERTYPE, and the reader calls it `base` from here on.
 */
const errorSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  isDistinct: z.boolean(),
  detailType: centralTypeSchema.optional(),
});

const recordSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  fields: z.array(recordFieldSchema).optional(),
});

const memberTypesSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  memberTypes: z.array(centralTypeSchema).optional(),
});

const constantSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  value: z.string(),
  type: centralTypeSchema,
});

const enumSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  members: z.array(namedSchema).optional(),
});

const clientSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  methods: z.array(methodSchema).optional(),
});

const listenerSchema = z.object({
  name: z.string(),
  initMethod: z.object({ parameters: z.array(parameterSchema).optional() }).optional(),
});

const serviceTypeSchema = z.object({
  name: z.string(),
  isDeprecated: z.boolean().optional(),
  methods: z.array(methodSchema).optional(),
});

const annotationSchema = z.object({
  name: z.string(),
  description: z.string().optional(),
  /** Comma-separated, e.g. `"service, type"`. */
  attachmentPoints: z.string().optional(),
});

/**
 * The module. Every array here is present in every package Central serves —
 * empty when the package has none — so they are required: an array going
 * missing means the payload changed shape, not that a package is unusual.
 */
const moduleSchema = z.object({
  id: z.string(),
  orgName: z.string(),
  summary: z.string().optional(),
  /**
   * The module's own written guide — the same bytes the published `.bala` keeps
   * at `docs/README.md`, verified identical for `ballerinax/kafka@4.6.5`.
   *
   * Optional against this package's usual rule that every field it reads is
   * required, because it is the one field whose absence must not cost the
   * caller anything else: a package that never wrote a `Module.md` should still
   * render its API. `--readme` reports the absence itself.
   */
  description: z.string().optional(),
  records: z.array(recordSchema),
  stringTypes: z.array(namedSchema),
  integerTypes: z.array(namedSchema),
  decimalTypes: z.array(namedSchema),
  booleanTypes: z.array(namedSchema),
  simpleNameReferenceTypes: z.array(namedSchema),
  arrayTypes: z.array(memberTypesSchema),
  errors: z.array(errorSchema),
  constants: z.array(constantSchema),
  enums: z.array(enumSchema),
  classes: z.array(namedSchema),
  objectTypes: z.array(namedSchema),
  unionTypes: z.array(memberTypesSchema),
  intersectionTypes: z.array(memberTypesSchema),
  clients: z.array(clientSchema),
  functions: z.array(methodSchema),
  listeners: z.array(listenerSchema),
  serviceTypes: z.array(serviceTypeSchema),
  annotations: z.array(annotationSchema),
});

export const centralDocsSchema = z.object({
  docsData: z.object({
    modules: z.array(moduleSchema).min(1),
  }),
});

export type CentralDocs = z.infer<typeof centralDocsSchema>;
export type CentralModule = z.infer<typeof moduleSchema>;
export type CentralMethod = z.infer<typeof methodSchema>;
export type CentralParameter = z.infer<typeof parameterSchema>;
export type CentralRecordField = z.infer<typeof recordFieldSchema>;
