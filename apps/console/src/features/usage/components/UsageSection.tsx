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
  Chip,
  CircularProgress,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { useProjectUsageList } from "../api/queries";
import { UsageFigure } from "./UsageFigure";

type ProjectUsageCard = components["schemas"]["ProjectUsageCard"];

// Settings → Usage (#291): the org's per-project agent spend, one card per
// project that ever recorded usage. The cost figure is the same folded value
// used across the console — USD primary, hover for the token breakdown.
// Cards for deleted projects stay (their spend was real) but render greyed.
export function UsageSection() {
  const usageList = useProjectUsageList();

  if (usageList.isPending) {
    return (
      <Box sx={{ display: "flex", justifyContent: "center", p: 6 }}>
        <CircularProgress aria-label="Loading usage" />
      </Box>
    );
  }

  if (usageList.isError) {
    return (
      <Alert
        severity="error"
        action={
          <Button onClick={() => void usageList.refetch()}>Retry</Button>
        }
      >
        Failed to load usage
        {usageList.error instanceof Error && usageList.error.message
          ? `: ${usageList.error.message}`
          : ""}
      </Alert>
    );
  }

  const cards = usageList.data.projects;

  if (cards.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ py: 3 }}>
        No agent usage yet — costs appear here as spec, design, and coding
        agents run.
      </Typography>
    );
  }

  return (
    <Stack spacing={1.5}>
      {/* The name alone is not a key: a project deleted and recreated under the
          same one is two cards — the live project and the incarnation whose
          spend it does not inherit. */}
      {cards.map((card) => (
        <ProjectCard key={`${card.projectName}:${card.deleted}`} card={card} />
      ))}
    </Stack>
  );
}

// Marketplace register turns belong to no project; the API reports their spend
// under the empty project key (which it also treats as a deleted slug).
function isMarketplaceCard(card: ProjectUsageCard): boolean {
  return card.projectName === "";
}

function ProjectCard({ card: reported }: { card: ProjectUsageCard }) {
  const marketplace = isMarketplaceCard(reported);
  const card = marketplace ? { ...reported, displayName: "Marketplace", deleted: false } : reported;
  return (
    <Card variant="outlined" sx={{ opacity: card.deleted ? 0.6 : 1 }}>
      <CardContent
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1.5,
          "&:last-child": { pb: 2 },
        }}
      >
        <Box sx={{ minWidth: 0 }}>
          <Typography variant="subtitle1" noWrap>
            {card.displayName}
          </Typography>
          <Typography variant="caption" color="text.secondary" noWrap>
            {marketplace ? "Resource registration" : card.projectName}
            {card.usage.model && ` · ${card.usage.model}`}
            {card.usage.costUsd === null && card.usage.host && ` via ${card.usage.host}`}
          </Typography>
        </Box>
        {card.deleted && (
          <Chip size="small" variant="outlined" label="Deleted project" />
        )}
        <Box sx={{ flexGrow: 1 }} />
        <UsageFigure
          usage={card.usage}
          phases={card.phases}
          context={`Agent spend — ${card.displayName}`}
        />
      </CardContent>
    </Card>
  );
}
