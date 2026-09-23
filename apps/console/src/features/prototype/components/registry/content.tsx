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

// Content: text, heading, badge, button — and the one button every other
// registry entry (heading actions, form actions, overlay footers, empty-state
// calls to action) renders through.

import { Button, Chip, Stack, Typography } from "@wso2/oxygen-ui";
import type {
  PrototypeBadgeNode,
  PrototypeButton,
  PrototypeButtonNode,
  PrototypeHeadingNode,
  PrototypeTextNode,
  PrototypeTone,
} from "@aep/prototype-model";
import { Selectable, usePrototypeRender } from "../renderContext";

/** The Oxygen colour a tone maps onto. */
export function toneColor(tone: PrototypeTone | undefined) {
  return !tone || tone === "default" ? "default" : tone;
}

/**
 * A model button. `framed` when the button is itself a node, already wrapped
 * by the renderer; otherwise it wraps itself so it stays selectable.
 */
export function ActionButton({
  button,
  fullWidth,
  framed,
}: {
  button: PrototypeButton;
  fullWidth?: boolean;
  framed?: boolean;
}) {
  const { activate } = usePrototypeRender();
  const control = (
    <Button
      type="button"
      variant={button.emphasis ? "contained" : "outlined"}
      color={button.emphasis === "danger" ? "error" : "primary"}
      fullWidth={Boolean(fullWidth)}
      onClick={() => activate(button.id, button.action)}
    >
      {button.label}
    </Button>
  );
  if (framed) return control;
  return (
    <Selectable id={button.id} inline={!fullWidth}>
      {control}
    </Selectable>
  );
}

export function TextView({ node }: { node: PrototypeTextNode }) {
  return (
    <Typography variant="body2" color="text.secondary">
      {node.text}
    </Typography>
  );
}

export function HeadingView({ node }: { node: PrototypeHeadingNode }) {
  return (
    <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={2}>
      <Typography variant="h5" component="h2">
        {node.text}
      </Typography>
      {node.actions && node.actions.length > 0 && (
        <Stack direction="row" spacing={1}>
          {node.actions.map((b) => (
            <ActionButton key={b.id} button={b} />
          ))}
        </Stack>
      )}
    </Stack>
  );
}

export function BadgeView({ node }: { node: PrototypeBadgeNode }) {
  return <Chip size="small" label={node.label} color={toneColor(node.tone)} />;
}

export function ButtonView({ node }: { node: PrototypeButtonNode }) {
  return <ActionButton button={node} framed />;
}
