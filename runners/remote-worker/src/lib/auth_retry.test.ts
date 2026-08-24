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
import http from "node:http";
import { FatalAuthError, fetchWith401Retry, staticTokenSource } from "./auth_retry.js";
import { ClientCredentialsTokenProvider } from "./oauth.js";

function listen(handler: http.RequestListener): Promise<{ url: string; close: () => Promise<void> }> {
  const server = http.createServer(handler);
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const addr = server.address();
      if (!addr || typeof addr === "string") {
        reject(new Error("bind failed"));
        return;
      }
      resolve({
        url: `http://127.0.0.1:${addr.port}`,
        close: () =>
          new Promise<void>((res, rej) => {
            server.close((err) => (err ? rej(err) : res()));
          }),
      });
    });
  });
}

test("fetchWith401Retry: 200 on first try does not remint", async () => {
  let hits = 0;
  const upstream = await listen((_req, res) => {
    hits++;
    res.writeHead(200).end("ok");
  });
  try {
    const tokens = ["tok-1"];
    const source = {
      getToken: async () => tokens[0]!,
      invalidate: () => {
        throw new Error("invalidate must not run on 200");
      },
    };
    const res = await fetchWith401Retry(upstream.url, { method: "GET" }, { source, canRefresh: true });
    assert.equal(res.status, 200);
    assert.equal(hits, 1);
  } finally {
    await upstream.close();
  }
});

test("fetchWith401Retry: 401 then remint then 200", async () => {
  const seen: string[] = [];
  const upstream = await listen((req, res) => {
    const auth = req.headers.authorization ?? "";
    seen.push(auth);
    if (auth === "Bearer stale") {
      res.writeHead(401).end("no");
      return;
    }
    res.writeHead(200).end("ok");
  });
  try {
    let token = "stale";
    const source = {
      getToken: async () => token,
      invalidate: () => {
        token = "fresh";
      },
    };
    const res = await fetchWith401Retry(
      upstream.url,
      { method: "POST", body: "{}" },
      { source, canRefresh: true },
    );
    assert.equal(res.status, 200);
    assert.deepEqual(seen, ["Bearer stale", "Bearer fresh"]);
  } finally {
    await upstream.close();
  }
});

test("fetchWith401Retry: 401 without refresh is fatal", async () => {
  const upstream = await listen((_req, res) => {
    res.writeHead(401).end("no");
  });
  try {
    await assert.rejects(
      () =>
        fetchWith401Retry(upstream.url, { method: "GET" }, { source: staticTokenSource("snap"), canRefresh: false }),
      (err: unknown) => err instanceof FatalAuthError && /no token refresh/.test(err.message),
    );
  } finally {
    await upstream.close();
  }
});

test("fetchWith401Retry: second 401 after refresh is fatal", async () => {
  const upstream = await listen((_req, res) => {
    res.writeHead(401).end("no");
  });
  try {
    let n = 0;
    const source = {
      getToken: async () => `t-${++n}`,
      invalidate: () => {
        /* next getToken issues t-2 */
      },
    };
    await assert.rejects(
      () => fetchWith401Retry(upstream.url, { method: "GET" }, { source, canRefresh: true }),
      (err: unknown) => err instanceof FatalAuthError && /after token refresh/.test(err.message),
    );
  } finally {
    await upstream.close();
  }
});

test("fetchWith401Retry: remint failure is fatal", async () => {
  const upstream = await listen((_req, res) => {
    res.writeHead(401).end("no");
  });
  try {
    let calls = 0;
    const mixed = {
      getToken: async () => {
        calls++;
        if (calls === 1) return "stale";
        throw new Error("thunder down");
      },
      invalidate: () => {
        /* next getToken throws */
      },
    };
    await assert.rejects(
      () => fetchWith401Retry(upstream.url, { method: "GET" }, { source: mixed, canRefresh: true }),
      (err: unknown) => err instanceof FatalAuthError && /token refresh failed: thunder down/.test(err.message),
    );
  } finally {
    await upstream.close();
  }
});

test("fetchWith401Retry: first getToken failure is fatal", async () => {
  const source = {
    getToken: async () => {
      throw new Error("thunder down");
    },
    invalidate: () => {
      throw new Error("invalidate must not run");
    },
  };
  await assert.rejects(
    () =>
      fetchWith401Retry("http://127.0.0.1:1/", { method: "GET" }, { source, canRefresh: true, fetchImpl: async () => {
        throw new Error("fetch must not run");
      } }),
    (err: unknown) => err instanceof FatalAuthError && /token mint failed: thunder down/.test(err.message),
  );
});

test("ClientCredentialsTokenProvider: invalidate forces a remint", async () => {
  let n = 0;
  const idp = await listen((_req, res) => {
    n++;
    res.writeHead(200, { "content-type": "application/json" }).end(
      JSON.stringify({ access_token: `tok-${n}`, expires_in: 3600 }),
    );
  });
  try {
    const p = new ClientCredentialsTokenProvider({
      tokenUrl: idp.url,
      clientId: "id",
      clientSecret: "sec",
    });
    assert.equal(await p.getToken(), "tok-1");
    assert.equal(await p.getToken(), "tok-1");
    p.invalidate();
    assert.equal(await p.getToken(), "tok-2");
  } finally {
    await idp.close();
  }
});
