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
 * The manifest's entry point, in stages that stop at the first failure: JSON,
 * the version (an unsupported `schemaVersion` is one finding, not a schema
 * dump), the shape, then the references.
 */

import { MANIFEST_FILE, type Finding } from "../findings.js";
import { formatJsonPath } from "./json-path.js";
import { referenceFindings } from "./references.js";
import { PROTOTYPE_SCHEMA_VERSION, manifestSchema } from "./schema.js";
import type { PrototypeManifest } from "./types.js";

export type ManifestResult = { ok: true; manifest: PrototypeManifest } | { ok: false; findings: Finding[] };

export function parseManifest(value: unknown): ManifestResult {
  const version = unsupportedVersion(value);
  if (version) return { ok: false, findings: [version] };

  const shaped = manifestSchema.safeParse(value);
  if (!shaped.success) {
    return {
      ok: false,
      findings: shaped.error.issues.map((i) => ({
        code: "SCHEMA_VIOLATION",
        file: MANIFEST_FILE,
        location: formatJsonPath(i.path),
        message: i.message,
      })),
    };
  }
  const findings = referenceFindings(shaped.data);
  return findings.length > 0 ? { ok: false, findings } : { ok: true, manifest: shaped.data };
}

/** `parseManifest` over the file's text: text that is not JSON is one `SCHEMA_VIOLATION` at the root. */
export function parseManifestJson(text: string): ManifestResult {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch (e) {
    const message = `prototype.json is not valid JSON: ${e instanceof Error ? e.message : String(e)}`;
    return { ok: false, findings: [{ code: "SCHEMA_VIOLATION", file: MANIFEST_FILE, location: "(root)", message }] };
  }
  return parseManifest(value);
}

function unsupportedVersion(value: unknown): Finding | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value) || !("schemaVersion" in value)) return undefined;
  const version = (value as { schemaVersion: unknown }).schemaVersion;
  if (version === PROTOTYPE_SCHEMA_VERSION) return undefined;
  return {
    code: "UNSUPPORTED_VERSION",
    file: MANIFEST_FILE,
    location: "schemaVersion",
    message: `schemaVersion ${JSON.stringify(version)} is not supported; write schemaVersion ${PROTOTYPE_SCHEMA_VERSION}`,
  };
}
