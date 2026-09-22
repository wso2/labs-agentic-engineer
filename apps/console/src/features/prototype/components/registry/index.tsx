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

// The controlled registry: one Oxygen renderer per v1 node kind. The mapped
// type makes a missing or misspelt kind a compile error, so a registry entry
// added to the model cannot silently render nothing.

import type { ReactNode } from "react";
import type { PrototypeNode, PrototypeNodeKind } from "@aep/prototype-model";
import { BadgeView, ButtonView, HeadingView, TextView } from "./content";
import { FiltersView, TableView, TimelineView } from "./data";
import { AlertView, EmptyStateView, StatView } from "./feedback";
import { FormView, ValidationSummaryView } from "./forms";
import { DetailView, GridView, SplitView, StackView } from "./layouts";
import { BreadcrumbsView, LinkView, StepperView, TabsView } from "./navigation";
import { ApprovalPanelView, TaskQueueView } from "./workflow";

type NodeRenderers = {
  [K in PrototypeNodeKind]: (props: { node: Extract<PrototypeNode, { kind: K }> }) => ReactNode;
};

const NODE_RENDERERS: NodeRenderers = {
  stack: StackView,
  grid: GridView,
  split: SplitView,
  detail: DetailView,
  breadcrumbs: BreadcrumbsView,
  tabs: TabsView,
  stepper: StepperView,
  text: TextView,
  heading: HeadingView,
  badge: BadgeView,
  stat: StatView,
  alert: AlertView,
  "empty-state": EmptyStateView,
  button: ButtonView,
  link: LinkView,
  form: FormView,
  "validation-summary": ValidationSummaryView,
  table: TableView,
  filters: FiltersView,
  timeline: TimelineView,
  "approval-panel": ApprovalPanelView,
  "task-queue": TaskQueueView,
};

/** The body of one node, by kind. */
export function NodeBody({ node }: { node: PrototypeNode }) {
  const Render = NODE_RENDERERS[node.kind] as (props: { node: PrototypeNode }) => ReactNode;
  return <Render node={node} />;
}
