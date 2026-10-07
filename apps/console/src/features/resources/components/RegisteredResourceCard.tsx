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

import { useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import {
  Alert,
  Box,
  Button,
  Checkbox,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControlLabel,
  IconButton,
  Skeleton,
  TextField,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { Plus, Trash2 } from "@wso2/oxygen-ui-icons-react";
import type { components } from "../../../generated/aep-api";
import { useEnvironments } from "../../deploy/api/deploy";
import { LeaveGuard } from "../../shell/components/LeaveGuard";
import { PHONE } from "../../shell/layout";
import { useDeleteExternalResource, useRegisterExternalResource, useUpdateExternalResource } from "../api/resources";
import {
  cellKey,
  draftOfResource,
  isResourceDirty,
  resourceProblems,
  resourceRequest,
  type DraftKey,
  type ResourceDraft,
} from "../model/resourceDraft";
import { deleteBlocker } from "../model/resources";
import { ContractField } from "./ContractField";
import { ResourceFrame } from "./ResourceCard";
import { DocsList, Section, UsedBy } from "./ResourceSections";
import { useProjectLabel } from "./ResourcesPage";

// A Registered External resource's card, the one kind the organization
// edits here: name, provider, description, contract document, keys and a
// value for each in every environment, and consumption instructions. One
// draft, one Save (register for a new one, update for a saved one); Delete in
// the header asks first. Its name and its keys' identity are fixed once
// registered: the platform refuses to change them.

type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];

export function RegisteredResourceCard({ saved }: { saved: ExternalResourceDTO | null }) {
  const navigate = useNavigate();
  const environments = useEnvironments();
  const label = useProjectLabel();
  const [draft, setDraft] = useState<ResourceDraft>(() => draftOfResource(saved));
  const [tried, setTried] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const register = useRegisterExternalResource();
  const update = useUpdateExternalResource(saved?.name ?? "");
  const save = saved ? update : register;

  const envs = environments.data ?? [];
  const envNames = envs.map((e) => e.name);
  const dirty = isResourceDirty(draft, saved);
  const problems = resourceProblems(draft, { saved, environments: envNames });
  const shown = tried ? problems : null;
  const set = <K extends keyof ResourceDraft>(field: K, value: ResourceDraft[K]) => setDraft((d) => ({ ...d, [field]: value }));
  const setKey = (index: number, next: Partial<DraftKey>) =>
    setDraft((d) => ({ ...d, keys: d.keys.map((k, i) => (i === index ? { ...k, ...next } : k)) }));

  const onSave = () => {
    setTried(true);
    if (!dirty || problems || save.isPending || !environments.data) return;
    const body = resourceRequest(draft, { saved, environments: envNames });
    save.mutate(body, {
      onSuccess: (record) => {
        if (!saved) void navigate({ to: "/resources/$name", params: { name: record.name }, replace: true, ignoreBlocker: true });
      },
    });
  };
  const blockedDelete = saved ? deleteBlocker(saved) : null;

  return (
    <ResourceFrame
      title={saved ? saved.name : "New resource"}
      entry={saved ? { kind: "registered", name: saved.name, resource: saved } : null}
      actions={
        <>
          {saved && (
            <Tooltip title={blockedDelete ?? ""}>
              <span>
                <Button color="error" disabled={blockedDelete !== null} onClick={() => setDeleting(true)}>
                  Delete
                </Button>
              </span>
            </Tooltip>
          )}
          <Button variant="contained" disabled={!dirty || save.isPending} onClick={onSave}>
            {save.isPending ? "Saving…" : saved ? "Save" : "Register"}
          </Button>
        </>
      }
    >
      <Box sx={{ display: "flex", flexDirection: "column", gap: 3, maxWidth: 760 }}>
        {!saved && (
          <Typography variant="body2" color="text.secondary">
            A service outside the platform, registered once for the whole organization: its values are held here, and
            any project that needs it reuses it.
          </Typography>
        )}
        {save.isError && <Alert severity="error">{save.error.message}</Alert>}
        <Box sx={{ display: "flex", gap: 2, [PHONE]: { flexDirection: "column" } }}>
          <TextField
            label="Name"
            required
            value={draft.name}
            disabled={saved !== null}
            onChange={(e) => set("name", e.target.value)}
            error={Boolean(shown?.name)}
            helperText={shown?.name ?? (saved ? undefined : "What projects call it: currency-service. It cannot change later.")}
            sx={{ flex: 1 }}
          />
          <TextField
            label="Provider"
            required
            value={draft.provider}
            onChange={(e) => set("provider", e.target.value)}
            error={Boolean(shown?.provider)}
            helperText={shown?.provider ?? "The service it is: Open Exchange Rates."}
            sx={{ flex: 1 }}
          />
        </Box>
        <TextField
          label="Description"
          required
          multiline
          minRows={2}
          value={draft.description}
          onChange={(e) => set("description", e.target.value)}
          error={Boolean(shown?.description)}
          helperText={shown?.description ?? "What it is."}
        />
        <Section title="Contract document">
          <ContractField
            current={saved?.contract}
            value={draft.contract}
            onChange={(next) => set("contract", next)}
            error={shown?.contract}
          />
        </Section>
        <Section title="Keys">
          <Typography variant="caption" color="text.secondary">
            {saved
              ? "What every consumer reads. A key and whether it is secret are fixed once registered."
              : "What every consumer reads, by name. A secret's value is never shown again."}
          </Typography>
          {shown?.keys && (
            <Typography variant="caption" color="error">
              {shown.keys}
            </Typography>
          )}
          {draft.keys.map((k, i) => (
            <Box key={i} sx={{ display: "flex", alignItems: "flex-start", gap: 1, [PHONE]: { flexWrap: "wrap" } }}>
              <TextField
                size="small"
                label="Key"
                value={k.key}
                disabled={saved !== null}
                onChange={(e) => setKey(i, { key: e.target.value })}
                error={Boolean(shown?.keyRows[i]?.key)}
                helperText={shown?.keyRows[i]?.key}
                sx={{ width: 200, fontFamily: "monospace" }}
              />
              <TextField
                size="small"
                label="What it is"
                value={k.description}
                onChange={(e) => setKey(i, { description: e.target.value })}
                error={Boolean(shown?.keyRows[i]?.description)}
                helperText={shown?.keyRows[i]?.description}
                sx={{ flex: 1, minWidth: 180 }}
              />
              <FormControlLabel
                label="Secret"
                disabled={saved !== null}
                control={<Checkbox size="small" checked={k.secret} onChange={(e) => setKey(i, { secret: e.target.checked })} />}
              />
              {!saved && (
                <IconButton
                  aria-label={`Remove ${k.key || "key"}`}
                  disabled={draft.keys.length === 1}
                  onClick={() => set("keys", draft.keys.filter((_, j) => j !== i))}
                >
                  <Trash2 size={16} />
                </IconButton>
              )}
            </Box>
          ))}
          {!saved && (
            <Box>
              <Button size="small" startIcon={<Plus size={14} />} onClick={() => set("keys", [...draft.keys, { key: "", description: "", secret: false }])}>
                Add a key
              </Button>
            </Box>
          )}
        </Section>
        <Section title="Values per environment">
          {environments.isError ? (
            <Alert severity="error">{environments.error.message}</Alert>
          ) : !environments.data ? (
            <Skeleton variant="rounded" height={80} />
          ) : envs.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              The organization has no environments yet, so there is nowhere to hold a value.
            </Typography>
          ) : (
            <ValuesGrid draft={draft} saved={saved} envs={envs} problems={shown?.values ?? {}} onValue={(k, v) => set("values", { ...draft.values, [k]: v })} />
          )}
        </Section>
        <TextField
          label="Consumption instructions"
          required
          multiline
          minRows={3}
          value={draft.consumptionInstructions}
          onChange={(e) => set("consumptionInstructions", e.target.value)}
          error={Boolean(shown?.consumptionInstructions)}
          helperText={shown?.consumptionInstructions ?? "How a project should use it, beyond what it is: limits, caching, which endpoints. The coding agent reads this."}
        />
        {(saved?.resourceDocs ?? []).length > 0 && (
          <Section title="Further reading">
            <DocsList docs={saved?.resourceDocs ?? []} />
          </Section>
        )}
        {saved && (
          <Section title="Used by">
            <UsedBy consumers={saved.consumers ?? []} label={label} />
          </Section>
        )}
      </Box>
      <LeaveGuard dirty={dirty} what={saved ? saved.name : "this new resource"} />
      {deleting && saved && <DeleteResourcePanel name={saved.name} onClose={() => setDeleting(false)} />}
    </ResourceFrame>
  );
}

