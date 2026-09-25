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

import {
  alpha,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Stack,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { ArrowUpRight, X } from "@wso2/oxygen-ui-icons-react";
import { createLink } from "@tanstack/react-router";
import { StatusChip } from "../../../components/StatusChip";
import { validationView, type StageTone } from "../lib/pipeline";
import {
  allConnectionsSet,
  connectionIsSet,
  type ConnectionRow,
  type ConnectionValues,
} from "../lib/promotion";

const LinkButton = createLink(Button);

// The promote dialog (Deployments UX, Turn 3 / option 2c): configuration is
// collected AT THE MOMENT OF PROMOTION, so the resting page stays a story
// rather than a form. The validation verdict leads — it is the reason
// promoting is safe — then one card per connection. A platform-provisioned
// connection needs no input and says so; everything else collects its
// production values, and Promote enables only when every one is set.
//
// The contract has no promote endpoint yet, so entered values live in the
// page's state (they survive closing the dialog, not a reload) and the
// enabled Promote hands control back to the page.

/** The verdict banner's sentence — the label validationView already owns,
 *  set in the dialog's own framing. Only a success verdict gets the
 *  "Validated" claim; every other tone leads neutrally so "Validated in dev —
 *  validation skipped." can never be said above a Promote button (#401
 *  review). */
function verdictSentence(label: string, tone: StageTone): string {
  return tone === "success"
    ? `Validated in dev — ${label}.`
    : `Validation in dev — ${label}.`;
}

function ConnectionCard({
  row,
  values,
  focused,
  onValueChange,
}: {
  row: ConnectionRow;
  values: ConnectionValues;
  /** The dialog was opened FOR this connection — its first field takes focus. */
  focused: boolean;
  onValueChange: (rowId: string, key: string, value: string) => void;
}) {
  const set = row.provisioned || connectionIsSet(row, values);
  const chip = row.provisioned
    ? { label: "Provisioned by platform", tone: "success" as const }
    : set
      ? { label: "Set", tone: "success" as const }
      : { label: "Required", tone: "warning" as const };

  return (
    <Box
      sx={(theme) => ({
        border: 1,
        borderColor: set
          ? "divider"
          : alpha(theme.palette.warning.main, 0.35),
        borderRadius: 2,
        p: 2,
      })}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
        <Typography variant="subtitle2" sx={{ flexGrow: 1, minWidth: 0 }}>
          {row.name}
          {row.detail && (
            <Typography
              component="span"
              variant="body2"
              color="text.secondary"
            >
              {" "}
              ({row.detail})
            </Typography>
          )}
        </Typography>
        <StatusChip label={chip.label} tone={chip.tone} appearance="soft" dot />
      </Stack>
      {row.provisioned ? (
        <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: "block" }}>
          A production instance is created for you — nothing to enter.
        </Typography>
      ) : (
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
            gap: 1.25,
            mt: 1.5,
          }}
        >
          {row.config.map((key, i) => (
            <TextField
              key={key.key}
              {...(focused && i === 0 && { autoFocus: true })}
              // The KEY labels the field; the description wraps below as
              // helper text instead of truncating in the floating label
              // (#401 feedback).
              label={key.key}
              {...(key.description && { helperText: key.description })}
              size="small"
              fullWidth
              // A secret's value is write-only here: masked while typing, and
              // never echoed back from anywhere (there is nowhere to echo it
              // back FROM — values go to the production data plane as sealed
              // secrets, not to the console).
              {...(key.secret && { type: "password" })}
              value={values[row.id]?.[key.key] ?? ""}
              onChange={(e) => onValueChange(row.id, key.key, e.target.value)}
              sx={{ "& input": { fontFamily: "monospace" } }}
            />
          ))}
        </Box>
      )}
    </Box>
  );
}

export function PromoteDialog({
  open,
  onClose,
  projectName,
  version,
  validation,
  rows,
  values,
  focusRowId,
  onValueChange,
  onPromote,
}: {
  open: boolean;
  onClose: () => void;
  projectName: string;
  /** The dev spec tag being promoted ("v1"). */
  version: string;
  /** Raw deploy.validation — the dialog derives its banner from it. */
  validation: string;
  rows: ConnectionRow[];
  values: ConnectionValues;
  /** The connection a Configure on the board asked for (ADR-0032): the
   *  dialog opens with that connection's first field focused. */
  focusRowId?: string;
  onValueChange: (rowId: string, key: string, value: string) => void;
  /** Called when Promote is pressed with every required value set. */
  onPromote: () => void;
}) {
  const verdict = validationView(validation);
  const needing = rows.filter((row) => !row.provisioned).length;
  const ready = allConnectionsSet(rows, values);

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle sx={{ pr: 6 }}>
        Promote {version} to production
        <IconButton
          aria-label="Close"
          onClick={onClose}
          size="small"
          sx={{ position: "absolute", right: 12, top: 12 }}
        >
          <X size={18} />
        </IconButton>
      </DialogTitle>
      <DialogContent>
        <Stack spacing={2}>
          {verdict && (
            <Box
              sx={(theme) => {
                const main =
                  verdict.tone === "ghost" || verdict.tone === "neutral"
                    ? theme.palette.text.secondary
                    : theme.palette[verdict.tone].main;
                return {
                  border: `1px solid ${alpha(main, 0.35)}`,
                  bgcolor: alpha(main, 0.06),
                  borderRadius: 2,
                  px: 1.75,
                  py: 1.25,
                  display: "flex",
                  alignItems: "center",
                  gap: 1.25,
                };
              }}
            >
              <Typography variant="body2" color="text.secondary" sx={{ flexGrow: 1 }}>
                {verdictSentence(verdict.label, verdict.tone)}
              </Typography>
              <LinkButton
                to="/projects/$projectName/validations"
                params={{ projectName }}
                size="small"
                color="inherit"
                endIcon={<ArrowUpRight size={14} aria-hidden />}
                sx={{ flexShrink: 0, fontWeight: 500 }}
              >
                View report
              </LinkButton>
            </Box>
          )}
          {rows.length > 0 ? (
            <>
              <Typography variant="body2" color="text.secondary">
                {needing > 0
                  ? `${needing} connection${needing === 1 ? " needs" : "s need"} production values. Dev credentials never travel to production.`
                  : "Every connection is provisioned by the platform — nothing to enter."}
              </Typography>
              <Stack spacing={1.5}>
                {rows.map((row) => (
                  <ConnectionCard
                    key={row.id}
                    row={row}
                    values={values}
                    focused={row.id === focusRowId}
                    onValueChange={onValueChange}
                  />
                ))}
              </Stack>
            </>
          ) : (
            <Typography variant="body2" color="text.secondary">
              This design declares no connections — there is no live
              configuration to collect.
            </Typography>
          )}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2.5 }}>
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ flexGrow: 1, minWidth: 0 }}
        >
          Values are kept for this session only — promotion isn't connected to
          the platform yet.
        </Typography>
        <Button onClick={onClose} variant="outlined" color="inherit">
          Cancel
        </Button>
        {/* A disabled control swallows its title, so the tooltip that explains
            WHY it is disabled lives on a wrapper the pointer still reaches. */}
        <span
          {...(!ready && { title: "Enabled when all required values are set" })}
        >
          <Button variant="contained" disabled={!ready} onClick={onPromote}>
            Promote
          </Button>
        </span>
      </DialogActions>
    </Dialog>
  );
}
