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

/** Content: text, heading, badge, stat, alert, empty state, button and link. */

import { useContext, type ReactNode } from "react";
import { usePress, type Pressable } from "../runtime/press.js";
import { SelectableBox, requireId } from "../runtime/selectable.js";
import { useThemed } from "../theme/context.js";
import { FormSubmitContext } from "./form-submit.js";

export type Tone = "default" | "info" | "success" | "warning" | "error";

export interface ButtonProps extends Pressable {
  id: string;
  label: string;
  /** `primary` for the screen's main action, `danger` for a destructive one; outlined otherwise. */
  emphasis?: "primary" | "danger" | undefined;
  disabled?: boolean | undefined;
  /** Submits the enclosing `<Form>` (which validates its fields, then calls its `onSubmit`). */
  submit?: boolean | undefined;
}

export interface ThemeButtonProps {
  label: string;
  emphasis?: "primary" | "danger" | undefined;
  disabled: boolean;
  /** Render a plain `type="button"`: submitting is the kit's, through `onPress`. */
  onPress: () => void;
}

/** A button. Give it `to` to navigate, `onPress` to do anything else, `submit` to submit its form. */
export function Button({ id, label, emphasis, disabled, submit, onPress, ...target }: ButtonProps) {
  const Themed = useThemed("Button");
  const submitForm = useContext(FormSubmitContext);
  const press = usePress({
    ...target,
    onPress: () => {
      onPress?.();
      if (submit) submitForm?.();
    },
  });
  return (
    <SelectableBox id={requireId("Button", id)} label={label} inline to={target.to}>
      <Themed label={label} emphasis={emphasis} disabled={disabled ?? false} onPress={press} />
    </SelectableBox>
  );
}

export interface LinkProps extends Pressable {
  id: string;
  label: string;
}

export interface ThemeLinkProps {
  label: string;
  onPress: () => void;
}

/** An inline link; presses like a Button. */
export function Link({ id, label, ...pressable }: LinkProps) {
  const Themed = useThemed("Link");
  const press = usePress(pressable);
  return (
    <SelectableBox id={requireId("Link", id)} label={label} inline to={pressable.to}>
      <Themed label={label} onPress={press} />
    </SelectableBox>
  );
}

export interface TextProps {
  id: string;
  text: string;
  /** `secondary` (default) for body copy, `primary` for emphasis. */
  tone?: "primary" | "secondary" | undefined;
}

export interface ThemeTextProps {
  text: string;
  tone: "primary" | "secondary";
}

/** A paragraph of body text. */
export function Text({ id, text, tone = "secondary" }: TextProps) {
  const Themed = useThemed("Text");
  return (
    <SelectableBox id={requireId("Text", id)} label={text}>
      <Themed text={text} tone={tone} />
    </SelectableBox>
  );
}

export interface HeadingProps {
  id: string;
  text: string;
  /** `page` (default) for a screen's title, `section` for a part of it. */
  level?: "page" | "section" | undefined;
  /** Buttons and Links beside the heading. */
  actions?: ReactNode;
}

export interface ThemeHeadingProps {
  text: string;
  level: "page" | "section";
  actions?: ReactNode;
}

/** A page or section title, with the actions beside it. */
export function Heading({ id, text, level = "page", actions }: HeadingProps) {
  const Themed = useThemed("Heading");
  return (
    <SelectableBox id={requireId("Heading", id)} label={text} container={actions !== undefined}>
      <Themed text={text} level={level} actions={actions} />
    </SelectableBox>
  );
}

export interface BadgeProps {
  id: string;
  label: string;
  tone?: Tone | undefined;
}

export interface ThemeBadgeProps {
  label: string;
  tone: Tone;
}

/** A status chip. */
export function Badge({ id, label, tone = "default" }: BadgeProps) {
  const Themed = useThemed("Badge");
  return (
    <SelectableBox id={requireId("Badge", id)} label={label} inline>
      <Themed label={label} tone={tone} />
    </SelectableBox>
  );
}

export interface StatProps {
  id: string;
  label: string;
  value: string;
}

export interface ThemeStatProps {
  label: string;
  value: string;
}

/** A headline number with its label. */
export function Stat({ id, label, value }: StatProps) {
  const Themed = useThemed("Stat");
  return (
    <SelectableBox id={requireId("Stat", id)} label={label}>
      <Themed label={label} value={value} />
    </SelectableBox>
  );
}

export interface AlertProps {
  id: string;
  tone: Tone;
  title?: string | undefined;
  text: string;
}

export interface ThemeAlertProps {
  tone: Tone;
  title?: string | undefined;
  text: string;
}

/** A callout: info, success, warning or error. */
export function Alert({ id, tone, title, text }: AlertProps) {
  const Themed = useThemed("Alert");
  return (
    <SelectableBox id={requireId("Alert", id)} label={title ?? text}>
      <Themed tone={tone} title={title} text={text} />
    </SelectableBox>
  );
}

export interface EmptyStateProps {
  id: string;
  title: string;
  text: string;
  /** The Buttons that start something. */
  actions?: ReactNode;
}

export interface ThemeEmptyStateProps {
  title: string;
  text: string;
  actions?: ReactNode;
}

/** What a list shows when there is nothing in it. */
export function EmptyState({ id, title, text, actions }: EmptyStateProps) {
  const Themed = useThemed("EmptyState");
  return (
    <SelectableBox id={requireId("EmptyState", id)} label={title} container={actions !== undefined}>
      <Themed title={title} text={text} actions={actions} />
    </SelectableBox>
  );
}
