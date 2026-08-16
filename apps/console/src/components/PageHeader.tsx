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

import type { ReactElement, ReactNode } from "react";
import { PageTitle, Stack } from "@wso2/oxygen-ui";
import { StatusChip, type StatusTone } from "./StatusChip";

export interface PageHeaderStatus {
  label: string;
  /** Accessible name, when the label hedges with a mark no screen reader says. */
  spokenLabel?: string;
  tone: StatusTone;
  variant?: "filled" | "outlined";
}

export interface PageHeaderBackTo {
  /** A pre-built router link element, e.g. `<Link to="/alerts" />` — kept a
   * plain element (not a route string) so this component stays router-
   * agnostic and every route's typed `to`/`params` still type-check at the
   * call site. */
  link: ReactElement<{ children?: ReactNode }>;
  label: string;
}

// Console-wide page header primitive (Task 5): every view composes the same
// title row — an optional back link, the title with an optional status
// chip beside it, an optional subtitle, and optional right-aligned actions —
// on top of Oxygen's own `PageTitle` compound component (the sample app's
// page-heading precedent), so the app has exactly one header pattern and one
// back-link style instead of the three that grew independently per page.
// Page-specific content that isn't part of the title row itself (the project
// identity block on Overview, say) renders as an ordinary sibling
// immediately below `PageHeader`, not through a prop here.
export interface PageHeaderProps {
  title: ReactNode;
  subtitle?: ReactNode;
  status?: PageHeaderStatus;
  backTo?: PageHeaderBackTo;
  actions?: ReactNode;
}

export function PageHeader({
  title,
  subtitle,
  status,
  backTo,
  actions,
}: PageHeaderProps) {
  return (
    <PageTitle>
      {backTo && (
        <PageTitle.BackButton component={backTo.link}>
          {backTo.label}
        </PageTitle.BackButton>
      )}
      <PageTitle.Header>
        {status ? (
          <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }}>
            <span>{title}</span>
            {/* Soft + dot: a status beside a large title reads as a live
                status indicator ("● Running"), not a solid button-like pill. */}
            <StatusChip
              label={status.label}
              tone={status.tone}
              appearance="soft"
              dot
              {...(status.spokenLabel ? { spokenLabel: status.spokenLabel } : {})}
              {...(status.variant ? { variant: status.variant } : {})}
            />
          </Stack>
        ) : (
          title
        )}
      </PageTitle.Header>
      {subtitle && <PageTitle.SubHeader>{subtitle}</PageTitle.SubHeader>}
      {actions && <PageTitle.Actions>{actions}</PageTitle.Actions>}
    </PageTitle>
  );
}
