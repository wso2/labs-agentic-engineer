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

/**
 * StartBuildDialog — what the Build click does, before it does it (ADR-0029).
 *
 * Two facts, in the order a person needs them: what this version is CALLED,
 * and what it CHANGES. The name is the git tag the build cuts (ADR-0030), so
 * the field is the first thing in the dialog and the last thing the user can
 * still change.
 *
 * The list is GROUPED, because a bare list of names cannot say what a name is:
 * `ceramics-db` and `currency-service` read identically, and one is a database
 * the platform is about to stand up while the other needs a provider and its
 * keys from the user. Same word, opposite obligations — so they are never one
 * group. The heading is the whole of what the dialog says about that: a line of
 * explanation under each was tried and read as clutter over a list this short.
 *
 * Rows are names plus a `new` or `removed` chip; no chip means the thing
 * changed. `removed` states a fact rather than promising an act — a build
 * deprovisions nothing, so the resource behind a removed dependency stays
 * until somebody takes it down from Resources.
 *
 * The requirements are not a row. Every row names something that exists once
 * the version is built, and the requirements are the input to that — they also
 * move on nearly every version, so the row carried no signal.
 *
 * An unchanged spec tree cuts nothing: the version is reused, its milestone
 * reopened, and the dialog says so with the field locked and the action reading
 * Rebuild.
 */

