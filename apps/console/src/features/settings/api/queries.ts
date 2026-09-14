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
import { configKeys, resourceKeys, skillsKeys } from "./keys";
import { apiErrorMessage } from "../../../api/errors";

type ConfigProjection = components["schemas"]["ConfigProjection"];
type CreateSkillInput = components["schemas"]["CreateSkillInput"];
type UpdateSkillInput = components["schemas"]["UpdateSkillInput"];

function errorMessage(error: unknown, fallback: string): string {
  return apiErrorMessage(error, fallback);
}

// --- Org config: GitHub + Anthropic (+ IDP, read-only here — out of scope
// for this feature, issue #96) --------------------------------------------

// `enabled` withholds the request for a caller holding neither
// ae:github-config nor ae:model-config — the BFF now requires one of the two
// to answer this at all (redacting whichever section the caller can't see),
// so a caller with neither would otherwise always land on a 403.
export function useConfig(enabled = true) {
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
    enabled,
  });
}

// The permission-free sibling of useConfig: just the two connectivity
// booleans, with none of ConfigProjection's identity/key detail — what
// OnboardingGate/OnboardingWizard need, and all they need, to decide whether
// the org is bootstrapped. Safe to call before the caller's AE permissions
// are necessarily provisioned (the very case useConfig can no longer cover).
export function useConfigStatus() {
  return useQuery({
    queryKey: configKeys.status,
    queryFn: async () => {
      const { data, error } = await client.GET("/config/status");
      if (error) {
        throw new Error(errorMessage(error, "Failed to load configuration status"));
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
      // The status query is separate from (and permission-free unlike)
      // configKeys.all, so a write here doesn't update it by itself —
      // invalidated rather than derived from `data`, since `data.llm` may
      // itself be redacted to null for a caller without ae:model-config.
      void queryClient.invalidateQueries({ queryKey: configKeys.status });
    },
  });
}

// The coding-agent key is an OVERRIDE on the key above, so these two mutations
// are "set/rotate the override" and "remove it", NOT connect/disconnect: with no
// override the coding agent reuses the org's default key, which is the default
// state and the one a fresh org is already in. Removing it therefore breaks
// nothing — it only changes which key coding runs bill.
export function useConnectCodingAnthropic() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (apiKey: string) => {
      const { data, error } = await client.PATCH("/config", {
        body: { codingLlm: { kind: "anthropic", apiKey } },
      });
      if (error) {
        throw new Error(
          errorMessage(error, "Failed to save the coding agent key"),
        );
      }
      return data;
    },
    onSuccess: (data: ConfigProjection) => {
      queryClient.setQueryData(configKeys.all, data);
      void queryClient.invalidateQueries({ queryKey: configKeys.status });
    },
  });
}

// codingLlm:null removes the override — the coding agent goes back to reusing
// the org's default key. Idempotent server-side.
export function useRemoveCodingAnthropic() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { data, error } = await client.PATCH("/config", {
        body: { codingLlm: null },
      });
      if (error) {
        throw new Error(
          errorMessage(error, "Failed to remove the coding agent key"),
        );
      }
      return data;
    },
    onSuccess: (data: ConfigProjection) => {
      queryClient.setQueryData(configKeys.all, data);
      void queryClient.invalidateQueries({ queryKey: configKeys.status });
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
      void queryClient.invalidateQueries({ queryKey: configKeys.status });
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
      void queryClient.invalidateQueries({ queryKey: configKeys.status });
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
      void queryClient.invalidateQueries({ queryKey: configKeys.status });
    },
  });
}

// --- Skills catalogue (repo-backed — see reconcile.go; no DB table) -------

// Returns the whole envelope: `skills` plus `repoUrl` (the org skills repo
// backing the catalogue — powers the Import dialog's via-PR guidance link).
// `enabled` lets a caller withhold the request entirely (e.g. the caller
// lacks ae:skill-view) rather than let it fire and land on the generic
// isError branch.
export function useSkills(enabled = true) {
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
    enabled,
  });
}

// `enabled` withholds the request for a caller without ae:skill-config — the
// BFF gates this endpoint on that permission alone (update badges are a
// config-adjacent concern, not a plain-view one), so a view-only caller would
// otherwise always land on a 403.
export function useSkillUpdates(enabled = true) {
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
    enabled,
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

// Non-destructive availability toggle (ADR-0014): withholds the skill from
// the platform's agents without touching its content, so the list and the
// detail view (opened via View) must both reflect the new state.
export function useSetSkillEnabled() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ name, enabled }: { name: string; enabled: boolean }) => {
      const { data, error } = await client.PATCH("/skills/{name}", {
        params: { path: { name } },
        body: { enabled },
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to update the skill"));
      }
      return data;
    },
    onSuccess: (_data, { name }) => {
      void queryClient.invalidateQueries({ queryKey: skillsKeys.lists() });
      void queryClient.invalidateQueries({ queryKey: skillsKeys.detail(name) });
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

// All-or-nothing: the BE's sync-skills takes no body and reconciles every
// embedded skill in one commit (`Reconcile`). There is no per-skill selection.
export function useSyncSkills() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { data, error } = await client.POST("/skills/sync", {});
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

// GET but modeled as a mutation (issue #743): it's an idempotent side-effecting
// check, not cached list data, and the onboarding wizard fires it on demand
// (auto on mount, then manually on Retry) rather than reading it reactively.
export function useEnsureAuthzRole() {
  return useMutation({
    mutationFn: async () => {
      const { data, error } = await client.GET("/authz/ensure");
      if (error) {
        throw new Error(errorMessage(error, "Failed to configure workspace"));
      }
      return data;
    },
  });
}

// --- Resources (org-settings "Resources" tabs: platform-provisioned types +
// the external-resource catalog) ------------------------------------------

// `enabled` defaults true: this hook is shared by OverviewDependencies (a
// project page, gated on ae:requirement-view) and ResourcesCatalog (the org
// Resources page, gated on ae:resource-view/ae:resource-config) — only the
// latter ever needs to withhold the request.
export function usePlatformResourceTypes(enabled = true) {
  return useQuery({
    queryKey: resourceKeys.platformTypes,
    queryFn: async () => {
      const { data, error } = await client.GET("/dependencies/platform-resource-types");
      if (error) {
        throw new Error(errorMessage(error, "Failed to load platform resource types"));
      }
      return data;
    },
    staleTime: 30_000,
    enabled,
  });
}

// `enabled` defaults true — shared by OverviewDependencies, DeploymentsPage,
// RegisterFormPage (edit-mode prefill), and ResourcesCatalog; same reasoning
// as usePlatformResourceTypes above.
export function useExternalResources(enabled = true) {
  return useQuery({
    queryKey: resourceKeys.external,
    queryFn: async () => {
      const { data, error } = await client.GET("/dependencies/external-resources");
      if (error) {
        throw new Error(errorMessage(error, "Failed to load external resources"));
      }
      return data;
    },
    staleTime: 30_000,
    enabled,
  });
}

export function useDeleteExternalResource() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (name: string) => {
      const { error } = await client.DELETE("/dependencies/external-resources/{name}", {
        params: { path: { name } },
      });
      if (error) {
        throw new Error(errorMessage(error, "Failed to delete the external resource"));
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: resourceKeys.external });
    },
  });
}
