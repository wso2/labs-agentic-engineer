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

import { useState } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  Collapse,
  IconButton,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Copy,
  Database,
} from "@wso2/oxygen-ui-icons-react";

import type { components } from "../../../generated/aep-api";
import { usePlatformResourceTypes } from "../api/queries";

type PlatformResourceType = components["schemas"]["PlatformResourceTypeDTO"];

// A parameter's schema value (openAPIV3Schema property) is an arbitrary object;
// pull out the human-relevant bits defensively — a resource type author may or
// may not set type/description.
function paramMeta(value: unknown): { type?: string; description?: string } {
  const meta: { type?: string; description?: string } = {};
  if (value && typeof value === "object") {
    const v = value as { type?: unknown; description?: unknown };
    if (typeof v.type === "string") meta.type = v.type;
    if (typeof v.description === "string") meta.description = v.description;
  }
  return meta;
}

export function PlatformResourcesSection() {
  const { data, isLoading, isError, error, refetch } =
    usePlatformResourceTypes();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [copied, setCopied] = useState<string | null>(null);

  const copy = (text: string) => {
    void navigator.clipboard?.writeText(text).then(() => {
      setCopied(text);
      window.setTimeout(
        () => setCopied((c) => (c === text ? null : c)),
        1200,
      );
    });
  };

  const toggle = (name: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });

  if (isLoading) {
    return (
      <Box sx={{ display: "flex", justifyContent: "center", py: 6 }}>
        <CircularProgress />
      </Box>
    );
  }

  if (isError || !data) {
    return (
      <Alert
        severity="error"
        action={<Button onClick={() => void refetch()}>Retry</Button>}
      >
        {error?.message ?? "Failed to load platform resources"}
      </Alert>
    );
  }

  if (data.length === 0) {
    return (
      <Box sx={{ textAlign: "center", py: 8 }}>
        <Database size={48} style={{ opacity: 0.3, marginBottom: 16 }} />
        <Typography variant="h6" gutterBottom>
          No platform resources available
        </Typography>
        <Typography variant="body2" color="text.secondary">
          No platform resource types are installed in this organization yet. A
          platform engineer installs them on the cluster.
        </Typography>
      </Box>
    );
  }

  return (
    <Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        Platform-provisioned resource types available in this organization.
        Declare one as a <code>platform-resource</code> dependency on a component
        to have the platform provision it.
      </Typography>

      <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        {data.map((rt) => (
          <ResourceTypeCard
            key={rt.name}
            rt={rt}
            open={expanded.has(rt.name)}
            onToggle={() => toggle(rt.name)}
            copied={copied}
            onCopy={copy}
          />
        ))}
      </Box>
    </Box>
  );
}

function ResourceTypeCard({
  rt,
  open,
  onToggle,
  copied,
  onCopy,
}: {
  rt: PlatformResourceType;
  open: boolean;
  onToggle: () => void;
  copied: string | null;
  onCopy: (text: string) => void;
}) {
  const outputs = rt.outputs ?? [];
  const paramEntries = Object.entries(rt.parameters ?? {});
  const hasParams = paramEntries.length > 0;

  return (
    <Card variant="outlined">
      <CardContent>
        <Box
          sx={{ display: "flex", alignItems: "center", gap: 0.5, mb: 0.5 }}
        >
          <Typography
            variant="body1"
            fontWeight={600}
            sx={{ fontFamily: "monospace" }}
          >
            {rt.name}
          </Typography>
          <Tooltip title={copied === rt.name ? "Copied" : "Copy name"}>
            <IconButton
              size="small"
              aria-label={`Copy ${rt.name}`}
              onClick={() => onCopy(rt.name)}
            >
              {copied === rt.name ? <Check size={14} /> : <Copy size={14} />}
            </IconButton>
          </Tooltip>
        </Box>

        <Typography variant="body2" color="text.secondary">
          {rt.description || "No description provided."}
        </Typography>

        {outputs.length > 0 && (
          <Box sx={{ mt: 1.5 }}>
            <Typography variant="caption" color="text.secondary">
              Outputs (env vars your component receives) — click to copy
            </Typography>
            <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5, mt: 0.5 }}>
              {outputs.map((o) => (
                <Tooltip key={o} title={copied === o ? "Copied" : "Copy"}>
                  <Chip
                    label={o}
                    size="small"
                    variant="outlined"
                    onClick={() => onCopy(o)}
                    icon={
                      copied === o ? <Check size={12} /> : <Copy size={12} />
                    }
                  />
                </Tooltip>
              ))}
            </Box>
          </Box>
        )}

        {hasParams && (
          <Box sx={{ mt: 1.5 }}>
            <Button
              size="small"
              variant="text"
              onClick={onToggle}
              startIcon={
                open ? <ChevronDown size={16} /> : <ChevronRight size={16} />
              }
              sx={{ px: 0.5 }}
            >
              {open ? "Hide parameters" : "Show parameters"}
            </Button>
            <Collapse in={open} unmountOnExit>
              <Box
                sx={{
                  mt: 1,
                  display: "flex",
                  flexDirection: "column",
                  gap: 1,
                }}
              >
                {paramEntries.map(([key, value]) => {
                  const { type, description } = paramMeta(value);
                  return (
                    <Box key={key}>
                      <Typography
                        variant="body2"
                        sx={{ fontFamily: "monospace" }}
                      >
                        {key}
                        {type ? (
                          <Typography
                            component="span"
                            variant="caption"
                            color="text.secondary"
                            sx={{ ml: 1 }}
                          >
                            {type}
                          </Typography>
                        ) : null}
                      </Typography>
                      {description ? (
                        <Typography variant="caption" color="text.secondary">
                          {description}
                        </Typography>
                      ) : null}
                    </Box>
                  );
                })}
              </Box>
            </Collapse>
          </Box>
        )}
      </CardContent>
    </Card>
  );
}
