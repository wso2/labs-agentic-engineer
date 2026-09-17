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

import { Button, PageContent } from "@wso2/oxygen-ui";
import { ArrowLeft, Lock } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "./EmptyState";

// The full-page block for a caller lacking view access to a whole section
// (Spec/Design, Builds, Deployments, …) — as opposed to disabling individual
// controls for a caller who can view but not edit. One shared shape so every
// section's denial reads the same way.
//
// No chip row naming what's restricted: the description already says so in
// prose, and repeating it as a row of tags underneath was redundant clutter
// nobody reads twice — dropped from every caller, not just hidden behind an
// unused prop.
export function PermissionRestrictedPage({
  title,
  description,
  backLabel,
  onBack,
}: {
  title: string;
  description: string;
  backLabel: string;
  onBack: () => void;
}) {
  return (
    <PageContent>
      <EmptyState
        icon={<Lock size={48} />}
        title={title}
        description={description}
        action={
          <Button variant="contained" startIcon={<ArrowLeft size={18} />} onClick={onBack}>
            {backLabel}
          </Button>
        }
      />
    </PageContent>
  );
}
