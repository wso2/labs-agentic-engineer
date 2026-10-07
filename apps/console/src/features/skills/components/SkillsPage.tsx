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
import { createLink } from "@tanstack/react-router";
import { Alert, Box, Button, ButtonBase, SearchBar, Skeleton, Tooltip, Typography } from "@wso2/oxygen-ui";
import { Plus, RefreshCw, Sparkles, Upload } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "../../../components/EmptyState";
import { Segmented } from "../../design/components/Segmented";
import { Tag } from "../../spec/components/Tag";
import { useSkills, useSkillUpdates, useSyncSkills } from "../api/skills";
import { KIND_BLURB, KIND_LABEL, SKILL_KINDS, skillRows, takeableUpdates, type KindFilter, type SkillRow } from "../model/skills";
import { ImportSkillPanel } from "./ImportSkillPanel";

// The Skills Page: the org's skill library, one list, searched and filtered
// by kind. A skill opens its Skill card over the page; New skill opens an
// empty one. Import is a Panel; Take updates refreshes, in one go, every
// skill the platform moved and the org never edited.

const RowLink = createLink(ButtonBase);
const ButtonLink = createLink(Button);

const KIND_FILTERS: { value: KindFilter; label: string }[] = [
  { value: "all", label: "All" },
  ...SKILL_KINDS.map((k) => ({ value: k, label: KIND_LABEL[k] })),
];

/** The kind's tag, with what the kind means on hover. */
export function KindTag({ kind }: { kind: SkillRow["kind"] }) {
  return (
    <Tooltip title={KIND_BLURB[kind]} describeChild>
      <Box component="span" sx={{ display: "inline-flex" }}>
        <Tag tone={null}>{KIND_LABEL[kind]}</Tag>
      </Box>
    </Tooltip>
  );
}

/** The tags a skill carries beside its kind. */
export function SkillTags({ disabled, toReview }: { disabled: boolean; toReview: boolean }) {
  return (
    <>
      {disabled && <Tag tone={null}>Disabled</Tag>}
      {toReview && <Tag tone="warning">Update to review</Tag>}
    </>
  );
}

function Row({ row }: { row: SkillRow }) {
  return (
    <RowLink
      to="/skills/$name"
      params={{ name: row.skill.name }}
      sx={{
        display: "flex",
        alignItems: "center",
        gap: 1.5,
        justifyContent: "stretch",
        textAlign: "start",
        px: 1.75,
        py: 1.25,
        "& + &": { borderTop: 1, borderColor: "divider" },
        "&:hover": { bgcolor: "action.hover" },
      }}
    >
      <Box component="span" sx={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column", gap: 0.25 }}>
        <Typography component="span" variant="body2" sx={{ fontWeight: 600, color: row.disabled ? "text.secondary" : "text.primary" }}>
          {row.skill.name}
        </Typography>
        <Typography component="span" variant="caption" color="text.secondary" noWrap>
          {row.skill.description}
        </Typography>
      </Box>
      <Box component="span" sx={{ display: "flex", flexWrap: "wrap", justifyContent: "flex-end", gap: 0.75, flexShrink: 0 }}>
        <SkillTags disabled={row.disabled} toReview={row.toReview} />
        <KindTag kind={row.kind} />
      </Box>
    </RowLink>
  );
}

/** Take updates (N): one call refreshes them all; it takes no selection. */
function TakeUpdatesButton({ names, pending, onTake }: { names: string[]; pending: boolean; onTake: () => void }) {
  if (names.length === 0) return null;
  return (
    <Tooltip describeChild title={`Refreshes ${names.join(", ")} to the platform's newest version. Skills you edited are never touched.`}>
      <Button variant="outlined" startIcon={<RefreshCw size={16} />} disabled={pending} onClick={onTake}>
        {pending ? "Taking updates…" : `Take updates (${names.length})`}
      </Button>
    </Tooltip>
  );
}

export function SkillsPage() {
  const skills = useSkills();
  const updates = useSkillUpdates();
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState<KindFilter>("all");
  const [importing, setImporting] = useState(false);
  const sync = useSyncSkills();

  let body: ReactNode;
  if (skills.isError) {
    body = (
      <Alert severity="error" action={<Button onClick={() => void skills.refetch()}>Retry</Button>}>
        {skills.error.message}
      </Alert>
    );
  } else if (!skills.data) {
    body = <Skeleton variant="rounded" height={240} />;
  } else if (skills.data.skills.length === 0) {
    body = (
      <EmptyState
        icon={<Sparkles size={48} />}
        title="No skills yet"
        description="Skills are what the agents follow as they design and build. Write one, or import one from the AgentSkills ecosystem."
      />
    );
  } else {
    const rows = skillRows(skills.data.skills, updates.data ?? [], { query, kind });
    body =
      rows.length === 0 ? (
        <Typography variant="body2" color="text.secondary" sx={{ py: 2 }}>
          No skills match.
        </Typography>
      ) : (
        <Box sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, overflow: "hidden" }}>
          {rows.map((row) => (
            <Row key={row.skill.name} row={row} />
          ))}
        </Box>
      );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 2.5, maxWidth: 960 }}>
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 2, flexWrap: "wrap" }}>
        <Box sx={{ flex: 1, minWidth: 240 }}>
          <Typography component="h1" variant="h4" sx={{ fontWeight: 600 }}>
            Skills
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            What the agents follow as they design and build: the platform&apos;s, your organization&apos;s, and those you imported.
          </Typography>
        </Box>
        <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap", justifyContent: "flex-end" }}>
          <TakeUpdatesButton names={takeableUpdates(updates.data ?? [])} pending={sync.isPending} onTake={() => sync.mutate()} />
          <Button variant="outlined" startIcon={<Upload size={16} />} onClick={() => setImporting(true)}>
            Import
          </Button>
          <ButtonLink to="/skills/new" variant="contained" startIcon={<Plus size={16} />}>
            New skill
          </ButtonLink>
        </Box>
      </Box>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, flexWrap: "wrap" }}>
        <Box sx={{ flex: 1, minWidth: 220, maxWidth: 420 }}>
          <SearchBar
            size="small"
            fullWidth
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search skills"
            slotProps={{ htmlInput: { "aria-label": "Search skills" } }}
          />
        </Box>
        <Segmented value={kind} options={KIND_FILTERS} onChange={setKind} label="Kind" />
      </Box>
      {sync.isSuccess && (
        <Alert severity="success" onClose={() => sync.reset()}>
          {sync.data.updated === 1
            ? "Took 1 update: that skill reads as the platform's newest."
            : `Took ${sync.data.updated} updates: those skills read as the platform's newest.`}
        </Alert>
      )}
      {sync.isError && (
        <Alert severity="error" onClose={() => sync.reset()}>
          {sync.error.message}
        </Alert>
      )}
      {body}
      {importing && <ImportSkillPanel repoUrl={skills.data?.repoUrl} onClose={() => setImporting(false)} />}
    </Box>
  );
}
