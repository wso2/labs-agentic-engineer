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

import type { GenUiComponentName } from "../src/catalog/index.js";

// One small spec per catalog component, showing it on its own with the props
// that matter. Keyed by component name, so a catalog component without a
// sample fails to compile. Where a component's options are the point (tones,
// levels, variants), a Stack holds one of each.
export const componentExamples = {
  Stack: {
    root: "row",
    elements: {
      row: { type: "Stack", props: { direction: "row", gap: 1 }, children: ["a", "b"] },
      a: { type: "StatusChip", props: { label: "First child" }, children: [] },
      b: { type: "StatusChip", props: { label: "Second child" }, children: [] },
    },
  },
  Card: {
    root: "card",
    elements: {
      card: {
        type: "Card",
        props: { title: "Checkout service", subtitle: "Web application" },
        children: ["body"],
      },
      body: {
        type: "Text",
        props: { text: "Children render inside the card, one under another." },
        children: [],
      },
    },
  },
  Heading: {
    root: "levels",
    elements: {
      levels: { type: "Stack", props: {}, children: ["page", "section", "subsection"] },
      page: { type: "Heading", props: { text: "Build v2", level: "page" }, children: [] },
      section: { type: "Heading", props: { text: "Deployments" }, children: [] },
      subsection: {
        type: "Heading",
        props: { text: "Development", level: "subsection" },
        children: [],
      },
    },
  },
  Text: {
    root: "texts",
    elements: {
      texts: { type: "Stack", props: { gap: 1 }, children: ["plain", "muted"] },
      plain: { type: "Text", props: { text: "A paragraph of plain text." }, children: [] },
      muted: {
        type: "Text",
        props: { text: "Secondary detail, in the muted tone.", tone: "muted" },
        children: [],
      },
    },
  },
  StatusChip: {
    root: "tones",
    elements: {
      tones: {
        type: "Stack",
        props: { direction: "row", gap: 1 },
        children: ["neutral", "info", "success", "warning", "error"],
      },
      neutral: { type: "StatusChip", props: { label: "Queued" }, children: [] },
      info: { type: "StatusChip", props: { label: "In progress", tone: "info" }, children: [] },
      success: { type: "StatusChip", props: { label: "Deployed", tone: "success" }, children: [] },
      warning: { type: "StatusChip", props: { label: "Blocked", tone: "warning" }, children: [] },
      error: { type: "StatusChip", props: { label: "Failed", tone: "error" }, children: [] },
    },
  },
  KeyValueList: {
    root: "lists",
    elements: {
      lists: { type: "Stack", props: { gap: 3 }, children: ["list", "columns"] },
      list: {
        type: "KeyValueList",
        props: {
          items: [
            { label: "Owner", value: "platform-team" },
            { label: "Version", value: "v0.3.1" },
          ],
        },
        children: [],
      },
      columns: {
        type: "KeyValueList",
        props: {
          layout: "columns",
          items: [
            { label: "Milestone", value: "Milestone #2" },
            { label: "Duration", value: "4h 00m" },
            { label: "Tasks", value: "5 done" },
          ],
        },
        children: [],
      },
    },
  },
  DataTable: {
    root: "table",
    elements: {
      table: {
        type: "DataTable",
        props: {
          columns: [
            { key: "service", label: "Service" },
            { key: "calls", label: "Calls / min" },
            { key: "p95", label: "p95" },
          ],
          rows: [
            { service: "orders-api", calls: 1240, p95: "82 ms" },
            { service: "catalog-api", calls: 860 },
          ],
        },
        children: [],
      },
    },
  },
  Alert: {
    root: "alerts",
    elements: {
      alerts: { type: "Stack", props: { gap: 1 }, children: ["warning", "error"] },
      warning: {
        type: "Alert",
        props: {
          tone: "warning",
          title: "Not ready to deploy yet",
          message: "stripe has no development configuration.",
        },
        children: [],
      },
      error: {
        type: "Alert",
        props: { tone: "error", message: "The build failed at the test stage." },
        children: [],
      },
    },
  },
  Progress: {
    root: "progress",
    elements: {
      progress: {
        type: "Progress",
        props: { value: 60, label: "3 of 5 tasks merged" },
        children: [],
      },
    },
  },
  Divider: {
    root: "split",
    elements: {
      split: { type: "Stack", props: {}, children: ["above", "rule", "below"] },
      above: { type: "Text", props: { text: "Above the rule." }, children: [] },
      rule: { type: "Divider", props: {}, children: [] },
      below: { type: "Text", props: { text: "Below the rule." }, children: [] },
    },
  },
  Metric: {
    root: "metrics",
    elements: {
      metrics: { type: "Stack", props: { direction: "row" }, children: ["tests", "cost"] },
      tests: { type: "Metric", props: { label: "Scenarios passing", value: "5 / 7" }, children: [] },
      cost: { type: "Metric", props: { label: "Agent cost", value: "$4.20" }, children: [] },
    },
  },
  CodeSnippet: {
    root: "code",
    elements: {
      code: {
        type: "CodeSnippet",
        props: {
          language: "bash",
          code: "curl -s https://checkout-dev.example.com/healthz\n# {\"status\":\"ok\"}",
        },
        children: [],
      },
    },
  },
  TaskList: {
    root: "tasks",
    elements: {
      tasks: {
        type: "TaskList",
        props: {
          tasks: [
            { number: 41, title: "Checkout API", state: "merged", updated: "Sep 10, 03:14 PM" },
            {
              number: 42,
              title: "Payment form",
              state: "in_progress",
              detail: "Coding agent is writing the card validation.",
            },
            { number: 43, title: "Refund webhook", state: "blocked" },
          ],
        },
        children: [],
      },
    },
  },
  Deployments: {
    root: "deployments",
    elements: {
      deployments: {
        type: "Deployments",
        props: {
          environments: [
            {
              environment: "Development",
              state: "deployed",
              version: "v0.3.1",
              url: "https://checkout-dev.example.com",
            },
            { environment: "Production", state: "not_deployed" },
          ],
        },
        children: [],
      },
    },
  },
  ValidationResults: {
    root: "results",
    elements: {
      results: {
        type: "ValidationResults",
        props: {
          scenarios: [
            { name: "Shopper pays with a saved card", result: "passed" },
            { name: "Declined card shows a retry prompt", result: "partial" },
            { name: "Refund reaches the shopper", result: "failed" },
          ],
        },
        children: [],
      },
    },
  },
  Section: {
    root: "section",
    elements: {
      section: {
        type: "Section",
        props: {
          title: "External resources",
          summary: "1 in this build",
          badge: { label: "1 of 1 configured", tone: "success" },
        },
        children: ["body"],
      },
      body: {
        type: "Text",
        props: { text: "Children render when the section is open. Click the title to collapse it." },
        children: [],
      },
    },
  },
  AgentTimeline: {
    root: "timeline",
    elements: {
      timeline: {
        type: "AgentTimeline",
        props: {
          total: "20m across 3 agents",
          lanes: [
            { name: "lead agent", start: 0, end: 100, duration: "20m00s" },
            { name: "Build orders-api", depth: 1, start: 5, end: 60, duration: "11m00s" },
            {
              name: "Wait for review",
              depth: 1,
              start: 60,
              end: 90,
              duration: "6m00s",
              state: "waiting",
            },
          ],
        },
        children: [],
      },
    },
  },
  Field: {
    root: "fields",
    elements: {
      fields: { type: "Stack", props: {}, children: ["email", "plan", "notes", "terms", "invalid"] },
      email: {
        type: "Field",
        props: {
          type: "email",
          label: "Contact email",
          required: true,
          value: { $bindState: "/sample/email" },
          helperText: "What you type is written to /sample/email.",
        },
        children: [],
      },
      plan: {
        type: "Field",
        props: {
          type: "select",
          label: "Plan",
          value: { $bindState: "/sample/plan" },
          options: [
            { value: "free", label: "Free" },
            { value: "team", label: "Team" },
            { value: "enterprise", label: "Enterprise" },
          ],
        },
        children: [],
      },
      notes: {
        type: "Field",
        props: { type: "textarea", label: "Notes", value: { $bindState: "/sample/notes" } },
        children: [],
      },
      terms: {
        type: "Field",
        props: {
          type: "checkbox",
          label: "Send receipts by email",
          value: { $bindState: "/sample/receipts" },
        },
        children: [],
      },
      invalid: {
        type: "Field",
        props: {
          type: "text",
          label: "Name",
          value: "",
          required: true,
          error: "Enter the customer's name.",
        },
        children: [],
      },
    },
  },
  Button: {
    root: "buttons",
    elements: {
      buttons: {
        type: "Stack",
        props: { direction: "row", gap: 1 },
        children: ["primary", "secondary", "danger"],
      },
      primary: {
        type: "Button",
        props: { label: "Approve" },
        on: { press: { action: "approveDependency", params: { dependencyId: "payments-db" } } },
        children: [],
      },
      secondary: {
        type: "Button",
        props: { label: "Open task #43", variant: "secondary" },
        on: { press: { action: "openTask", params: { taskNumber: 43 } } },
        children: [],
      },
      danger: {
        type: "Button",
        props: { label: "Reject", variant: "danger" },
        on: {
          press: {
            action: "rejectDependency",
            params: { dependencyId: "payments-db", reason: "Not needed yet" },
          },
        },
        children: [],
      },
    },
  },
} satisfies Record<GenUiComponentName, unknown>;
