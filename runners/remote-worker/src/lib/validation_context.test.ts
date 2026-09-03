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

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
  fetchValidationContext,
  validationContextUrl,
  VALIDATION_CONTEXT_FILE,
} from "./validation_context.js";

const CYCLE = "9d90f001-67bb-4c51-a5f3-7fd808c06c36";
const BEARER = "task-jwt";

async function tmpFile(): Promise<string> {
  const dir = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-valctx-"));
  return path.join(dir, "validation-context.json");
}

/** A fetch stub that records the request it was handed. */
function stubFetch(status: number, body: string) {
  const calls: { url: string; auth: string }[] = [];
  const impl = (async (url: string | URL | Request, init?: RequestInit) => {
    const headers = new Headers(init?.headers);
    calls.push({ url: String(url), auth: headers.get("Authorization") ?? "" });
    return new Response(body, { status });
  }) as unknown as typeof fetch;
  return { impl, calls };
}

// The path is keyed by the CYCLE the runner was dispatched for. It used to be
// /internal/v1/executions/{id}/validation-context, resolved against a table the
// milestone supervisor never writes — a 404 on every validation run.
test("validationContextUrl targets the cycle-scoped validation path", () => {
  assert.equal(
    validationContextUrl("https://bff.example", CYCLE),
    `https://bff.example/internal/v1/validation/${CYCLE}/context`,
  );
  // A trailing slash on AEP_PLATFORM_URL must not double up.
  assert.equal(
    validationContextUrl("https://bff.example/", CYCLE),
    `https://bff.example/internal/v1/validation/${CYCLE}/context`,
  );
});

test("writes the platform's payload verbatim and reports its endpoints", async () => {
  const file = await tmpFile();
  const payload = JSON.stringify({
    endpoints: [{ component: "hello-webapp", url: "https://hello.example" }],
    criteriaPath: "specs/validation/validation-criteria.json",
    somethingNewer: "must survive",
  });
  const { impl, calls } = stubFetch(200, payload);

  const ctx = await fetchValidationContext({
    platformUrl: "https://bff.example",
    cycleId: CYCLE,
    bearer: BEARER,
    file,
    fetchImpl: impl,
  });

  assert.equal(ctx.endpoints.length, 1);
  assert.equal(ctx.endpoints[0]?.url, "https://hello.example");
  assert.equal(calls[0]?.auth, `Bearer ${BEARER}`);
  // Verbatim: the skill's contract is the platform's payload, so a field this
  // runner does not model still reaches it.
  assert.equal(await fs.promises.readFile(file, "utf8"), payload);
});

// The failure that mattered: the platform does not recognise the runner's cycle
// id. This must throw so the caller can exit before the agent starts, rather than
// leave an agent to work out where the app is on its own.
test("a 404 throws and writes nothing", async () => {
  const file = await tmpFile();
  const { impl } = stubFetch(404, '{"code":"not_found","message":"no validation cycle with this id"}');

  await assert.rejects(
    fetchValidationContext({
      platformUrl: "https://bff.example",
      cycleId: CYCLE,
      bearer: BEARER,
      file,
      fetchImpl: impl,
    }),
    /HTTP 404/,
  );
  assert.equal(fs.existsSync(file), false);
});

// Validation is dispatched at deployed-green, so an empty endpoint list means the
// platform cannot say where the system is. "No targets" is precisely the state
// that made the agent start probing, so it is a failure and not an empty success.
test("a context with no endpoints throws", async () => {
  const file = await tmpFile();
  const { impl } = stubFetch(200, JSON.stringify({ endpoints: [], criteriaPath: "x" }));

  await assert.rejects(
    fetchValidationContext({
      platformUrl: "https://bff.example",
      cycleId: CYCLE,
      bearer: BEARER,
      file,
      fetchImpl: impl,
    }),
    /no deployed endpoints/,
  );
  assert.equal(fs.existsSync(file), false);
});

