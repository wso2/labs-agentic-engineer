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
  Typography,
} from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";

type PreflightItem = components["schemas"]["PreflightItem"];
type BuildInputItem = components["schemas"]["BuildInputItem"];

type SpecState = {
  specUrl: string;
  specContent: string;
};

interface DependencyGroup {
  key: string;
  items: PreflightItem[];
  representative: PreflightItem;
  usedBy: string[];
}

function isResolutionItem(item: PreflightItem): boolean {
  return (
    item.kind === "external-spec" ||
    item.kind === "external-ambiguous" ||
    item.kind === "external-unresolved" ||
    item.kind === "org-service"
  );
}

function isChatOnlyItem(item: PreflightItem): boolean {
  return item.kind !== "external-spec";
}

/**
 * A shared unresolved dependency should still render once, with every
 * consumer visible. Non-blocking kinds are discarded before grouping: their
 * configuration now lives on Builds and platform approvals are automatic.
 */
function groupResolutionItems(items: PreflightItem[]): DependencyGroup[] {
  const buckets = new Map<string, PreflightItem[]>();
  for (const item of items) {
    if (!isResolutionItem(item)) continue;
    const key = `${item.kind}:${item.dependency}`;
    const bucket = buckets.get(key);
    if (bucket) bucket.push(item);
    else buckets.set(key, [item]);
  }

  return [...buckets.entries()].map(([key, bucket]) => {
    const sorted = [...bucket].sort((a, b) =>
      a.component.localeCompare(b.component),
    );
    return {
      key,
      items: sorted,
      representative: sorted[0]!,
      usedBy: [...new Set(sorted.map((item) => item.component))].sort(),
    };
  });
}

function emptySpecState(): SpecState {
  return { specUrl: "", specContent: "" };
}

function initialSpecState(groups: DependencyGroup[]): Record<string, SpecState> {
  return Object.fromEntries(
    groups
      .filter((group) => group.representative.kind === "external-spec")
      .map((group) => [group.key, emptySpecState()]),
  );
}

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

function ResolutionPanel({
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
      {onResolveViaChat ? (
        <Button
          size="small"
          sx={{ alignSelf: "flex-start" }}
          onClick={() => onResolveViaChat(item)}
        >
          Resolve via chat
        </Button>
      ) : null}
    </Stack>
  );
}

function ExternalSpecPanel({
  item,
  usedBy,
  state,
  onChange,
  onResolveViaChat,
}: {
  item: PreflightItem;
  usedBy: string[];
  state: SpecState;
  onChange: (patch: Partial<SpecState>) => void;
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
        onChange={(event) => onChange({ specUrl: event.target.value })}
        fullWidth
        size="small"
      />
      <Typography variant="caption" color="text.secondary">
        or paste the spec directly
      </Typography>
      <TextField
        label="Spec content"
        value={state.specContent}
        onChange={(event) => onChange({ specContent: event.target.value })}
        multiline
        minRows={4}
        fullWidth
        size="small"
      />
      {onResolveViaChat ? (
        <Button
          size="small"
          sx={{ alignSelf: "flex-start" }}
          onClick={() => onResolveViaChat(item)}
        >
          Resolve via chat
        </Button>
      ) : null}
    </Stack>
  );
}

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
  onResolveDependency?: (item: PreflightItem) => void;
  submitting?: boolean;
}) {
  const groups = useMemo(() => groupResolutionItems(items), [items]);
  const externalSpecGroups = groups.filter(
    (group) => group.representative.kind === "external-spec",
  );
  const chatOnlyGroups = groups.filter((group) =>
    isChatOnlyItem(group.representative),
  );
  const [specState, setSpecState] = useState<Record<string, SpecState>>(() =>
    initialSpecState(groups),
  );

  useEffect(() => {
    if (open) setSpecState(initialSpecState(groups));
    // Reopening starts a fresh local resolution attempt. Item refreshes while
    // open keep any external-spec text the user has already entered.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const canContinue =
    externalSpecGroups.length > 0 &&
    chatOnlyGroups.length === 0 &&
    externalSpecGroups.every((group) => {
      const state = specState[group.key] ?? emptySpecState();
      return state.specUrl.trim() !== "" || state.specContent.trim() !== "";
    });

  const resolveViaChat = onResolveDependency;

  function updateSpecState(group: DependencyGroup, patch: Partial<SpecState>) {
    setSpecState((previous) => ({
      ...previous,
      [group.key]: {
        ...(previous[group.key] ?? emptySpecState()),
        ...patch,
      },
    }));
  }

  function handleContinue() {
    const inputs = externalSpecGroups.flatMap((group) => {
      const state = specState[group.key] ?? emptySpecState();
      return group.items.map((item): BuildInputItem =>
        state.specUrl.trim() !== ""
          ? {
              component: item.component,
              dependency: item.dependency,
              kind: "external-spec",
              specUrl: state.specUrl.trim(),
            }
          : {
              component: item.component,
              dependency: item.dependency,
              kind: "external-spec",
              specContent: state.specContent.trim(),
            },
      );
    });
    onContinue(inputs);
  }

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
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
        <Typography variant="h6" sx={{ mb: 2 }}>
          Dependencies to resolve
        </Typography>

        {chatOnlyGroups.length > 0 ? (
          <Stack spacing={3} sx={{ mb: externalSpecGroups.length > 0 ? 3 : 0 }}>
            {chatOnlyGroups.map((group) => (
              <ResolutionPanel
                key={group.key}
                item={group.representative}
                usedBy={group.usedBy}
                onResolveViaChat={resolveViaChat}
              />
            ))}
          </Stack>
        ) : null}

        {chatOnlyGroups.length > 0 && externalSpecGroups.length > 0 ? (
          <Divider sx={{ mb: 3 }} />
        ) : null}

        {externalSpecGroups.length > 0 ? (
          <Stack spacing={3} sx={{ mb: 3 }}>
            {externalSpecGroups.map((group) => (
              <ExternalSpecPanel
                key={group.key}
                item={group.representative}
                usedBy={group.usedBy}
                state={specState[group.key] ?? emptySpecState()}
                onChange={(patch) => updateSpecState(group, patch)}
                onResolveViaChat={resolveViaChat}
              />
            ))}
          </Stack>
        ) : null}

        <Stack direction="row" spacing={2} justifyContent="flex-end">
          {/* canContinue also requires every chat-only blocker to be gone, so
              with both kinds present the developer can fill each spec field and
              still find Continue disabled. Name the remaining step rather than
              leaving a dead button to explain itself. */}
          {externalSpecGroups.length > 0 && chatOnlyGroups.length > 0 ? (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ alignSelf: "center", mr: "auto" }}
            >
              Resolve the dependencies above in chat before continuing.
            </Typography>
          ) : null}
          <Button onClick={onClose} disabled={submitting}>
            Cancel
          </Button>
          {externalSpecGroups.length > 0 ? (
            <Button
              variant="contained"
              loading={submitting}
              disabled={!canContinue || submitting}
              onClick={handleContinue}
            >
              Continue
            </Button>
          ) : null}
        </Stack>
      </Box>
    </Drawer>
  );
}