type Environment = components["schemas"]["EnvironmentDTO"];

/** One row per key, one field per environment; a secret already held reads "kept" until a new value is typed. */
function ValuesGrid({
  draft,
  saved,
  envs,
  problems,
  onValue,
}: {
  draft: ResourceDraft;
  saved: ExternalResourceDTO | null;
  envs: readonly Environment[];
  problems: Record<string, string>;
  onValue: (cell: string, value: string) => void;
}) {
  const keys = draft.keys.filter((k) => k.key.trim());
  if (keys.length === 0) return <Typography variant="body2" color="text.secondary">Name a key first.</Typography>;
  const held = (env: string, key: string) =>
    (saved?.envCells ?? []).some((c) => c.environment === env && c.key === key && c.status === "configured");
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
      {keys.map((k) => (
        <Box key={k.key} sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
          <Typography variant="caption" sx={{ fontFamily: "monospace", fontWeight: 600 }}>
            {k.key}
          </Typography>
          <Box sx={{ display: "grid", gridTemplateColumns: `repeat(${Math.min(envs.length, 3)}, minmax(0, 1fr))`, gap: 1, [PHONE]: { gridTemplateColumns: "1fr" } }}>
            {envs.map((env) => {
              const cell = cellKey(env.name, k.key.trim());
              const kept = k.secret && held(env.name, k.key);
              return (
                <TextField
                  key={cell}
                  size="small"
                  label={env.displayName}
                  type={k.secret ? "password" : "text"}
                  autoComplete="off"
                  value={draft.values[cell] ?? ""}
                  {...(kept ? { placeholder: "Kept" } : {})}
                  onChange={(e) => onValue(cell, e.target.value)}
                  error={Boolean(problems[cell])}
                  helperText={problems[cell] ?? (kept ? "Blank keeps the value held" : undefined)}
                  slotProps={{ inputLabel: { shrink: true } }}
                />
              );
            })}
          </Box>
        </Box>
      ))}
    </Box>
  );
}

function DeleteResourcePanel({ name, onClose }: { name: string; onClose: () => void }): ReactNode {
  const navigate = useNavigate();
  const del = useDeleteExternalResource(name);
  return (
    <Dialog open onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>Delete {name}?</DialogTitle>
      <DialogContent>
        <DialogContentText>
          The organization stops holding it and its values. A project that needs the service later defines its own.
        </DialogContentText>
        {del.isError && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {del.error.message}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button
          color="error"
          variant="contained"
          disabled={del.isPending}
          onClick={() => del.mutate(undefined, { onSuccess: () => void navigate({ to: "/resources", ignoreBlocker: true }) })}
        >
          {del.isPending ? "Deleting…" : "Delete"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
