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

import { useMemo, useState } from "react";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  ListingTable,
  MenuItem,
  Skeleton,
  Stack,
  TextField,
  Typography,
  alpha,
  type Theme,
} from "@wso2/oxygen-ui";
import { ListChecks } from "@wso2/oxygen-ui-icons-react";
import { Link, createLink, useNavigate } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { useProjectStatus } from "../../projects/api/queries";
import { useBuilds } from "../api/queries";
import { useTicker } from "../hooks/useTicker";
import { runStamp } from "../lib/format";
import {
  isAwaitingValues,
  isDurationOpen,
  ledgerDuration,
  ledgerStatus,
  milestoneLabel,
} from "../lib/ledger";

type BuildSummary = components["schemas"]["BuildSummary"];
type DeployStage = components["schemas"]["DeployStage"];

/**
 * The Builds page: ONE ROW PER VERSION (ADR-0021 §1).
 *
 * This replaced the now-first landing page, which opened on the newest
 * version's live run and offered no way to see two versions at once. The live
 * row is still one click from that surface — it is the build detail page now —
 * and it tints and pulses here so a reader arriving mid-run still lands on the
 * moving thing.
 */

const STATUS_FILTERS = [
  { value: "all", label: "All statuses" },
  { value: "running", label: "In progress" },
  { value: "completed", label: "Completed" },
  { value: "failed", label: "Failed" },
  // Cancelled is its OWN filter and not a flavour of Failed, matching the
  // vocabulary the API now speaks. It also has to exist: `failed` matches on an
  // error tone and a cancelled row is neutral, so without this the row would
  // match no filter at all and vanish from every view but "All statuses" — the
  // same trap the deploy-gate park clause below was written for.
  { value: "cancelled", label: "Cancelled" },
] as const;

type StatusFilter = (typeof STATUS_FILTERS)[number]["value"];

/**
 * Filter on the status the ROW RENDERS, not on `build.status`.
 *
 * They are not the same thing: a completed version whose rollout is under way
 * shows "Deploying to development" and IS live, and one whose rollout failed
 * shows "Deploy failed". Filtering on the raw build status hid both from the
 * filters that name exactly what they are.
 */
function matchesFilter(
  build: BuildSummary,
  filter: StatusFilter,
  deploy: DeployStage | undefined,
): boolean {
  if (filter === "all") return true;
  const status = ledgerStatus(build, deploy);
  switch (filter) {
    case "running":
      // A version parked at the deploy gate is not live — its row is quiet on
      // purpose — but it IS an unfinished version, and it belongs to the filter
      // for those. Without this clause it would match no filter at all and
      // vanish from every view but "All statuses", which is the one version the
      // reader most needs to find.
      return status.live || isAwaitingValues(build);
    case "failed":
      return status.tone === "error";
    case "cancelled":
      return build.status === "cancelled";
    case "completed":
      // A version that is completed AND still moving belongs under Running; it
      // would otherwise appear under both.
      return build.status === "completed" && !status.live;
    default:
      return true;
  }
}

const LinkButton = createLink(Button);

const COLUMNS = [
  { key: "version", label: "Version", width: 104 },
  { key: "milestone", label: "Milestone" },
  { key: "status", label: "Status", width: 190 },
  { key: "duration", label: "Duration", width: 110 },
  { key: "started", label: "Started", width: 150 },
];

