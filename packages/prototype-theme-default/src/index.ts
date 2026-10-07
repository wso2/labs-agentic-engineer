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
 * `@wso2/prototype-theme-default`: every kit component as plain React and
 * CSS custom properties — no component library, so the runtimes stay small.
 * The registry's type makes leaving a component out a compile error.
 */

import type { PrototypeTheme } from "@wso2/prototype-kit";
import { Alert, Badge, Button, EmptyState, Heading, Link, Stat, StatGroup, Text } from "./components/content.js";
import { Table, Timeline } from "./components/data.js";
import { Field, Filters, Form, ValidationSummary } from "./components/forms.js";
import { Detail, Grid, Screen, Section, Split, Stack } from "./components/layout.js";
import { Breadcrumbs, Navigation, Stepper, Tabs } from "./components/navigation.js";
import { Dialog, Drawer } from "./components/overlays.js";
import { AppShell } from "./components/shell.js";
import { ThemeProvider } from "./provider.js";

const theme: PrototypeTheme = {
  name: "@wso2/prototype-theme-default",
  Provider: ThemeProvider,
  registry: {
    Screen,
    AppShell,
    Section,
    Stack,
    Grid,
    Split,
    Detail,
    Alert,
    Badge,
    Button,
    EmptyState,
    Heading,
    Link,
    Stat,
    StatGroup,
    Text,
    Breadcrumbs,
    Navigation,
    Stepper,
    Tabs,
    Field,
    Filters,
    Form,
    ValidationSummary,
    Table,
    Timeline,
    Dialog,
    Drawer,
  },
};

export default theme;
