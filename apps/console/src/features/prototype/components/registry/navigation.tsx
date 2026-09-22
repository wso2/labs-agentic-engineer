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

// Navigation: the application's side and top navigation, breadcrumbs, tabs,
// stepper and link. Every entry here changes view state only.

import {
  Box,
  Breadcrumbs,
  Link,
  Sidebar,
  Stack,
  Step,
  StepButton,
  Stepper,
  Tab,
  Tabs,
  Typography,
} from "@wso2/oxygen-ui";
import type {
  PrototypeBreadcrumbsNode,
  PrototypeLinkNode,
  PrototypeNavigation,
  PrototypeNavigationItem,
  PrototypeStepperNode,
  PrototypeTabsNode,
} from "@aep/prototype-model";
import { navigationItemsForRole } from "../../model/visibility";
import { pressProps, Selectable, usePrototypeRender } from "../renderContext";

/** The item that leads to the current screen, if the role sees one. */
function activeItemId(items: PrototypeNavigationItem[], screenId: string): string | undefined {
  return items.find((i) => i.action.kind === "navigate" && i.action.screenId === screenId)?.id;
}

export function SideNavigation({ navigation }: { navigation: PrototypeNavigation }) {
  const { model, view, activate } = usePrototypeRender();
  const items = navigationItemsForRole(navigation.items, view.roleId);
  return (
    <Box
      component="nav"
      aria-label={`${model.name} navigation`}
      data-prototype-component-id={navigation.id}
      sx={{ flexShrink: 0, borderRight: 1, borderColor: "divider", bgcolor: "background.paper", overflow: "auto" }}
    >
      <Sidebar
        width={220}
        activeItem={activeItemId(items, view.screenId) ?? ""}
        onSelect={(id) => {
          const item = items.find((i) => i.id === id);
          if (item) activate(item.id, item.action);
        }}
        sx={{ height: "100%", position: "static" }}
      >
        <Sidebar.Nav>
          <Sidebar.Category>
            <Sidebar.CategoryLabel>{model.name}</Sidebar.CategoryLabel>
            {items.map((i) => (
              <Selectable key={i.id} id={i.id}>
                <Sidebar.Item id={i.id}>
                  <Sidebar.ItemLabel>{i.label}</Sidebar.ItemLabel>
                </Sidebar.Item>
              </Selectable>
            ))}
          </Sidebar.Category>
        </Sidebar.Nav>
      </Sidebar>
    </Box>
  );
}

export function TopNavigation({ navigation }: { navigation: PrototypeNavigation }) {
  const ctx = usePrototypeRender();
  const items = navigationItemsForRole(navigation.items, ctx.view.roleId);
  return (
    <Box
      component="nav"
      aria-label={`${ctx.model.name} navigation`}
      data-prototype-component-id={navigation.id}
      sx={{ px: 3, borderBottom: 1, borderColor: "divider", bgcolor: "background.paper" }}
    >
      <Tabs value={activeItemId(items, ctx.view.screenId) ?? false}>
        {items.map((i) => (
          <Tab key={i.id} value={i.id} label={i.label} {...pressProps(ctx, i.id, i.action)} />
        ))}
      </Tabs>
    </Box>
  );
}

export function BreadcrumbsView({ node }: { node: PrototypeBreadcrumbsNode }) {
  const { activate } = usePrototypeRender();
  return (
    <Breadcrumbs>
      {node.items.map((item) => (
        <Selectable key={item.id} id={item.id} inline>
          {item.action ? (
            <Link
              component="button"
              type="button"
              underline="hover"
              onClick={() => item.action && activate(item.id, item.action)}
            >
              {item.label}
            </Link>
          ) : (
            <Typography color="text.primary">{item.label}</Typography>
          )}
        </Selectable>
      ))}
    </Breadcrumbs>
  );
}

export function TabsView({ node }: { node: PrototypeTabsNode }) {
  const ctx = usePrototypeRender();
  const active = node.tabs.find((t) => t.id === ctx.view.activeTabIds[node.id]) ?? node.tabs[0];
  return (
    <Stack spacing={2}>
      <Tabs value={active?.id ?? false}>
        {node.tabs.map((t) => (
          <Tab
            key={t.id}
            value={t.id}
            label={t.label}
            {...pressProps(ctx, t.id, { kind: "set-tab", tabsId: node.id, tabId: t.id })}
          />
        ))}
      </Tabs>
      {active && <Stack spacing={2}>{ctx.renderNodes(active.content)}</Stack>}
    </Stack>
  );
}

export function StepperView({ node }: { node: PrototypeStepperNode }) {
  const ctx = usePrototypeRender();
  const index = Math.max(0, node.steps.findIndex((s) => s.id === ctx.view.activeStepIds[node.id]));
  const active = node.steps[index];
  return (
    <Stack spacing={3}>
      <Stepper nonLinear activeStep={index}>
        {node.steps.map((s) => (
          <Step key={s.id}>
            <StepButton {...pressProps(ctx, s.id, { kind: "set-step", stepperId: node.id, stepId: s.id })}>
              {s.label}
            </StepButton>
          </Step>
        ))}
      </Stepper>
      {active && <Stack spacing={2}>{ctx.renderNodes(active.content)}</Stack>}
    </Stack>
  );
}

export function LinkView({ node }: { node: PrototypeLinkNode }) {
  const { activate } = usePrototypeRender();
  return (
    <Link component="button" type="button" onClick={() => activate(node.id, node.action)}>
      {node.label}
    </Link>
  );
}
