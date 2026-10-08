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

import { useCallback, useState, type ReactNode } from "react";
import { useNavigate } from "@tanstack/react-router";
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Skeleton,
  TextField,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { CardFrame } from "../../shell/components/CardFrame";
import { LeaveGuard } from "../../shell/components/LeaveGuard";
import { PHONE } from "../../shell/layout";
import {
  useCreateSkill,
  useDeleteSkill,
  useSetSkillEnabled,
  useSkill,
  useSkillUpdates,
  useUpdateSkill,
} from "../api/skills";
import { draftOf, draftProblems, isDirty, skillMdOf, type SavedSkill, type SkillDraft } from "../model/skillDraft";
import { splitFrontmatter } from "../model/skillMd";
import { inConflict, normalizeKind, takeableUpdates } from "../model/skills";
import { KindTag, SkillTags } from "./SkillsPage";
import { SkillBodyEditor } from "./SkillBodyEditor";

// A Skill card, over the Skills Page (`/skills/$name`; a new one at
// `/skills/new`): the skill's name and one-line description as fields, its
// body in a document editor like the Spec card's (not collaborative: there
// is no room), and one draft with one Save that writes the whole skill.
// Enable or Disable, and Delete, sit in the header.
//
// It sets no Turn scope: the skill agent, which would edit the draft from the
// chat, does not exist yet, so the org's chat beside it stays as it is.

type SkillDetail = components["schemas"]["SkillDetailBody"];

export function SkillCard({ name }: { name: string | null }) {
  const navigate = useNavigate();
  const close = useCallback(() => void navigate({ to: "/skills" }), [navigate]);
  const detail = useSkill(name ?? "", name !== null);

  if (name === null) return <SkillCardBody key="new" detail={null} onClose={close} />;
  if (detail.data) return <SkillCardBody key={`${detail.data.name}:${detail.data.contentSha}`} detail={detail.data} onClose={close} />;
  return (
    <CardFrame name={`Skill ${name}`} heading={<Heading title={name} />} closeHint="Back to Skills" onClose={close}>
      {detail.isError ? (
        <Alert severity="error" action={<Button onClick={() => void detail.refetch()}>Retry</Button>}>
          {detail.error.message}
        </Alert>
      ) : (
        <Skeleton variant="rounded" height={320} />
      )}
    </CardFrame>
  );
}

