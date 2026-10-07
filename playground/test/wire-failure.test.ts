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
 * Whose failure a bring-up was. Every input here is the shape docker actually
 * printed, captured from compose 5.1 / BuildKit against Dockerfiles broken on
 * purpose (a failing RUN, a COPY of a missing file, an unparseable line, a
 * base image on a host that does not resolve) and a compose project with one
 * port held by another container — so no docker is needed to run these.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  classifyBuild,
  classifyDevServer,
  classifyUp,
  failedLine,
  parseInspect,
  WIRE_EXIT,
  type ContainerFacts,
} from "../src/engine/wire/failure.js";
import { renderBuildRecord } from "../src/engine/wire/docker.js";
import { run } from "../src/engine/wire/runtime.js";

const b64 = (text: string): string => Buffer.from(text).toString("base64");
const lines = (...records: unknown[]): string => records.map((r) => JSON.stringify(r)).join("\n");

const FROM = "sha256:from";
const RUN = "sha256:run";

// --- build ------------------------------------------------------------------

test("build: a Dockerfile step that fails is the app's, with the step's own output", () => {
  const output = lines(
    { statuses: [{ id: "sha256:layer", vertex: FROM, name: "downloading", current: 0 }] },
    { vertexes: [{ digest: FROM, name: "[1/2] FROM docker.io/library/alpine:3.20", completed: "2026-10-04T10:40:09.44Z" }] },
    { vertexes: [{ digest: RUN, inputs: [FROM], name: "[2/2] RUN npm ci", started: "2026-10-04T10:40:09.45Z" }] },
    { logs: [{ vertex: RUN, stream: 1, data: b64("npm ERR! missing script\n") }] },
    {
      vertexes: [
        {
          digest: RUN,
          inputs: [FROM],
          name: "[2/2] RUN npm ci",
          completed: "2026-10-04T10:40:09.81Z",
          error: 'process "/bin/sh -c npm ci" did not complete successfully: exit code: 1',
        },
      ],
    },
    { error: true, message: "Dockerfile:2\n\nfailed to solve: process did not complete successfully: exit code: 1\n" },
  );
  const failure = classifyBuild(output);
  assert.equal(failure.cause, "app");
  assert.match(failure.reason, /\[2\/2\] RUN npm ci: process .* exit code: 1/);
  assert.equal(failure.log, "npm ERR! missing script\n");
});

test("build: a COPY of a file the project does not have is the app's — the inputs arrive on the vertex's LATER record", () => {
  const output = lines(
    { vertexes: [{ digest: RUN, name: "[2/2] COPY missing.txt /x" }] },
    {
      vertexes: [
        {
          digest: RUN,
          inputs: [FROM, "sha256:context"],
          name: "[2/2] COPY missing.txt /x",
          completed: "2026-10-04T10:40:48.63Z",
          error: 'failed to calculate checksum of ref: "/missing.txt": not found',
        },
      ],
    },
  );
  assert.equal(classifyBuild(output).cause, "app");
});

test("build: a base image that cannot be fetched is the environment's", () => {
  const output = lines(
    { vertexes: [{ digest: "sha256:def", name: "[internal] load build definition from Dockerfile", completed: "2026-10-04T10:40:13.99Z" }] },
    {
      vertexes: [
        {
          digest: "sha256:meta",
          name: "[internal] load metadata for registry.invalid.example/nope:1",
          started: "2026-10-04T10:40:14.00Z",
          completed: "2026-10-04T10:40:14.03Z",
          error: "failed to do request: dial tcp: lookup registry.invalid.example: no such host",
        },
      ],
    },
    { error: true, message: "failed to solve: registry.invalid.example/nope:1: failed to resolve source metadata\n" },
  );
  const failure = classifyBuild(output);
  assert.equal(failure.cause, "environment");
  assert.match(failure.reason, /load metadata for registry\.invalid\.example\/nope:1: .*no such host/);
});

test("build: a Dockerfile BuildKit refuses to parse has no failing vertex, and is the app's", () => {
  const output = lines(
    { vertexes: [{ digest: "sha256:def", name: "[internal] load build definition from Dockerfile", completed: "2026-10-04T10:40:13Z" }] },
    { error: true, message: "Dockerfile:2\n\n   2 | >>> BOGUS thing\n\nfailed to solve: dockerfile parse error on line 2: unknown instruction: BOGUS\n" },
  );
  const failure = classifyBuild(output);
  assert.equal(failure.cause, "app");
  assert.match(failure.reason, /unknown instruction: BOGUS/);
});

test("build: the FIRST failure is the cause — another service's pull cancelled after it is not", () => {
  const output = lines(
    {
      vertexes: [
        {
          digest: "sha256:other-pull",
          name: "[other 1/3] FROM docker.io/library/node:22",
          completed: "2026-10-04T10:40:12Z",
          error: "context canceled",
        },
      ],
    },
    { vertexes: [{ digest: RUN, inputs: [FROM], name: "[api 3/3] RUN bal build", completed: "2026-10-04T10:40:10Z", error: "exit code: 1" }] },
  );
  const failure = classifyBuild(output);
  assert.equal(failure.cause, "app");
  assert.match(failure.reason, /RUN bal build/);
});

test("build: prose compose prints beside the records is ignored, not fatal", () => {
  assert.equal(classifyBuild("not json\n{torn\n").cause, "app");
});

