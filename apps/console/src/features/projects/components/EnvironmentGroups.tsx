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

import { Box, IconButton, Stack, Tooltip, Typography } from "@wso2/oxygen-ui";
import { Settings } from "@wso2/oxygen-ui-icons-react";
import type { StatusTone } from "../../../components/StatusChip";
import type { ComponentLine, ConnectionLine } from "../lib/deploymentFlow";
import type { ConnectionRow } from "../lib/promotion";

// The two grouped lists an environment card carries under its first step
// (ADR-0032): the components and the connections, each a bordered group with
// a headline count and one line per item — a dot for its state, the name, the
// kind in a lighter hand, and the state's word (or its action) on the right.

function GroupList({
  title,
  caption,
  children,
}: {
  title: string;
  caption: string;
  children: React.ReactNode;
}) {
  return (
    <Box
      role="group"
      aria-label={`${title} — ${caption}`}
      sx={{ border: 1, borderColor: "divider", borderRadius: 1, overflow: "hidden" }}
    >
      <Stack direction="row" spacing={1} sx={{ alignItems: "baseline", px: 1.5, py: 0.75 }}>
        <Typography variant="caption" sx={{ fontWeight: 700 }}>
          {title}
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {caption}
        </Typography>
      </Stack>
      {children}
    </Box>
  );
}

function toneColor(tone: StatusTone): string {
  return tone === "neutral" ? "text.disabled" : `${tone}.main`;
}

function GroupRow({
  tone,
  name,
  kind,
  trailing,
}: {
  tone: StatusTone;
  name: string;
  kind: string;
  trailing: React.ReactNode;
}) {
  return (
    <Stack
      direction="row"
      spacing={1.25}
      sx={{ alignItems: "center", px: 1.5, py: 0.75, borderTop: 1, borderColor: "divider" }}
    >
      <Box
        aria-hidden
        sx={{ width: 6, height: 6, borderRadius: "50%", flexShrink: 0, bgcolor: toneColor(tone) }}
      />
      <Typography
        variant="body2"
        sx={{ flexGrow: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}
      >
        <Box component="span" sx={{ fontWeight: 600 }}>
          {name}
        </Box>
        {kind && (
          <Typography component="span" variant="caption" color="text.secondary">
            {" "}
            {kind}
          </Typography>
        )}
      </Typography>
      {trailing}
    </Stack>
  );
}

/** "Live" · "Waiting" · "Needs stripe" — the word, in the state's colour. */
function StateWord({ label, tone }: { label: string; tone: StatusTone }) {
  return (
    <Typography variant="caption" sx={{ color: toneColor(tone), whiteSpace: "nowrap", flexShrink: 0 }}>
      {label}
    </Typography>
  );
}

export function ComponentsGroup({
  lines,
  caption,
}: {
  lines: ComponentLine[];
  caption: string;
}) {
  return (
    <GroupList title="Components" caption={caption}>
      {lines.map((l) => (
        <GroupRow
          key={l.card.componentName}
          tone={l.tone}
          name={l.card.displayName}
          kind={l.kind}
          trailing={<StateWord label={l.label} tone={l.tone} />}
        />
      ))}
    </GroupList>
  );
}

const CONNECTION_TONE: Record<ConnectionLine["state"], StatusTone> = {
  set: "success",
  provisioned: "success",
  missing: "warning",
  platform: "neutral",
  unknown: "neutral",
};

/**
 * The connections group. A line that can be configured carries a gear — with
 * the connection's name in its accessible name, or every row reads "Configure"
 * to a screen reader (#401 review) — beside its state, so the ask and the way
 * to answer it sit together. A gear rather than the accent pill: the group is
 * a readout, and one pill per row made it read as a row of actions.
 */
export function ConnectionsGroup({
  lines,
  caption,
  onConfigure,
}: {
  lines: ConnectionLine[];
  caption: string;
  onConfigure: (row: ConnectionRow) => void;
}) {
  return (
    // "Dependencies" is what this surface calls them; the component and its
    // props keep the contract's word.
    <GroupList title="Dependencies" caption={caption}>
      {lines.map((l) => (
        <GroupRow
          key={l.row.id}
          tone={CONNECTION_TONE[l.state]}
          name={l.row.name}
          kind={l.row.detail ?? (l.row.kind === "external" ? "external" : "")}
          trailing={
            <>
              {l.label && <StateWord label={l.label} tone={CONNECTION_TONE[l.state]} />}
              {l.configure && (
                <Tooltip title="Configure">
                  <IconButton
                    size="small"
                    aria-label={`Configure ${l.row.name}`}
                    onClick={(event) => {
                      // The whole environment card is a click target; a gear
                      // inside it is not a way to navigate.
                      event.stopPropagation();
                      onConfigure(l.row);
                    }}
                  >
                    <Settings size={15} />
                  </IconButton>
                </Tooltip>
              )}
            </>
          }
        />
      ))}
    </GroupList>
  );
}
