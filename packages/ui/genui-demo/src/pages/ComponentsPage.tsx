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
  Box,
  Card,
  CardContent,
  CardHeader,
  Chip,
  CodeBlock,
  Link,
  ListingTable,
  Paper,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import {
  genUiActions,
  genUiComponents,
  type GenUiComponentName,
  type GenUiDispatchOutcome,
  type GenUiSpec,
} from "@aep/ui-genui";
import { componentExamples } from "@aep/ui-genui/examples";
import { GenUiView } from "@aep/ui-genui-oxygen";
import { createHandlers } from "../handlers.js";
import { propRows, type PropRow } from "../schema.js";

const componentNames = Object.keys(genUiComponents) as GenUiComponentName[];
const handlers = createHandlers();

function PropsTable({ rows }: { rows: PropRow[] }) {
  if (rows.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary">
        No props.
      </Typography>
    );
  }
  return (
    <ListingTable.Container>
      <ListingTable density="compact">
        <ListingTable.Head>
          <ListingTable.Row>
            <ListingTable.Cell>Prop</ListingTable.Cell>
            <ListingTable.Cell>Type</ListingTable.Cell>
            <ListingTable.Cell>Required</ListingTable.Cell>
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {rows.map((row) => (
            <ListingTable.Row key={row.name}>
              <ListingTable.Cell>
                <code>{row.name}</code>
              </ListingTable.Cell>
              <ListingTable.Cell>
                <code>{row.type}</code>
              </ListingTable.Cell>
              <ListingTable.Cell>{row.required ? "yes" : "no"}</ListingTable.Cell>
            </ListingTable.Row>
          ))}
        </ListingTable.Body>
      </ListingTable>
    </ListingTable.Container>
  );
}

function ComponentEntry({ name }: { name: GenUiComponentName }) {
  const def = genUiComponents[name];
  const spec = componentExamples[name];
  const [lastOutcome, setLastOutcome] = useState<GenUiDispatchOutcome | null>(null);

  return (
    <Card id={name}>
      <CardHeader
        title={name}
        subheader={def.description}
        action={
          <Stack direction="row" spacing={1}>
            {def.hasChildren ? <Chip label="has children" /> : null}
            {def.events.map((event) => (
              <Chip key={event} label={`event: ${event}`} color="info" />
            ))}
          </Stack>
        }
      />
      <CardContent>
        <Stack spacing={2}>
          <PropsTable rows={propRows(def.props)} />
          <Box
            sx={{
              display: "grid",
              gap: 2,
              gridTemplateColumns: { lg: "1fr 1fr" },
              alignItems: "start",
            }}
          >
            <Stack spacing={1}>
              <Typography variant="overline" color="text.secondary">
                JSON (what the model writes)
              </Typography>
              <CodeBlock code={JSON.stringify(spec, null, 2)} language="json" />
            </Stack>
            <Stack spacing={1}>
              <Typography variant="overline" color="text.secondary">
                Rendered with Oxygen UI
              </Typography>
              <Paper variant="outlined">
                <Box sx={{ p: 2 }}>
                  <GenUiView
                    spec={spec as GenUiSpec}
                    handlers={handlers}
                    onActionOutcome={setLastOutcome}
                  />
                </Box>
              </Paper>
              {lastOutcome ? (
                <Typography variant="caption" sx={{ fontFamily: "monospace" }}>
                  {lastOutcome.status} · {lastOutcome.action}
                </Typography>
              ) : null}
            </Stack>
          </Box>
        </Stack>
      </CardContent>
    </Card>
  );
}

function ActionsEntry() {
  return (
    <Card id="actions">
      <CardHeader
        title="Actions"
        subheader="What a Button's press event may trigger. A spec names the action and its params; the host supplies the handler, and params are checked against these schemas before it runs."
      />
      <CardContent>
        <Stack spacing={3}>
          {Object.entries(genUiActions).map(([name, def]) => (
            <Stack key={name} spacing={1}>
              <Typography variant="subtitle2" component="h3">
                <code>{name}</code>
              </Typography>
              <Typography variant="body2" color="text.secondary">
                {def.description}
              </Typography>
              <PropsTable rows={propRows(def.params)} />
            </Stack>
          ))}
        </Stack>
      </CardContent>
    </Card>
  );
}

/** Every catalog component on its own: what it is for, its props, its JSON, and what renders. */
export function ComponentsPage() {
  return (
    <Box
      sx={{
        display: "grid",
        gap: 3,
        gridTemplateColumns: { md: "12rem 1fr" },
        alignItems: "start",
      }}
    >
      <Stack spacing={0.5} component="nav" sx={{ position: { md: "sticky" }, top: 16 }}>
        {componentNames.map((name) => (
          <Link key={name} href={`#${name}`} variant="body2">
            {name}
          </Link>
        ))}
        <Link href="#actions" variant="body2">
          Actions
        </Link>
      </Stack>
      <Stack spacing={3}>
        {componentNames.map((name) => (
          <ComponentEntry key={name} name={name} />
        ))}
        <ActionsEntry />
      </Stack>
    </Box>
  );
}
