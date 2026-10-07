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

import { createLink, useRouterState } from "@tanstack/react-router";
import { Box, Button } from "@wso2/oxygen-ui";
import { BasePage } from "../features/shell/components/BasePage";
import { EmptyState } from "./EmptyState";

const ButtonLink = createLink(Button);

/** The project an unknown address names, if it names one (`/projects/acme-expenses/…`). */
export function projectNamed(pathname: string): string | null {
  const m = /^\/projects\/([^/]+)/.exec(pathname);
  return m && m[1] !== "new" ? decodeURIComponent(m[1]!) : null;
}

/**
 * What an address the console does not have shows: a Page that says so and
 * offers a way on. Old console addresses are not redirected; the way on is the
 * Dashboard, and the project's overview when the address names a project.
 */
export function NotFoundPage() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const project = projectNamed(pathname);
  return (
    <BasePage>
      <EmptyState
        title="This page doesn't exist"
        description={`Nothing lives at ${pathname}. It may be an address from the old console.`}
        action={
          <Box sx={{ display: "flex", gap: 1, justifyContent: "center" }}>
            {project && (
              <ButtonLink to="/projects/$projectName" params={{ projectName: project }} variant="contained">
                Open {project}
              </ButtonLink>
            )}
            <ButtonLink to="/" variant={project ? "text" : "contained"}>
              Dashboard
            </ButtonLink>
          </Box>
        }
      />
    </BasePage>
  );
}
