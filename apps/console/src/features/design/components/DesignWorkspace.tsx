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
import { Box, Button, Skeleton, Typography } from "@wso2/oxygen-ui";
import { EmptyState } from "../../../components/EmptyState";
import { BuildButton } from "../../builds/components/BuildButton";
import { MakePrototypeButton } from "../../prototype/components/MakePrototypeButton";
import { PHONE } from "../../shell/layout";
import { useSpecWorkspace } from "../../spec/useSpecWorkspace";
import { useDesignModel } from "../useDesignModel";
import { blockingDependencies, openArtifact } from "../model/artifacts";
import { openComments } from "../model/comments";
import { useDepthFilter } from "../useDepthFilter";
import { useDesignTurns } from "../useDesignTurns";
import { ArtifactPicker, ArtifactRail, type RailProps } from "./ArtifactRail";
import { ArtifactView } from "./ArtifactView";
import { DependencyView } from "./DependencyView";

function names(ids: string[], name: (id: string) => string): string {
  const list = ids.map(name);
  return list.length <= 1 ? (list[0] ?? "") : `${list.slice(0, -1).join(", ")} and ${list.at(-1)}`;
}

function Note({ children }: { children: ReactNode }) {
  return (
    <Box
      sx={{
        border: 1,
        borderStyle: "dashed",
        borderColor: "divider",
        borderRadius: 2.5,
        px: 2,
        py: 1.75,
        maxWidth: "64ch",
        display: "flex",
        flexDirection: "column",
        alignItems: "flex-start",
        gap: 1.25,
        fontSize: "0.875rem",
      }}
    >
      {children}
    </Box>
  );
}

/**
 * The design card's header actions: Address comments, the design turn when
 * there is something to design, Make prototype once the design has a web
 * application, and Build once something is designed.
 */
export function DesignActions({ projectName }: { projectName: string }) {
  const design = useDesignModel(projectName).data;
  const { workspace } = useSpecWorkspace(projectName);
  const turns = useDesignTurns(projectName);
  if (!design || !workspace) return null;
  const open = openComments(design.comments).length;
  const label = workspace.design.label;
  const running = design.running !== null;
  return (
    <>
      {open > 0 && (
        <Button
          size="small"
          variant="contained"
          color="info"
          disabled={!turns.ready || running}
          onClick={() => turns.addressComments(open)}
        >
          {design.running?.kind === "address" ? "Addressing comments…" : `Address comments · ${open}`}
        </Button>
      )}
      {label && design.revision > 0 && (
        <Button size="small" variant="outlined" disabled={!turns.ready || running} onClick={() => turns.design()}>
          {design.running?.kind === "design" ? "Designing…" : label}
        </Button>
      )}
      <MakePrototypeButton projectName={projectName} />
      <BuildButton projectName={projectName} size="small" />
    </>
  );
}

/**
 * The design card's body: the design review. Before anything is designed it
 * says what design takes and offers it; while the design turn runs it points
 * to the chat; then it is the artifact list and the open artifact (`?art=`),
 * or the dependency that blocks a feature.
 */
export function DesignWorkspace({ projectName, art }: { projectName: string; art?: string | undefined }) {
  const design = useDesignModel(projectName);
  const { model: spec, workspace } = useSpecWorkspace(projectName);
  const [filter, setFilter] = useDepthFilter();
  const turns = useDesignTurns(projectName);

  if (design.isError || spec.isError) {
    const retry = () => void (design.isError ? design.refetch() : spec.refetch());
    return (
      <Box sx={{ p: 3.5 }}>
        <EmptyState
          title="Couldn't open the design"
          description={(design.error ?? spec.error)?.message ?? ""}
          action={
            <Button variant="outlined" onClick={retry}>
              Try again
            </Button>
          }
        />
      </Box>
    );
  }

  const scrollSx = { flex: 1, minWidth: 0, overflowY: "auto", px: 3.5, pt: 3, pb: 11, [PHONE]: { px: 2, pb: 17.5 } } as const;
  if (!design.data || !spec.data || !workspace) {
    return (
      <Box sx={scrollSx} aria-busy>
        <Skeleton width={180} />
        <Skeleton variant="text" sx={{ fontSize: "1.5rem" }} width={260} />
        <Skeleton variant="rounded" height={160} sx={{ mt: 2 }} />
      </Box>
    );
  }

  const model = design.data;
  const features = spec.data.features;
  const featureName = (id: string) => features.find((f) => f.id === id)?.name ?? id;
  const label = workspace.design.label;

  if (model.artifacts.length === 0) {
    return (
      <Box sx={scrollSx}>
        <Note>
          {model.running?.kind === "design" ? (
            <Typography variant="body2">
              Designing {names(model.running.features, featureName)}. Follow along in the chat.
            </Typography>
          ) : (
            <>
              <Typography variant="body2">
                Nothing is designed yet. Design takes every interviewed feature, including lines still marked assumed.
              </Typography>
              {label ? (
                <Button size="small" variant="contained" disabled={!turns.ready} onClick={() => turns.design()}>
                  {label}
                </Button>
              ) : (
                <Typography variant="body2" color="text.secondary">
                  Interview a feature first.
                </Typography>
              )}
            </>
          )}
        </Note>
      </Box>
    );
  }

  const blocking = blockingDependencies(model.dependencies);
  const open = openArtifact(model.artifacts, model.dependencies, art);
  const current = open ? (open.kind === "artifact" ? open.artifact.id : art ?? null) : null;
  const railProps: RailProps = {
    projectName,
    artifacts: model.artifacts,
    blocking,
    comments: model.comments,
    revision: model.revision,
    outOfDate: workspace.design.outOfDate,
    current,
    filter,
    onFilter: setFilter,
    featureName,
  };

  return (
    <Box sx={{ display: "flex", height: "100%", minHeight: 0 }}>
      <ArtifactRail {...railProps} />
      <Box sx={scrollSx}>
        <ArtifactPicker {...railProps} />
        {model.running && (
          <Typography variant="body2" color="primary.main" sx={{ mb: 2 }}>
            {model.running.kind === "design"
              ? `Updating the design for ${names(model.running.features, featureName)}. Follow along in the chat.`
              : "Working through the open comments. Follow along in the chat."}
          </Typography>
        )}
        {open?.kind === "dependency" ? (
          <DependencyView projectName={projectName} dependency={open.dependency} featureName={featureName(open.dependency.featureId)} />
        ) : open ? (
          <ArtifactView
            key={open.artifact.id}
            projectName={projectName}
            artifact={open.artifact}
            design={model}
            features={features}
            outOfDate={workspace.design.outOfDate}
          />
        ) : null}
      </Box>
    </Box>
  );
}
