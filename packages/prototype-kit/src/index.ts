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

/**
 * `@wso2/prototype-kit`: everything a `prototype.tsx` imports besides React —
 * `defineApp`, the hooks, and the component catalog — plus the theme contract
 * a theme implements. There is no escape hatch to raw HTML, styles or the
 * network.
 */

export { defineApp, type PrototypeAppDefinition } from "./app.js";
export {
  PROTOTYPE_TODAY,
  useCollection,
  useDisplayState,
  useNav,
  useParams,
  useRole,
  useToday,
  useValue,
  type Collection,
  type KitNav,
} from "./runtime/hooks.js";
export type { Pressable } from "./runtime/press.js";
export type { SelectableRootProps } from "./runtime/selectable.js";
export type { KitComponentName, KitComponentProps, PrototypeTheme, ThemeRegistry } from "./theme/contract.js";

export { Detail, Grid, Screen, Section, Split, Stack } from "./components/layout.js";
export { AppShell } from "./components/shell.js";
export type { AppShellProps, AppShellUser, ThemeAppShellProps } from "./components/shell.js";
export type {
  DetailField,
  DetailProps,
  GridProps,
  ScreenProps,
  SectionProps,
  SplitProps,
  StackProps,
  ThemeDetailProps,
  ThemeGridProps,
  ThemeScreenProps,
  ThemeSectionProps,
  ThemeSplitProps,
  ThemeStackProps,
} from "./components/layout.js";
export { Alert, Badge, Button, EmptyState, Heading, Link, Stat, StatGroup, Text } from "./components/content.js";
export type {
  AlertProps,
  BadgeProps,
  ButtonProps,
  EmptyStateProps,
  HeadingProps,
  LinkProps,
  StatGroupProps,
  StatIcon,
  StatProps,
  TextProps,
  ThemeAlertProps,
  ThemeBadgeProps,
  ThemeButtonProps,
  ThemeEmptyStateProps,
  ThemeHeadingProps,
  ThemeLinkProps,
  ThemeStatGroupProps,
  ThemeStatProps,
  ThemeTextProps,
  Tone,
} from "./components/content.js";
export { Breadcrumbs, Navigation, Stepper, Tabs } from "./components/navigation.js";
export type {
  BreadcrumbItem,
  BreadcrumbsProps,
  NavigationItem,
  NavigationProps,
  Panel,
  StepperProps,
  TabsProps,
  ThemeBreadcrumbItem,
  ThemeBreadcrumbsProps,
  ThemeNavigationItem,
  ThemeNavigationProps,
  ThemePanelHeader,
  ThemeStepperProps,
  ThemeTabsProps,
} from "./components/navigation.js";
export { Field, Filters, Form, ValidationSummary } from "./components/forms.js";
export type {
  FieldProps,
  FieldType,
  FiltersProps,
  FormProps,
  ThemeFieldProps,
  ThemeFiltersProps,
  ThemeFormProps,
  ThemeValidationSummaryProps,
  ValidationSummaryProps,
} from "./components/forms.js";
export { Table, Timeline } from "./components/data.js";
export type {
  TableAction,
  TableColumn,
  TableProps,
  TableRow,
  TableStatus,
  ThemeTableAction,
  ThemeTableCell,
  ThemeTableColumn,
  ThemeTableProps,
  ThemeTableRow,
  ThemeTimelineProps,
  TimelineEntry,
  TimelineProps,
} from "./components/data.js";
export { Dialog, Drawer } from "./components/overlays.js";
export type { DialogProps, DrawerProps, ThemeDialogProps, ThemeDrawerProps } from "./components/overlays.js";
