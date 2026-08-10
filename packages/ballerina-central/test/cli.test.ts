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
 * The command's contract: stdout carries the syntax string and nothing else,
 * stderr carries one JSON failure, and the exit code says whether re-running
 * could help.
 *
 * The skill depends on all three — it redirects stdout straight into a file,
 * and it has no fallback source to fall back to, so a non-zero exit has to say
 * plainly which of the two things went wrong.
 */

import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { parseArgs, run, type CliStreams } from "../src/cli.js";
import type { FetchLike } from "../src/central/client.js";
import { loadRawFixture, readSnapshot } from "./corpus.js";

function capture(): CliStreams & { stdout: () => string; stderr: () => string } {
  let out = "";
  let error = "";
  return {
    out: (text) => {
      out += text;
    },
    errorOut: (text) => {
      error += text;
    },
    stdout: () => out,
    stderr: () => error,
  };
}

/** Central, replayed: the versions endpoint then the docs endpoint. */
function centralFor(slug: string, version: string): FetchLike {
  const docs = loadRawFixture(slug);
  return (url) =>
    Promise.resolve(
      new Response(JSON.stringify(url.includes("/docs/") ? docs : [version]), { status: 200 }),
    );
}

test("no arguments prints usage and exits 2", async () => {
  const streams = capture();
  assert.equal(await run([], streams), 2);
  assert.equal(streams.stdout(), "");
  assert.match(streams.stderr(), /^Usage: bal-library/);
});

test("--help is the same as no arguments", async () => {
  const streams = capture();
  assert.equal(await run(["--help"], streams), 2);
  assert.match(streams.stderr(), /^Usage: bal-library/);
});

test("a version suffix in the package name is rejected before any request", async () => {
  const streams = capture();
  const exitCode = await run(["ballerinax/github:6.0.0"], streams, {
    fetch: () => {
      throw new Error("must not reach the network");
    },
  });
  assert.equal(exitCode, 2);
  assert.equal(streams.stdout(), "");
  const failure = JSON.parse(streams.stderr()) as { kind: string; suggestion: string };
  assert.equal(failure.kind, "validation");
  assert.match(failure.suggestion, /Drop any ':version' suffix/);
});

test("an unknown package exits 1 and names itself, rather than printing nothing at 0", async () => {
  const streams = capture();
  const exitCode = await run(["ballerinax/nope"], streams, {
    fetch: () => Promise.resolve(new Response(JSON.stringify([]), { status: 200 })),
    maxAttempts: 1,
    baseDelayMs: 1,
  });
  assert.equal(exitCode, 1);
  assert.equal(streams.stdout(), "");
  assert.equal((JSON.parse(streams.stderr()) as { kind: string }).kind, "package-not-found");
});

test("--project-dir without a directory is a usage error", async () => {
  const streams = capture();
  assert.equal(await run(["ballerinax/github", "--project-dir"], streams), 2);
  assert.equal((JSON.parse(streams.stderr()) as { kind: string }).kind, "validation");
});

test("a third positional argument is rejected rather than ignored", () => {
  const parsed = parseArgs(["ballerinax/github", "6.0.0", "extra"]);
  assert.notEqual(parsed, null);
  assert.equal(parsed?.ok, false);
});

test("--project-dir and a version are both understood", () => {
  const parsed = parseArgs(["ballerinax/github", "6.0.0", "--project-dir", "stars-api"]);
  assert.equal(parsed?.ok, true);
  if (!parsed?.ok) return;
  assert.equal(parsed.value.qualified.org, "ballerinax");
  assert.equal(parsed.value.qualified.name, "github");
  assert.equal(parsed.value.options.version, "6.0.0");
  assert.equal(parsed.value.options.projectDir, "stars-api");
});

test("a successful lookup writes the resolved header and the library, and nothing else", async () => {
  const streams = capture();
  const exitCode = await run(["ballerinax/sap"], streams, { fetch: centralFor("ballerinax__sap", "1.3.1") });
  assert.equal(exitCode, 0);
  assert.equal(streams.stderr(), "");
  assert.equal(streams.stdout(), `// Resolved: ballerinax/sap:1.3.1\n${readSnapshot("ballerinax__sap")}`);
});

test("--readme prints the package's guide as Markdown, on the same one fetch", async () => {
  const streams = capture();
  const exitCode = await run(["ballerinax/sap", "--readme"], streams, {
    fetch: centralFor("ballerinax__sap", "1.3.1"),
  });
  assert.equal(exitCode, 0);
  assert.equal(streams.stderr(), "");
  assert.match(streams.stdout(), /^<!-- Resolved: ballerinax\/sap:1\.3\.1 -->\n<!-- Module: sap -->\n## Overview/);
  // No Ballerina leaked into the Markdown document.
  assert.doesNotMatch(streams.stdout(), /^\/\/ Resolved:/m);
});

test("--readme and the API document resolve the same version the same way", () => {
  const api = parseArgs(["ballerinax/github", "6.0.0"]);
  const readme = parseArgs(["ballerinax/github", "6.0.0", "--readme"]);
  assert.ok(api?.ok && readme?.ok);
  assert.equal(api.value.document, "api");
  assert.equal(readme.value.document, "readme");
  assert.deepEqual(api.value.options, readme.value.options);
});

test("a package with no guide exits 1, not 0 with an empty document", async () => {
  const stripped = structuredClone(loadRawFixture("ballerinax__sap")) as {
    docsData: { modules: { description?: string }[] };
  };
  for (const module of stripped.docsData.modules) delete module.description;

  const streams = capture();
  const exitCode = await run(["ballerinax/sap", "1.3.1", "--readme"], streams, {
    fetch: () => Promise.resolve(new Response(JSON.stringify(stripped), { status: 200 })),
  });
  assert.equal(exitCode, 1);
  assert.equal(streams.stdout(), "");
  const failure = JSON.parse(streams.stderr()) as { kind: string; qualified: string };
  assert.equal(failure.kind, "no-readme");
  assert.equal(failure.qualified, "ballerinax/sap:1.3.1");
});

test("a missing guide costs the caller nothing else — the API still renders", async () => {
  const stripped = structuredClone(loadRawFixture("ballerinax__sap")) as {
    docsData: { modules: { description?: string }[] };
  };
  for (const module of stripped.docsData.modules) delete module.description;

  const streams = capture();
  const exitCode = await run(["ballerinax/sap", "1.3.1"], streams, {
    fetch: () => Promise.resolve(new Response(JSON.stringify(stripped), { status: 200 })),
  });
  assert.equal(exitCode, 0);
  assert.match(streams.stdout(), /^\/\/ Resolved: ballerinax\/sap:1\.3\.1\n/);
});

test("a locked Dependencies.toml version outranks Central's latest", async () => {
  const projectDir = mkdtempSync(join(tmpdir(), "bal-library-"));
  writeFileSync(
    join(projectDir, "Dependencies.toml"),
    '[[package]]\norg = "ballerinax"\nname = "sap"\nversion = "1.3.1"\n',
  );
  const streams = capture();
  const exitCode = await run(["ballerinax/sap", "--project-dir", projectDir], streams, {
    fetch: (url) => {
      // Reaching the versions endpoint would mean the lock was ignored.
      assert.match(url, /\/docs\//);
      return Promise.resolve(new Response(JSON.stringify(loadRawFixture("ballerinax__sap")), { status: 200 }));
    },
  });
  assert.equal(exitCode, 0);
  assert.match(streams.stdout(), /^\/\/ Resolved: ballerinax\/sap:1\.3\.1\n/);
});
