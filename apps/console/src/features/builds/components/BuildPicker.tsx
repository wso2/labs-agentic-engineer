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

import { useId, useMemo, useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import {
  Alert,
  AlertTitle,
  Box,
  Button,
  Checkbox,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Typography,
} from "@wso2/oxygen-ui";
import { chatStore } from "../../agent-chat/useProjectChat";
import { BuildRefusedError, useStartBuild } from "../api/builds";
import { BuildPickerContext, useBuildOffer } from "../buildPicker";
import type { BuildSelection } from "../buildSelection";
import { startedNote } from "../model/nextSteps";
import {
  canStart,
  defaultSelection,
  pickerSummary,
  pickWarnings,
  selectionNames,
  togglePick,
  type BuildOffer,
  type PickRow,
} from "../model/picker";

/** The project's build picker: its context for the entry points, and the dialog while it is open. */
export function BuildPickerHost({ projectName, children }: { projectName: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const controls = useMemo(() => ({ open: () => setOpen(true) }), []);
  return (
    <BuildPickerContext.Provider value={controls}>
      {children}
      {open && <BuildPicker projectName={projectName} onClose={() => setOpen(false)} />}
    </BuildPickerContext.Provider>
  );
}

const rowSx = { px: 1.5, py: 1.125, fontSize: "0.8125rem" } as const;

function Row({ row, checked, onToggle }: { row: PickRow; checked: boolean; onToggle: (on: boolean) => void }) {
  const detailId = useId();
  const title = row.kind === "feature" ? `${row.id} ${row.name}` : `${row.id} ${row.text}`;
  if (row.state === "fixed") {
    // Nothing to pick: the line says why, where the box would be.
    return (
      <Box sx={{ ...rowSx, pl: 5.25, color: "text.secondary" }}>
        {title} · {row.detail.join(" · ")}
      </Box>
    );
  }
  const off = row.state === "disabled";
  return (
    <Box
      component="label"
      sx={{
        ...rowSx,
        display: "grid",
        gridTemplateColumns: "22px 1fr",
        columnGap: 1,
        rowGap: 0.25,
        alignItems: "start",
        cursor: off ? "default" : "pointer",
        color: off ? "text.secondary" : "text.primary",
      }}
    >
      <Checkbox
        size="small"
        checked={checked}
        disabled={off}
        onChange={(e) => onToggle(e.target.checked)}
        slotProps={{ input: { "aria-describedby": detailId } }}
        sx={{ p: 0, mt: 0.125 }}
      />
      <Box component="span" sx={{ fontWeight: 600 }}>
        {title}
      </Box>
      <Box
        component="span"
        id={detailId}
        sx={{ gridColumn: 2, fontSize: "0.78rem", color: row.warn ? "warning.main" : "text.secondary" }}
      >
        {row.detail.join(" · ")}
      </Box>
    </Box>
  );
}

function Summary({ offer }: { offer: BuildOffer }) {
  return (
    <Box
      component="dl"
      sx={{
        m: 0,
        border: 1,
        borderColor: "divider",
        borderRadius: 2.5,
        bgcolor: "background.default",
        px: 1.5,
        py: 1.25,
        fontFamily: "monospace",
        fontSize: "0.75rem",
        display: "grid",
        gridTemplateColumns: "max-content 1fr",
        columnGap: 1.5,
        rowGap: 0.25,
        "& dd": { m: 0 },
      }}
    >
      {pickerSummary(offer).map((line) => (
        <Box key={line.label} sx={{ display: "contents" }}>
          <Box component="dt" sx={{ color: "text.secondary" }}>
            {line.label}
          </Box>
          <Box component="dd">{line.text}</Box>
        </Box>
      ))}
    </Box>
  );
}

function Refusal({ error }: { error: Error }) {
  const problems = error instanceof BuildRefusedError ? error.problems : [];
  if (problems.length === 0) return <Alert severity="error">{error.message}</Alert>;
  return (
    <Alert severity="warning">
      <AlertTitle>Not ready to build yet</AlertTitle>
      <Box component="ul" sx={{ m: 0, pl: 2 }}>
        {problems.map((p, i) => (
          <li key={`${p.field ?? ""}:${i}`}>{p.message}</li>
        ))}
      </Box>
    </Alert>
  );
}

/**
 * "What goes into v1?": pick designed features, see what else the build runs,
 * and start it. Once it has started the Builds card opens and the chat says
 * what is building.
 */
function BuildPicker({ projectName, onClose }: { projectName: string; onClose: () => void }) {
  const offer = useBuildOffer(projectName);
  const start = useStartBuild(projectName);
  const navigate = useNavigate();
  const [chosen, setChosen] = useState<BuildSelection | null>(null);
  const titleId = useId();

  if (!offer) return null;
  // What the user ticked, less anything the live spec has since taken out of
  // the picker (a design gone out of date while it was open).
  const offered = new Set(offer.rows.filter((r) => r.state === "offered").map((r) => r.id));
  const base = chosen ?? defaultSelection(offer);
  const selection: BuildSelection = {
    features: base.features.filter((id) => offered.has(id)),
    productWide: base.productWide.filter((id) => offered.has(id)),
  };
  const warnings = pickWarnings(offer, selection);
  const toggle = (id: string, on: boolean) => setChosen(togglePick(offer, selection, id, on));
  const ticked = (row: PickRow) =>
    row.kind === "feature" ? selection.features.includes(row.id) : selection.productWide.includes(row.id);

  const startBuild = () => {
    start.mutate(selection, {
      onSuccess: (tag) => {
        const version = tag || offer.version;
        onClose();
        void navigate({ to: "/projects/$projectName/builds/$version", params: { projectName, version } });
        const note = startedNote(version, selectionNames(offer, selection), null);
        chatStore.post(projectName, note.text, note.actions);
      },
    });
  };

  return (
    <Dialog open onClose={start.isPending ? undefined : onClose} maxWidth="sm" fullWidth aria-labelledby={titleId}>
      <DialogTitle id={titleId}>What goes into {offer.version}?</DialogTitle>
      <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 1.75 }}>
        <Typography variant="body2" color="text.secondary">
          Pick designed features. Whatever they need is pulled in. The spec is frozen as {offer.version} when the build
          starts; you can keep editing afterwards.
        </Typography>
        <Box
          role="group"
          aria-label="Features"
          sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, "& > * + *": { borderTop: 1, borderColor: "divider" } }}
        >
          {offer.rows.map((row) => (
            <Row key={row.id} row={row} checked={ticked(row)} onToggle={(on) => toggle(row.id, on)} />
          ))}
        </Box>
        {warnings.map((w) => (
          <Alert key={w} severity="warning">
            {w}
          </Alert>
        ))}
        <Summary offer={offer} />
        {start.error && <Refusal error={start.error} />}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={start.isPending}>
          Cancel
        </Button>
        <Button variant="contained" loading={start.isPending} disabled={!canStart(selection)} onClick={startBuild}>
          Start build
        </Button>
      </DialogActions>
    </Dialog>
  );
}