import { useEffect, useState } from "react";
import {
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { VERSION_NAME_HINT, versionNameError } from "../lib/versionName";

type BuildChange = components["schemas"]["BuildChange"];

/** The scroll bound: the frame stays put however long the list runs. */
const LIST_MAX_HEIGHT = 280;

/**
 * The groups, in reading order: what you are building, then what it needs from
 * you, then what the platform hands you.
 */
const GROUPS: { kind: BuildChange["kind"]; title: string }[] = [
  { kind: "component", title: "Components" },
  { kind: "external", title: "External dependencies" },
  { kind: "platform-resource", title: "Platform resources" },
];

/** What the group of rows is called, which depends on what the project has. */
function changesHeading(currentVersion: string, specUnchanged: boolean): string {
  if (specUnchanged) return `No spec changes since ${currentVersion}`;
  if (!currentVersion) return "What this version creates";
  return `What changed since ${currentVersion}`;
}

function ChangeRow({ change }: { change: BuildChange }) {
  return (
    <Stack
      direction="row"
      alignItems="center"
      justifyContent="space-between"
      spacing={1}
    >
      <Typography variant="body2">{change.name}</Typography>
      {change.state !== "changed" && (
        <Chip size="small" variant="outlined" label={change.state} />
      )}
    </Stack>
  );
}

/** One kind's rows under its heading. An empty group renders nothing. */
function ChangeGroup({
  title,
  changes,
}: {
  title: string;
  changes: BuildChange[];
}) {
  if (changes.length === 0) return null;
  return (
    <Stack spacing={1}>
      <Typography variant="subtitle2" sx={{ fontWeight: 700 }}>
        {title}
      </Typography>
      <Stack spacing={1} sx={{ pl: 1.5 }}>
        {changes.map((change) => (
          <ChangeRow key={`${change.kind}:${change.name}`} change={change} />
        ))}
      </Stack>
    </Stack>
  );
}

export function StartBuildDialog({
  open,
  currentVersion,
  suggestedVersion,
  specUnchanged,
  changes,
  takenVersions,
  submitting = false,
  onClose,
  onBuild,
}: {
  open: boolean;
  /** The newest version's name; empty when the project has never been built. */
  currentVersion: string;
  /** What the field is prefilled with — the server's suggestion. */
  suggestedVersion: string;
  /** The spec tree matches the newest version's: this build rebuilds it. */
  specUnchanged: boolean;
  changes: BuildChange[];
  /** Every version name already in use, so a collision is caught before submit. */
  takenVersions: string[];
  submitting?: boolean;
  onClose: () => void;
  /** The name to cut. Empty on a rebuild, which cuts nothing. */
  onBuild: (version: string) => void;
}) {
  const [name, setName] = useState(suggestedVersion);
  // The dialog is mounted before its inputs arrive (preflight resolves on the
  // click), and it reopens for a project whose version has moved on since. Seed
  // on every OPEN rather than on mount, or the second build offers the first
  // build's name.
  useEffect(() => {
    if (open) setName(suggestedVersion);
  }, [open, suggestedVersion]);

  // A rebuild names nothing: the existing version is reused, so its name is
  // shown as a fact and never validated against the tags it is already in.
  const error = specUnchanged ? null : versionNameError(name, takenVersions);
  const shown = specUnchanged ? currentVersion : name;
  // The action names the tag that will exist, which is the trimmed name — the
  // user's stray spacing is not part of it.
  const action = specUnchanged
    ? `Rebuild ${currentVersion}`
    : `Build ${name.trim()}`;
  const removed = changes.some((c) => c.state === "removed");

  return (
    <Dialog
      data-testid="start-build-dialog"
      open={open}
      onClose={onClose}
      maxWidth="xs"
      fullWidth
    >
      <DialogTitle>Start build</DialogTitle>
      <DialogContent>
        <TextField
          label="Version"
          value={shown}
          onChange={(e) => setName(e.target.value)}
          disabled={specUnchanged || submitting}
          error={error !== null}
          // A locked field takes no name, so the rule for one is noise under
          // it; the heading below already says why it is locked.
          helperText={specUnchanged ? " " : (error ?? VERSION_NAME_HINT)}
          size="small"
          fullWidth
          slotProps={{ htmlInput: { "data-testid": "version-name" } }}
          // DialogContent zeroes its top padding under a DialogTitle, which
          // clips an outlined field's floating label. The first field buys it
          // back.
          //
          // The field takes the SAME fill as the list below it: the two are the
          // dialog's two pieces of content, and a transparent field beside a
          // filled list reads as two different materials.
          sx={{
            mt: 1,
            mb: 2.5,
            "& .MuiOutlinedInput-root": { bgcolor: "background.default" },
          }}
        />
        <Typography variant="subtitle2" sx={{ mb: 1 }}>
          {changesHeading(currentVersion, specUnchanged)}
        </Typography>
        {changes.length > 0 ? (
          // Its own surface, so the frame that scrolls is visible as a frame:
          // `background.default` is the opaque tone under the dialog's paper,
          // which sets the list apart without inventing a colour.
          <Box
            sx={{
              maxHeight: LIST_MAX_HEIGHT,
              overflowY: "auto",
              bgcolor: "background.default",
              border: 1,
              borderColor: "divider",
              borderRadius: 1,
              p: 1.5,
            }}
          >
            <Stack spacing={2.5}>
              {GROUPS.map((group) => (
                <ChangeGroup
                  key={group.kind}
                  title={group.title}
                  changes={changes.filter((c) => c.kind === group.kind)}
                />
              ))}
            </Stack>
          </Box>
        ) : (
          <Typography variant="body2" color="text.secondary">
            {specUnchanged
              ? "The build reuses this version and picks its tasks back up."
              : "Nothing in the design moved since the last version."}
          </Typography>
        )}
        {removed && (
          <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: "block" }}>
            A removed dependency keeps its resource. Take it down from Resources.
          </Typography>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button
          variant="contained"
          loading={submitting}
          disabled={error !== null || submitting}
          // An untouched field sends NO name, so the platform's suggestion
          // stays a suggestion: if another build claims it first, the next one
          // is offered rather than refused. A name the user actually typed is
          // sent, and a collision on it is theirs to see.
          onClick={() =>
            onBuild(
              specUnchanged || name.trim() === suggestedVersion
                ? ""
                : name.trim(),
            )
          }
        >
          {action}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
