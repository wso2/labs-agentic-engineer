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

import { z } from "zod";
import type { GenUiComponentDef } from "./types.js";

const tone = z.enum(["neutral", "info", "success", "warning", "error"]);

/** The catalog's tones. Each design system maps them to its own colours. */
export type GenUiTone = z.output<typeof tone>;

/**
 * The components a model may use. Keep this list small and the descriptions
 * short: every entry is sent to the model on every generation request.
 */
export const genUiComponents = {
  Stack: {
    props: z.object({
      direction: z.enum(["row", "column"]).optional(),
      gap: z.number().int().min(0).max(6).optional(),
      justify: z.enum(["start", "between"]).optional(),
    }),
    description:
      "Lays its children out in a column (default) or a row. gap is a spacing step, default 2. justify 'between' pushes a row's first and last children to the edges.",
    hasChildren: true,
    events: [],
  },
  Card: {
    props: z.object({
      title: z.string(),
      subtitle: z.string().optional(),
    }),
    description: "A panel with a title that groups related content.",
    hasChildren: true,
    events: [],
  },
  Heading: {
    props: z.object({
      text: z.string(),
      level: z.enum(["page", "section", "subsection"]).optional(),
    }),
    description:
      "A heading. page for the one title of a whole view; defaults to section.",
    hasChildren: false,
    events: [],
  },
  Text: {
    props: z.object({
      text: z.string(),
      tone: z.enum(["default", "muted"]).optional(),
    }),
    description: "A paragraph of plain text. Use muted for secondary detail.",
    hasChildren: false,
    events: [],
  },
  StatusChip: {
    props: z.object({
      label: z.string(),
      tone: tone.optional(),
    }),
    description:
      "A small coloured status label, e.g. 'In progress' (info), 'Deployed' (success), 'Failed' (error).",
    hasChildren: false,
    events: [],
  },
  KeyValueList: {
    props: z.object({
      items: z
        .array(z.object({ label: z.string(), value: z.string() }))
        .min(1),
      layout: z.enum(["list", "columns"]).optional(),
    }),
    description:
      "Label/value pairs describing one thing, e.g. owner, version, endpoint. columns lays them side by side as a summary strip.",
    hasChildren: false,
    events: [],
  },
  DataTable: {
    props: z.object({
      columns: z
        .array(z.object({ key: z.string(), label: z.string() }))
        .min(1),
      rows: z.array(
        z.record(z.string(), z.union([z.string(), z.number(), z.null()])),
      ),
    }),
    description:
      "A table. Each row maps a column key to its cell value; a missing key renders as a dash.",
    hasChildren: false,
    events: [],
  },
  Alert: {
    props: z.object({
      tone: tone.exclude(["neutral"]).optional(),
      title: z.string().optional(),
      message: z.string(),
    }),
    description:
      "A callout for something the reader must notice. Defaults to info.",
    hasChildren: false,
    events: [],
  },
  Progress: {
    props: z.object({
      value: z.number().min(0).max(100),
      label: z.string().optional(),
    }),
    description: "A progress bar from 0 to 100 with an optional caption.",
    hasChildren: false,
    events: [],
  },
  Divider: {
    props: z.object({}),
    description: "A horizontal rule between sections.",
    hasChildren: false,
    events: [],
  },
  Metric: {
    props: z.object({
      label: z.string(),
      value: z.union([z.string(), z.number()]),
    }),
    description:
      "One headline number with its label, e.g. tests passed, cost this month. Put several in a row Stack.",
    hasChildren: false,
    events: [],
  },
  CodeSnippet: {
    props: z.object({
      code: z.string(),
      language: z
        .enum(["bash", "json", "typescript", "javascript", "tsx", "jsx", "css", "html"])
        .optional(),
    }),
    description: "A block of code or command output, syntax highlighted.",
    hasChildren: false,
    events: [],
  },
  TaskList: {
    props: z.object({
      tasks: z
        .array(
          z.object({
            number: z.number().int().positive(),
            title: z.string(),
            state: z.enum(["pending", "in_progress", "pr_sent", "blocked", "merged"]),
            detail: z.string().optional(),
            updated: z.string().optional(),
          }),
        )
        .min(1),
    }),
    description:
      "The delivery tasks of a build (GitHub issues) with each one's state, an optional one-line detail, and when it last changed.",
    hasChildren: false,
    events: [],
  },
  Deployments: {
    props: z.object({
      environments: z
        .array(
          z.object({
            environment: z.string(),
            state: z.enum(["not_deployed", "deploying", "deployed", "failed"]),
            version: z.string().optional(),
            url: z.url({ protocol: /^https?$/ }).optional(),
          }),
        )
        .min(1),
    }),
    description:
      "Where the project is deployed: one row per environment with its state, version and URL.",
    hasChildren: false,
    events: [],
  },
  ValidationResults: {
    props: z.object({
      scenarios: z
        .array(
          z.object({
            name: z.string(),
            result: z.enum(["passed", "partial", "failed", "skipped"]),
          }),
        )
        .min(1),
    }),
    description:
      "Acceptance-criteria scenarios checked against a deployment, each with its result.",
    hasChildren: false,
    events: [],
  },
  Section: {
    props: z.object({
      title: z.string(),
      summary: z.string().optional(),
      badge: z.object({ label: z.string(), tone: tone.optional() }).optional(),
      collapsed: z.boolean().optional(),
    }),
    description:
      "A titled, collapsible part of a page (e.g. Tasks, Build logs). summary is a short count beside the title; badge a status beside it. Open unless collapsed.",
    hasChildren: true,
    events: [],
  },
  AgentTimeline: {
    props: z.object({
      total: z.string(),
      lanes: z
        .array(
          z.object({
            name: z.string(),
            depth: z.number().int().min(0).max(3).optional(),
            start: z.number().min(0).max(100),
            end: z.number().min(0).max(100),
            duration: z.string(),
            state: z.enum(["working", "waiting"]).optional(),
          }),
        )
        .min(1),
    }),
    description:
      "When each agent of a coding run was busy: one lane per agent, start and end as percentages of the run, depth for sub-agents. total summarises the run, e.g. '34m26s across 5 agents'.",
    hasChildren: false,
    events: [],
  },
  Button: {
    props: z.object({
      label: z.string(),
      variant: z.enum(["primary", "secondary", "danger"]).optional(),
      disabled: z.boolean().optional(),
    }),
    description:
      "A button. Bind its press event to an action. Use danger for destructive actions.",
    hasChildren: false,
    events: ["press"],
  },
} as const satisfies Record<string, GenUiComponentDef>;

export type GenUiComponentName = keyof typeof genUiComponents;

/** The parsed props an implementation of component `K` receives. */
export type GenUiPropsOf<K extends GenUiComponentName> = z.output<
  (typeof genUiComponents)[K]["props"]
>;
