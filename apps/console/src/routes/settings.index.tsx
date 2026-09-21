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

import { Navigate, createFileRoute } from "@tanstack/react-router";
import { useHasAnyPermission, useHasPermission } from "../auth/permissions";
import { SettingsBlockedPage } from "../features/settings/components/SettingsBlockedPage";

export const Route = createFileRoute("/settings/")({
  component: SettingsIndexPage,
});

export type SettingsLandingPath =
  | "/settings/credentials"
  | "/settings/skills"
  | "/settings/usage";

// Bare /settings has no content of its own — land on the first section this
// caller actually has something to do on, in the same priority as the
// sidebar's tab order (SettingsLayout's SECTIONS). Skills is reachable on
// EITHER of its two permissions since it gates its own content rather than
// needing a decision here (see SkillsSection); Credentials needs either
// half (GitHub or Anthropic) since each card gates independently. `null`
// means none of the three has anything to show at all.
//
// A pure function (not a hook) so this priority order is unit-testable
// without rendering — see settings.index.test.tsx.
export function resolveSettingsLandingPath(perms: {
  credentials: boolean;
  skills: boolean;
  usage: boolean;
}): SettingsLandingPath | null {
  if (perms.credentials) return "/settings/credentials";
  if (perms.skills) return "/settings/skills";
  if (perms.usage) return "/settings/usage";
  return null;
}

export function SettingsIndexPage() {
  const credentials = useHasAnyPermission(["ae:github-config", "ae:model-config"]);
  // Exact-match ae:skill-view, NOT OR'd with ae:skill-config — matches
  // SettingsLayout's identical hasSkillsAccess gate.
  const skills = useHasPermission("ae:skill-view");
  const usage = useHasPermission("ae:usage-view");

  const target = resolveSettingsLandingPath({ credentials, skills, usage });
  if (target) return <Navigate to={target} replace />;
  return <SettingsBlockedPage />;
}
