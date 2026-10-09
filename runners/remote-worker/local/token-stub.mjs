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

// Local-dev platform stub. Stands in for the platform so the runner can be
// exercised without aep-api: it plays the publisher token endpoint
// (POST /oauth2/token) and, for a validation run, the validation-context
// endpoint, which the runner PREFLIGHTS before starting the agent (deployed
// endpoints are never in the issue). Without it the local validation run exits
// before the agent starts. Git needs no stub: the runner authenticates with the
// GITHUB_TOKEN run-local.sh passes through, as a dispatched Job does.
//
//   GET /internal/v1/runs/{cycleId}/validation-context
//     -> { endpoints:[{component,url}] }
//
// The default endpoints are localhost placeholders. The validation-task skill
// judges an app that is already running and starts nothing, so set
// VALIDATION_CONTEXT_JSON to an app reachable from the container. Test logins
// are not here: the skill reads them off the roles gate ticket, as in a
// dispatched run.
//
// SECURITY: keep the bind address on loopback (the default) and never expose
// this beyond your machine. When STUB_BEARER is set (run-local.sh always sets it
// to the per-run AEP_BEARER), callers must present that exact
// `Authorization: Bearer` value — the runner already sends it on every
// validation-context call, so this costs nothing and de-fangs a non-loopback bind.

import http from "node:http";

const port = Number(process.env.STUB_PORT || 8377);
if (!Number.isInteger(port) || port < 1 || port > 65535) {
  console.error(`[token-stub] STUB_PORT is not a valid port: ${process.env.STUB_PORT}`);
  process.exit(1);
}
const bind = process.env.STUB_BIND || "127.0.0.1";
const expectedBearer = process.env.STUB_BEARER ?? "";
const stubClientId = process.env.STUB_CLIENT_ID ?? "local-publisher";
const stubClientSecret = process.env.STUB_CLIENT_SECRET ?? "local-publisher-secret";

// Runner callbacks live under runs/ and are keyed by the CYCLE id the runner
// carries (AEP_TASK_ID).
const VALIDATION_CONTEXT_RE = /^\/internal\/v1\/runs\/([^/]+)\/validation-context$/;
const JSON_HEADERS = { "Content-Type": "application/json", "Cache-Control": "no-store" };

// The validation-context payload the stub returns: localhost placeholders unless
// VALIDATION_CONTEXT_JSON names a running app.
const validationContext = process.env.VALIDATION_CONTEXT_JSON
  ? JSON.parse(process.env.VALIDATION_CONTEXT_JSON)
  : {
      endpoints: [
        { component: "hello-web", url: "http://localhost:5173" },
        { component: "hello-api", url: "http://localhost:9090" },
      ],
    };

const server = http.createServer((req, res) => {
  const url = new URL(req.url ?? "/", "http://localhost");
  if (req.method === "GET" && url.pathname === "/healthz") {
    res.writeHead(200).end("ok");
    return;
  }
  if (req.method === "POST" && url.pathname === "/oauth2/token") {
    const expected =
      "Basic " + Buffer.from(`${stubClientId}:${stubClientSecret}`, "utf8").toString("base64");
    if (req.headers.authorization !== expected) {
      console.error("[token-stub] 401 oauth2/token bad client credentials");
      res.writeHead(401, JSON_HEADERS).end('{"error":"invalid_client"}');
      return;
    }
    const accessToken = expectedBearer !== "" ? expectedBearer : "stub-access-token";
    console.error("[token-stub] 200 oauth2/token");
    res.writeHead(200, JSON_HEADERS);
    res.end(JSON.stringify({ access_token: accessToken, token_type: "bearer", expires_in: 3600 }));
    return;
  }
  // The one runner callback that requires the per-run bearer: validation-context
  // (GET). Everything else 404s.
  //
  // There is no validation-report callback: the report is COMMITTED to the repo
  // and the platform reads it at the validation cycle's merge commit. The stub
  // used to ack a POST the real API never implemented, which taught the runner
  // that reporting was best-effort — it is now required.
  const contextM = req.method === "GET" ? VALIDATION_CONTEXT_RE.exec(url.pathname) : null;
  if (!contextM) {
    console.error(`[token-stub] 404 ${req.method} ${url.pathname}`);
    res.writeHead(404, JSON_HEADERS).end('{"error":"not found"}');
    return;
  }
  if (expectedBearer !== "" && req.headers.authorization !== `Bearer ${expectedBearer}`) {
    console.error(`[token-stub] 401 bad or missing bearer for ${url.pathname}`);
    res.writeHead(401, JSON_HEADERS).end('{"error":"unauthorized"}');
    return;
  }
  console.error(`[token-stub] 200 validation-context for cycle ${contextM[1]}`);
  res.writeHead(200, JSON_HEADERS).end(JSON.stringify(validationContext));
});

server.on("error", (err) => {
  console.error(`[token-stub] server error: ${err.message}`);
  process.exit(1);
});

server.listen(port, bind, () => {
  console.error(`[token-stub] listening on http://${bind}:${port}`);
});
