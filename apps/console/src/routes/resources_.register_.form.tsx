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

import { createFileRoute, useRouterState } from "@tanstack/react-router";
import { RegisterFormPage } from "../features/marketplace/components/RegisterFormPage";
import { decodePromoteTarget } from "../features/marketplace/lib/promoteTarget";

export const Route = createFileRoute("/resources_/register_/form")({
  validateSearch: (
    search: Record<string, unknown>,
  ): { name?: string; promote?: string } => {
    const next: { name?: string; promote?: string } = {};
    if (typeof search.name === "string") next.name = search.name;
    // The project's own resource the organization takes over (see promoteTarget.ts).
    if (decodePromoteTarget(search.promote)) next.promote = search.promote as string;
    return next;
  },
  component: RegisterFormRoute,
});

function registerPromptOf(state: unknown): string {
  if (typeof state !== "object" || state === null) return "";
  const prompt = (state as { registerPrompt?: unknown }).registerPrompt;
  return typeof prompt === "string" ? prompt : "";
}

function RegisterFormRoute() {
  const { name, promote } = Route.useSearch();
  const prompt = useRouterState({
    select: (s) => registerPromptOf(s.location.state),
  });
  const promoteTarget = decodePromoteTarget(promote);
  return (
    <RegisterFormPage
      {...(prompt ? { prompt } : {})}
      {...(name !== undefined ? { name } : {})}
      {...(promoteTarget ? { promote: promoteTarget } : {})}
    />
  );
}
