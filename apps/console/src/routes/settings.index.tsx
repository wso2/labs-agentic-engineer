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
import { usePermissions, type Permissions } from "../auth/permissions";
import {
  SECTIONS,
  type SettingsSectionPath,
} from "../features/settings/components/SettingsLayout";
import { SettingsBlockedPage } from "../features/settings/components/SettingsBlockedPage";

export const Route = createFileRoute("/settings/")({
  component: SettingsIndexPage,
});

// Bare /settings has no content of its own — land on the first section this
// caller actually has something to do on.
//
// Both the priority order and each section's gate are read off SettingsLayout's
// SECTIONS rather than restated here. They were stated twice before, and the
// duplicate had to say in a comment that it matched: a landing rule that
// disagreed with the sidebar would send a caller to a tab the sidebar had
// disabled, which is a redirect loop's worth of confusion for a one-line edit
// nobody made in both places.
//
// `null` means no section has anything to show at all.
//
// A pure function (not a hook) so the order is unit-testable without
// rendering — see settings.index.test.tsx.
export function resolveSettingsLandingPath(
  can: Permissions,
): SettingsSectionPath | null {
  return SECTIONS.find((section) => section.allowed(can))?.path ?? null;
}

export function SettingsIndexPage() {
  const target = resolveSettingsLandingPath(usePermissions());
  if (target) return <Navigate to={target} replace />;
  return <SettingsBlockedPage />;
}
