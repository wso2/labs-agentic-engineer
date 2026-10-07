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
import { Box, ButtonBase, Typography } from "@wso2/oxygen-ui";
import { useStartInterview } from "../../agent-chat/useStartInterview";
import { useBuildAction } from "../../builds/buildPicker";
import { nextUpBuild } from "../../builds/model/picker";
import { useDesignTurns } from "../../design/useDesignTurns";
import { PHONE } from "../../shell/layout";
import { withBuild, type FeatureView, type NextUpItem } from "../model/workspace";

const ItemLink = createLink(ButtonBase);

const itemSx = {
  width: "100%",
  display: "grid",
  gridTemplateColumns: "22px 1fr auto",
  columnGap: 1,
  alignItems: "baseline",
  justifyItems: "start",
  textAlign: "start",
  px: 0.75,
  py: 0.625,
  borderRadius: 1.5,
  fontSize: "0.875rem",
  "&:hover": { bgcolor: "action.hover" },
  [PHONE]: { gridTemplateColumns: "22px 1fr", "& > :last-child": { gridColumn: 2 } },
} as const;

function Item({
  item,
  n,
  projectName,
  onTurn,
}: {
  item: NextUpItem;
  n: number;
  projectName: string;
  /** Start this item's turn (an interview, the design) or open the build picker; null when it is not one or cannot now. */
  onTurn: (() => void) | null;
}) {
  const body = (
    <>
      <Box component="span" sx={{ fontFamily: "monospace", fontSize: "0.75rem", color: "text.secondary" }}>
        {n}
      </Box>
      <span>{item.label}</span>
      <Box component="span" sx={{ fontSize: "0.75rem", color: item.urgent ? "warning.main" : "text.secondary" }}>
        {item.why}
      </Box>
    </>
  );
  const { target } = item;
  // An interview or the design starts where it is offered: a turn in the
  // chat, beside the feature or the design card.
  if (item.kind === "interview" || item.kind === "design" || target.card === "builds") {
    return (
      <ButtonBase sx={itemSx} disabled={!onTurn} onClick={onTurn ?? undefined}>
        {body}
      </ButtonBase>
    );
  }
  return target.card === "design" ? (
    <ItemLink to="/projects/$projectName/design" params={{ projectName }} sx={itemSx}>
      {body}
    </ItemLink>
  ) : (
    <ItemLink
      to="/projects/$projectName/spec"
      params={{ projectName }}
      search={{ file: target.file, ...(target.at ? { at: target.at } : {}) }}
      sx={itemSx}
    >
      {body}
    </ItemLink>
  );
}

/** What to do next, worked out from the documents; each item goes where the work is. */
export function NextUp({
  items,
  features,
  projectName,
}: {
  items: NextUpItem[];
  features: FeatureView[];
  projectName: string;
}) {
  const interview = useStartInterview(projectName);
  const design = useDesignTurns(projectName);
  const build = useBuildAction(projectName);
  const all = withBuild(items, nextUpBuild(build.offer));
  if (all.length === 0) return null;
  const onTurn = (item: NextUpItem) => {
    if (item.kind === "build") return build.open;
    if (item.kind === "design") return design.ready ? () => design.design() : null;
    if (item.kind !== "interview") return null;
    const { target } = item;
    const feature = target.card === "spec" ? features.find((f) => f.id === target.file) : undefined;
    return feature && interview.ready ? () => interview.start(feature) : null;
  };
  return (
    <Box
      component="section"
      aria-labelledby="next-up-heading"
      sx={{
        border: 1,
        borderColor: "divider",
        borderRadius: 2.5,
        px: 1.75,
        py: 1.5,
        mb: 2.75,
        maxWidth: "72ch",
        display: "flex",
        flexDirection: "column",
        gap: 1,
      }}
    >
      <Box sx={{ display: "flex", alignItems: "baseline", gap: 1.25 }}>
        <Typography
          id="next-up-heading"
          component="h2"
          sx={{ fontSize: "0.6875rem", letterSpacing: "0.08em", textTransform: "uppercase", fontWeight: 600, color: "text.secondary" }}
        >
          Next up
        </Typography>
        <Typography variant="caption" color="text.secondary">
          worked out from the documents
        </Typography>
      </Box>
      <Box component="ol" sx={{ listStyle: "none", m: 0, p: 0, display: "flex", flexDirection: "column", gap: 0.25 }}>
        {all.map((item, i) => (
          <li key={`${item.kind}:${item.label}`}>
            <Item item={item} n={i + 1} projectName={projectName} onTurn={onTurn(item)} />
          </li>
        ))}
      </Box>
    </Box>
  );
}
