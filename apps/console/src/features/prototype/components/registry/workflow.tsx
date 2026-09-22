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

// Workflow: approval-panel and task-queue. Deciding is an action like any
// other — it opens a dialog or navigates; nothing is approved.

import { Card, CardContent, CardHeader, Stack, Typography } from "@wso2/oxygen-ui";
import type { PrototypeApprovalPanelNode, PrototypeTaskQueueNode } from "@aep/prototype-model";
import { ActionButton } from "./content";
import { RecordTable } from "./data";

export function ApprovalPanelView({ node }: { node: PrototypeApprovalPanelNode }) {
  return (
    <Card>
      <CardHeader title={node.title} />
      <CardContent>
        <Stack spacing={2}>
          <Typography variant="body2" color="text.secondary">
            {node.summary}
          </Typography>
          <Stack spacing={1}>
            {node.actions.map((b) => (
              <ActionButton key={b.id} button={b} fullWidth />
            ))}
          </Stack>
        </Stack>
      </CardContent>
    </Card>
  );
}

export function TaskQueueView({ node }: { node: PrototypeTaskQueueNode }) {
  return <RecordTable tableId={node.id} title={node.title} columns={node.columns} rows={node.rows} onRow={node.onRow} />;
}