// `null` is valid JSON, so it survives JSON.parse and reaches the endpoint check
// as an object-shaped nothing. Reading `.endpoints` off it threw a bare TypeError
// — the one failure this preflight cannot name, in the log a human reads when a
// validation run dies before the agent starts.
test("a body that parses to a non-object throws a named error, not a TypeError", async () => {
  for (const body of ["null", "5", '"a string"', "[]"]) {
    const file = await tmpFile();
    const { impl } = stubFetch(200, body);
    await assert.rejects(
      fetchValidationContext({
        platformUrl: "https://bff.example",
        cycleId: CYCLE,
        bearer: BEARER,
        file,
        fetchImpl: impl,
      }),
      (err: Error) => {
        assert.equal(err instanceof TypeError, false, `${body} produced a TypeError`);
        // The shape-specific message, not merely "some error": every one of these
        // bodies must be stopped by the top-level object guard. Accepting the
        // endpoints message too would let a primitive fall through to that later
        // check and still pass, proving nothing about the guard under test.
        assert.match(err.message, /validation context is not a JSON object/);
        return true;
      },
      `body ${body}`,
    );
    assert.equal(fs.existsSync(file), false);
  }
});

// The oracle's path is required by the internal contract, and the skill is told
// it is always there. Defaulting it to "" handed the agent a well-formed file
// that broke that promise, and an agent that cannot find the oracle goes looking.
test("a context with no criteria path throws", async () => {
  for (const criteria of [undefined, "", 42]) {
    const file = await tmpFile();
    const { impl } = stubFetch(
      200,
      JSON.stringify({
        endpoints: [{ component: "c", url: "https://x.example" }],
        ...(criteria === undefined ? {} : { criteriaPath: criteria }),
      }),
    );
    await assert.rejects(
      fetchValidationContext({
        platformUrl: "https://bff.example",
        cycleId: CYCLE,
        bearer: BEARER,
        file,
        fetchImpl: impl,
      }),
      /no validation-criteria path/,
      `criteriaPath ${JSON.stringify(criteria)}`,
    );
    assert.equal(fs.existsSync(file), false);
  }
});

// `mode` is honoured only when the write CREATES the file, so a context file left
// at this fixed path under a world-writable /tmp would otherwise keep whatever
// permissions it already had.
test("the context file ends up 0600 even when the path already exists", async () => {
  const file = await tmpFile();
  await fs.promises.writeFile(file, "stale", { mode: 0o666 });
  await fs.promises.chmod(file, 0o666);
  const { impl } = stubFetch(
    200,
    JSON.stringify({
      endpoints: [{ component: "c", url: "https://x.example" }],
      criteriaPath: "specs/validation/validation-criteria.json",
    }),
  );

  await fetchValidationContext({
    platformUrl: "https://bff.example",
    cycleId: CYCLE,
    bearer: BEARER,
    file,
    fetchImpl: impl,
  });

  assert.equal(fs.statSync(file).mode & 0o777, 0o600);
});

// The target is a fixed, predictable name under a world-writable /tmp, so it can
// be pre-created as a symlink pointing somewhere else. The write must replace the
// entry rather than follow it — otherwise the payload lands on the attacker's
// chosen file, with this process's privileges. (A property test, not an
// implementation one: an unlink-then-write also clears a symlink that is already
// sitting there. What it cannot do is close the window between the two calls.)
test("a symlink squatting on the target path is replaced, not followed", async () => {
  const file = await tmpFile();
  const decoy = path.join(path.dirname(file), "decoy.txt");
  await fs.promises.writeFile(decoy, "untouched");
  await fs.promises.symlink(decoy, file);
  const payload = JSON.stringify({
    endpoints: [{ component: "c", url: "https://x.example" }],
    criteriaPath: "specs/validation/validation-criteria.json",
  });
  const { impl } = stubFetch(200, payload);

  await fetchValidationContext({
    platformUrl: "https://bff.example",
    cycleId: CYCLE,
    bearer: BEARER,
    file,
    fetchImpl: impl,
  });

  assert.equal(fs.lstatSync(file).isSymbolicLink(), false, "the symlink survived the write");
  assert.equal(await fs.promises.readFile(file, "utf8"), payload);
  assert.equal(await fs.promises.readFile(decoy, "utf8"), "untouched");
});

