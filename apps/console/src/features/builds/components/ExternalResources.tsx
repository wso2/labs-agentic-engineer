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

import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Snackbar,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { CircleCheck } from "@wso2/oxygen-ui-icons-react";
import { useQueryClient } from "@tanstack/react-query";
import { LogSection } from "../../../components/LogSection";
import { StatusChip } from "../../../components/StatusChip";
import { projectKeys } from "../../projects/api/keys";
import { useProjectDependencyReadiness } from "../../projects/api/queries";
import { ConnectionValuesDialog } from "../../projects/components/ConnectionValuesDialog";
import { useDesignDependencies } from "../../spec/api/queries";
import {
  asConnectionRow,
  declaredExternalCount,
  externalResourceHeadline,
  externalResourceRows,
  type ExternalResourceRow,
} from "../lib/externalResourceRows";

/**
 * The environment this page's values are for.
 *
 * The Builds page is DEVELOPMENT-only — a version is built and deployed to dev,
 * and other environments are reached by promotion from the Deployments board.
 * The Deployments page pins its own dev column the same way; there is no shared
 * constant to import, so this is the same literal, named and explained here.
 */
const DEV_ENVIRONMENT = "development";

/** The anchor the summary card's parked-run notice jumps to — the one place on
 *  the build page where a person can supply anything. */
export const EXTERNAL_RESOURCES_ANCHOR = "external-resources";

/**
 * The inset every non-row block in this section shares with a row's own
 * padding. The section renders `LogSection` with `disablePadding` so its row
 * dividers reach both edges of the card, which makes the padding this file's
 * to keep consistent rather than the wrapper's.
 */
const SECTION_PADDING = { px: 2.25, py: 2 } as const;

/**
 * External resources — where a person hands the platform the credentials their
 * design's external dependencies need.
 *
 * This used to be a drawer in front of the Build button, which made every
 * external dependency block the build (ADR-0023). It lives on the BUILD PAGE
 * instead, as a `LogSection` peer of Tasks, because ADR-0021 §4 settled what
 * this surface is: a hold is not a stage of its own, it is a row that needs
 * you, rendered like every other row that needs you. Outstanding values are
 * exactly that — work a person must do before this version can go anywhere —
 * so they sit directly under the task list they are a peer of, and above the
 * two log sections, which are a record rather than a request.
 *
 * The section renders its own `LogSection` rather than being wrapped in one by
 * the page: it returns null when there is nothing to supply, and a wrapper
 * would leave an empty titled card behind on every project without an external
 * dependency.
 *
 * `projectName` alone is the whole input, and deliberately so: readiness is
 * PROJECT + ENVIRONMENT scoped, not per version. The values live on the
 * project's bindings, so every version's page shows the same answer, and a
 * value supplied here releases whichever run is parked on it.
 */
