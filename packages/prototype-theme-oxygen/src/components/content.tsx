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

import { Alert as OxygenAlert, AlertTitle, Box, Button as OxygenButton, Card, CardContent, Chip, Link as OxygenLink, ListingTable, PageTitle, Typography } from "@wso2/oxygen-ui";
import {
  Activity,
  Bell,
  Briefcase,
  Bug,
  Building2,
  Calendar,
  CalendarCheck,
  CalendarClock,
  CalendarDays,
  ChartColumn,
  CircleCheck,
  CircleX,
  ClipboardList,
  Clock,
  Cloud,
  Cpu,
  CreditCard,
  Database,
  DollarSign,
  FileText,
  Gauge,
  Globe,
  HeartPulse,
  Hourglass,
  Inbox,
  Layers,
  ListChecks,
  Lock,
  Mail,
  MessageSquare,
  Package,
  Plane,
  Receipt,
  Rocket,
  Server,
  Shield,
  ShoppingCart,
  Star,
  Tag,
  Ticket,
  Timer,
  TrendingDown,
  TrendingUp,
  TriangleAlert,
  Truck,
  User,
  UserCheck,
  Users,
  Wallet,
  Zap,
  type LucideIcon,
} from "@wso2/oxygen-ui-icons-react";
import type { ThemeAlertProps, ThemeBadgeProps, ThemeButtonProps, ThemeEmptyStateProps, ThemeHeadingProps, ThemeLinkProps, ThemeStatGroupProps, ThemeStatProps, ThemeTextProps, StatIcon, Tone } from "@wso2/prototype-kit";
import { SectionHeader } from "./layout.js";

export function Button({ label, emphasis, disabled, onPress }: ThemeButtonProps) {
  return (
    <OxygenButton
      type="button"
      variant={emphasis === undefined ? "outlined" : "contained"}
      color={emphasis === "danger" ? "error" : "primary"}
      disabled={disabled}
      disableRipple
      onClick={onPress}
    >
      {label}
    </OxygenButton>
  );
}

export function Link({ label, onPress }: ThemeLinkProps) {
  return (
    <OxygenLink component="button" type="button" variant="body2" onClick={onPress}>
      {label}
    </OxygenLink>
  );
}

export function Text({ text, tone }: ThemeTextProps) {
  return (
    <Typography variant="body2" color={tone === "primary" ? "text.primary" : "text.secondary"}>
      {text}
    </Typography>
  );
}

export function Heading({ text, level, actions }: ThemeHeadingProps) {
  if (level === "page") {
    return (
      <PageTitle sx={{ mb: 0 }}>
        <PageTitle.Header>{text}</PageTitle.Header>
        {actions !== undefined && <PageTitle.Actions>{actions}</PageTitle.Actions>}
      </PageTitle>
    );
  }
  return <SectionHeader title={text} actions={actions} />;
}

export function Badge({ label, tone }: ThemeBadgeProps) {
  return <Chip size="small" label={label} color={tone} variant={tone === "default" ? "outlined" : "filled"} />;
}

/** Every kit icon, from Oxygen's set: the record's type makes leaving one out a compile error. */
const STAT_ICONS: Record<StatIcon, LucideIcon> = {
  Activity,
  Bell,
  Briefcase,
  Bug,
  Building2,
  Calendar,
  CalendarCheck,
  CalendarClock,
  CalendarDays,
  ChartColumn,
  CircleCheck,
  CircleX,
  ClipboardList,
  Clock,
  Cloud,
  Cpu,
  CreditCard,
  Database,
  DollarSign,
  FileText,
  Gauge,
  Globe,
  HeartPulse,
  Hourglass,
  Inbox,
  Layers,
  ListChecks,
  Lock,
  Mail,
  MessageSquare,
  Package,
  Plane,
  Receipt,
  Rocket,
  Server,
  Shield,
  ShoppingCart,
  Star,
  Tag,
  Ticket,
  Timer,
  TrendingDown,
  TrendingUp,
  TriangleAlert,
  Truck,
  User,
  UserCheck,
  Users,
  Wallet,
  Zap,
};

/** A tone as an Oxygen palette colour: `default` is the primary. */
function paletteOf(tone: Tone): "primary" | "info" | "success" | "warning" | "error" {
  return tone === "default" ? "primary" : tone;
}

/**
 * The KPI tile with a caption, as Oxygen's design guidance composes it:
 * `Card` > `CardContent` > overline label, value, caption. `StatCard` holds
 * only a label and a value (its children are discarded), so it cannot carry
 * the hint; the same tile draws every stat, so a group reads as one.
 */
export function Stat({ label, value, hint, icon, tone }: ThemeStatProps) {
  const Icon = icon && STAT_ICONS[icon];
  return (
    <Card variant="outlined" sx={{ height: "100%" }}>
      <CardContent>
        <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 1 }}>
          <Typography variant="overline" component="p" color="text.secondary" sx={{ lineHeight: 1.6 }}>
            {label}
          </Typography>
          {Icon && (
            <Box aria-hidden="true" sx={{ display: "flex", color: `${paletteOf(tone)}.main` }}>
              <Icon size={20} />
            </Box>
          )}
        </Box>
        <Typography variant="h4" component="p" sx={{ fontWeight: 600, mt: 0.5 }}>
          {value}
        </Typography>
        {hint && (
          <Typography variant="caption" component="p" color="text.secondary">
            {hint}
          </Typography>
        )}
      </CardContent>
    </Card>
  );
}

export function StatGroup({ children }: ThemeStatGroupProps) {
  return <Box sx={{ display: "grid", gap: 2, gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))" }}>{children}</Box>;
}

export function Alert({ tone, title, text }: ThemeAlertProps) {
  return (
    <OxygenAlert role="status" severity={tone === "default" ? "info" : tone}>
      {title && <AlertTitle>{title}</AlertTitle>}
      {text}
    </OxygenAlert>
  );
}

export function EmptyState({ title, text, actions }: ThemeEmptyStateProps) {
  return (
    <Card variant="outlined">
      <ListingTable.EmptyState
        title={title}
        description={text}
        minHeight={0}
        sx={{ py: 5 }}
        {...(actions !== undefined ? { action: <Box sx={{ display: "flex", gap: 1 }}>{actions}</Box> } : {})}
      />
    </Card>
  );
}
