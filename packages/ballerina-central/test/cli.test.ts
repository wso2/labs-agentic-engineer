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
 * The command's contract: stdout carries the requested document and nothing else,
 * stderr carries one JSON failure, and the exit code says whether re-running could
 * help.
 *
 * The skill depends on all three — it redirects stdout straight into a file, and
 * it has no fallback source, so a non-zero exit has to say plainly which kind of
 * thing went wrong. The verb grammar adds one more thing to pin: every mistyped or
 * version-skewed call must fail LOUDLY at exit 2 rather than resolving as
 * something else and reporting a Central failure the agent will retry.
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
    Promise.resolve(new Response(JSON.stringify(url.includes("/docs/") ? docs : [version]), { status: 200 }));
}

const NEVER: FetchLike = () => {
  throw new Error("must not reach the network");
};

function failure(streams: { stderr: () => string }): Record<string, unknown> {
  return JSON.parse(streams.stderr()) as Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// Usage and argument errors
// ---------------------------------------------------------------------------

test("no arguments prints usage and exits 2", async () => {
  const streams = capture();
  assert.equal(await run([], streams), 2);
  assert.equal(streams.stdout(), "");
  assert.match(streams.stderr(), /^Usage: bal-library/);
});

test("--help is the same as no arguments, and names the cache", async () => {
  const streams = capture();
  assert.equal(await run(["--help"], streams), 2);
  assert.match(streams.stderr(), /^Usage: bal-library/);
  // The one place the cache is allowed to speak: outside the document and outside
  // the Failure contract, which is how an operator proves it is alive in a runner.
  assert.match(streams.stderr(), /^Cache: disabled$/m);
});

test("a version suffix in the package name is rejected before any request", async () => {
  const streams = capture();
  assert.equal(await run(["ballerinax/github:6.0.0"], streams, { fetch: NEVER }), 2);
  assert.equal(streams.stdout(), "");
  const error = failure(streams);
  assert.equal(error["kind"], "validation");
  assert.match(String(error["suggestion"]), /Drop any ':version' suffix/);
});

test("an unrecognised flag is a usage error, not a version Central is asked about", async () => {
  // The regression this pins: `--refresh` on a stale binary used to resolve as the
  // VERSION, so it reported `package-not-found` at exit 1 — which the skill teaches
  // means "Central could not answer, run it once more". The agent then retried a
  // command that could never succeed.
  const streams = capture();
  assert.equal(await run(["ballerina/http", "--nonesuch"], streams, { fetch: NEVER }), 2);
  assert.equal(streams.stdout(), "");
  const error = failure(streams);
  assert.equal(error["kind"], "validation");
  assert.match(String(error["message"]), /Unknown option '--nonesuch'/);
  assert.match(String(error["suggestion"]), /Known flags are/);
});

test("a first positional that is neither a package nor a verb names the four verbs", async () => {
  const streams = capture();
  assert.equal(await run(["opps", "ballerinax/github"], streams, { fetch: NEVER }), 2);
  const error = failure(streams);
  assert.equal(error["kind"], "validation");
  assert.match(String(error["suggestion"]), /overview, ops, type, api/);
});

test("a verb leads because a verb after the package reads as a version", () => {
  // Today's binary parses both; the point is what YESTERDAY's does with each. A
  // leading verb has no slash and fails the qualified-name regex at exit 2, which
  // is loud. A trailing one lands in the version slot and comes back as
  // `package-not-found` at exit 1, which the skill teaches means "retry".
  assert.equal(parseArgs(["type", "ballerinax/github", "FullRepository"])?.ok, true);
  const skewed = parseArgs(["ballerinax/github", "ops"]);
  assert.equal(skewed?.ok, true);
  if (skewed?.ok) assert.equal(skewed.value.options.version, "ops");
});

test("--project-dir and --client without a value are usage errors", async () => {
  for (const argv of [
    ["ballerinax/github", "--project-dir"],
    ["ops", "ballerinax/github", "--client"],
    // The value must not be silently taken from the next flag either.
    ["ops", "ballerinax/github", "--client", "--sigs"],
  ]) {
    const streams = capture();
    assert.equal(await run(argv, streams, { fetch: NEVER }), 2, argv.join(" "));
    assert.equal(failure(streams)["kind"], "validation");
    assert.ok(String(failure(streams)["suggestion"]).length > 0);
  }
});

test("--flag=value is accepted, because it is the form half the world types", async () => {
  const streams = capture();
  const exitCode = await run(["ops", "ballerina/http", "--client=FailoverClient"], streams, {
    fetch: centralFor("ballerina__http", "2.16.6"),
  });
  assert.equal(exitCode, 0);
  assert.match(streams.stdout(), /`FailoverClient`/);

  const parsed = parseArgs(["ballerinax/github", "--project-dir=stars-api"]);
  assert.equal(parsed?.ok, true);
  if (parsed?.ok) assert.equal(parsed.value.options.projectDir, "stars-api");
});

test("-h is the short form of --help wherever it appears", async () => {
  for (const argv of [["-h"], ["ops", "ballerinax/github", "-h"], ["type", "x/y", "Name", "-h"]]) {
    const streams = capture();
    assert.equal(await run(argv, streams, { fetch: NEVER }), 2, argv.join(" "));
    assert.match(streams.stderr(), /^Usage: bal-library/);
  }
});

test("each verb rejects a positional it has no meaning for", () => {
  assert.equal(parseArgs(["ballerinax/github", "6.0.0", "extra"])?.ok, false);
  assert.equal(parseArgs(["ops", "ballerinax/github", "repos", "extra"])?.ok, false);
  assert.equal(parseArgs(["api", "ballerinax/github", "6.0.0"])?.ok, false);
  assert.equal(parseArgs(["type", "ballerinax/github"])?.ok, false, "type needs at least one name");
});

test("a flag the verb does not take is rejected, not silently ignored", async () => {
  // The regression this pins: `overview --deps` used to exit 0 with the flag
  // dropped. That is the same silent class as an unknown flag resolving to a
  // version — the caller believes it asked for something it did not get, and
  // nothing in the output says otherwise. It is also the version-skew shape,
  // differing only in which side is ahead.
  const cases: [string[], RegExp][] = [
    [["ballerinax/kafka", "--deps"], /--deps belongs to 'type'/],
    [["overview", "ballerinax/kafka", "--sigs"], /--sigs belongs to 'ops'/],
    [["type", "ballerinax/kafka", "Error", "--sigs"], /--sigs belongs to 'ops'/],
    [["type", "ballerinax/kafka", "Error", "--client", "Foo"], /--client belongs to 'overview' and 'ops'/],
    [["api", "ballerinax/kafka", "--client", "Foo"], /--client belongs to 'overview' and 'ops'/],
    [["api", "ballerinax/kafka", "--deps"], /--deps belongs to 'type'/],
    [["ops", "ballerinax/kafka", "--deps"], /--deps belongs to 'type'/],
  ];
  for (const [argv, expected] of cases) {
    const streams = capture();
    assert.equal(await run(argv, streams, { fetch: NEVER }), 2, argv.join(" "));
    assert.equal(streams.stdout(), "", argv.join(" "));
    const error = failure(streams);
    assert.equal(error["kind"], "validation");
    assert.match(String(error["suggestion"]), expected, argv.join(" "));
  }
});

test("each verb still accepts the flags it does take", () => {
  for (const argv of [
    ["overview", "ballerinax/github", "--client", "Client"],
    ["ops", "ballerinax/github", "repos", "--client", "Client", "--sigs"],
    ["type", "ballerinax/github", "FullRepository", "--deps"],
    // Global flags apply everywhere.
    ["api", "ballerinax/github", "--refresh", "--project-dir", "x"],
  ]) {
    assert.equal(parseArgs(argv)?.ok, true, argv.join(" "));
  }
});

// ---------------------------------------------------------------------------
// Verb dispatch
// ---------------------------------------------------------------------------

test("a package on its own is the overview, and the overview is Markdown", async () => {
  const streams = capture();
  const exitCode = await run(["ballerinax/kafka"], streams, { fetch: centralFor("ballerinax__kafka", "4.6.5") });
  assert.equal(exitCode, 0);
  assert.equal(streams.stderr(), "");
  assert.match(streams.stdout(), /^<!-- bal-library overview v1 -->\n# ballerinax\/kafka 4\.6\.5\n/);
  // Nothing outside a fence that could be mistaken for a declaration.
  assert.doesNotMatch(streams.stdout(), /^client class /m);
});

test("the explicit overview verb and the default are the same document", async () => {
  const bare = capture();
  const explicit = capture();
  await run(["ballerinax/kafka", "4.6.5"], bare, { fetch: centralFor("ballerinax__kafka", "4.6.5") });
  await run(["overview", "ballerinax/kafka", "4.6.5"], explicit, { fetch: centralFor("ballerinax__kafka", "4.6.5") });
  assert.equal(bare.stdout(), explicit.stdout());
});

test("api is the whole Ballerina document, under two provenance lines", async () => {
  const streams = capture();
  const exitCode = await run(["api", "ballerinax/sap"], streams, { fetch: centralFor("ballerinax__sap", "1.3.1") });
  assert.equal(exitCode, 0);
  assert.equal(streams.stderr(), "");
  assert.equal(
    streams.stdout(),
    `// Resolved: ballerinax/sap:1.3.1\n// Source: central\n${readSnapshot("ballerinax__sap")}`,
  );
});

test("type prints declarations and nothing else", async () => {
  const streams = capture();
  const exitCode = await run(["type", "ballerinax/kafka", "TopicPartition"], streams, {
    fetch: centralFor("ballerinax__kafka", "4.6.5"),
  });
  assert.equal(exitCode, 0);
  assert.match(streams.stdout(), /^\/\/ ballerinax\/kafka:4\.6\.5\n\/\/ Source: central\n/);
  assert.match(streams.stdout(), /^type TopicPartition record \{$/m);
  // The code register carries no report furniture. Note that `#` is NOT the test:
  // a leading `# ` here is a Ballerina doc comment, which is the language's own
  // syntax and is exactly what belongs in this register.
  assert.doesNotMatch(streams.stdout(), /```/, "no fences: the whole document is Ballerina");
  assert.doesNotMatch(streams.stdout(), /<!-- bal-library/, "no report format marker");
  assert.doesNotMatch(streams.stdout(), /^\| /m, "no Markdown tables");
});

test("a type name that does not resolve is exit 2 with candidates, and writes no partial document", async () => {
  const streams = capture();
  const exitCode = await run(["type", "ballerinax/kafka", "TopicPartition", "TopicPartitio"], streams, {
    fetch: centralFor("ballerinax__kafka", "4.6.5"),
  });
  assert.equal(exitCode, 2);
  // All-or-nothing: "exit 0 means stdout is complete" is what redirecting callers
  // rely on, so one bad name suppresses the good one too.
  assert.equal(streams.stdout(), "");
  const error = failure(streams);
  assert.equal(error["kind"], "symbol-not-found");
  assert.deepEqual(error["requested"], ["TopicPartitio"]);
  assert.ok((error["candidates"] as string[]).includes("TopicPartition"));
});

test("a name that differs only in case or punctuation still resolves", async () => {
  const streams = capture();
  const exitCode = await run(["type", "ballerinax/kafka", "topic_partition"], streams, {
    fetch: centralFor("ballerinax__kafka", "4.6.5"),
  });
  assert.equal(exitCode, 0);
  assert.match(streams.stdout(), /^type TopicPartition record \{$/m);
});

test("ops navigates a client's paths as Markdown", async () => {
  const streams = capture();
  const exitCode = await run(["ops", "ballerinax/googleapis.gmail"], streams, {
    fetch: centralFor("ballerinax__googleapis.gmail", "4.2.0"),
  });
  assert.equal(exitCode, 0);
  assert.match(streams.stdout(), /^<!-- bal-library ops v1 -->\n# Operations — ballerinax\/googleapis\.gmail/);
});

test("ops on a package with several resource-function clients names them instead of guessing", async () => {
  const streams = capture();
  const exitCode = await run(["ops", "ballerina/http"], streams, { fetch: centralFor("ballerina__http", "2.16.6") });
  assert.equal(exitCode, 2);
  const error = failure(streams);
  assert.equal(error["kind"], "validation");
  assert.match(String(error["suggestion"]), /--client/);
  for (const name of ["Client", "FailoverClient", "LoadBalanceClient", "StatusCodeClient"]) {
    assert.match(String(error["suggestion"]), new RegExp(name));
  }
});

test("--client resolves the ambiguity", async () => {
  const streams = capture();
  const exitCode = await run(["ops", "ballerina/http", "--client", "FailoverClient"], streams, {
    fetch: centralFor("ballerina__http", "2.16.6"),
  });
  assert.equal(exitCode, 0);
  assert.match(streams.stdout(), /`FailoverClient`/);
});

test("--client naming a client that does not exist lists the ones that do", async () => {
  const streams = capture();
  const exitCode = await run(["ops", "ballerina/http", "--client", "Nope"], streams, {
    fetch: centralFor("ballerina__http", "2.16.6"),
  });
  assert.equal(exitCode, 2);
  assert.match(String(failure(streams)["suggestion"]), /FailoverClient/);
});

test("a package with no resource functions gets an honest empty report at exit 0", async () => {
  const streams = capture();
  const exitCode = await run(["ops", "ballerinax/kafka"], streams, {
    fetch: centralFor("ballerinax__kafka", "4.6.5"),
  });
  assert.equal(exitCode, 0);
  assert.match(streams.stdout(), /Resource functions \| none in any client/);
  assert.match(streams.stdout(), /^## Clients$/m);
});

// ---------------------------------------------------------------------------
// Version resolution
// ---------------------------------------------------------------------------

test("an unknown package exits 1 and names itself, rather than printing nothing at 0", async () => {
  const streams = capture();
  const exitCode = await run(["ballerinax/nope"], streams, {
    fetch: () => Promise.resolve(new Response(JSON.stringify([]), { status: 200 })),
    maxAttempts: 1,
    baseDelayMs: 1,
  });
  assert.equal(exitCode, 1);
  assert.equal(streams.stdout(), "");
  assert.equal(failure(streams)["kind"], "package-not-found");
});

test("a locked Dependencies.toml version outranks Central's latest", async () => {
  const projectDir = mkdtempSync(join(tmpdir(), "bal-library-"));
  writeFileSync(
    join(projectDir, "Dependencies.toml"),
    '[[package]]\norg = "ballerinax"\nname = "sap"\nversion = "1.3.1"\n',
  );
  const streams = capture();
  const exitCode = await run(["api", "ballerinax/sap", "--project-dir", projectDir], streams, {
    fetch: (url) => {
      // Reaching the versions endpoint would mean the lock was ignored.
      assert.match(url, /\/docs\//);
      return Promise.resolve(new Response(JSON.stringify(loadRawFixture("ballerinax__sap")), { status: 200 }));
    },
  });
  assert.equal(exitCode, 0);
  assert.match(streams.stdout(), /^\/\/ Resolved: ballerinax\/sap:1\.3\.1\n/);
});

test("--project-dir applies to every verb, because after a build it is the only correct version", () => {
  for (const argv of [
    ["ballerinax/github", "--project-dir", "stars-api"],
    ["ops", "ballerinax/github", "repos", "--project-dir", "stars-api"],
    ["type", "ballerinax/github", "FullRepository", "--project-dir", "stars-api"],
    ["api", "ballerinax/github", "--project-dir", "stars-api"],
  ]) {
    const parsed = parseArgs(argv);
    assert.equal(parsed?.ok, true, argv.join(" "));
    if (parsed?.ok) assert.equal(parsed.value.options.projectDir, "stars-api");
  }
});

test("a package with no guide still renders every other section", async () => {
  const stripped = structuredClone(loadRawFixture("ballerinax__sap")) as {
    docsData: { modules: { description?: string }[] };
  };
  for (const module of stripped.docsData.modules) delete module.description;

  const streams = capture();
  const exitCode = await run(["ballerinax/sap", "1.3.1"], streams, {
    fetch: () => Promise.resolve(new Response(JSON.stringify(stripped), { status: 200 })),
  });
  assert.equal(exitCode, 0);
  assert.match(streams.stdout(), /^## Guide$/m);
  assert.match(streams.stdout(), /publishes no guide/);
  assert.match(streams.stdout(), /^## Client `Client`$/m, "the API half is unaffected");
});