export function BuildsLedger({ projectName }: { projectName: string }) {
  const builds = useBuilds(projectName);
  const navigate = useNavigate();
  const [filter, setFilter] = useState<StatusFilter>("all");

  // Which version reached an environment. The project layout already polls
  // this, so react-query serves it from cache.
  //
  // There is deliberately NO task read here, and so no Tasks column: an
  // untagged list-tasks response cannot be attributed to versions (the server
  // leaves `lineage.specTag` empty when the query spans versions, and nothing
  // else on a task names its version), and a tag-scoped read would be one
  // GitHub-backed request PER ROW. The per-version breakdown lives on the build
  // page, one click away, where the read is scoped to begin with.
  const status = useProjectStatus(projectName);
  const deploy: DeployStage | undefined = status.data?.deploy;

  // A running row's Duration counts against `Date.now()`; without a clock of
  // its own the column would freeze at whatever it read on first paint.
  useTicker((builds.data ?? []).some(isDurationOpen));

  const rows = useMemo(
    () => (builds.data ?? []).filter((b) => matchesFilter(b, filter, deploy)),
    [builds.data, filter, deploy],
  );

  // The header renders through every state below so the back link stays
  // reachable while builds load or fail — the pattern every adopted page uses.
  const backTo = {
    link: <Link to="/projects/$projectName" params={{ projectName }} />,
    label: "Back to Overview",
  };

  const header = (actions?: React.ReactNode) => (
    <PageHeader
      title="Builds"
      subtitle="Every version of your spec that was handed to the coding agents. One build runs at a time — the rest wait in order."
      backTo={backTo}
      {...(actions ? { actions } : {})}
    />
  );

  if (builds.isPending) {
    return (
      <>
        {header()}
        {/* Skeleton ROWS, not a spinner: the reader knows a table is coming and
            the page does not jump when it arrives. */}
        <Stack spacing={1} sx={{ mt: 2 }}>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} variant="rounded" height={64} />
          ))}
        </Stack>
      </>
    );
  }

  if (builds.isError) {
    return (
      <>
        {header()}
        <Alert
          severity="error"
          action={<Button onClick={() => void builds.refetch()}>Retry</Button>}
        >
          Failed to load builds
          {builds.error instanceof Error && builds.error.message
            ? `: ${builds.error.message}`
            : ""}
        </Alert>
      </>
    );
  }

  if ((builds.data ?? []).length === 0) {
    return (
      <>
        {header()}
        <EmptyState
          icon={<ListChecks size={48} />}
          title="No builds yet"
          description="A build hands your design to coding agents, which write your components and open pull requests."
          action={
            <LinkButton
              variant="contained"
              to="/projects/$projectName/spec"
              params={{ projectName }}
            >
              Go to the spec
            </LinkButton>
          }
        />
      </>
    );
  }

  const statusFilter = (
    <TextField
      select
      size="small"
      value={filter}
      onChange={(e) => setFilter(e.target.value as StatusFilter)}
      label="Status"
      sx={{ width: 180 }}
    >
      {STATUS_FILTERS.map((o) => (
        <MenuItem key={o.value} value={o.value}>
          {o.label}
        </MenuItem>
      ))}
    </TextField>
  );

  return (
    <>
      {header(statusFilter)}

      {rows.length === 0 ? (
        // Filtered to nothing is NOT the same as having no builds, and must not
        // borrow the "no builds yet" copy — the reader would think they had lost
        // their history.
        <EmptyState
          compact
          bordered
          description={`No ${(STATUS_FILTERS.find((o) => o.value === filter)?.label ?? filter).toLowerCase()} builds. Clear the filter to see every version.`}
          action={<Button onClick={() => setFilter("all")}>Clear filter</Button>}
        />
      ) : (
        <ListingTable.Container sx={{ width: "100%" }}>
          <ListingTable density="standard">
            <ListingTable.Head>
              <ListingTable.Row>
                {COLUMNS.map((c) => (
                  <ListingTable.Cell
                    key={c.key}
                    {...(c.width ? { sx: { width: c.width } } : {})}
                  >
                    {c.label}
                  </ListingTable.Cell>
                ))}
              </ListingTable.Row>
            </ListingTable.Head>
            <ListingTable.Body>
              {rows.map((build) => (
                <LedgerRow
                  key={build.tag}
                  build={build}
                  {...(deploy ? { deploy } : {})}
                  onOpen={() =>
                    void navigate({
                      to: "/projects/$projectName/builds/$tag",
                      params: { projectName, tag: build.tag },
                    })
                  }
                />
              ))}
            </ListingTable.Body>
          </ListingTable>
        </ListingTable.Container>
      )}
    </>
  );
}

function LedgerRow({
  build,
  deploy,
  onOpen,
}: {
  build: BuildSummary;
  deploy?: DeployStage | undefined;
  onOpen: () => void;
}) {
  const status = ledgerStatus(build, deploy);
  // The ROW's liveness is the STATUS's liveness, not the build's: a completed
  // version whose rollout is under way is moving, and tinting on `build.status`
  // made the row go quiet at exactly the moment it had something to say.
  const live = status.live;

  return (
    <ListingTable.Row
      hover
      clickable
      onClick={onOpen}
      // A live row tints so the moving version is findable without reading
      // every status cell. alpha() over a theme colour, so it holds in both
      // schemes — a hardcoded near-white tint would vanish in dark mode.
      {...(live
        ? { sx: { bgcolor: (t: Theme) => alpha(t.palette.info.main, 0.06) } }
        : {})}
    >
      <ListingTable.Cell>
        <Typography
          variant="subtitle2"
          sx={{ fontWeight: 600, fontVariantNumeric: "tabular-nums" }}
        >
          {build.tag}
        </Typography>
      </ListingTable.Cell>

      <ListingTable.Cell sx={{ minWidth: 0 }}>
        <Typography
          variant="body2"
          sx={{
            fontWeight: 500,
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
          }}
        >
          {milestoneLabel(build)}
        </Typography>
      </ListingTable.Cell>

      <ListingTable.Cell>
        <StatusChip
          label={status.label}
          tone={status.tone}
          appearance="soft"
          dot
        />
      </ListingTable.Cell>

      <ListingTable.Cell>
        <Typography variant="body2" sx={{ fontVariantNumeric: "tabular-nums" }}>
          {ledgerDuration(build)}
        </Typography>
      </ListingTable.Cell>

      <ListingTable.Cell>
        <Typography variant="body2" color="text.secondary">
          {runStamp(build.startedAt) || "—"}
        </Typography>
      </ListingTable.Cell>
    </ListingTable.Row>
  );
}

/** Kept for the loading state's sake — a spinner the router can mount. */
export function BuildsLedgerFallback() {
  return (
    <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
      <CircularProgress aria-label="Loading builds" />
    </Box>
  );
}
