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
 * The parts of the page the matrix does not draw: the org groups this project
 * introduces, the heading that opens the role cards, and what each wireframe
 * screen takes to reach.
 *
 * The two document sections are here rather than in the matrix because neither
 * crosses a role. A group is a directory object the roles are assigned TO, and
 * a screen's requirement is a single handle — a column would be a column of
 * one.
 */

import { Box, Stack, Typography } from "@wso2/oxygen-ui";

import type { SecurityDesign } from "../../api/securityDesign";

/**
 * Only the groups this project INTRODUCES. A role may be assigned to a group
 * the org already has; that one is named on the role, not declared here.
 */
export function GroupsBlock({ doc }: { doc: SecurityDesign }) {
  if (doc.groups.length === 0) return null;
  return (
    <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}>
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 0.5 }}>
        New org groups
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
        Groups this project adds to the organisation directory at Build. They
        are shared — other projects can assign roles to them too.
      </Typography>
      <Stack spacing={0.5}>
        {doc.groups.map((group) => (
          <Typography key={group.name} variant="body2">
            <Box component="span" sx={{ fontWeight: 600 }}>
              {group.name}
            </Box>
            {" — "}
            {group.description}
          </Typography>
        ))}
      </Stack>
    </Box>
  );
}

export function RolesIntro() {
  return (
    <Box>
      <Typography variant="h5" sx={{ mb: 0.5 }}>
        Roles &amp; users
      </Typography>
      <Typography variant="body2" color="text.secondary">
        These roles are created on the platform identity provider when you
        click Build — the same directory every project shares, so a role
        another project already uses is reused rather than duplicated.
      </Typography>
    </Box>
  );
}

/** What a caller must hold to reach each screen the wireframes declare. */
export function ScreensBlock({ doc }: { doc: SecurityDesign }) {
  if (doc.screens.length === 0) return null;
  return (
    <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, p: 2 }}>
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 0.5 }}>
        Screens
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
        What a person must hold to reach each screen.
      </Typography>
      <Stack spacing={0.5}>
        {doc.screens.map((screen) => (
          <Stack
            key={`${screen.component}:${screen.screen}`}
            direction="row"
            spacing={1}
            alignItems="center"
          >
            <Typography variant="body2">{screen.screen}</Typography>
            <Typography variant="caption" color="text.secondary">
              {screen.component}
            </Typography>
            {screen.requires === null ? (
              <Typography variant="body2" color="text.secondary">
                Any signed-in person
              </Typography>
            ) : screen.requires === "public" ? (
              <Typography variant="body2" color="text.secondary">
                Open to everyone, no sign-in
              </Typography>
            ) : (
              <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
                {screen.requires}
              </Typography>
            )}
          </Stack>
        ))}
      </Stack>
    </Box>
  );
}
