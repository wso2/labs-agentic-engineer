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

import { Chip, Link, ListingTable, Typography } from "@wso2/oxygen-ui";
import {
  deploymentStateLabels,
  scenarioResultLabels,
  taskStateLabels,
  type GenUiPropsOf,
  type GenUiRenderProps,
  type GenUiStateLabel,
} from "@aep/ui-genui";
import { CHIP_COLOR } from "./status.js";

function StateChip({ state }: { state: GenUiStateLabel }) {
  return <Chip label={state.label} color={CHIP_COLOR[state.tone]} />;
}

export function GenUiTaskList({
  props,
}: GenUiRenderProps<GenUiPropsOf<"TaskList">>) {
  const showUpdated = props.tasks.some((task) => task.updated);
  return (
    <ListingTable.Container>
      <ListingTable>
        <ListingTable.Head>
          <ListingTable.Row>
            <ListingTable.Cell>Task</ListingTable.Cell>
            <ListingTable.Cell>Title</ListingTable.Cell>
            <ListingTable.Cell>Status</ListingTable.Cell>
            {showUpdated ? <ListingTable.Cell>Updated</ListingTable.Cell> : null}
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {props.tasks.map((task) => (
            <ListingTable.Row key={task.number}>
              <ListingTable.Cell>#{task.number}</ListingTable.Cell>
              <ListingTable.Cell>
                {task.title}
                {task.detail ? (
                  <Typography variant="caption" color="text.secondary" component="p">
                    {task.detail}
                  </Typography>
                ) : null}
              </ListingTable.Cell>
              <ListingTable.Cell>
                <StateChip state={taskStateLabels[task.state]} />
              </ListingTable.Cell>
              {showUpdated ? (
                <ListingTable.Cell>{task.updated ?? "—"}</ListingTable.Cell>
              ) : null}
            </ListingTable.Row>
          ))}
        </ListingTable.Body>
      </ListingTable>
    </ListingTable.Container>
  );
}

export function GenUiDeployments({
  props,
}: GenUiRenderProps<GenUiPropsOf<"Deployments">>) {
  return (
    <ListingTable.Container>
      <ListingTable>
        <ListingTable.Head>
          <ListingTable.Row>
            <ListingTable.Cell>Environment</ListingTable.Cell>
            <ListingTable.Cell>Version</ListingTable.Cell>
            <ListingTable.Cell>Status</ListingTable.Cell>
            <ListingTable.Cell>URL</ListingTable.Cell>
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {props.environments.map((env, index) => (
            // Environment names may repeat in model output; the index keeps keys unique.
            <ListingTable.Row key={`${index}-${env.environment}`}>
              <ListingTable.Cell>{env.environment}</ListingTable.Cell>
              <ListingTable.Cell>{env.version ?? "—"}</ListingTable.Cell>
              <ListingTable.Cell>
                <StateChip state={deploymentStateLabels[env.state]} />
              </ListingTable.Cell>
              <ListingTable.Cell>
                {env.url ? (
                  <Link href={env.url} target="_blank" rel="noopener noreferrer">
                    {env.url}
                  </Link>
                ) : (
                  "—"
                )}
              </ListingTable.Cell>
            </ListingTable.Row>
          ))}
        </ListingTable.Body>
      </ListingTable>
    </ListingTable.Container>
  );
}

export function GenUiValidationResults({
  props,
}: GenUiRenderProps<GenUiPropsOf<"ValidationResults">>) {
  return (
    <ListingTable.Container>
      <ListingTable>
        <ListingTable.Head>
          <ListingTable.Row>
            <ListingTable.Cell>Scenario</ListingTable.Cell>
            <ListingTable.Cell>Result</ListingTable.Cell>
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {props.scenarios.map((scenario, index) => (
            <ListingTable.Row key={`${index}-${scenario.name}`}>
              <ListingTable.Cell>{scenario.name}</ListingTable.Cell>
              <ListingTable.Cell>
                <StateChip state={scenarioResultLabels[scenario.result]} />
              </ListingTable.Cell>
            </ListingTable.Row>
          ))}
        </ListingTable.Body>
      </ListingTable>
    </ListingTable.Container>
  );
}
