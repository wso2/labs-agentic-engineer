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

import { Alert } from "@wso2/oxygen-ui";
import { createGenUiView, type GenUiDesignSystem } from "@aep/ui-genui";
import { GenUiButton, GenUiTextField } from "./components/controls.js";
import {
  GenUiCodeSnippet,
  GenUiDataTable,
  GenUiKeyValueList,
  GenUiMetric,
} from "./components/data.js";
import { GenUiCard, GenUiDivider, GenUiStack } from "./components/layout.js";
import {
  GenUiDeployments,
  GenUiTaskList,
  GenUiValidationResults,
} from "./components/platform.js";
import { GenUiSection } from "./components/section.js";
import { GenUiAlert, GenUiProgress, GenUiStatusChip } from "./components/status.js";
import { GenUiHeading, GenUiText } from "./components/text.js";
import { GenUiAgentTimeline } from "./components/timeline.js";

function InvalidElement({ message }: { message: string }) {
  return (
    <Alert severity="warning" variant="outlined">
      {message}
    </Alert>
  );
}

/** The GenUI catalog rendered with WSO2 Oxygen UI components, unstyled beyond the theme. */
export const oxygenDesignSystem: GenUiDesignSystem = {
  name: "Oxygen UI",
  components: {
    Stack: GenUiStack,
    Card: GenUiCard,
    Heading: GenUiHeading,
    Text: GenUiText,
    StatusChip: GenUiStatusChip,
    KeyValueList: GenUiKeyValueList,
    DataTable: GenUiDataTable,
    Alert: GenUiAlert,
    Progress: GenUiProgress,
    Divider: GenUiDivider,
    Metric: GenUiMetric,
    CodeSnippet: GenUiCodeSnippet,
    TaskList: GenUiTaskList,
    Deployments: GenUiDeployments,
    ValidationResults: GenUiValidationResults,
    Section: GenUiSection,
    AgentTimeline: GenUiAgentTimeline,
    TextField: GenUiTextField,
    Button: GenUiButton,
  },
  InvalidElement,
};

/**
 * Renders a generated UI with Oxygen UI. The host supplies the
 * OxygenUIThemeProvider, so the UI takes on the host's theme.
 */
export const GenUiView = createGenUiView(oxygenDesignSystem);
export type { GenUiViewProps } from "@aep/ui-genui";
