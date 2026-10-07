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
import { ApiRequestError } from "../../../api/errors";

// The org's skill library (the org skills repo, read and written through
// aep-api; there is no table behind it). Copied from the old console's
// settings queries.

type CreateSkillInput = components["schemas"]["CreateSkillInput"];
type UpdateSkillInput = components["schemas"]["UpdateSkillInput"];
type SkillDetailBody = components["schemas"]["SkillDetailBody"];

const skillsKeys = {
  all: ["skills"] as const,
  list: () => [...skillsKeys.all, "list"] as const,
  updates: () => [...skillsKeys.all, "updates"] as const,
  detail: (name: string) => [...skillsKeys.all, "detail", name] as const,
};

/** Every skill, and the repo they live in. */
export function useSkills() {
  return useQuery({
    queryKey: skillsKeys.list(),
    queryFn: async () => {
      const { data, error } = await client.GET("/skills");
      if (error) throw new ApiRequestError(error, "Couldn't load the skills");
      return { skills: data.skills ?? [], repoUrl: data.repoUrl };
    },
    staleTime: 30_000,
  });
}

/** The platform's skills whose state differs from what the org last took. */
export function useSkillUpdates() {
  return useQuery({
    queryKey: skillsKeys.updates(),
    queryFn: async () => {
      const { data, error } = await client.GET("/skills/updates");
      if (error) throw new ApiRequestError(error, "Couldn't load the platform's skill updates");
      return data.updates ?? [];
    },
    staleTime: 30_000,
  });
}

export function useSkill(name: string, enabled = true) {
  return useQuery({
    queryKey: skillsKeys.detail(name),
    queryFn: async () => {
      const { data, error } = await client.GET("/skills/{name}", { params: { path: { name } } });
      if (error) throw new ApiRequestError(error, "Couldn't load the skill");
      return data;
    },
    enabled: enabled && name !== "",
  });
}

/** After a write: the list, the updates and the skill itself are read again. */
function useAfterWrite() {
  const queryClient = useQueryClient();
  return (saved?: SkillDetailBody) => {
    if (saved) queryClient.setQueryData(skillsKeys.detail(saved.name), saved);
    void queryClient.invalidateQueries({ queryKey: skillsKeys.list() });
    void queryClient.invalidateQueries({ queryKey: skillsKeys.updates() });
  };
}

export function useCreateSkill() {
  const afterWrite = useAfterWrite();
  return useMutation({
    mutationFn: async (body: CreateSkillInput) => {
      const { data, error } = await client.POST("/skills", { body });
      if (error) throw new ApiRequestError(error, "Couldn't create the skill");
      return data;
    },
    onSuccess: afterWrite,
  });
}

export function useUpdateSkill(name: string) {
  const afterWrite = useAfterWrite();
  return useMutation({
    mutationFn: async (body: UpdateSkillInput) => {
      const { data, error } = await client.PUT("/skills/{name}", { params: { path: { name } }, body });
      if (error) throw new ApiRequestError(error, "Couldn't save the skill");
      return data;
    },
    onSuccess: afterWrite,
  });
}

export function useDeleteSkill(name: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      const { error } = await client.DELETE("/skills/{name}", { params: { path: { name } } });
      if (error) throw new ApiRequestError(error, "Couldn't delete the skill");
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: skillsKeys.list() });
      void queryClient.invalidateQueries({ queryKey: skillsKeys.updates() });
    },
  });
}

/** Enable or disable: withholds the skill from the platform's agents without touching its content. */
export function useSetSkillEnabled(name: string) {
  const afterWrite = useAfterWrite();
  return useMutation({
    mutationFn: async (enabled: boolean) => {
      const { data, error } = await client.PATCH("/skills/{name}", { params: { path: { name } }, body: { enabled } });
      if (error) throw new ApiRequestError(error, enabled ? "Couldn't enable the skill" : "Couldn't disable the skill");
      return data;
    },
    onSuccess: afterWrite,
  });
}

/** Import an AgentSkills tarball (a gzip-compressed SKILL.md and its references). */
export function useImportSkill() {
  const afterWrite = useAfterWrite();
  return useMutation({
    mutationFn: async (file: File) => {
      const form = new FormData();
      form.append("file", file);
      // openapi-fetch passes FormData through and the browser sets the
      // multipart boundary; the declared `{ file: string }` only describes
      // the schema, hence the cast.
      const { data, error } = await client.POST("/skills/import", { body: form as unknown as { file: string } });
      if (error) throw new ApiRequestError(error, "Couldn't import the skill");
      return data;
    },
    onSuccess: () => afterWrite(),
  });
}

/**
 * Take the platform's updates: refreshes every skill the org never edited, in
 * one commit (it takes no selection), and creates the org's skills repo when
 * it is missing, which is why onboarding runs it too.
 */
export function useSyncSkills() {
  const afterWrite = useAfterWrite();
  return useMutation({
    mutationFn: async () => {
      const { data, error } = await client.POST("/skills/sync", {});
      if (error) throw new ApiRequestError(error, "Couldn't take the platform's skill updates");
      return data;
    },
    onSuccess: () => afterWrite(),
  });
}
