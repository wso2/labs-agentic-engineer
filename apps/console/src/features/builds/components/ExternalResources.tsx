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

import { useEffect, useMemo, useRef, useState } from "react";
import {
  Alert,
  Box,
  Button,
  ButtonBase,
  Card,
  CardContent,
  Collapse,
  Snackbar,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { ChevronRight } from "@wso2/oxygen-ui-icons-react";
import { StatusChip, type StatusTone } from "../../../components/StatusChip";
import { ConnectionValuesDialog } from "../../projects/components/ConnectionValuesDialog";
import { useProjectDependencyReadiness } from "../../projects/api/queries";
import { useDesignDependencies } from "../../spec/api/queries";
import {
  externalResourceRows,
  type ExternalResourceRow,
} from "../lib/externalResourceRows";

/**
 * EXTERNAL RESOURCES — the third-party services this project consumes, and the
 * way to hand the platform their development values.
 *
 * It sits BELOW the run, because Build is this page's primary action and this
 * section is not on its critical path: a run no longer collects values before it
 * dispatches (#440), it parks at the deploy gate if any are still missing
 * (ADR-0020). The run card's own hold notice is what makes this loud when it
 * actually matters, and links here.
 *
 * External ONLY, deliberately. A platform resource — a database, an identity app
 * — has no values for anyone to supply, so it has no row here even though it is
 * also a dependency. The Deployments page reports on the wider set.
 *
 * The fields live in the same dialog the Deployments page uses. One design for
 * one job, and it keeps ten keys across four resources from landing on the
 * reader at once.
 */

type ReadinessState = "unknown" | "not-provisioned" | "unset" | "configured";

// Short on purpose. These sit in a column beside three siblings, and the run
// spine's own gate chips next door are already terse ("provisioning",
// "provisioned", "needs you") — a longer label truncated mid-word instead.
const presentation = {
  unknown: { label: "Unknown", tone: "neutral" },
  "not-provisioned": { label: "Provisioning", tone: "info" },
  unset: { label: "Needs values", tone: "warning" },
  configured: { label: "Configured", tone: "success" },
} as const satisfies Record<ReadinessState, { label: string; tone: StatusTone }>;

interface ResourceRow {
  row: ExternalResourceRow;
  state: ReadinessState;
  /** How many keys are outstanding — server truth where the readiness read
   *  gave it, the declared key count as the fallback. */
  missing: number;
}

/** The section's own headline: what it still wants, or that it wants nothing. */
function summarise(rows: ResourceRow[]): { label: string; tone: StatusTone } {
  const unset = rows.filter((r) => r.state === "unset").length;
  if (unset > 0) {
    return { label: `${unset} of ${rows.length} need values`, tone: "warning" };
  }
  if (rows.every((r) => r.state === "configured")) {
    return { label: `${rows.length} of ${rows.length} configured`, tone: "success" };
  }
  const provisioning = rows.filter((r) => r.state === "not-provisioned").length;
  if (provisioning > 0) {
    return {
      label: `Provisioning ${provisioning} of ${rows.length}`,
      tone: "info",
    };
  }
  return { label: `${rows.length} declared`, tone: "neutral" };
}

function ResourceRowItem({
  entry,
  onConfigure,
}: {
  entry: ResourceRow;
  onConfigure: () => void;
}) {
  const { row, state, missing } = entry;
  const status = presentation[state];
  // Only two states have anything for a person to do. A resource the platform is
  // still standing up rejects a save, and one whose readiness never arrived
  // would be a save against a state we do not know.
  const action =
    state === "unset"
      ? { label: "Configure", variant: "contained" as const }
      : state === "configured"
        ? { label: "Update values", variant: "outlined" as const }
        : undefined;

  // Why a row offers nothing matters more than what it is, so for the one state
  // that is explicably actionless the reason takes the secondary line. It also
  // keeps every row to a single line of secondary text, which is what lets the
  // columns line up.
  const secondary =
    state === "not-provisioned"
      ? "Values can be saved once the platform finishes provisioning"
      : row.description;

  return (
    <Box
      component="li"
      aria-label={row.name}
      sx={{
        // Stacked by default, side by side once the CARD is wide enough. A
        // VIEWPORT breakpoint was wrong here: this section lives in the run's
        // column, so a 1440px window can still leave it ~500px — and fixed
        // columns then squeezed the name to a word per line underneath an
        // overlapping chip. A container query asks the only question that
        // matters: how much room does this row actually have?
        display: "flex",
        flexDirection: "column",
        gap: 1,
        py: 1.5,
        px: 2,
        borderTop: 1,
        borderColor: "divider",
        "&:first-of-type": { borderTop: 0 },
        "@container (min-width: 32rem)": {
          flexDirection: "row",
          alignItems: "center",
          gap: 2,
        },
      }}
    >
      <Box sx={{ minWidth: 0, flexGrow: 1 }}>
        <Typography component="h3" variant="body2" sx={{ fontWeight: 600 }}>
          {row.name}
        </Typography>
        {secondary && (
          <Typography variant="caption" color="text.secondary" component="p">
            {secondary}
          </Typography>
        )}
      </Box>
      {/* State, count and action travel together: inline while the row is
          narrow, and a fixed-width track set once it is wide. Because that set
          is a constant width pinned to the row's right edge, every row's chips
          and buttons line up as columns without the name having to be
          truncated to a guessed width. */}
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          flexWrap: "wrap",
          gap: 1.5,
          "@container (min-width: 32rem)": {
            display: "grid",
            gridTemplateColumns: "8rem 4.5rem 8.5rem",
            // Content-width, not track-width: stretching made every chip the
            // same size as the longest label, which reads as a button.
            justifyItems: "start",
            flexShrink: 0,
            gap: 0,
            columnGap: 1.5,
          },
        }}
      >
        <StatusChip label={status.label} tone={status.tone} appearance="soft" />
        {/* The count is the reason to open THIS row rather than another, so it
            shows only while something is actually outstanding. */}
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ fontVariantNumeric: "tabular-nums" }}
        >
          {state === "unset"
            ? `${missing} ${missing === 1 ? "value" : "values"}`
            : ""}
        </Typography>
        <Box sx={{ "@container (min-width: 32rem)": { justifySelf: "end" } }}>
          {action && (
            <Button
              size="small"
              variant={action.variant}
              onClick={onConfigure}
              // Every row's button carries the same visible label, so the
              // accessible name has to say which resource it acts on.
              aria-label={`${action.label} for ${row.name}`}
            >
              {action.label}
            </Button>
          )}
        </Box>
      </Box>
    </Box>
  );
}

