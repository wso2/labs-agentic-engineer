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

import { useCallback, useMemo, useState } from "react";
import { Alert, Box, Typography } from "@wso2/oxygen-ui";
import { createLink } from "@tanstack/react-router";
import { ButtonBase } from "@wso2/oxygen-ui";
import {
  usePinComment,
  useReplyToComment,
  useResolveComment,
  type ArtifactKind,
  type CommentAnchor,
  type DesignArtifact,
  type DesignModel,
} from "../api/designModel";
import { artifactMarker } from "../model/artifacts";
import { unresolvedOn } from "../model/comments";
import type { SpecFeature } from "../../spec/api/specModel";
import { CommentSurface, type Pin } from "./CommentSurface";
import { CommentsPanel } from "./CommentsPanel";
import { MarkerTag, DepthLabel } from "./ArtifactTags";
import { Segmented } from "./Segmented";
import { TeachingBox } from "./TeachingBox";
import { FlowView } from "./viewers/FlowView";
import { AcceptanceArtifact, ArchitectureArtifact, ContractArtifact, PrototypeArtifact } from "./viewers/PackagedViewers";
import { DataModelView, RolesView, SecurityView } from "./viewers/RecordViews";
import { DocumentView } from "./viewers/DocumentView";

const FeatureLink = createLink(ButtonBase);

/**
 * Artifacts the user works in (clicks through, pans, expands) take a
 * Use it · Comment switch; on the rest, a click anywhere pins a comment.
 */
const INTERACTIVE: ReadonlySet<ArtifactKind> = new Set(["prototype", "flow", "architecture", "contract", "acceptance"]);

type Mode = "use" | "comment";

const MODES: { value: Mode; label: string }[] = [
  { value: "use", label: "Use it" },
  { value: "comment", label: "Comment" },
];

function ArtifactBody({
  artifact,
  projectName,
  onScreen,
}: {
  artifact: DesignArtifact;
  projectName: string;
  onScreen: (screen: string) => void;
}) {
  const { source } = artifact;
  switch (source.kind) {
    case "prototype":
      return <PrototypeArtifact source={source} onScreen={onScreen} />;
    case "flow":
      return <FlowView projectName={projectName} lanes={source.lanes} steps={source.steps} />;
    case "roles":
      return <RolesView source={source} />;
    case "data":
      return <DataModelView source={source} />;
    case "architecture":
      return <ArchitectureArtifact source={source} layoutKey={`${projectName}:design`} />;
    case "contract":
      return <ContractArtifact source={source} />;
    case "security":
      return <SecurityView source={source} />;
    case "acceptance":
      return <AcceptanceArtifact source={source} />;
    case "document":
      return <DocumentView markdown={source.markdown} />;
  }
}

/**
 * One artifact, open: what it is and which features it covers, how feedback
 * works (until the user has done it once), the artifact itself with its
 * comments pinned over it, and the comments under it. Keyed by the artifact,
 * so moving to another starts in Use it with nothing half-written.
 */
export function ArtifactView({
  projectName,
  artifact,
  design,
  features,
  outOfDate,
}: {
  projectName: string;
  artifact: DesignArtifact;
  design: DesignModel;
  features: SpecFeature[];
  outOfDate: string[];
}) {
  const interactive = INTERACTIVE.has(artifact.source.kind);
  const [mode, setMode] = useState<Mode>(interactive ? "use" : "comment");
  const [screen, setScreen] = useState<string | null>(null);
  const [draft, setDraft] = useState<Omit<CommentAnchor, "view"> | null>(null);
  const pin = usePinComment(projectName);
  const resolve = useResolveComment(projectName);
  const reply = useReplyToComment(projectName);
  const onScreen = useCallback((next: string) => setScreen(next), []);

  const view = artifact.source.kind === "prototype" ? screen : null;
  const comments = useMemo(() => design.comments.filter((c) => c.artifactId === artifact.id), [design.comments, artifact.id]);
  const pins = useMemo<Pin[]>(
    () => [
      ...unresolvedOn(comments, artifact.id)
        .filter((c) => c.anchor.view === view)
        .map((c) => ({ key: c.id, n: c.n, status: c.status, anchor: c.anchor, title: `${c.anchor.label}: ${c.text}` })),
      ...(draft ? [{ key: "draft", n: null, status: "draft" as const, anchor: draft, title: draft.label }] : []),
    ],
    [comments, artifact.id, view, draft],
  );
  // The comments whose element is gone from the artifact as it is drawn now.
  const [gone, setGone] = useState<ReadonlySet<string>>(() => new Set());
  const onGone = useCallback(
    (keys: string[]) =>
      setGone((prev) => (keys.length === prev.size && keys.every((k) => prev.has(k)) ? prev : new Set(keys))),
    [],
  );
  const marker = artifactMarker(artifact, design.revision, outOfDate);
  const busy = pin.isPending || resolve.isPending || reply.isPending;
  const failure = pin.error ?? resolve.error ?? reply.error;

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.75, minWidth: 0 }}>
      <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
        <Box sx={{ display: "flex", gap: 1, alignItems: "baseline" }}>
          <DepthLabel depth={artifact.depth} />
          {marker && <MarkerTag marker={marker} />}
        </Box>
        <Typography component="h1" sx={{ fontSize: "1.375rem", fontWeight: 600, letterSpacing: "-0.01em", lineHeight: 1.3 }}>
          {artifact.title}
        </Typography>
        <Box sx={{ display: "flex", gap: 0.75, flexWrap: "wrap" }} aria-label="Features it covers">
          {artifact.features.map((id) => (
            <FeatureLink
              key={id}
              to="/projects/$projectName/spec"
              params={{ projectName }}
              search={{ file: id }}
              sx={{ fontSize: "0.75rem", border: 1, borderColor: "divider", borderRadius: 1.5, px: 0.875, lineHeight: 1.7, "&:hover": { bgcolor: "action.hover" } }}
            >
              <Box component="span" sx={{ fontFamily: "monospace", mr: 0.5 }}>
                {id}
              </Box>
              {features.find((f) => f.id === id)?.name ?? ""}
            </FeatureLink>
          ))}
        </Box>
      </Box>
      {design.commenting && <TeachingBox taught={design.taught} />}
      {design.commenting && (
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.25, flexWrap: "wrap" }}>
        {interactive ? (
          <Segmented
            label="Mode"
            value={mode}
            options={MODES}
            onChange={(next) => {
              setMode(next);
              setDraft(null);
            }}
          />
        ) : null}
        <Typography variant="caption" color="text.secondary">
          {mode === "comment" ? "Click anything to pin a comment" : "Switch to Comment to pin one"}
        </Typography>
      </Box>
      )}
      <CommentSurface
        commenting={design.commenting && mode === "comment"}
        pins={pins}
        fallbackLabel={view ? `the ${view} screen` : artifact.title}
        onPick={setDraft}
        onGone={onGone}
      >
        <ArtifactBody artifact={artifact} projectName={projectName} onScreen={onScreen} />
      </CommentSurface>
      {failure && <Alert severity="error">{failure.message}</Alert>}
      {design.commenting && (
      <CommentsPanel
        comments={comments}
        gone={gone}
        draft={draft}
        busy={busy}
        onPin={(text) =>
          draft &&
          pin.mutate(
            { artifactId: artifact.id, anchor: { ...draft, view }, text },
            { onSuccess: () => setDraft(null) },
          )
        }
        onCancelDraft={() => setDraft(null)}
        onResolve={(id) => resolve.mutate(id)}
        onReply={(id, text) => reply.mutate({ commentId: id, text })}
      />
      )}
    </Box>
  );
}