// Clearing the path before writing it means a write that then fails has destroyed
// the previous context and put nothing in its place. Staging into a private
// directory and renaming makes the replacement all-or-nothing: the old file is
// only ever unlinked by the rename that replaces it.
test("a failed write leaves the previous context intact", async () => {
  const file = await tmpFile();
  const good = JSON.stringify({
    endpoints: [{ component: "c", url: "https://good.example" }],
    criteriaPath: "specs/validation/validation-criteria.json",
  });
  const first = stubFetch(200, good);
  await fetchValidationContext({
    platformUrl: "https://bff.example",
    cycleId: CYCLE,
    bearer: BEARER,
    file,
    fetchImpl: first.impl,
  });

  const realWriteFile = fs.promises.writeFile;
  const second = stubFetch(
    200,
    JSON.stringify({
      endpoints: [{ component: "c", url: "https://new.example" }],
      criteriaPath: "specs/validation/validation-criteria.json",
    }),
  );
  try {
    // Deliberately replacing the binding for this one call, restored below. The
    // only way to reach the failure branch without a real full disk.
    fs.promises.writeFile = async () => {
      throw new Error("disk full");
    };
    await assert.rejects(
      fetchValidationContext({
        platformUrl: "https://bff.example",
        cycleId: CYCLE,
        bearer: BEARER,
        file,
        fetchImpl: second.impl,
      }),
      /disk full/,
    );
  } finally {
    fs.promises.writeFile = realWriteFile;
  }

  assert.equal(await fs.promises.readFile(file, "utf8"), good);
  assert.equal(fs.statSync(file).mode & 0o777, 0o600);
});

// The staging directory is an implementation detail and must not outlive the
// call — a run that left one behind on every attempt would litter /tmp in the
// one place this code is careful about.
test("no staging directory is left behind", async () => {
  const file = await tmpFile();
  const dir = path.dirname(file);
  const { impl } = stubFetch(
    200,
    JSON.stringify({
      endpoints: [{ component: "c", url: "https://x.example" }],
      criteriaPath: "specs/validation/validation-criteria.json",
    }),
  );

  await fetchValidationContext({
    platformUrl: "https://bff.example",
    cycleId: CYCLE,
    bearer: BEARER,
    file,
    fetchImpl: impl,
  });

  assert.deepEqual(
    (await fs.promises.readdir(dir)).filter((e) => e.startsWith(".aep-valctx-")),
    [],
  );
});

test("an unset platform URL throws before any request", async () => {
  const { impl, calls } = stubFetch(200, "{}");
  await assert.rejects(
    fetchValidationContext({ platformUrl: "", cycleId: CYCLE, bearer: BEARER, fetchImpl: impl }),
    /AEP_PLATFORM_URL is unset/,
  );
  assert.equal(calls.length, 0);
});

// The default target is outside the work tree AND outside `.aep/`, which the base
// skill forbids the agent from reading — it must not have to break that rule to
// find its own targets.
test("the default context file is outside the workspace and outside .aep", () => {
  assert.equal(VALIDATION_CONTEXT_FILE.startsWith("/tmp/"), true);
  assert.equal(VALIDATION_CONTEXT_FILE.includes("/.aep/"), false);
});

test("source path remints once on 401 then writes the context", async () => {
  const file = await tmpFile();
  const payload = JSON.stringify({
    endpoints: [{ component: "hello-webapp", url: "https://hello.example" }],
    criteriaPath: "specs/validation/validation-criteria.json",
  });
  let n = 0;
  const impl = (async (_url: string | URL | Request, init?: RequestInit) => {
    n++;
    const auth = new Headers(init?.headers).get("Authorization") ?? "";
    if (auth === "Bearer stale") {
      return new Response("no", { status: 401 });
    }
    return new Response(payload, { status: 200 });
  }) as unknown as typeof fetch;
  let token = "stale";
  const ctx = await fetchValidationContext({
    platformUrl: "https://bff.example",
    cycleId: CYCLE,
    bearer: "unused",
    source: {
      getToken: async () => token,
      invalidate: () => {
        token = "fresh";
      },
    },
    canRefresh: true,
    file,
    fetchImpl: impl,
  });
  assert.equal(n, 2);
  assert.equal(ctx.endpoints.length, 1);
});