export function ExternalResources({
  projectName,
  /**
   * The `?connections` search value. `open` expands the section; any other
   * value names a resource whose dialog opens with it — which is what lets the
   * colleague holding a key arrive on THEIR field instead of on a list.
   */
  connections,
}: {
  projectName: string;
  connections: string | undefined;
}) {
  const designDependencies = useDesignDependencies(projectName);
  const readiness = useProjectDependencyReadiness(projectName);
  const rows = useMemo(
    () => externalResourceRows(designDependencies.data),
    [designDependencies.data],
  );

  const entries = useMemo<ResourceRow[]>(() => {
    const byName = new Map(
      (readiness.data?.dependencies ?? []).map((d) => [d.name, d]),
    );
    return rows.map((row) => {
      const found = byName.get(row.name);
      return {
        row,
        state: (found?.state ?? "unknown") as ReadinessState,
        missing: found?.missingKeys?.length || row.config.length,
      };
    });
  }, [rows, readiness.data]);

  const deepLinkName =
    connections && connections !== "open" ? connections : undefined;

  const [target, setTarget] = useState<ExternalResourceRow | null>(null);
  const [saved, setSaved] = useState<string | null>(null);
  // null while the reader has not touched it: the section's own state answers
  // for it, so a row turning `unset` mid-session opens the section, and a
  // deliberate collapse is never re-opened underneath the reader.
  const [toggled, setToggled] = useState<boolean | null>(null);

  // Honour ?connections=<name> once per name. Rows arrive asynchronously, so
  // this cannot be read at mount; the ref is what stops a dialog the reader
  // closed from re-opening on the next render.
  //
  // A link can only open what a row itself would open. A resource the platform
  // is still standing up offers no button precisely because a save against it is
  // rejected, and a deep link is not a licence to route around that — the
  // section still expands, so the reader sees why nothing opened.
  const opened = useRef<string | null>(null);
  useEffect(() => {
    if (!deepLinkName || opened.current === deepLinkName) return;
    const match = entries.find(
      (entry) => entry.row.name.toLowerCase() === deepLinkName.toLowerCase(),
    );
    if (!match) return;
    opened.current = deepLinkName;
    if (match.state === "unset" || match.state === "configured") {
      setTarget(match.row);
    }
  }, [deepLinkName, entries]);

  // Nothing to say yet, and nothing to say ever: no card. A project with no
  // external resources should not carry a section explaining that it has none,
  // and a spinner in a section that may not exist reads as a broken one.
  if (designDependencies.isPending || readiness.isPending) return null;

  if (designDependencies.isError) {
    return (
      <Alert
        severity="error"
        sx={{ mb: 3 }}
        action={
          <Button onClick={() => void designDependencies.refetch()}>Retry</Button>
        }
      >
        Failed to load this project&apos;s external resources
      </Alert>
    );
  }

  if (entries.length === 0) return null;

  const summary = summarise(entries);
  // Collapse only on POSITIVE knowledge that there is nothing to do. A resource
  // still provisioning, or one whose readiness never arrived, is not a reason to
  // fold the section away — that would hide rows on the strength of a state we
  // do not have. Configured everywhere is a receipt; anything else stays open.
  const settled = entries.every((entry) => entry.state === "configured");
  const expanded = toggled ?? (!settled || connections !== undefined);

  return (
    <>
      {/* The same outlined Card and uppercase overline every other panel on this
          page uses. An Accordion brought its own radius, elevation and heading
          style, which read as a component borrowed from somewhere else. */}
      <Card variant="outlined">
        {/* Even padding on all four sides. CardContent's default gives its last
            child a 24px bottom against a 16px top, which reads as slack under an
            expanded list and — worse — leaves the header visibly off-centre when
            the section is collapsed to just that row. */}
        <CardContent sx={{ p: 2, "&:last-child": { pb: 2 } }}>
          <ButtonBase
            onClick={() => setToggled(!expanded)}
            aria-expanded={expanded}
            sx={{
              width: "100%",
              justifyContent: "flex-start",
              gap: 1,
              // The heading and its chip wrap rather than overflow. Without this
              // the summary was clipped mid-word in a narrow column, which is
              // the one part of a collapsed section a reader has to be able to
              // read.
              flexWrap: "wrap",
              rowGap: 0.5,
              py: 0.5,
              textAlign: "left",
            }}
          >
            <ChevronRight
              size={13}
              style={{
                flexShrink: 0,
                transition: "transform 0.15s",
                transform: expanded ? "rotate(90deg)" : "none",
              }}
            />
            <Typography
              component="h2"
              variant="caption"
              sx={{
                fontWeight: 700,
                letterSpacing: "0.08em",
                textTransform: "uppercase",
                color: "text.secondary",
              }}
            >
              External resources
            </Typography>
            <StatusChip
              label={summary.label}
              tone={summary.tone}
              appearance="soft"
            />
            <Box sx={{ flexGrow: 1 }} />
          </ButtonBase>
          <Collapse in={expanded} unmountOnExit>
            <Stack spacing={2} sx={{ mt: 1.5 }}>
              <Typography variant="body2" color="text.secondary">
                Development values for the third-party services this project
                uses. A deploy waits until every one of them is configured.
              </Typography>
              {/* Readiness failing is not fatal — the design still names the
                  resources, so they are listed as unknown rather than hidden,
                  and the failure is reported where the states would have been. */}
              {readiness.isError && (
                <Alert
                  severity="warning"
                  action={
                    <Button onClick={() => void readiness.refetch()}>
                      Retry
                    </Button>
                  }
                >
                  Failed to load readiness — states below are unknown
                </Alert>
              )}
              <Box
                component="ul"
                sx={{
                  listStyle: "none",
                  m: 0,
                  p: 0,
                  border: 1,
                  borderColor: "divider",
                  borderRadius: 1,
                  // What the rows size themselves against — see the row's own
                  // container query.
                  containerType: "inline-size",
                }}
              >
                {entries.map((entry) => (
                  <ResourceRowItem
                    key={entry.row.id}
                    entry={entry}
                    onConfigure={() => setTarget(entry.row)}
                  />
                ))}
              </Box>
            </Stack>
          </Collapse>
        </CardContent>
      </Card>

      {target && (
        <ConnectionValuesDialog
          open
          onClose={() => setTarget(null)}
          onSaved={() => {
            setSaved(target.name);
            setTarget(null);
          }}
          projectName={projectName}
          connection={target}
          environment="development"
        />
      )}

      {/* Says only what it knows. Whether the RUN resumes depends on every other
          resource too, and the run card is the one surface entitled to answer
          that — a toast promising it would be wrong half the time. */}
      <Snackbar
        open={saved !== null}
        autoHideDuration={6000}
        onClose={() => setSaved(null)}
      >
        <Alert severity="success" onClose={() => setSaved(null)}>
          Values saved — {saved} is configured.
        </Alert>
      </Snackbar>
    </>
  );
}
