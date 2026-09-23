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
  deploymentStateLabels,
  scenarioResultLabels,
  taskStateLabels,
  type GenUiPropsOf,
  type GenUiRenderProps,
} from "@aep/ui-genui";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "#shadcn/components/ui/table";
import { StateBadge } from "./status.js";

export function GenUiTaskList({
  props,
}: GenUiRenderProps<GenUiPropsOf<"TaskList">>) {
  const showUpdated = props.tasks.some((task) => task.updated);
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Task</TableHead>
          <TableHead>Title</TableHead>
          <TableHead>Status</TableHead>
          {showUpdated ? <TableHead>Updated</TableHead> : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.tasks.map((task) => (
          <TableRow key={task.number}>
            <TableCell>#{task.number}</TableCell>
            <TableCell className="whitespace-normal">
              {task.title}
              {task.detail ? (
                <p className="text-xs text-muted-foreground">{task.detail}</p>
              ) : null}
            </TableCell>
            <TableCell>
              <StateBadge state={taskStateLabels[task.state]} />
            </TableCell>
            {showUpdated ? <TableCell>{task.updated ?? "—"}</TableCell> : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function GenUiDeployments({
  props,
}: GenUiRenderProps<GenUiPropsOf<"Deployments">>) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Environment</TableHead>
          <TableHead>Version</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>URL</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.environments.map((env, index) => (
          // Environment names may repeat in model output; the index keeps keys unique.
          <TableRow key={`${index}-${env.environment}`}>
            <TableCell>{env.environment}</TableCell>
            <TableCell>{env.version ?? "—"}</TableCell>
            <TableCell>
              <StateBadge state={deploymentStateLabels[env.state]} />
            </TableCell>
            <TableCell>
              {env.url ? (
                // shadcn's Typography link style; it has no link component.
                <a
                  href={env.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-medium text-primary underline underline-offset-4"
                >
                  {env.url}
                </a>
              ) : (
                "—"
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function GenUiValidationResults({
  props,
}: GenUiRenderProps<GenUiPropsOf<"ValidationResults">>) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Scenario</TableHead>
          <TableHead>Result</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.scenarios.map((scenario, index) => (
          <TableRow key={`${index}-${scenario.name}`}>
            <TableCell>{scenario.name}</TableCell>
            <TableCell>
              <StateBadge state={scenarioResultLabels[scenario.result]} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
