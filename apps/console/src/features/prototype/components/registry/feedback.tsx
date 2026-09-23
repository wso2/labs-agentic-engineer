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

// Feedback: alert, empty-state, stat.

import { Alert, AlertTitle, Card, CardContent, Stack, StatCard, Typography } from "@wso2/oxygen-ui";
import { Inbox } from "@wso2/oxygen-ui-icons-react";
import type { PrototypeAlertNode, PrototypeEmptyStateNode, PrototypeStatNode } from "@aep/prototype-model";
import { ActionButton } from "./content";

export function AlertView({ node }: { node: PrototypeAlertNode }) {
  return (
    <Alert severity={node.tone === "default" ? "info" : node.tone}>
      {node.title && <AlertTitle>{node.title}</AlertTitle>}
      {node.text}
    </Alert>
  );
}

export function EmptyStateView({ node }: { node: PrototypeEmptyStateNode }) {
  return (
    <Card variant="outlined">
      <CardContent>
        <Stack alignItems="center" spacing={1} sx={{ py: 4, textAlign: "center" }}>
          <Inbox size={32} aria-hidden />
          <Typography variant="h6" component="h3">
            {node.title}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {node.text}
          </Typography>
          {node.action && <ActionButton button={node.action} />}
        </Stack>
      </CardContent>
    </Card>
  );
}

export function StatView({ node }: { node: PrototypeStatNode }) {
  return <StatCard label={node.label} value={node.value} />;
}
