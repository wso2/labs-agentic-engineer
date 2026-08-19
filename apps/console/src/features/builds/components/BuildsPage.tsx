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

import { useEffect, useRef } from "react";
import {
  Alert,
  Autocomplete,
  Box,
  Button,
  CircularProgress,
  Stack,
  TextField,
  Typography,
  type TextFieldProps,
} from "@wso2/oxygen-ui";
import { Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { PageHeader } from "../../../components/PageHeader";
import { useAllTasks } from "../../tasks/api/queries";
import { taskKeys } from "../../tasks/api/keys";
import { partitionIssues } from "../../tasks/lib/issueRows";
import { useProjectStatus } from "../../projects/api/queries";
import { useBuildRuns, useBuilds } from "../api/queries";
import { openCycleClaims } from "../lib/milestoneBuckets";
import { buildCycles, isDeliveryRun, versionIsLive } from "../lib/runView";
import { EarlierSessions } from "./EarlierSessions";
import { MilestonePanel } from "./MilestonePanel";
import { RunHistoryList } from "./RunHistoryList";
import { RunStory } from "./RunStory";
import { ExternalResources } from "./ExternalResources";

/**
 * The Builds page is ONE VERSION'S STORY, latest by default.
 *
 * There is no ledger list in between: navigating here while a run is live lands
 * straight on that run, with its feed already open. Old versions are reached
 * through this page's own version picker, which writes `?tag=v<N>`.
 *
 * Two data planes, priced apart. The run rows and cycle records are DB-only, so
 * they poll at 5s while the version is moving. The issue list is GitHub-backed,
 * so it polls only while a run is live — plus exactly one fetch at settle, when
 * the run's last writes (issues closed by merge) have landed.
 */
export function BuildsPage({
  projectName,
  tag,
  onTagChange,
  connections,
}: {
  projectName: string;
  tag: string | undefined;
  onTagChange: (tag: string | undefined) => void;
  connections: string | undefined;
}) {
  const builds = useBuilds(projectName);

  // An unknown/absent ?tag falls back to the newest version (the list is
  // newest-first), so a stale shared link degrades to "latest", not a 404.
  const newest = builds.data?.[0];
  const selected = builds.data?.find((b) => b.tag === tag) ?? newest;
  const selectedTag = selected?.tag;

  const runs = useBuildRuns(projectName, selectedTag);
  const runList = runs.data?.runs ?? [];
  // Liveness is asked of EVERY run, not just the ones this page renders: a
  // revalidation in flight means the version is still moving, and the poll that
  // keeps this page fresh is keyed on the same predicate (queries.ts).
  const live = versionIsLive(runList);
  // The runs that delivered the version. A run that only re-judged it has no build
  // session to show and its verdict lives on the Validation board — leading with one
  // made this page claim nothing had been dispatched. See isDeliveryRun: the test is
  // what the run DID, so a revalidation that repaired and rebuilt still belongs here.
  const deliveryRuns = runList.filter(isDeliveryRun);
  // Newest first, and only the newest can be live — so the head is the run the
  // page leads with and the tail is history.
  const current = deliveryRuns[0];
  const earlier = deliveryRuns.slice(1);

  // This session's CYCLES. Validation is not one of them, so it is filtered
  // the same way the card filters it.
  const currentCycles = buildCycles(current?.cycles ?? []);
  const earlierSessions = currentCycles.slice(0, -1);

  const status = useProjectStatus(projectName);
  // The platform records a CLONE url, which carries a `.git` suffix — appending
  // a path to it straight off yields `…/repo.git/issues`, which 404s. Strip the
  // suffix (and any trailing slash) to get the repo's web root.
  const repoUrl = status.data?.repoUrl
    ?.replace(/\/+$/, "")
    .replace(/\.git$/, "");
  const issuesUrl = repoUrl ? `${repoUrl}/issues` : undefined;

  // Which OPEN cycle claimed an in-flight issue. `resolves` is the merge
  // policy's recorded matched set, so this is a fact rather than a guess — and
  // only the still-open cycle counts: a closed cycle's merge already closed its
  // issues, so its claims are history, not something "in flight".
  const claims = openCycleClaims(currentCycles);
  const openIndex = currentCycles.findIndex((c) => !c.endedAt);
  // Before its pull request a live session has recorded nothing, so the panel
  // PRESUMES it works the open issues — the NOW panel's own inference. The
  // note keeps the two strengths apart: "Claimed by" is the merge policy's
  // recorded set; "With" is the presumption (possession without proof),
  // exact at the PR.
  const presumeOpenWork = openIndex !== -1 && claims.size === 0;
  const claimedBy = (issue: {
    issueNumber: number;
    derivedStatus: string;
  }): string | undefined => {
    if (openIndex === -1) return undefined;
    const label = `build session ${openIndex + 1} · ${currentCycles[openIndex]?.kind ?? ""}`.trim();
    if (claims.has(issue.issueNumber)) return `Claimed by ${label}`;
    if (presumeOpenWork && issue.derivedStatus === "pending") {
      return `With ${label}`;
    }
    return undefined;
  };

  // One query feeds the milestone panel AND the run card. The card needs it because only the issue plane
  // can tell a gate hold apart from an empty working set, and undefined until
  // it lands is what stops a card accusing a run of having no work on the
  // strength of a list that has not arrived.
  const issues = useAllTasks(projectName, selectedTag, { live });
  const partition = issues.data ? partitionIssues(issues.data) : undefined;
  // Both populations WHOLE, closed members included. The run's rail is the
  // version's story, and a provisioned connection is as much a part of how a
  // version came to exist as a merged pull request — while a build session's own
  // issues are closed by the very merge that completed it, so narrowing to the
  // open ones would empty every finished session.
  const milestone = partition && {
    gates: partition.gates,
    work: partition.work,
    ledger: partition.ledger,
  };

  // One final issue fetch at settle. The GitHub-backed list stops polling the
  // moment the run turns terminal, but the writes that settle a version (the
  // merge that closes the last issue) can land in the same instant — so the
  // live→settled edge triggers exactly one more read.
  const queryClient = useQueryClient();
  const wasLive = useRef(false);
  useEffect(() => {
    if (wasLive.current && !live && selectedTag) {
      void queryClient.invalidateQueries({
        queryKey: taskKeys.list(projectName, selectedTag),
      });
    }
    wasLive.current = live;
  }, [live, projectName, selectedTag, queryClient]);

  // The header renders through every state below so the back link stays
  // reachable while builds load or fail — the pattern every adopted page uses.
  const backTo = {
    link: <Link to="/projects/$projectName" params={{ projectName }} />,
    label: "Back to Overview",
  };

  if (builds.isPending) {
    return (
      <>
        <PageHeader title="Builds" backTo={backTo} />
        <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
          <CircularProgress aria-label="Loading builds" />
        </Box>
        <ExternalResources projectName={projectName} connections={connections} />
      </>
    );
  }

  if (builds.isError) {
    return (
      <>
        <PageHeader title="Builds" backTo={backTo} />
        <Alert
          severity="error"
          action={<Button onClick={() => void builds.refetch()}>Retry</Button>}
        >
          Failed to load builds
          {builds.error instanceof Error && builds.error.message
            ? `: ${builds.error.message}`
            : ""}
        </Alert>
        <ExternalResources projectName={projectName} connections={connections} />
      </>
    );
  }

  if (!newest || !selected || !selectedTag) {
    return (
      <>
        <PageHeader title="Builds" backTo={backTo} />
        <Typography variant="body2" color="text.secondary" sx={{ py: 3 }}>
          No builds yet — publish your spec and click Build in the spec view to
          start the first one.
        </Typography>
        <ExternalResources projectName={projectName} connections={connections} />
      </>
    );
  }

  // The version picker sits at the page-header level so it reads as a
  // page-level control, and the version's story spans full width beneath it.
  const versionSelector = (
    <Autocomplete
      options={builds.data.map((b) => b.tag)}
      value={selected.tag}
      onChange={(_, value) =>
        // Selecting the newest version clears ?tag — the default view.
        onTagChange(value && value !== newest.tag ? value : undefined)
      }
      disableClearable
      size="small"
      sx={{ width: 180, flexShrink: 0 }}
      renderInput={(params) => (
        // MUI's render params don't declare `| undefined` on their optional
        // props, which exactOptionalPropertyTypes rejects — the cast is the
        // documented escape hatch for this spread.
        <TextField {...(params as TextFieldProps)} label="Version" />
      )}
    />
  );

  return (
    <>
      {/* No version subtitle: the picker on the right already names the
          version, and repeating it under the title said it twice. */}
      <PageHeader title="Builds" backTo={backTo} actions={versionSelector} />

      {runs.isError ? (
        <Alert
          severity="error"
          sx={{ mb: 3 }}
          action={<Button onClick={() => void runs.refetch()}>Retry</Button>}
        >
          Failed to load {selected.tag}'s build sessions
          {runs.error instanceof Error && runs.error.message
            ? `: ${runs.error.message}`
            : ""}
        </Alert>
      ) : runs.isPending ? (
        <Box sx={{ display: "flex", justifyContent: "center", p: 4 }}>
          <CircularProgress aria-label="Loading the version's build sessions" />
        </Box>
      ) : runList.length === 0 ? (
        <Alert severity="info" sx={{ mb: 3 }}>
          {selected.tag} has no run rows — the version was tagged before
          this platform started keeping them.
        </Alert>
      ) : (
        // Now-first: the CURRENT run leads at full detail, and the milestone
        // sits beside it so "what is happening" and "how much is left" are one
        // glance apart. Earlier runs of the same milestone — the spec build that
        // created the version, then any incident adopted into it — collapse to
        // history rows, because a settled run is a record, not a thing to watch.
        <Box
          sx={{
            display: "grid",
            gap: 2,
            alignItems: "start",
            mb: 4,
            // The milestone drops below the run on narrow viewports rather than
            // squeezing the log into an unreadable column.
            gridTemplateColumns: { xs: "1fr", lg: "minmax(0, 1fr) 320px" },
          }}
        >
          <Stack spacing={2} sx={{ minWidth: 0 }}>
            {current && (
              <RunStory
                projectName={projectName}
                tag={selected.tag}
                run={current}
                {...(milestone ? { milestone } : {})}
              />
            )}
            {/* IN the run's column, directly under its card. Full width below
                the grid put it after whichever of the two columns was taller —
                a long milestone, or a version with many earlier sessions — so
                the one thing on this page a person can act on was the thing they
                had to scroll for. Above the history blocks for the same reason:
                a settled session is a record, and a record must not outrank an
                open request for a value. */}
            <ExternalResources projectName={projectName} connections={connections} />
            {/* The current run's own earlier sessions, then the milestone's
                earlier runs — both are history, and both belong below the
                card rather than inside it. */}
            <EarlierSessions cycles={earlierSessions} />
            <RunHistoryList runs={earlier} tag={selected.tag} />
          </Stack>

          {/* Every view ships loading and error states (api-guidelines #2):
              the old issue table carried them for this query; its replacement
              must too, or a failed fetch reads as "no milestone". Data first:
              react-query keeps the last good list through a failed background
              refetch, and cached progress beats an error card. */}
          {milestone ? (
            <MilestonePanel
              tag={selected.tag}
              {...(current?.milestoneTitle ? { title: current.milestoneTitle } : {})}
              work={milestone.work}
              gates={milestone.gates}
              ledger={milestone.ledger}
              claimed={claims}
              presumeOpenWork={presumeOpenWork}
              claimedBy={claimedBy}
              {...(issuesUrl ? { issuesUrl } : {})}
            />
          ) : issues.isError ? (
            <Alert
              severity="error"
              action={<Button onClick={() => void issues.refetch()}>Retry</Button>}
            >
              Failed to load the version&apos;s issues
              {issues.error instanceof Error && issues.error.message
                ? `: ${issues.error.message}`
                : ""}
            </Alert>
          ) : (
            <Box sx={{ display: "flex", justifyContent: "center", p: 4 }}>
              <CircularProgress aria-label="Loading the version's issues" />
            </Box>
          )}
        </Box>
      )}
    </>
  );
}
