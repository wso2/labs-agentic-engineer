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

/** Content: text, heading, badge, stat and stat group, alert, empty state, button and link. */

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
  /** `page` (default) for a screen's title; `section` titles a part of it. Prefer `<Section>` for a part with its own actions or records. */
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

/**
 * The icons a `<Stat>` may show, named as in WSO2's Oxygen icon set (Lucide's
 * names). A theme draws each or none; an unknown name fails the check.
 */
export type StatIcon =
  | "Activity"
  | "Bell"
  | "Briefcase"
  | "Bug"
  | "Building2"
  | "Calendar"
  | "CalendarCheck"
  | "CalendarClock"
  | "CalendarDays"
  | "ChartColumn"
  | "CircleCheck"
  | "CircleX"
  | "ClipboardList"
  | "Clock"
  | "Cloud"
  | "Cpu"
  | "CreditCard"
  | "Database"
  | "DollarSign"
  | "FileText"
  | "Gauge"
  | "Globe"
  | "HeartPulse"
  | "Hourglass"
  | "Inbox"
  | "Layers"
  | "ListChecks"
  | "Lock"
  | "Mail"
  | "MessageSquare"
  | "Package"
  | "Plane"
  | "Receipt"
  | "Rocket"
  | "Server"
  | "Shield"
  | "ShoppingCart"
  | "Star"
  | "Tag"
  | "Ticket"
  | "Timer"
  | "TrendingDown"
  | "TrendingUp"
  | "TriangleAlert"
  | "Truck"
  | "User"
  | "UserCheck"
  | "Users"
  | "Wallet"
  | "Zap";

/** The names at run time; the record's type keeps it in step with `StatIcon`. */
const STAT_ICONS: Readonly<Record<StatIcon, true>> = {
  Activity: true,
  Bell: true,
  Briefcase: true,
  Bug: true,
  Building2: true,
  Calendar: true,
  CalendarCheck: true,
  CalendarClock: true,
  CalendarDays: true,
  ChartColumn: true,
  CircleCheck: true,
  CircleX: true,
  ClipboardList: true,
  Clock: true,
  Cloud: true,
  Cpu: true,
  CreditCard: true,
  Database: true,
  DollarSign: true,
  FileText: true,
  Gauge: true,
  Globe: true,
  HeartPulse: true,
  Hourglass: true,
  Inbox: true,
  Layers: true,
  ListChecks: true,
  Lock: true,
  Mail: true,
  MessageSquare: true,
  Package: true,
  Plane: true,
  Receipt: true,
  Rocket: true,
  Server: true,
  Shield: true,
  ShoppingCart: true,
  Star: true,
  Tag: true,
  Ticket: true,
  Timer: true,
  TrendingDown: true,
  TrendingUp: true,
  TriangleAlert: true,
  Truck: true,
  User: true,
  UserCheck: true,
  Users: true,
  Wallet: true,
  Zap: true,
};

export interface StatProps {
  id: string;
  label: string;
  value: string;
  /** A short line under the value that gives it scale or context: "of 20 days", "3 due this week". */
  hint?: string | undefined;
  /** An icon beside the label. */
  icon?: StatIcon | undefined;
  /** Colours the icon (`default` is the theme's primary). */
  tone?: Tone | undefined;
}

export interface ThemeStatProps {
  label: string;
  value: string;
  hint?: string | undefined;
  /** Already checked against the kit's icon names. */
  icon?: StatIcon | undefined;
  tone: Tone;
}

/** A headline number with its label. Put a screen's stats side by side in a `<StatGroup>`. */
export function Stat({ id, label, value, hint, icon, tone = "default" }: StatProps) {
  const Themed = useThemed("Stat");
  requireId("Stat", id);
  if (icon !== undefined && !Object.hasOwn(STAT_ICONS, icon)) {
    throw new Error(`<Stat> icon ${JSON.stringify(icon)} is not one of the kit's icons: ${Object.keys(STAT_ICONS).join(", ")}`);
  }
  return (
    <SelectableBox id={id} label={label}>
      <Themed label={label} value={value} hint={hint} icon={icon} tone={tone} />
    </SelectableBox>
  );
}

export interface StatGroupProps {
  /** The `<Stat>`s, in reading order. */
  children?: ReactNode;
}

export interface ThemeStatGroupProps {
  children?: ReactNode;
}

/** A row of `<Stat>`s at equal widths, wrapping on a narrow window. */
export function StatGroup({ children }: StatGroupProps) {
  const Themed = useThemed("StatGroup");
  return <Themed>{children}</Themed>;
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