export function ExternalResources({ projectName }: { projectName: string }) {
  const design = useDesignDependencies(projectName);
  const readiness = useProjectDependencyReadiness(projectName, DEV_ENVIRONMENT);
  const rows = externalResourceRows(design.data, readiness.data);

  // The dependency whose values are being entered, and the confirmation the
  // save leaves behind.
  const [target, setTarget] = useState<ExternalResourceRow | null>(null);
  const [saved, setSaved] = useState(false);
  const queryClient = useQueryClient();

  // THREE STATES, kept apart on purpose. `rows` is empty in all three, and
  // collapsing any two of them is a lie a person acts on:
  //   - both reads answered  → there is genuinely nothing to supply
  //   - either read pending  → not known yet
  //   - either read failed   → not known, and it will not arrive on its own
  const failed = design.isError || readiness.isError;
  const loading = design.isPending || readiness.isPending;
  // What the DESIGN alone proves: how many externals somebody could be asked
  // to supply. Not how many still need values — only readiness knows that, and
  // it also knows which of them this project can supply at all.
  const declared = declaredExternalCount(design.data);

  // Nothing to say: a project with nothing to supply asks nobody for anything,
  // and an empty reassurance about it is worse than silence. Held until both
  // reads have settled, so the section does not flash absent.
  if (!loading && !failed && rows.length === 0) return null;

  // A read failed, so what is outstanding is unknown — but the section must
  // still appear, because a run parked in `waiting` sends people here by name.
  // The one exception: the design read SUCCEEDED and declares no external
  // dependency at all. Readiness enumerates from that same design, so there is
  // provably nothing at stake and an error card would be pure noise.
  if (failed && design.isSuccess && declared === 0) return null;

  // Only meaningful once both reads answered — see `known` below.
  const outstanding = rows.filter((row) => row.display === "needs-values").length;
  // Whether the section may speak about what is outstanding. A failed readiness
  // read leaves react-query's last good `data` in place, so `rows` can be
  // non-empty and stale; a summary drawn from it would sit above the error card
  // claiming to know exactly what the error says is unknown.
  const known = !failed && !loading && rows.length > 0;

  return (
    <Box id={EXTERNAL_RESOURCES_ANCHOR}>
      <LogSection
        title="External resources"
        meta={
          known ? (
            <StatusChip
              label={externalResourceHeadline(rows)}
              tone={outstanding === 0 ? "success" : "warning"}
              appearance="soft"
              dot={outstanding > 0}
            />
          ) : undefined
        }
        // The rows are divider-separated, and a divider that stops short of the
        // card's edge reads as a mistake rather than a rule. So the section owns
        // its own padding: `SECTION_PADDING` on everything that is not a row,
        // and each row pads itself inside a full-bleed border.
        disablePadding
      >
      {known && (
        <Typography
          variant="body2"
          color="text.secondary"
          sx={SECTION_PADDING}
        >
          {outstanding === 0
            ? "Every external dependency has its development configuration."
            : "The agent builds while you supply these. The version is not deployed until every one of them has its development configuration."}
        </Typography>
      )}

      {/* Every view ships loading and error states (api-guidelines #2). Both
          reads can fail loudly, and neither failure may be dressed up: not as
          "nothing to configure" (the deploy still waits on values this page
          would be silent about) and not as "everything is outstanding" either.
          All the section may add here is the count the design read proves. */}
      {failed ? (
        <Stack spacing={1} sx={SECTION_PADDING}>
          <Alert
            severity="error"
            action={
              <Button
                onClick={() => {
                  if (design.isError) void design.refetch();
                  if (readiness.isError) void readiness.refetch();
                }}
              >
                Retry
              </Button>
            }
          >
            {failureMessage(design.isError, readiness.isError)}
            {readErrorDetail(readiness.isError ? readiness.error : design.error)}
          </Alert>
          {design.isSuccess && declared > 0 && (
            <Typography variant="body2" color="text.secondary">
              {`This project declares ${declared} external ${
                declared === 1 ? "dependency" : "dependencies"
              }. Which of them still need development configuration is unknown until this loads.`}
            </Typography>
          )}
        </Stack>
      ) : loading ? (
        <Box
          sx={{ ...SECTION_PADDING, display: "flex", justifyContent: "center" }}
        >
          <CircularProgress
            size={20}
            aria-label="Loading the external resources"
          />
        </Box>
      ) : (
        <Box>
          {rows.map((row) => {
            const done = row.display === "configured";
            return (
              <Stack
                key={row.name}
                direction="row"
                spacing={2}
                sx={{
                  ...SECTION_PADDING,
                  alignItems: "center",
                  flexWrap: "wrap",
                  rowGap: 1,
                  // A rule above every row: between two rows it separates them,
                  // and above the first it closes off the section's description.
                  borderTop: 1,
                  borderColor: "divider",
                }}
              >
                {/* The row's own two lines. `flexBasis` keeps a long description
                    from squeezing the status and the button onto their own line
                    before the row genuinely runs out of width. */}
                <Box sx={{ minWidth: 0, flexGrow: 1, flexBasis: "24rem" }}>
                  <Typography variant="body2" sx={{ fontWeight: 600 }}>
                    {row.name}
                  </Typography>
                  <Typography
                    variant="body2"
                    color="text.secondary"
                    sx={{ mt: 0.25 }}
                  >
                    {secondaryLine(row)}
                  </Typography>
                </Box>
                {/* Status as plain toned text rather than a pill: the section
                    header already carries a pill, and a second one on every row
                    competes with the button for the eye it is meant to lead to
                    the button. */}
                <Stack
                  direction="row"
                  spacing={0.75}
                  sx={{
                    alignItems: "center",
                    flexShrink: 0,
                    color: done ? "success.main" : "warning.main",
                  }}
                >
                  {done ? (
                    <CircleCheck size={16} aria-hidden />
                  ) : (
                    <Box
                      aria-hidden
                      sx={{
                        width: 6,
                        height: 6,
                        borderRadius: "50%",
                        bgcolor: "warning.main",
                      }}
                    />
                  )}
                  <Typography
                    variant="body2"
                    sx={{ color: "inherit", fontWeight: 500 }}
                  >
                    {done ? "Configured" : "Needs configuration"}
                  </Typography>
                </Stack>
                <Button
                  size="small"
                  // The outstanding row is the only thing on this page a person
                  // can act on, so it gets the page's one filled button; a row
                  // that is already done offers a quieter way back in.
                  variant={done ? "outlined" : "contained"}
                  color={done ? "inherit" : "primary"}
                  // The row's name is in its own label; the button's accessible
                  // name must carry it too, or every row reads the same. It must
                  // also CONTAIN the visible text (WCAG 2.5.3 Label in Name) —
                  // "Configure stripe" under a button reading "Configure now"
                  // leaves a voice-control user saying a name that does not
                  // match what they can see.
                  aria-label={`${
                    done ? "Edit configuration for" : "Configure now:"
                  } ${row.name}`}
                  onClick={() => setTarget(row)}
                  sx={{ flexShrink: 0 }}
                >
                  {done ? "Edit configuration" : "Configure now"}
                </Button>
              </Stack>
            );
          })}
        </Box>
      )}

      </LogSection>

      {target && (
        <ConnectionValuesDialog
          open
          onClose={() => setTarget(null)}
          onSaved={() => {
            setTarget(null);
            setSaved(true);
            // The save re-authors the resource, so the readiness read is stale
            // the moment it returns — and the run's own dispatch/deploy story
            // reads the same state, so the project's reads go with it.
            void queryClient.invalidateQueries({
              queryKey: projectKeys.dependencyReadiness(
                projectName,
                DEV_ENVIRONMENT,
              ),
            });
            void queryClient.invalidateQueries({
              queryKey: projectKeys.status(projectName),
            });
          }}
          projectName={projectName}
          connection={asConnectionRow(target)}
          environment={DEV_ENVIRONMENT}
        />
      )}
      <Snackbar
        open={saved}
        autoHideDuration={6000}
        onClose={() => setSaved(false)}
      >
        <Alert severity="success" onClose={() => setSaved(false)}>
          Configuration saved — the deployment no longer waits on this one.
        </Alert>
      </Snackbar>
    </Box>
  );
}

/**
 * Which read failed, said plainly. The two failures leave the person with
 * different amounts of knowledge, so they must not share a sentence: without
 * the design the console does not even know what the project declares, while
 * without readiness it knows the list but not what is still outstanding.
 */
function failureMessage(designFailed: boolean, readinessFailed: boolean): string {
  if (designFailed && readinessFailed) {
    return "Failed to load this project's external resources";
  }
  if (designFailed) {
    return "Failed to load this project's external dependencies";
  }
  return "Failed to load which external resources still need configuration";
}

/** The failing read's own message, appended when it carries one. */
function readErrorDetail(error: unknown): string {
  return error instanceof Error && error.message ? `: ${error.message}` : "";
}

/** The row's second line: the design's own sentence when it has one, otherwise
 *  what is outstanding — never nothing, so rows keep an even height. */
function secondaryLine(row: ExternalResourceRow): string {
  if (row.description) return row.description;
  if (row.display === "configured") {
    return `${row.config.length} setting${row.config.length === 1 ? "" : "s"} stored`;
  }
  return `${row.missingCount} setting${row.missingCount === 1 ? "" : "s"} outstanding`;
}