function Heading({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <Box sx={{ flex: 1, minWidth: 0, display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
      <Typography component="h2" noWrap sx={{ fontSize: "1rem", fontWeight: 600, minWidth: 0 }}>
        {title}
      </Typography>
      {children}
    </Box>
  );
}

function savedOf(detail: SkillDetail | null): SavedSkill | null {
  return detail && { name: detail.name, description: detail.description, skillMd: detail.skillMd };
}

/** The card for one version of a skill (or a new one): a newer version saved is a fresh card, its draft clean. */
function SkillCardBody({ detail, onClose }: { detail: SkillDetail | null; onClose: () => void }) {
  const navigate = useNavigate();
  const saved = savedOf(detail);
  const isNew = detail === null;
  const editable = detail?.editable ?? true;
  const [draft, setDraft] = useState<SkillDraft>(() => draftOf(saved));
  const [tried, setTried] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const updates = useSkillUpdates();
  const create = useCreateSkill();
  const update = useUpdateSkill(detail?.name ?? "");
  const save = isNew ? create : update;

  const dirty = editable && isDirty(draft, saved);
  const problems = draftProblems(draft, isNew);
  const shownProblems = tried ? problems : {};
  const conflict = detail !== null && inConflict(updates.data ?? [], detail.name);
  const waitingUpdate = detail !== null && takeableUpdates(updates.data ?? []).includes(detail.name);

  const onSave = () => {
    setTried(true);
    if (!dirty || Object.keys(problems).length > 0 || save.isPending) return;
    const skillMd = skillMdOf(draft, saved);
    if (isNew) {
      const name = draft.name.trim();
      create.mutate(
        { name, skillMd, references: {} },
        { onSuccess: () => void navigate({ to: "/skills/$name", params: { name }, replace: true, ignoreBlocker: true }) },
      );
    } else {
      // The files beside SKILL.md go back as they were: the card does not edit them.
      update.mutate({ skillMd, references: detail.references });
    }
  };

  const title = isNew ? "New skill" : detail.name;
  return (
    <CardFrame
      name={isNew ? "New skill" : `Skill ${detail.name}`}
      heading={
        <Heading title={title}>
          {detail && (
            <>
              <KindTag kind={normalizeKind(detail.kind)} />
              <SkillTags disabled={!detail.enabled} toReview={conflict} />
            </>
          )}
        </Heading>
      }
      actions={
        <>
          {detail && <EnableButton detail={detail} />}
          {detail?.deletable && (
            <Button color="error" onClick={() => setDeleting(true)}>
              Delete
            </Button>
          )}
          {editable && (
            <Button variant="contained" disabled={!dirty || save.isPending} onClick={onSave}>
              {save.isPending ? "Saving…" : isNew ? "Create" : "Save"}
            </Button>
          )}
        </>
      }
      closeHint="Back to Skills"
      onClose={onClose}
      openKey={detail?.name ?? "new"}
    >
      <Box sx={{ display: "flex", flexDirection: "column", gap: 2.5, maxWidth: "72ch" }}>
        {conflict && (
          <Alert severity="warning">
            <strong>The platform changed this skill too.</strong> Your organization edited it, and the platform has
            since shipped a newer version of its own. Agents keep using yours. Comparing the two and choosing needs a
            platform update that is not available yet.
          </Alert>
        )}
        {waitingUpdate && (
          <Alert severity="info">
            The platform has a newer version of this skill. Take updates on the Skills page brings it in, as long as
            you have not edited the skill.
          </Alert>
        )}
        {!editable && (
          <Typography variant="body2" color="text.secondary">
            The platform&apos;s own skill: it reads here, and only the platform changes it. Disabling it withholds it
            from the agents.
          </Typography>
        )}
        {save.isError && <Alert severity="error">{save.error.message}</Alert>}
        <Box sx={{ display: "flex", gap: 2, [PHONE]: { flexDirection: "column" } }}>
          <TextField
            label="Name"
            value={draft.name}
            onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
            disabled={!isNew}
            required={isNew}
            error={Boolean(shownProblems.name)}
            helperText={shownProblems.name ?? (isNew ? "Lowercase, words joined by hyphens. It cannot change later." : undefined)}
            sx={{ width: 260, flexShrink: 0, [PHONE]: { width: "100%" } }}
          />
          <TextField
            label="Description"
            value={draft.description}
            onChange={(e) => setDraft((d) => ({ ...d, description: e.target.value }))}
            disabled={!editable}
            required={editable}
            fullWidth
            error={Boolean(shownProblems.description)}
            helperText={shownProblems.description ?? (editable ? "One line: what the skill is for, and when an agent should use it." : undefined)}
          />
        </Box>
        <Box sx={{ borderTop: 1, borderColor: "divider", pt: 2 }}>
          <SkillBodyEditor
            markdown={detail ? splitFrontmatter(detail.skillMd).body : ""}
            editable={editable}
            onChange={(body) => setDraft((d) => ({ ...d, body }))}
            docKey={detail ? `${detail.name}:${detail.contentSha}` : "new"}
          />
        </Box>
        {detail && <OtherFiles detail={detail} />}
      </Box>
      <LeaveGuard dirty={dirty} what={isNew ? "this new skill" : detail.name} />
      {deleting && detail && <DeleteSkillPanel name={detail.name} onClose={() => setDeleting(false)} />}
    </CardFrame>
  );
}

/** The files beside SKILL.md: listed, kept as they are on Save. */
function OtherFiles({ detail }: { detail: SkillDetail }) {
  const files = [...Object.keys(detail.references ?? {}), ...detail.binaryReferences].sort();
  if (files.length === 0) return null;
  return (
    <Typography variant="caption" color="text.secondary">
      Also in this skill, unchanged by Save: {files.join(", ")}.
    </Typography>
  );
}

function EnableButton({ detail }: { detail: SkillDetail }) {
  const setEnabled = useSetSkillEnabled(detail.name);
  // The coding or validation run reads a required skill as its workflow and
  // cannot start without it; the platform refuses to disable one.
  if (detail.required) {
    return (
      <Tooltip title="Every build needs this skill: it carries a run's workflow.">
        <span>
          <Button disabled>Disable</Button>
        </span>
      </Tooltip>
    );
  }
  return (
    <Tooltip title={setEnabled.isError ? setEnabled.error.message : detail.enabled ? "Withhold it from the agents. Nothing in it changes." : "Give it back to the agents."}>
      <span>
        <Button
          color={setEnabled.isError ? "error" : "inherit"}
          disabled={setEnabled.isPending}
          onClick={() => setEnabled.mutate(!detail.enabled)}
        >
          {detail.enabled ? "Disable" : "Enable"}
        </Button>
      </span>
    </Tooltip>
  );
}

function DeleteSkillPanel({ name, onClose }: { name: string; onClose: () => void }) {
  const navigate = useNavigate();
  const del = useDeleteSkill(name);
  return (
    <Dialog open onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>Delete {name}?</DialogTitle>
      <DialogContent>
        <DialogContentText>
          It goes from your organization&apos;s skills repo, and the agents stop reading it. Unsaved changes go with it.
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
          onClick={() => del.mutate(undefined, { onSuccess: () => void navigate({ to: "/skills", ignoreBlocker: true }) })}
        >
          {del.isPending ? "Deleting…" : "Delete"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
