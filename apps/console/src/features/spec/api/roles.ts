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
 * The Security panel's LIVE half: what the platform actually created on the
 * identity provider, as opposed to what the design declares.
 *
 * The design half comes from the collab room (`rolesDesign.ts`), so the two are
 * read from different places on purpose — the room shows an edit the moment it
 * is made, and this shows the world as it was at the last Build. Rendering them
 * side by side is what makes "new at Build" and "already there" legible.
 *
 * Reveal and rotate are POSTs and their answers are never cached: a password is
 * a deliberate, momentary disclosure, not a value a query client should hold and
 * re-serve.
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";
import type { components } from "../../../generated/aep-api";

export type ProjectRoleState = components["schemas"]["ProjectRoleState"];
export type ProjectTestUserState =
  components["schemas"]["ProjectTestUserState"];
export type TestUserPassword = components["schemas"]["TestUserPassword"];

/** The panel's live state, with the nullable wire arrays normalised away. */
export interface ProjectRolesLiveState {
  directoryAvailable: boolean;
  roles: ProjectRoleState[];
  testUsers: ProjectTestUserState[];
}

export const rolesKeys = {
  all: (projectName: string) => ["roles", projectName] as const,
};

function toError(error: unknown, fallback: string): Error {
  return new Error(apiErrorMessage(error, fallback));
}

export function useProjectRoles(projectName: string, enabled: boolean) {
  return useQuery({
    queryKey: rolesKeys.all(projectName),
    enabled,
    queryFn: async (): Promise<ProjectRolesLiveState> => {
      const { data, error } = await client.GET(
        "/projects/{projectName}/roles",
        {
          params: { path: { projectName } },
        },
      );
      if (error) throw toError(error, "Failed to load roles and test users");
      return {
        directoryAvailable: data?.directoryAvailable ?? false,
        roles: data?.roles ?? [],
        testUsers: data?.testUsers ?? [],
      };
    },
    // Short, not Infinity: a Build in another tab changes this, and the panel
    // showing "will be created" for an account that now exists is the one
    // stale read a user would actually notice.
    staleTime: 15_000,
  });
}

export function useRevealTestUserPassword(projectName: string) {
  return useMutation({
    mutationFn: async (username: string): Promise<TestUserPassword> => {
      const { data, error } = await client.POST(
        "/projects/{projectName}/roles/test-users/{username}/reveal",
        { params: { path: { projectName, username } } },
      );
      if (error) throw toError(error, "Failed to reveal the password");
      return data;
    },
    // Deliberately no cache write: the revealed password lives in the
    // component's own state for as long as it is on screen, and nowhere else.
    gcTime: 0,
  });
}

export function useRotateTestUserPassword(projectName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (username: string): Promise<TestUserPassword> => {
      const { data, error } = await client.POST(
        "/projects/{projectName}/roles/test-users/{username}/rotate",
        { params: { path: { projectName, username } } },
      );
      if (error) throw toError(error, "Failed to rotate the password");
      return data;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: rolesKeys.all(projectName) });
    },
    gcTime: 0,
  });
}

export function useDeleteTestUser(projectName: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (username: string) => {
      const { error } = await client.DELETE(
        "/projects/{projectName}/roles/test-users/{username}",
        { params: { path: { projectName, username } } },
      );
      if (error) throw toError(error, "Failed to delete the test user");
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: rolesKeys.all(projectName) });
    },
  });
}
