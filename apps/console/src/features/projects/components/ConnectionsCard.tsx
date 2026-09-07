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
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import type { ConnectionRow } from "../lib/promotion";
import { AccentPill } from "./AccentPill";

/** A status line: a dot, a name, a fact or an action on the right. */
function ConnectionLine({
  label,
  trailing,
}: {
  label: string;
  trailing?: React.ReactNode;
}) {
  return (
    <Stack direction="row" spacing={1.25} sx={{ alignItems: "center" }}>
      <Box
        sx={{
          width: 6,
          height: 6,
          borderRadius: "50%",
          bgcolor: "success.main",
          flexShrink: 0,
        }}
      />
      <Typography variant="body2" sx={{ flexGrow: 1, minWidth: 0 }}>
        {label}
      </Typography>
      {trailing}
    </Stack>
  );
}

/**
 * The design's connections, and the way to hand the platform their REAL
 * values after build-time placeholders (#395 follow-up): Configure re-collects
 * an external connection's dev values through the same provisioning surface
 * the build drawer used. Its own card under the ledger (ADR-0027) — the side
 * panel it lived in went with the story rail, and this is the one thing that
 * panel said which nothing else on the page does.
 */
export function ConnectionsCard({
  connections,
  registeredNames,
  catalogUnknown,
  catalogError,
  onConfigure,
}: {
  connections: ConnectionRow[];
  /** Registered Externals hold values on the org plane — no Configure. */
  registeredNames: Set<string>;
  /** The org catalog is still loading or failed: a name that MIGHT be
   *  Registered must not open the project values dialog. */
  catalogUnknown: boolean;
  catalogError?: { message: string; retry: () => void };
  onConfigure: (row: ConnectionRow) => void;
}) {
  return (
    <Card variant="outlined">
      <CardContent>
        <Stack direction="row" spacing={1.25} sx={{ alignItems: "center", mb: 1.5 }}>
          <Typography variant="subtitle2" sx={{ fontWeight: 600 }}>
            Connections
          </Typography>
          <Typography variant="caption" color="text.secondary">
            what the design depends on, and the values it runs with
          </Typography>
        </Stack>
        {catalogError && (
          <Alert
            severity="error"
            sx={{ mb: 1.5 }}
            action={<Button onClick={catalogError.retry}>Retry</Button>}
          >
            Failed to load org catalog{catalogError.message ? `: ${catalogError.message}` : ""}
          </Alert>
        )}
        <Stack spacing={1.25}>
          {connections.length > 0 ? (
            connections.map((row) => (
              <ConnectionLine
                key={row.id}
                label={row.detail ? `${row.name} (${row.detail})` : row.name}
                trailing={
                  row.kind === "external" &&
                  row.config.length > 0 &&
                  !catalogUnknown &&
                  !registeredNames.has(row.name) ? (
                    // Project External only — Registered names skip Configure
                    // (the org catalog owns the value plane). The row label
                    // holds the connection name; the button's accessible name
                    // must too, or every row reads "Configure" to a screen
                    // reader (#401 review).
                    <AccentPill
                      aria-label={`Configure ${row.name}`}
                      onClick={() => onConfigure(row)}
                    >
                      Configure
                    </AccentPill>
                  ) : row.provisioned ? (
                    <Typography variant="caption" color="success.main">
                      provisioned
                    </Typography>
                  ) : row.kind === "external" &&
                    (catalogUnknown || registeredNames.has(row.name)) ? (
                    undefined
                  ) : (
                    // Config-carrying but not user-updatable here (a platform
                    // resource like an identity app): the platform owns its
                    // credentials, so say that instead of inferring
                    // "provisioned" from the else-branch (#401 review).
                    <Typography variant="caption" color="text.secondary">
                      platform-managed
                    </Typography>
                  )
                }
              />
            ))
          ) : (
            <Typography variant="body2" color="text.secondary">
              This design declares no connections.
            </Typography>
          )}
        </Stack>
      </CardContent>
    </Card>
  );
}
