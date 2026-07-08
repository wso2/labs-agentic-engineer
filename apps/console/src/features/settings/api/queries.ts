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

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { configKeys, skillsKeys } from "./keys";

type ConfigProjection = components["schemas"]["ConfigProjection"];
type CreateSkillInput = components["schemas"]["CreateSkillInput"];
type UpdateSkillInput = components["schemas"]["UpdateSkillInput"];

function errorMessage(error: unknown, fallback: string): string {
  const e = error as { detail?: string; title?: string } | undefined;
  return e?.detail ?? e?.title ?? fallback;
}

// --- Org config: GitHub + Anthropic (+ IDP, read-only here — out of scope
// for this feature, issue #96) --------------------------------------------

export function useConfig() {
  return useQuery({
    queryKey: configKeys.all,
    queryFn: async () => {
      const { data, error } = await client.GET("/config");
      if (error) {
        throw new Error(errorMessage(error, "Failed to load configuration"));
      }
      return data;
    },
    staleTime: 30_000,
  });
}

// Connect and replace are the same call (issue #96 decision: inline, no
// confirm — the PATCH's server-side probe-before-persist validation is the
// safety net).
export function useConnectAnthropic() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (apiKey: string) => {
      const { data, error } = await client.PATCH("/config", {
        body: { llm: { kind: "anthropic", apiKey } },
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to connect the Anthropic key"));
      }
      return data;
    },
    onSuccess: (data: ConfigProjection) => {
      queryClient.setQueryData(configKeys.all, data);
    },
  });
}

// llm:null disconnects directly (unlike gitProvider, which the BE rejects
// as null and requires the dedicated disconnect endpoint instead).
export function useDisconnectAnthropic() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { data, error } = await client.PATCH("/config", {
        body: { llm: null },
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to disconnect the Anthropic key"));
      }
      return data;
    },
    onSuccess: (data: ConfigProjection) => {
      queryClient.setQueryData(configKeys.all, data);
    },
  });
}

export function useConnectGitHubPat() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { pat: string; githubLogin?: string }) => {
      const { data, error } = await client.PATCH("/config", {
        body: {
          gitProvider: {
            kind: "github",
            mode: "pat",
            pat: input.pat,
            ...(input.githubLogin ? { githubLogin: input.githubLogin } : {}),
          },
        },
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to connect GitHub"));
      }
      return data;
    },
    onSuccess: (data: ConfigProjection) => {
      queryClient.setQueryData(configKeys.all, data);
    },
  });
}

// The cascade endpoint, not a plain patch — PATCH {gitProvider: null} is
// rejected by the BE for exactly this reason.
export function useDisconnectGitProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (uninstall: boolean) => {
      const { error } = await client.POST("/config/git-provider/disconnect", {
        params: { query: { uninstall } },
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to disconnect GitHub"));
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: configKeys.all });
    },
  });
}

// --- Skills catalogue (repo-backed — see reconcile.go; no DB table) -------

// Returns the whole envelope: `skills` plus `repoUrl` (the org skills repo
// backing the catalogue — powers the Import dialog's via-PR guidance link).
export function useSkills() {
  return useQuery({
    queryKey: skillsKeys.lists(),
    queryFn: async () => {
      const { data, error } = await client.GET("/skills");
      if (error) {
        throw new Error(errorMessage(error, "Failed to load skills"));
      }
      return { skills: data.skills ?? [], repoUrl: data.repoUrl };
    },
    staleTime: 30_000,
  });
}

export function useSkillUpdates() {
  return useQuery({
    queryKey: skillsKeys.updates(),
    queryFn: async () => {
      const { data, error } = await client.GET("/skills/updates");
      if (error) {
        throw new Error(errorMessage(error, "Failed to load skill updates"));
      }
      return data.updates ?? [];
    },
    staleTime: 30_000,
  });
}

export function useSkill(name: string) {
  return useQuery({
    queryKey: skillsKeys.detail(name),
    queryFn: async () => {
      const { data, error } = await client.GET("/skills/{name}", {
        params: { path: { name } },
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to load skill"));
      }
      return data;
    },
    enabled: name.length > 0,
  });
}

export function useCreateSkill() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: CreateSkillInput) => {
      const { data, error } = await client.POST("/skills", { body });
      if (error) {
        throw new Error(errorMessage(error, "Failed to create the skill"));
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: skillsKeys.lists() });
    },
  });
}

export function useUpdateSkill(name: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: UpdateSkillInput) => {
      const { data, error } = await client.PUT("/skills/{name}", {
        params: { path: { name } },
        body,
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to update the skill"));
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: skillsKeys.lists() });
      void queryClient.invalidateQueries({ queryKey: skillsKeys.detail(name) });
    },
  });
}

export function useDeleteSkill() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (name: string) => {
      const { error } = await client.DELETE("/skills/{name}", {
        params: { path: { name } },
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to delete the skill"));
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: skillsKeys.lists() });
    },
  });
}

export function useImportSkill() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (file: File) => {
      const formData = new FormData();
      formData.append("file", file);
      // openapi-fetch's default bodySerializer passes FormData through
      // untouched and lets the browser set the multipart boundary — the
      // declared `{file: string}` request type only describes the JSON
      // Schema shape, not the wire body, hence the cast.
      const { data, error } = await client.POST("/skills/import", {
        body: formData as unknown as { file: string },
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to import the skill"));
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: skillsKeys.lists() });
    },
  });
}

// Server-side fetch of a direct file URL (raw SKILL.md or tarball) — the
// BE fetches so the browser never hits CORS (issue #96 re-grill decision).
export function useImportSkillUrl() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (url: string) => {
      const { data, error, response } = await client.POST("/skills/import-url", {
        body: { url },
      });
      // Gate on response.ok, not just `error`: a non-ok response with an
      // empty body (e.g. the gateway's 405 before this endpoint ships, part
      // of BE handshake #100) leaves `error` undefined, which would slip
      // through an `if (error)` check and read as a silent success.
      if (error || !response.ok) {
        throw new Error(
          response.status === 405
            ? "Import by URL isn't available on this server yet."
            : errorMessage(error, "Failed to import from the URL"),
        );
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: skillsKeys.lists() });
    },
  });
}

export function useSyncSkills() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (names: string[]) => {
      const { data, error } = await client.POST("/skills/sync", {
        body: names.length > 0 ? { names } : undefined,
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to sync skills"));
      }
      return data;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: skillsKeys.lists() });
      void queryClient.invalidateQueries({ queryKey: skillsKeys.updates() });
    },
  });
}
