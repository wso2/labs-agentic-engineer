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

import { describe, expect, it } from "vitest";
import {
  agentModelEnv,
  defaultJudgeProvider,
  judgeEnv,
  judgeProvider,
  resolveConnection,
  type ModelConnection,
} from "../src/connection.js";

const OLLAMA_ENV = {
  AEP_EVAL_MODEL_API_KEY: "ollama-key-value",
  AEP_EVAL_MODEL_FORMAT: "openai-compatible",
  AEP_EVAL_MODEL_BASE_URL: "https://ollama.com/v1",
  AEP_EVAL_MODEL_NAME: "gpt-oss:20b",
  AEP_EVAL_MODEL_AUTH_SCHEME: "bearer",
};

const OLLAMA: ModelConnection = {
  format: "openai-compatible",
  baseURL: "https://ollama.com/v1",
  model: "gpt-oss:20b",
  authScheme: "bearer",
  key: "ollama-key-value",
};

const FIRST_PARTY: ModelConnection = {
  format: "anthropic",
  baseURL: "https://api.anthropic.com/v1",
  model: "claude-sonnet-5-5",
  authScheme: "x-api-key",
  key: "sk-ant-local",
};

describe("resolveConnection", () => {
  it("reads the connection the platform mounted", () => {
    expect(resolveConnection({ ...OLLAMA_ENV, AEP_EVAL_KEY_MANAGED: "1" })).toEqual(OLLAMA);
  });

  // In a build pod the key never arrives as ANTHROPIC_API_KEY: that name
  // belongs to Claude Code, which ranks it above CLAUDE_CODE_OAUTH_TOKEN.
  it("prefers the evaluation key over ANTHROPIC_API_KEY", () => {
    expect(resolveConnection({ ...OLLAMA_ENV, ANTHROPIC_API_KEY: "sk-ant-other" })?.key).toBe("ollama-key-value");
  });

  it("takes Anthropic's API for any part of the connection that is absent", () => {
    expect(resolveConnection({ AEP_EVAL_MODEL_API_KEY: "k-eval" })).toEqual({ ...FIRST_PARTY, key: "k-eval" });
  });

  // A developer's machine: ANTHROPIC_API_KEY is the only key there is, and it
  // goes to Anthropic's API — never to a URL some other variable names.
  it("falls back to ANTHROPIC_API_KEY on Anthropic's API when not managed", () => {
    expect(
      resolveConnection({
        ANTHROPIC_API_KEY: "sk-ant-local",
        AEP_EVAL_MODEL_BASE_URL: "https://elsewhere.example/v1",
        AEP_EVAL_MODEL_FORMAT: "openai-compatible",
      }),
    ).toEqual(FIRST_PARTY);
  });

  // The pod's ANTHROPIC_API_KEY is the CODING credential, which an org may
  // have ring-fenced for coding and nothing else.
  it("does not fall back to the pod's coding credential", () => {
    expect(resolveConnection({ AEP_EVAL_KEY_MANAGED: "1", ANTHROPIC_API_KEY: "sk-ant-coding" })).toBeUndefined();
  });

  it("never falls back to the coding agent's OAuth token", () => {
    expect(resolveConnection({ CLAUDE_CODE_OAUTH_TOKEN: "coding-token" })).toBeUndefined();
  });

  // ESO can materialise an empty secret; "" is no key.
  it("treats an empty key as no key", () => {
    expect(resolveConnection({ ANTHROPIC_API_KEY: "" })).toBeUndefined();
    expect(resolveConnection({ AEP_EVAL_MODEL_API_KEY: "", ANTHROPIC_API_KEY: "sk-ant-local" })).toEqual(FIRST_PARTY);
  });

  it("refuses a format or auth scheme it does not know", () => {
    expect(() => resolveConnection({ ...OLLAMA_ENV, AEP_EVAL_MODEL_FORMAT: "gemini" })).toThrow(/AEP_EVAL_MODEL_FORMAT/);
    expect(() => resolveConnection({ ...OLLAMA_ENV, AEP_EVAL_MODEL_AUTH_SCHEME: "basic" })).toThrow(
      /AEP_EVAL_MODEL_AUTH_SCHEME/,
    );
  });
});

describe("agentModelEnv", () => {
  it("is the variable set a deployed ai-agent is given, without the governed-proxy header", () => {
    expect(agentModelEnv(OLLAMA)).toEqual({
      MODEL_API_KEY: "ollama-key-value",
      MODEL_ENDPOINT: "https://ollama.com/v1",
      MODEL_NAME: "gpt-oss:20b",
      MODEL_API_FORMAT: "openai-compatible",
      MODEL_API_AUTH_SCHEME: "bearer",
    });
  });
});

describe("judgeProvider", () => {
  it("grades on Chat Completions for an OpenAI-compatible connection, colon and all", () => {
    expect(judgeProvider(OLLAMA)).toEqual({
      id: "openai:chat:gpt-oss:20b",
      config: { apiBaseUrl: "https://ollama.com/v1" },
    });
  });

  // @anthropic-ai/sdk posts to <root>/v1/messages; the connection stores <root>/v1.
  it("gives the Anthropic provider the root, not the /v1 base", () => {
    expect(judgeProvider(FIRST_PARTY)).toEqual({
      id: "anthropic:messages:claude-sonnet-5-5",
      config: { apiBaseUrl: "https://api.anthropic.com" },
    });
    expect(defaultJudgeProvider()).toEqual(judgeProvider(FIRST_PARTY));
  });

  it("removes x-api-key per request on the Anthropic format with Bearer", () => {
    const judge = judgeProvider({ ...FIRST_PARTY, baseURL: "https://gw.example/anthropic/v1/", authScheme: "bearer" });
    expect(judge.config).toEqual({ apiBaseUrl: "https://gw.example/anthropic", headers: { "x-api-key": null } });
  });

  // promptfoo's anthropic factory keeps only the third ':' field of the id.
  it("refuses an Anthropic-format model whose name promptfoo would truncate", () => {
    expect(() => judgeProvider({ ...FIRST_PARTY, model: "gpt-oss:20b" })).toThrow(/AGENT_EVAL_GRADER/);
  });

  it("never carries the key", () => {
    for (const conn of [OLLAMA, FIRST_PARTY, { ...FIRST_PARTY, authScheme: "bearer" as const }]) {
      expect(JSON.stringify(judgeProvider(conn))).not.toContain(conn.key);
    }
  });
});

describe("judgeEnv", () => {
  it("presents the key only under the format's own provider variable", () => {
    expect(judgeEnv(OLLAMA)).toEqual({ OPENAI_API_KEY: "ollama-key-value" });
    expect(judgeEnv(FIRST_PARTY)).toEqual({ ANTHROPIC_API_KEY: "sk-ant-local" });
  });

  it("adds the Bearer header on the Anthropic format with Bearer", () => {
    expect(judgeEnv({ ...FIRST_PARTY, authScheme: "bearer" })).toEqual({
      ANTHROPIC_API_KEY: "sk-ant-local",
      ANTHROPIC_CUSTOM_HEADERS: "Authorization: Bearer sk-ant-local",
    });
  });
});
