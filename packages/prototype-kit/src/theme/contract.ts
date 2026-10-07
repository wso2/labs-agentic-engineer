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
 * The theme contract. A kit component is a stub: it resolves its id and
 * selection wrapper, applies press semantics, and renders the active theme's
 * implementation with the props below. The registry is a mapped type over
 * every component, so a theme that leaves one out fails to compile.
 */

import type { ComponentType, ReactNode } from "react";
import type { ThemeAlertProps, ThemeBadgeProps, ThemeButtonProps, ThemeEmptyStateProps, ThemeHeadingProps, ThemeLinkProps, ThemeStatGroupProps, ThemeStatProps, ThemeTextProps } from "../components/content.js";
import type { ThemeBreadcrumbsProps, ThemeNavigationProps, ThemeStepperProps, ThemeTabsProps } from "../components/navigation.js";
import type { ThemeFieldProps, ThemeFiltersProps, ThemeFormProps, ThemeValidationSummaryProps } from "../components/forms.js";
import type { ThemeTableProps, ThemeTimelineProps } from "../components/data.js";
import type { ThemeDialogProps, ThemeDrawerProps } from "../components/overlays.js";
import type { ThemeDetailProps, ThemeGridProps, ThemeScreenProps, ThemeSectionProps, ThemeSplitProps, ThemeStackProps } from "../components/layout.js";
import type { ThemeAppShellProps } from "../components/shell.js";

/** The props each kit component's theme implementation receives. */
export interface KitComponentProps {
  Screen: ThemeScreenProps;
  AppShell: ThemeAppShellProps;
  Section: ThemeSectionProps;
  Stack: ThemeStackProps;
  Grid: ThemeGridProps;
  Split: ThemeSplitProps;
  Detail: ThemeDetailProps;
  Alert: ThemeAlertProps;
  Badge: ThemeBadgeProps;
  Button: ThemeButtonProps;
  EmptyState: ThemeEmptyStateProps;
  Heading: ThemeHeadingProps;
  Link: ThemeLinkProps;
  Stat: ThemeStatProps;
  StatGroup: ThemeStatGroupProps;
  Text: ThemeTextProps;
  Breadcrumbs: ThemeBreadcrumbsProps;
  Navigation: ThemeNavigationProps;
  Stepper: ThemeStepperProps;
  Tabs: ThemeTabsProps;
  Field: ThemeFieldProps;
  Filters: ThemeFiltersProps;
  Form: ThemeFormProps;
  ValidationSummary: ThemeValidationSummaryProps;
  Table: ThemeTableProps;
  Timeline: ThemeTimelineProps;
  Dialog: ThemeDialogProps;
  Drawer: ThemeDrawerProps;
}

export type KitComponentName = keyof KitComponentProps;

export type ThemeRegistry = { [K in KitComponentName]: ComponentType<KitComponentProps[K]> };

export interface PrototypeTheme {
  /** The theme's package name, for messages. */
  name: string;
  registry: ThemeRegistry;
  /**
   * Wraps every render: the theme's styles and any context its components
   * need. `colorScheme` is the host's resolved scheme; absent (the render
   * check, a host that names none), the theme picks, typically the system's.
   */
  Provider?: ComponentType<{ children: ReactNode; colorScheme?: "light" | "dark" | undefined }> | undefined;
}
