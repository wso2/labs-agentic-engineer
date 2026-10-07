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

import type { ReactNode } from "react";
import { createLink, useNavigate } from "@tanstack/react-router";
import { Box, ButtonBase, NativeSelect, Typography } from "@wso2/oxygen-ui";
import type { DesignArtifact, DesignComment, DesignDependency } from "../api/designModel";
import { artifactMarker, DEPTH_FILTERS, dependencyKey, filterArtifacts, type DepthFilter } from "../model/artifacts";
import { unresolvedOn } from "../model/comments";
import { PHONE } from "../../shell/layout";
import { soft } from "../../spec/components/Tag";
import { DepthLabel, MarkerTag } from "./ArtifactTags";
import { Segmented } from "./Segmented";

const ItemLink = createLink(ButtonBase);

function Eyebrow({ children }: { children: ReactNode }) {
  return (
    <Typography
      component="h3"
      sx={{ px: 1.25, pt: 1, pb: 0.5, fontSize: "0.6875rem", letterSpacing: "0.08em", textTransform: "uppercase", fontWeight: 600, color: "text.secondary" }}
    >
      {children}
    </Typography>
  );
}

export interface RailProps {
  projectName: string;
  artifacts: DesignArtifact[];
  blocking: DesignDependency[];
  comments: DesignComment[];
  revision: number;
  outOfDate: string[];
  /** The open artifact's or dependency's key. */
  current: string | null;
  filter: DepthFilter;
  onFilter: (next: DepthFilter) => void;
  featureName: (id: string) => string;
}

function BlockItem({ projectName, dependency, current, featureName }: { projectName: string; dependency: DesignDependency; current: string | null; featureName: string }) {
  const key = dependencyKey(dependency.featureId);
  const selected = key === current;
  return (
    <ItemLink
      to="/projects/$projectName/design"
      params={{ projectName }}
      search={{ art: key }}
      aria-current={selected ? "page" : undefined}
      sx={{
        width: "100%",
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-start",
        textAlign: "start",
        gap: 0.25,
        px: 1.25,
        py: 1,
        borderRadius: 2,
        border: 1,
        borderColor: "warning.main",
        bgcolor: soft("warning"),
        boxShadow: selected ? "0 0 0 2px var(--oxygen-palette-warning-main)" : "none",
      }}
    >
      <Box component="b" sx={{ fontSize: "0.8125rem", fontWeight: 600 }}>
        {featureName} waits on {dependency.needs}
      </Box>
      <Box component="span" sx={{ fontSize: "0.72rem", color: "warning.main" }}>
        blocks {featureName} only
      </Box>
    </ItemLink>
  );
}

function ArtifactItem({
  projectName,
  artifact,
  selected,
  marker,
  comments,
}: {
  projectName: string;
  artifact: DesignArtifact;
  selected: boolean;
  marker: ReturnType<typeof artifactMarker>;
  comments: number;
}) {
  return (
    <ItemLink
      to="/projects/$projectName/design"
      params={{ projectName }}
      search={{ art: artifact.id }}
      aria-current={selected ? "page" : undefined}
      sx={{
        width: "100%",
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-start",
        textAlign: "start",
        gap: 0.375,
        px: 1.25,
        py: 1,
        borderRadius: 2,
        bgcolor: selected ? soft("primary") : "transparent",
        "&:hover": { bgcolor: selected ? soft("primary") : "action.hover" },
      }}
    >
      <Box component="span" sx={{ fontSize: "0.8125rem", fontWeight: 500, lineHeight: 1.35, color: selected ? "primary.main" : "text.primary" }}>
        {artifact.title}
      </Box>
      <Box component="span" sx={{ display: "flex", flexWrap: "wrap", alignItems: "baseline", columnGap: 1, rowGap: 0.25, fontSize: "0.6875rem", color: "text.secondary" }}>
        <DepthLabel depth={artifact.depth} />
        <Box component="span" sx={{ fontFamily: "monospace" }}>
          {artifact.features.join(" ")}
        </Box>
        {marker && <MarkerTag marker={marker} />}
        {comments > 0 && (
          <Box component="span" sx={{ color: "info.main", fontWeight: 600 }}>
            {comments} comment{comments === 1 ? "" : "s"}
          </Box>
        )}
      </Box>
    </ItemLink>
  );
}

/**
 * The design's one list: what blocks a build first, then every artifact,
 * filtered All · Business · Technical, each with the features it covers, what
 * happened to it lately, and its unresolved comments.
 */
export function ArtifactRail(props: RailProps) {
  const { projectName, artifacts, blocking, comments, revision, outOfDate, current, filter, onFilter, featureName } = props;
  const { shown, hidden } = filterArtifacts(artifacts, filter);
  const other = filter === "business" ? "Technical" : "Business";
  return (
    <Box
      component="nav"
      aria-label="Design artifacts"
      sx={{
        width: 290,
        flexShrink: 0,
        borderRight: 1,
        borderColor: "divider",
        overflowY: "auto",
        px: 1,
        pt: 1.5,
        pb: 10,
        display: "flex",
        flexDirection: "column",
        gap: 0.375,
        [PHONE]: { display: "none" },
      }}
    >
      {blocking.length > 0 && <Eyebrow>Blocking a build</Eyebrow>}
      {blocking.map((d) => (
        <BlockItem key={d.featureId} projectName={projectName} dependency={d} current={current} featureName={featureName(d.featureId)} />
      ))}
      <Eyebrow>Design artifacts</Eyebrow>
      <Box sx={{ px: 0.5, pb: 0.75 }}>
        <Segmented label="Filter by depth" value={filter} options={DEPTH_FILTERS} onChange={onFilter} fill />
      </Box>
      {shown.map((a) => (
        <ArtifactItem
          key={a.id}
          projectName={projectName}
          artifact={a}
          selected={a.id === current}
          marker={artifactMarker(a, revision, outOfDate)}
          comments={unresolvedOn(comments, a.id).length}
        />
      ))}
      {hidden > 0 && (
        <Typography sx={{ fontSize: "0.75rem", color: "text.secondary", px: 1.25, py: 0.75 }}>
          {hidden} more under {other}
        </Typography>
      )}
    </Box>
  );
}

/** At phone width the rail gives way to this: the filter and a picker above the artifact. */
export function ArtifactPicker(props: RailProps) {
  const { projectName, artifacts, blocking, current, filter, onFilter, featureName } = props;
  const navigate = useNavigate();
  const { shown } = filterArtifacts(artifacts, filter);
  const options = [
    ...blocking.map((d) => ({ key: dependencyKey(d.featureId), label: `Blocking: ${featureName(d.featureId)} waits on ${d.needs}` })),
    ...shown.map((a) => ({ key: a.id, label: a.title })),
  ];
  const value = options.some((o) => o.key === current) ? current! : "";
  return (
    <Box sx={{ display: "none", flexDirection: "column", gap: 1, mb: 2.5, [PHONE]: { display: "flex" } }}>
      <Segmented label="Filter by depth" value={filter} options={DEPTH_FILTERS} onChange={onFilter} fill />
      <NativeSelect
        value={value}
        onChange={(e) => void navigate({ to: "/projects/$projectName/design", params: { projectName }, search: { art: e.target.value } })}
        inputProps={{ "aria-label": "Open artifact" }}
        sx={{ width: "100%" }}
      >
        {value === "" && <option value="">Pick an artifact</option>}
        {options.map((o) => (
          <option key={o.key} value={o.key}>
            {o.label}
          </option>
        ))}
      </NativeSelect>
    </Box>
  );
}
