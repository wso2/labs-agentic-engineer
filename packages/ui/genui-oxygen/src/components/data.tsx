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
  CodeBlock,
  Grid,
  List,
  ListItem,
  ListItemText,
  ListingTable,
  StatCard,
  Typography,
} from "@wso2/oxygen-ui";
import type { GenUiPropsOf, GenUiRenderProps } from "@aep/ui-genui";

export function GenUiKeyValueList({
  props,
}: GenUiRenderProps<GenUiPropsOf<"KeyValueList">>) {
  if (props.layout === "columns") {
    return (
      <Grid container spacing={2}>
        {props.items.map((item, index) => (
          <Grid key={`${index}-${item.label}`} size="grow">
            <Typography variant="overline" color="text.secondary" component="p">
              {item.label}
            </Typography>
            <Typography variant="body2">{item.value}</Typography>
          </Grid>
        ))}
      </Grid>
    );
  }
  return (
    <List dense disablePadding>
      {props.items.map((item, index) => (
        // Labels may repeat in model output, so the index keeps keys unique.
        <ListItem key={`${index}-${item.label}`} disableGutters>
          <ListItemText primary={item.value} secondary={item.label} />
        </ListItem>
      ))}
    </List>
  );
}

export function GenUiDataTable({
  props,
}: GenUiRenderProps<GenUiPropsOf<"DataTable">>) {
  return (
    <ListingTable.Container>
      <ListingTable>
        <ListingTable.Head>
          <ListingTable.Row>
            {props.columns.map((column) => (
              <ListingTable.Cell key={column.key}>{column.label}</ListingTable.Cell>
            ))}
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {props.rows.map((row, rowIndex) => (
            <ListingTable.Row key={rowIndex}>
              {props.columns.map((column) => (
                <ListingTable.Cell key={column.key}>
                  {row[column.key] ?? "—"}
                </ListingTable.Cell>
              ))}
            </ListingTable.Row>
          ))}
        </ListingTable.Body>
      </ListingTable>
    </ListingTable.Container>
  );
}

export function GenUiMetric({ props }: GenUiRenderProps<GenUiPropsOf<"Metric">>) {
  return <StatCard label={props.label} value={props.value} />;
}

export function GenUiCodeSnippet({
  props,
}: GenUiRenderProps<GenUiPropsOf<"CodeSnippet">>) {
  // Omitted rather than passed as undefined, so CodeBlock keeps its own default.
  return (
    <CodeBlock
      code={props.code}
      {...(props.language ? { language: props.language } : {})}
    />
  );
}
