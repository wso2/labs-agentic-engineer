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

import { createLink } from "@tanstack/react-router";
import { Box, ButtonBase } from "@wso2/oxygen-ui";
import { useFailingIn } from "../../builds/hooks/useFailingIn";
import { stageTone, type FeatureView } from "../model/workspace";
import { Tag } from "./Tag";

const RowLink = createLink(ButtonBase);

/**
 * The product's features, one row each: ID, name, stage, purpose, and chips
 * for what needs the user. A row opens the feature's file in the spec card.
 * The product page draws these in place of prd.md's Features list, and the
 * project overview shows the same rows.
 */
export function FeatureRows({ projectName, features }: { projectName: string; features: FeatureView[] }) {
  const failingIn = useFailingIn(projectName);
  if (features.length === 0) return null;
  return (
    <Box
      component="ul"
      aria-label="Features"
      sx={{ listStyle: "none", m: 0, p: 0, border: 1, borderColor: "divider", borderRadius: 2.5, overflow: "hidden" }}
    >
      {features.map((f) => (
        <Box component="li" key={f.id} sx={{ "& + &": { borderTop: 1, borderColor: "divider" } }}>
          <RowLink
            to="/projects/$projectName/spec"
            params={{ projectName }}
            search={{ file: f.id }}
            sx={{
              width: "100%",
              display: "grid",
              gridTemplateColumns: "30px minmax(0, 1fr) auto",
              columnGap: 1.25,
              rowGap: 0.5,
              alignItems: "baseline",
              justifyItems: "start",
              textAlign: "start",
              px: 1.5,
              py: 1.125,
              "&:hover": { bgcolor: "action.hover" },
            }}
          >
            <Box component="span" sx={{ fontFamily: "monospace", fontSize: "0.75rem", color: "text.secondary" }}>
              {f.id}
            </Box>
            <Box component="span" sx={{ fontWeight: 600, fontSize: "0.875rem" }}>
              {f.name}
            </Box>
            <Box component="span" sx={{ justifySelf: "end" }}>
              <Tag tone={stageTone(f.stage)}>{f.stage}</Tag>
            </Box>
            <Box component="span" sx={{ gridColumn: "2 / 4", fontSize: "0.8125rem", color: "text.secondary" }}>
              {f.purpose}
            </Box>
            {(f.chips.length > 0 || failingIn.has(f.id)) && (
              <Box component="span" sx={{ gridColumn: "2 / 4", display: "inline-flex", flexWrap: "wrap", gap: 0.5 }}>
                {f.chips.map((c) => (
                  <Tag key={c.label} tone={c.tone}>
                    {c.label}
                  </Tag>
                ))}
                {failingIn.has(f.id) && <Tag tone="warning">failing in {failingIn.get(f.id)}</Tag>}
              </Box>
            )}
          </RowLink>
        </Box>
      ))}
    </Box>
  );
}
