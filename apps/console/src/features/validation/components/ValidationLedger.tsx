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
  Button,
  ListingTable,
  MenuItem,
  Skeleton,
  Stack,
  TextField,
  Typography,
  alpha,
  type Theme,
} from "@wso2/oxygen-ui";
import { CircleCheck } from "@wso2/oxygen-ui-icons-react";
import { Link, createLink, useNavigate } from "@tanstack/react-router";
import { EmptyState } from "../../../components/EmptyState";
import { PageHeader } from "../../../components/PageHeader";
import { StatusChip } from "../../../components/StatusChip";
import type { components } from "../../../generated/aep-api";
import { useTicker } from "../../builds/hooks/useTicker";
import { runStamp } from "../../builds/lib/format";
import { buildDuration } from "../../builds/lib/ledger";
import { validationChip } from "../lib/chip";
import { validationIsLive } from "../lib/lifecycle";
import { useValidations } from "../api/queries";

type ValidationSummary = components["schemas"]["ValidationSummary"];

/**
 * The Validation page: ONE ROW PER VERSION, the same shape Builds has.
 *
 * It replaced a page pinned to the newest milestone, which could not be pointed
 * anywhere else — so every earlier version's verdict, and every earlier
 * attempt's report, was unreachable even though the platform keeps both
 * forever.
 *
 * The row set is deliberately the build ledger's, down to a version that has
 * never been validated getting a row: an absent row and a never-validated one
 * are the same screen to a reader, and "never validated" is the most actionable
 * thing this page has to say.
 */

// `predicate` is the label as the empty-state sentence needs it — "No versions
// are …". Most labels already read as one lowercased; "Needs attention" is a
// noun phrase and does not, which is why the sentence carries its own wording
// rather than lowercasing the menu's.
const STATE_FILTERS = [
  { value: "all", label: "All states", predicate: "listed" },
  { value: "live", label: "In progress", predicate: "in progress" },
  { value: "passed", label: "Validated", predicate: "validated" },
  { value: "failed", label: "Needs attention", predicate: "in need of attention" },
  { value: "unvalidated", label: "Not validated", predicate: "not validated" },
];

type StateFilter = (typeof STATE_FILTERS)[number]["value"];

/**
 * The filter groups by what a reader would DO about a row, not by the enum.
 *
 * "Needs attention" folds every state where the version did not come out clean
 * — failed, error, inconclusive — because a reader scanning for problems wants
 * one filter, not three. The verdict cell still says which.
 */
function matchesFilter(row: ValidationSummary, filter: StateFilter): boolean {
  switch (filter) {
    case "live":
      return validationIsLive(row.state);
    case "passed":
      return row.state === "passed" || row.state === "partial";
    case "failed":
      return (
        row.state === "failed" ||
        row.state === "unreported" ||
        row.state === "inconclusive"
      );
    case "unvalidated":
      return row.state === "none" || row.state === "skipped" || row.state === "cancelled";
    default:
      return true;
  }
}

/** Is this row's attempt still running — what makes its Duration tick. */
function attemptIsOpen(row: ValidationSummary): boolean {
  return Boolean(row.startedAt) && !row.endedAt;
}

const LinkButton = createLink(Button);

const COLUMNS = [
  { key: "version", label: "Version", width: 104 },
  { key: "milestone", label: "Milestone" },
  { key: "verdict", label: "Verdict", width: 190 },
  { key: "duration", label: "Duration", width: 110 },
  { key: "validated", label: "Last validated", width: 150 },
];

