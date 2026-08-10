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
 * Add a package to the corpus: `pnpm record-fixture ballerinax/kafka [version]`.
 *
 * Extending coverage is meant to be this cheap — a bug report becomes a fixture
 * and a snapshot, and the snapshot is the review. Fixtures are stored gzipped
 * because they are minified JSON nobody reads a diff of (`ballerinax/github` is
 * 12MB raw, 332KB compressed); snapshots are stored as plain text because the
 * diff IS the artifact.
 */

import { writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import { CENTRAL_BASE_URL, fetchJson, parseCentralDocs } from "../src/central/client.js";
import { parseQualifiedName, parseVersion } from "../src/qualified.js";
import { describeFailure } from "../src/result.js";
import { fixtureSlug, renderFixture, writeSnapshot } from "../test/corpus.js";

const packageDir = dirname(dirname(fileURLToPath(import.meta.url)));

const [rawName, rawVersion] = process.argv.slice(2);
if (rawName === undefined) {
  process.stderr.write("Usage: pnpm record-fixture <org/name> [version]\n");
  process.exit(2);
}

const qualified = parseQualifiedName(rawName);
if (!qualified.ok) {
  process.stderr.write(`${describeFailure(qualified.error)}\n`);
  process.exit(2);
}

let version: string;
if (rawVersion === undefined) {
  const url = `${CENTRAL_BASE_URL}registry/packages/${qualified.value.org}/${qualified.value.name}`;
  const versions = await fetchJson(url);
  if (!versions.ok || !Array.isArray(versions.value) || typeof versions.value[0] !== "string") {
    process.stderr.write(`could not resolve a version for ${rawName}\n`);
    process.exit(1);
  }
  version = versions.value[0];
} else {
  const parsed = parseVersion(rawVersion);
  if (!parsed.ok) {
    process.stderr.write(`${describeFailure(parsed.error)}\n`);
    process.exit(2);
  }
  version = parsed.value;
}

const docsUrl = `${CENTRAL_BASE_URL}docs/${qualified.value.org}/${qualified.value.name}/${version}`;
const raw = await fetchJson(docsUrl);
if (!raw.ok) {
  process.stderr.write(`${JSON.stringify(raw.error)}\n`);
  process.exit(1);
}

// Recorded only if it parses: a fixture the reader cannot read is not a test
// case, it is a bug report about the schema.
const parsed = parseCentralDocs(raw.value, `${rawName}:${version}`);
if (!parsed.ok) {
  process.stderr.write(`${JSON.stringify(parsed.error, null, 2)}\n`);
  process.exit(1);
}

const slug = fixtureSlug(rawName);
const fixturePath = join(packageDir, "test", "__fixtures__", `${slug}.json.gz`);
writeFileSync(fixturePath, gzipSync(Buffer.from(JSON.stringify(raw.value)), { level: 9 }));
writeSnapshot(slug, renderFixture(parsed.value));

process.stdout.write(`recorded ${rawName}:${version} -> ${slug}\n`);
