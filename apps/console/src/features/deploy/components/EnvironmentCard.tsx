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

import { createLink } from "@tanstack/react-router";
import { Box, Button, Chip, Link, Skeleton, Typography } from "@wso2/oxygen-ui";
import { ExternalLink } from "@wso2/oxygen-ui-icons-react";
import { componentKindLabel, type ColumnComponent, type ColumnTone, type EnvironmentColumn } from "../model/pipeline";
import type { PromoteView } from "../model/promote";
import { tryItKind } from "../model/tryIt";

const ButtonLink = createLink(Button);

const CHIP_COLOR: Record<ColumnTone, "success" | "info" | "error" | "default"> = {
  success: "success",
  info: "info",
  error: "error",
  neutral: "default",
};

const COMPONENT_STATE: Record<Exclude<ColumnComponent["kind"], "ready">, string> = {
  converging: "Deploying",
  failed: "Deploy failed",
  "not-deployed": "Not deployed",
};

function ComponentLine({ component }: { component: ColumnComponent }) {
  const kind = componentKindLabel(component.type);
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography variant="body2" sx={{ fontWeight: 600 }}>
        {component.displayName}
        {kind && (
          <Typography component="span" variant="caption" color="text.secondary" sx={{ fontWeight: 400 }}>
            {" "}
            · {kind}
          </Typography>
        )}
      </Typography>
      {component.kind === "ready" && component.url ? (
        <Link
          href={component.url}
          target="_blank"
          rel="noreferrer"
          variant="caption"
          underline="hover"
          sx={{
            fontFamily: "monospace",
            display: "flex",
            alignItems: "center",
            gap: 0.5,
            overflow: "hidden",
            textOverflow: "ellipsis",
            whiteSpace: "nowrap",
          }}
        >
          <Box component="span" sx={{ overflow: "hidden", textOverflow: "ellipsis" }}>
            {component.url}
          </Box>
          <ExternalLink size={12} aria-hidden style={{ flexShrink: 0 }} />
        </Link>
      ) : (
        <Typography
          variant="caption"
          color={component.kind === "failed" ? "error.main" : "text.secondary"}
          sx={{ display: "block" }}
        >
          {component.kind === "ready" ? "Running" : COMPONENT_STATE[component.kind]}
        </Typography>
      )}
    </Box>
  );
}

function Dependencies({ column }: { column: EnvironmentColumn }) {
  if (column.dependencies === null) return <Skeleton width="60%" />;
  if (column.dependencies.length === 0) return null;
  return (
    <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
      {column.dependencies.map((d) => (
        <Chip
          key={d.name}
          size="small"
          variant="outlined"
          color={d.ready ? "success" : "warning"}
          label={d.label}
        />
      ))}
    </Box>
  );
}

function Promote({ promote }: { promote: PromoteView }) {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5, borderTop: 1, borderColor: "divider", pt: 1.5 }}>
      <Button variant="contained" disabled fullWidth>
        {promote.label}
      </Button>
      <Typography variant="caption" color="text.secondary">
        {promote.reason}
      </Typography>
      {promote.warning && (
        <Typography variant="caption" color="warning.main">
          {promote.warning}
        </Typography>
      )}
    </Box>
  );
}

/** One environment's column on the board. */
export function EnvironmentCard({
  projectName,
  column,
  promote,
  onTry,
}: {
  projectName: string;
  column: EnvironmentColumn;
  promote: PromoteView | null;
  onTry: () => void;
}) {
  const tryable = column.components.some((c) => tryItKind(c) !== null);
  return (
    <Box
      component="section"
      aria-label={column.label}
      sx={{
        flex: 1,
        minWidth: 0,
        border: 1,
        borderColor: "divider",
        borderRadius: 2.5,
        p: 2,
        bgcolor: "background.paper",
        display: "flex",
        flexDirection: "column",
        gap: 1.5,
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 1 }}>
        <Typography component="h2" variant="overline" color="text.secondary" sx={{ flex: 1, lineHeight: 1.5 }}>
          {column.label}
        </Typography>
        <Chip size="small" color={CHIP_COLOR[column.state.tone]} label={column.state.label} />
      </Box>
      {column.version && (
        <Typography sx={{ fontSize: 28, fontWeight: 700, lineHeight: 1, fontFamily: "monospace" }}>
          {column.version}
        </Typography>
      )}
      {column.components.length > 0 ? (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
          {column.components.map((c) => (
            <ComponentLine key={c.name} component={c} />
          ))}
        </Box>
      ) : (
        <Typography variant="body2" color="text.secondary">
          Nothing deployed yet
        </Typography>
      )}
      <Dependencies column={column} />
      <Box sx={{ flex: 1 }} />
      <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
        <Button size="small" variant="outlined" disabled={!tryable} onClick={onTry}>
          Try it
        </Button>
        <ButtonLink
          size="small"
          variant="outlined"
          to="/projects/$projectName/deploy/$env/configure"
          params={{ projectName, env: column.name }}
        >
          Configure
        </ButtonLink>
      </Box>
      {promote && <Promote promote={promote} />}
    </Box>
  );
}