export function ValidationLedger({ projectName }: { projectName: string }) {
  const validations = useValidations(projectName);
  const navigate = useNavigate();
  const [filter, setFilter] = useState<StateFilter>("all");

  // A running attempt's Duration counts against `Date.now()`; without a clock
  // of its own the column would freeze at whatever it read on first paint.
  useTicker((validations.data ?? []).some(attemptIsOpen));

  const rows = useMemo(
    () => (validations.data ?? []).filter((v) => matchesFilter(v, filter)),
    [validations.data, filter],
  );

  const backTo = {
    link: <Link to="/projects/$projectName" params={{ projectName }} />,
    label: "Back to Overview",
  };

  const header = (actions?: React.ReactNode) => (
    <PageHeader
      title="Validations"
      subtitle="Every version of your spec is validated against its acceptance criteria on the deployed system."
      backTo={backTo}
      {...(actions ? { actions } : {})}
    />
  );

  if (validations.isPending) {
    return (
      <>
        {header()}
        <Stack spacing={1} sx={{ mt: 2 }}>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} variant="rounded" height={64} />
          ))}
        </Stack>
      </>
    );
  }

  if (validations.isError) {
    return (
      <>
        {header()}
        <Alert
          severity="error"
          action={<Button onClick={() => void validations.refetch()}>Retry</Button>}
        >
          Failed to load validations
          {validations.error instanceof Error && validations.error.message
            ? `: ${validations.error.message}`
            : ""}
        </Alert>
      </>
    );
  }

  if ((validations.data ?? []).length === 0) {
    return (
      <>
        {header()}
        <EmptyState
          icon={<CircleCheck size={48} />}
          title="Nothing to validate yet"
          description="Once a version is built and deployed, the deployed system is driven against the acceptance criteria in your spec. Results appear here."
          action={
            <LinkButton
              variant="contained"
              to="/projects/$projectName/builds"
              params={{ projectName }}
            >
              Go to Builds
            </LinkButton>
          }
        />
      </>
    );
  }

  const stateFilter = (
    <TextField
      select
      size="small"
      value={filter}
      onChange={(e) => setFilter(e.target.value as StateFilter)}
      label="State"
      sx={{ width: 180 }}
    >
      {STATE_FILTERS.map((o) => (
        <MenuItem key={o.value} value={o.value}>
          {o.label}
        </MenuItem>
      ))}
    </TextField>
  );

  return (
    <>
      {header(stateFilter)}

      {rows.length === 0 ? (
        <EmptyState
          compact
          bordered
          description={`No versions are ${STATE_FILTERS.find((o) => o.value === filter)?.predicate ?? filter}. Clear the filter to see every version.`}
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
              {rows.map((row) => (
                <LedgerRow
                  key={row.tag}
                  row={row}
                  onOpen={() =>
                    void navigate({
                      to: "/projects/$projectName/validations/$tag",
                      params: { projectName, tag: row.tag },
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

function LedgerRow({ row, onOpen }: { row: ValidationSummary; onOpen: () => void }) {
  // One mapper owns label and tone for every surface that renders this
  // vocabulary (ADR-0016); this row is one of its consumers, not a new reading
  // of the same enum. `null` is the states with nothing to say — `none` is the
  // only one that reaches a row, and it says so in words instead.
  const chip = validationChip(row.state);
  const live = validationIsLive(row.state);

  return (
    <ListingTable.Row
      hover
      clickable
      onClick={onOpen}
      {...(live
        ? { sx: { bgcolor: (t: Theme) => alpha(t.palette.info.main, 0.06) } }
        : {})}
    >
      <ListingTable.Cell>
        <Typography
          variant="subtitle2"
          sx={{ fontWeight: 600, fontVariantNumeric: "tabular-nums" }}
        >
          {row.tag}
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
          Milestone #{row.milestoneNumber}
        </Typography>
      </ListingTable.Cell>

      <ListingTable.Cell>
        {chip ? (
          <StatusChip
            label={chip.label}
            tone={chip.tone}
            appearance="soft"
            dot
            {...(chip.spokenLabel ? { spokenLabel: chip.spokenLabel } : {})}
          />
        ) : (
          <Typography variant="body2" color="text.secondary">
            Not validated
          </Typography>
        )}
      </ListingTable.Cell>

      <ListingTable.Cell>
        <Typography variant="body2" sx={{ fontVariantNumeric: "tabular-nums" }}>
          {buildDuration(row.startedAt, row.endedAt) || "—"}
        </Typography>
      </ListingTable.Cell>

      <ListingTable.Cell>
        {/* The attempt's END, so a running row shows a dash while its Duration
            ticks beside it: an open attempt has no "last validated" yet. */}
        <Typography variant="body2" color="text.secondary">
          {runStamp(row.endedAt) || "—"}
        </Typography>
      </ListingTable.Cell>
    </ListingTable.Row>
  );
}
