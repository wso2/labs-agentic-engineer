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

import { useEffect, useMemo, useState } from "react";
import {
  Box,
  Button,
  Chip,
  Divider,
  Drawer,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { useHasPermission } from "../../../auth/permissions";
import type { components } from "../../../generated/aep-api";
import type { DependencyResolutionIntent } from "../../projects/lib/dependencyResolutionMessage.js";
import { approvalInputsFor } from "../lib/buildInputs";

type PreflightItem = components["schemas"]["PreflightItem"];
type BuildInputItem = components["schemas"]["BuildInputItem"];

/**
 * The RESOLUTION blockers — the only thing this drawer is for. A dependency
 * whose identity the design cannot settle blocks the version cut: nothing
 * downstream can be authored for a dependency nobody can name. Everything else
 * preflight reports (external config values, platform-resource approvals) is
 * collected on the Builds page while the coding agent runs and enforced at the
 * deploy gate, so it never opens this drawer.
 */
const RESOLUTION_KINDS = new Set([
  "external-ambiguous",
  "external-unresolved",
  "external-spec",
  "org-service",
]);

function isResolutionKind(kind: PreflightItem["kind"]): boolean {
  return RESOLUTION_KINDS.has(kind);
}

/**
 * Kinds with NO local form: the drawer can only surface the reason and hand
 * off to chat (#252 Task 10, restoring the proceed gate Task 1 orphaned).
 * "external-spec" deliberately stays OUT of this set — it keeps its own local
 * form (paste a spec URL/content), which the build request carries as an
 * `external-spec` input and the BFF commits to HEAD before the tag is cut
 * (InputsCoordinator.ApplyPreTag), so Continue can satisfy it without chat.
 */
const CHAT_ONLY_KINDS = new Set(["external-ambiguous", "external-unresolved"]);

function isChatOnlyKind(kind: PreflightItem["kind"]): boolean {
  return CHAT_ONLY_KINDS.has(kind);
}

/** Per-group working state — only external-spec has anything to type. */
type ItemState = {
  /** external-spec: URL field. */
  specUrl: string;
  /** external-spec: pasted-content field. */
  specContent: string;
};

/**
 * #252 Task 15 — cross-component dependency dedupe key: two PreflightItems are
 * "the same shared dependency" when they carry the same `kind` and the same
 * `dependency` name. Task 14 lifted the ComponentType!=service preflight
 * guard, so a project-scoped shared dependency now surfaces one PreflightItem
 * PER consuming component — this key is what re-collapses those into one card.
 * (No resourceType qualifier is needed here: platform resources never reach
 * this drawer, which only ever renders resolution blockers.)
 */
function dependencyIdentity(item: PreflightItem): string {
  return `${item.kind}:${item.dependency}`;
}

/**
 * One rendered card after cross-component grouping (#252 Task 15). `items`
 * holds every underlying PreflightItem the card represents — length 1 for a
 * component-local dependency. `representative` (the first item, sorted by
 * component) drives the card; `usedBy` is the sorted, deduped list of
 * consuming components, rendered as the "Used by" indicator only when it has
 * 2+ entries.
 */
interface DependencyGroup {
  key: string;
  items: PreflightItem[];
  representative: PreflightItem;
  usedBy: string[];
}

/** Cross-component dedupe (#252 Task 15) — see dependencyIdentity above. */
export function groupPreflightItems(items: PreflightItem[]): DependencyGroup[] {
  const buckets = new Map<string, PreflightItem[]>();
  for (const item of items) {
    const key = dependencyIdentity(item);
    const bucket = buckets.get(key);
    if (bucket) bucket.push(item);
    else buckets.set(key, [item]);
  }

  const groups: DependencyGroup[] = [];
  for (const [key, bucketItems] of buckets) {
    const sorted = [...bucketItems].sort((a, b) =>
      a.component.localeCompare(b.component),
    );
    groups.push({
      key,
      items: sorted,
      representative: sorted[0]!,
      usedBy: [...new Set(sorted.map((i) => i.component))].sort(),
    });
  }
  return groups;
}

/** Fresh per-group state as if the drawer just opened. */
function seedState(): ItemState {
  return { specUrl: "", specContent: "" };
}

// #252 Task 15: keyed by DependencyGroup.key, not per-raw-item — a merged
// group's spec is pasted ONCE and shared by every underlying item it
// represents (see handleContinue's fan-out below).
function initialState(groups: DependencyGroup[]): Record<string, ItemState> {
  const state: Record<string, ItemState> = {};
  for (const group of groups) state[group.key] = seedState();
  return state;
}

/** True when this group no longer stands between the user and the build. */
function isSatisfied(item: PreflightItem, state: ItemState): boolean {
  // A chat-only blocker (ambiguous / needs-input) can ONLY be cleared by
  // resolving it in chat and the parent refetching preflight — once resolved,
  // it simply stops appearing in `items`. While it is present, Continue stays
  // disabled, so this never reports satisfied locally.
  if (isChatOnlyKind(item.kind)) return false;
  if (item.kind === "external-spec") {
    return state.specUrl.trim() !== "" || state.specContent.trim() !== "";
  }
  // org-service is an approval, never a Continue blocker: clicking Continue is
  // the consent, and the build publishes the cross-project endpoint.
  return true;
}

/**
 * The cross-component "Used by" indicator (#252 Task 15): rendered only for a
 * merged group (2+ consuming components) — a component-local dependency (the
 * common case) shows nothing extra.
 */
function UsedByLine({ usedBy }: { usedBy: string[] }) {
  if (usedBy.length < 2) return null;
  return (
    <Stack direction="row" spacing={0.5} alignItems="center" flexWrap="wrap">
      <Typography variant="caption" color="text.secondary">
        Used by:
      </Typography>
      {usedBy.map((name) => (
        <Chip key={name} size="small" variant="outlined" label={name} />
      ))}
    </Stack>
  );
}

/**
 * "Resolve via chat" — the one affordance every panel here shares (#252 Task
 * 17). Each item in this drawer is by definition NOT resolved, so the intent
 * is always RESOLVE; the reconsider/hamburger path belongs to the design view,
 * where an already-resolved dependency is what the user is looking at.
 */
function ResolveViaChatButton({
  item,
  onResolveViaChat,
}: {
  item: PreflightItem;
  onResolveViaChat?: ((item: PreflightItem) => void) | undefined;
}) {
  if (!onResolveViaChat) return null;
  return (
    <Button
      size="small"
      sx={{ alignSelf: "flex-start" }}
      onClick={() => onResolveViaChat(item)}
    >
      Resolve via chat
    </Button>
  );
}

/**
 * A blocker raised from the shared resolver (ambiguous / needs-input): no
 * local form can satisfy it — only "Resolve via chat", which seeds the
 * dependency-resolution turn. The parent refetches preflight when that turn
 * ends, so the item disappears from `items` once resolved.
 */
function BlockerPanel({
  item,
  usedBy,
  onResolveViaChat,
}: {
  item: PreflightItem;
  usedBy: string[];
  onResolveViaChat?: ((item: PreflightItem) => void) | undefined;
}) {
  return (
    <Stack spacing={1}>
      <Typography variant="subtitle1">{item.dependency}</Typography>
      <Typography variant="body2" color="text.secondary">
        {item.description}
      </Typography>
      <UsedByLine usedBy={usedBy} />
      <ResolveViaChatButton item={item} onResolveViaChat={onResolveViaChat} />
    </Stack>
  );
}

/**
 * An external dependency with no API spec yet. Unlike the other blockers this
 * one HAS a local form: a URL or pasted content rides along with the build
 * request as an `external-spec` input, which the BFF commits to HEAD before
 * cutting the tag — so the version is cut against the spec the user just gave.
 */
function ExternalSpecPanel({
  item,
  usedBy,
  state,
  onUrlChange,
  onContentChange,
  onResolveViaChat,
}: {
  item: PreflightItem;
  usedBy: string[];
  state: ItemState;
  onUrlChange: (value: string) => void;
  onContentChange: (value: string) => void;
  onResolveViaChat?: ((item: PreflightItem) => void) | undefined;
}) {
  return (
    <Stack spacing={2}>
      <Typography variant="subtitle1">{item.dependency}</Typography>
      <Typography variant="body2" color="text.secondary">
        {item.description}
      </Typography>
      <UsedByLine usedBy={usedBy} />
      <TextField
        label="Spec URL"
        value={state.specUrl}
        onChange={(e) => onUrlChange(e.target.value)}
        fullWidth
        size="small"
      />
      <Typography variant="caption" color="text.secondary">
        or paste the spec directly
      </Typography>
      <TextField
        label="Spec content"
        value={state.specContent}
        onChange={(e) => onContentChange(e.target.value)}
        multiline
        minRows={4}
        fullWidth
        size="small"
      />
      <ResolveViaChatButton item={item} onResolveViaChat={onResolveViaChat} />
    </Stack>
  );
}

/**
 * An unresolved cross-project endpoint. Informational plus a chat handoff:
 * preflight only raises an org-service item while its status is
 * unresolved/blocked/ambiguous, so it is never already-resolved here.
 */
function OrgServicePanel({
  item,
  usedBy,
  onResolveViaChat,
}: {
  item: PreflightItem;
  usedBy: string[];
  onResolveViaChat?: ((item: PreflightItem) => void) | undefined;
}) {
  return (
    <Stack spacing={1}>
      <Typography variant="subtitle1">{item.dependency}</Typography>
      <UsedByLine usedBy={usedBy} />
      {/* The kind sits right under the name; the verbose action note is on
          hover (dotted underline hints it's interactive) instead of inline. */}
      <Tooltip title="We'll publish this cross-project endpoint — it updates and rebuilds the owning project; your build continues, and the consuming task waits until it's published.">
        <Typography
          component="span"
          variant="caption"
          color="text.secondary"
          sx={{
            alignSelf: "flex-start",
            cursor: "help",
            borderBottom: "1px dotted",
            borderColor: "text.disabled",
          }}
        >
          Cross-project endpoint
        </Typography>
      </Tooltip>
      {/* The dependency's own description from the design, when the author
          gave one. Clamp to 3 lines so a long note doesn't blow out the
          drawer; the full text is available on hover. */}
      {item.description ? (
        <Typography
          variant="body2"
          color="text.secondary"
          title={item.description}
          sx={{
            display: "-webkit-box",
            WebkitLineClamp: 3,
            WebkitBoxOrient: "vertical",
            overflow: "hidden",
          }}
        >
          {item.description}
        </Typography>
      ) : null}
      <ResolveViaChatButton item={item} onResolveViaChat={onResolveViaChat} />
    </Stack>
  );
}

/**
 * The resolution drawer: the dependencies whose identity is still unsettled,
 * and the only thing standing between Build and the cut-version ceremony.
 *
 * `items` is the whole preflight response; the drawer renders only the
 * resolution blockers in it. The rest still matters to the request Continue
 * sends — a build carries the platform-resource / org-service approvals
 * derived from the same list (approvalInputsFor) — but it is not something the
 * user is asked about here.
 */
export function BuildDependencyDrawer({
  open,
  items,
  onClose,
  onContinue,
  onResolveDependency,
  submitting = false,
}: {
  open: boolean;
  items: PreflightItem[];
  onClose: () => void;
  onContinue: (inputs: BuildInputItem[]) => void;
  // Fires the Task 5 seeded-message flow for one item (#252 Task 10/17). Every
  // item in this drawer is unresolved, so the intent is always "resolve".
  // Optional so a caller that hasn't wired chat resolution yet (or a test) can
  // omit it — the affordance simply doesn't render.
  onResolveDependency?: (
    item: PreflightItem,
    intent: DependencyResolutionIntent,
  ) => void;
  // True while the parent's build call (POST /build) triggered by Continue is
  // in flight: the Continue button shows a spinner and both buttons disable so
  // the request can't be double-submitted or the drawer dismissed mid-call.
  // The parent closes the drawer on success.
  submitting?: boolean;
}) {
  const hasBuild = useHasPermission("ae:build");
  // #252 Task 15: cross-component dedupe happens once per `items` change, so a
  // shared dependency declared on multiple components renders as one card
  // instead of one per consuming component.
  const groups = useMemo(
    () => groupPreflightItems(items.filter((i) => isResolutionKind(i.kind))),
    [items],
  );

  const [state, setState] = useState<Record<string, ItemState>>(() =>
    initialState(groups),
  );

  // Fresh state every time the drawer transitions to open, so a reopened
  // drawer never retains the prior session's pasted spec.
  useEffect(() => {
    if (open) {
      setState(initialState(groups));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const canContinue = groups.every((group) =>
    isSatisfied(group.representative, state[group.key] ?? seedState()),
  );

  function updateState(group: DependencyGroup, patch: Partial<ItemState>) {
    setState((prev) => ({
      ...prev,
      [group.key]: { ...(prev[group.key] ?? seedState()), ...patch },
    }));
  }

  function handleContinue() {
    // The only thing collected here is a pasted external spec; a merged
    // group's ONE pasted spec fans out to a BuildInputItem per underlying
    // (component, dependency) pair, so the wire contract is unchanged. The
    // approvals the build also needs come from the FULL preflight list, not
    // from what this drawer rendered.
    const specInputs = groups
      .filter((group) => group.representative.kind === "external-spec")
      .flatMap((group) => {
        const groupState = state[group.key] ?? seedState();
        return group.items.map((item): BuildInputItem => {
          const base = { component: item.component, dependency: item.dependency };
          return groupState.specUrl.trim() !== ""
            ? { ...base, kind: "external-spec", specUrl: groupState.specUrl }
            : { ...base, kind: "external-spec", specContent: groupState.specContent };
        });
      });
    onContinue([...specInputs, ...approvalInputsFor(items)]);
  }

  const blockerGroups = groups.filter((g) =>
    isChatOnlyKind(g.representative.kind),
  );
  const externalSpecGroups = groups.filter(
    (g) => g.representative.kind === "external-spec",
  );
  const orgServiceGroups = groups.filter(
    (g) => g.representative.kind === "org-service",
  );

  const resolveViaChat = onResolveDependency
    ? (item: PreflightItem) => onResolveDependency(item, "resolve")
    : undefined;

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      // Force an opaque surface: the theme's `background.paper` is itself
      // semi-transparent (#000000c5 ≈ 0.77 alpha) — a glass surface — so the
      // page behind bleeds through and the dependency text is hard to read.
      // `background.default` is the fully-opaque version of the same surface
      // (#000000 in dark / the light default), so it reads solid in both themes.
      slotProps={{
        paper: {
          sx: {
            bgcolor: "background.default",
            backgroundImage: "none",
            backdropFilter: "none",
          },
        },
      }}
    >
      <Box sx={{ width: 420, p: 3 }}>
        <Typography variant="h6" sx={{ mb: 1 }}>
          Dependencies to resolve
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
          These dependencies have no identity yet, so the version can&apos;t be
          cut against them. Connection values are collected later, on the build
          itself.
        </Typography>

        {groups.length === 0 ? (
          <Typography variant="body2" sx={{ mb: 3 }}>
            Everything is resolved — continue to build.
          </Typography>
        ) : null}

        {blockerGroups.length > 0 ? (
          <>
            <Stack spacing={3} sx={{ mb: 3 }}>
              {blockerGroups.map((group) => (
                <BlockerPanel
                  key={group.key}
                  item={group.representative}
                  usedBy={group.usedBy}
                  onResolveViaChat={resolveViaChat}
                />
              ))}
            </Stack>
            <Divider sx={{ mb: 3 }} />
          </>
        ) : null}

        {externalSpecGroups.length > 0 ? (
          <>
            <Stack spacing={3} sx={{ mb: 3 }}>
              {externalSpecGroups.map((group) => (
                <ExternalSpecPanel
                  key={group.key}
                  item={group.representative}
                  usedBy={group.usedBy}
                  state={state[group.key] ?? seedState()}
                  onUrlChange={(value) => updateState(group, { specUrl: value })}
                  onContentChange={(value) =>
                    updateState(group, { specContent: value })
                  }
                  onResolveViaChat={resolveViaChat}
                />
              ))}
            </Stack>
            <Divider sx={{ mb: 3 }} />
          </>
        ) : null}

        {orgServiceGroups.length > 0 ? (
          <Stack spacing={2} sx={{ mb: 3 }}>
            {orgServiceGroups.map((group) => (
              <OrgServicePanel
                key={group.key}
                item={group.representative}
                usedBy={group.usedBy}
                onResolveViaChat={resolveViaChat}
              />
            ))}
          </Stack>
        ) : null}

        <Stack direction="row" spacing={2} justifyContent="flex-end">
          <Button onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          <Button
            variant="contained"
            loading={submitting}
            disabled={!canContinue || submitting || !hasBuild}
            onClick={handleContinue}
          >
            Continue
          </Button>
        </Stack>
      </Box>
    </Drawer>
  );
}