test("build log: a step's start, its output and its error read as text; download progress is dropped", () => {
  assert.deepEqual(renderBuildRecord(JSON.stringify({ statuses: [{ id: "x", current: 1 }] })), []);
  assert.deepEqual(renderBuildRecord(JSON.stringify({ vertexes: [{ name: "[1/2] RUN x", started: "t" }] })), ["# [1/2] RUN x"]);
  assert.deepEqual(renderBuildRecord(JSON.stringify({ logs: [{ data: b64("a\nb\n") }] })), ["a", "b"]);
  assert.deepEqual(renderBuildRecord(JSON.stringify({ vertexes: [{ name: "[1/2] RUN x", error: "boom" }] })), ["✗ [1/2] RUN x: boom"]);
  assert.deepEqual(renderBuildRecord("plain text"), ["plain text"]);
});

// --- up ---------------------------------------------------------------------

/** `docker inspect` of the three containers a port clash and a crash left behind. */
const INSPECT = JSON.stringify([
  {
    Name: "/p-crash-1",
    Config: { Labels: { "com.docker.compose.service": "crash" } },
    State: { Status: "exited", StartedAt: "2026-10-04T10:44:45.07Z", ExitCode: 7, Error: "" },
  },
  {
    Name: "/p-api-1",
    Config: { Labels: { "com.docker.compose.service": "api" } },
    State: {
      Status: "created",
      StartedAt: "0001-01-01T00:00:00Z",
      ExitCode: 128,
      Error: "driver failed programming external connectivity on endpoint p-api-1: Bind for 0.0.0.0:19090 failed: port is already allocated",
    },
  },
  {
    Name: "/p-db-1",
    Config: { Labels: { "com.docker.compose.service": "db" } },
    State: { Status: "running", StartedAt: "2026-10-04T10:44:44.93Z", ExitCode: 0, Error: "", Health: { Status: "healthy" } },
  },
]);

test("inspect: docker's zero StartedAt means never started; the compose service names each container", () => {
  const facts = parseInspect(INSPECT);
  assert.deepEqual(
    facts.map((c) => [c.service, c.status, c.startedAt === null, c.exitCode, c.health]),
    [
      ["crash", "exited", false, 7, ""],
      ["api", "created", true, 128, ""],
      ["db", "running", false, 0, "healthy"],
    ],
  );
  assert.deepEqual(parseInspect("Error: no such object"), []);
});

test("up: a container the daemon could not start (a port someone else holds) is the environment's, and outranks a crash", () => {
  const failure = classifyUp(parseInspect(INSPECT), new Set(["api", "crash"]));
  assert.equal(failure.cause, "environment");
  assert.equal(failure.service, "api");
  assert.match(failure.reason, /port is already allocated/);
});

const started = (service: string, extra: Partial<ContainerFacts>): ContainerFacts => ({
  service,
  status: "running",
  startedAt: "2026-10-04T10:00:00Z",
  exitCode: 0,
  error: "",
  health: "healthy",
  ...extra,
});

test("up: the project's own service that exits or never turns healthy is the app's", () => {
  const exited = classifyUp([started("db", {}), started("api", { status: "exited", exitCode: 1, health: "" })], new Set(["api"]));
  assert.deepEqual([exited.cause, exited.service], ["app", "api"]);
  assert.match(exited.reason, /exited with code 1/);

  const unhealthy = classifyUp([started("api", { health: "unhealthy" })], new Set(["api"]));
  assert.equal(unhealthy.cause, "app");
  assert.match(unhealthy.reason, /never turned healthy/);
});

test("up: a container wire supplies (the database) dying is the environment's", () => {
  const failure = classifyUp([started("db", { status: "exited", exitCode: 1 })], new Set(["api"]));
  assert.deepEqual([failure.cause, failure.service], ["environment", "db"]);
});

test("up: no container to point at is the environment's", () => {
  assert.equal(classifyUp([], new Set(["api"])).cause, "environment");
  // A dependent compose never started (waiting on a healthy dependency) is a consequence, not a cause.
  assert.equal(classifyUp([started("api", { status: "created", startedAt: null })], new Set(["api"])).cause, "environment");
});

// --- the dev server ---------------------------------------------------------

test("dev server: exiting on its own is the app's; exiting off a port someone holds, or never answering, is not", () => {
  const url = "http://localhost:5173";
  assert.equal(classifyDevServer({ exitCode: 1, portHeldByOther: false, url }).cause, "app");
  assert.equal(classifyDevServer({ exitCode: 1, portHeldByOther: true, url }).cause, "environment");
  assert.equal(classifyDevServer({ exitCode: null, portHeldByOther: false, url }).cause, "environment");
});

// --- the contract -----------------------------------------------------------

test("contract: one line per failure, the reason folded onto it; a distinct exit status per cause", () => {
  assert.equal(failedLine({ cause: "app", reason: "api started and\n  exited with code 1" }), "FAILED app api started and ⏎ exited with code 1");
  assert.equal(failedLine({ cause: "environment", reason: "" }), "FAILED environment (no reason given)");
  assert.notEqual(WIRE_EXIT.app, WIRE_EXIT.environment);
  // 0 is success, 1 an unclassified failure, 2 a command `play` does not know.
  assert.ok(![0, 1, 2].includes(WIRE_EXIT.app) && ![0, 1, 2].includes(WIRE_EXIT.environment));
});

// --- the line plumbing the build classifier rides on ------------------------

test("run: a line written in pieces arrives whole, in the output and to onLine", async () => {
  const seen: string[] = [];
  const script = `process.stdout.write('{"a":'); setTimeout(() => process.stdout.write('1}\\nnext'), 50);`;
  const result = await run(process.execPath, ["-e", script], { capture: true, onLine: (line) => seen.push(line) });
  assert.equal(result.code, 0);
  assert.deepEqual(seen, ['{"a":1}', "next"]);
  assert.equal(result.output, '{"a":1}\nnext\n');
});
