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

// PROTOTYPE (throwaway, issue #813). The expense-approval fixture: one
// web-application, three roles, four display states, every v1 node kind.

import type { PrototypeModelV1 } from "./model";

export const expenseApproval: PrototypeModelV1 = {
  schemaVersion: 1,
  component: "approvals-portal",
  name: "Expense approvals",
  defaultScreenId: "screen.queue",
  roles: [
    { id: "approver", name: "Approver" },
    { id: "employee", name: "Employee" },
    { id: "finance", name: "Finance admin" },
  ],
  states: [
    { id: "state.default", name: "Default" },
    { id: "state.invalid", name: "Validation errors" },
    { id: "state.empty", name: "Nothing to show" },
    { id: "state.failed", name: "Integration failed" },
  ],
  flows: [
    { id: "flow.approve", name: "Approve an expense", roleId: "approver", screenIds: ["screen.queue", "screen.detail"] },
    { id: "flow.submit", name: "Submit an expense", roleId: "employee", screenIds: ["screen.new", "screen.mine"] },
    { id: "flow.month-end", name: "Month-end review", roleId: "finance", screenIds: ["screen.reports", "screen.detail"] },
  ],
  navigation: [
    {
      id: "nav.main",
      kind: "side-nav",
      items: [
        { id: "nav.queue", label: "Approval queue", action: { kind: "navigate", screenId: "screen.queue" }, roleIds: ["approver", "finance"] },
        { id: "nav.mine", label: "My expenses", action: { kind: "navigate", screenId: "screen.mine" }, roleIds: ["employee"] },
        { id: "nav.new", label: "New expense", action: { kind: "navigate", screenId: "screen.new" }, roleIds: ["employee"] },
        { id: "nav.reports", label: "Reports", action: { kind: "navigate", screenId: "screen.reports" }, roleIds: ["finance"] },
      ],
    },
  ],
  screens: [
    {
      id: "screen.queue",
      name: "Approval queue",
      roleIds: ["approver", "finance"],
      navigationId: "nav.main",
      content: [
        { id: "heading.queue", kind: "heading", text: "Approval queue", actions: [{ id: "btn.export", label: "Export", action: { kind: "show-drawer", drawerId: "drawer.export" } }] },
        {
          id: "alert.sync-failed",
          kind: "alert",
          tone: "error",
          title: "Expense feed unavailable",
          text: "The ERP integration has not responded since 08:12. Amounts may be stale.",
          showIn: ["state.failed"],
        },
        {
          id: "grid.stats",
          kind: "grid",
          columns: 3,
          content: [
            { id: "stat.pending", kind: "stat", label: "Awaiting approval", value: "12" },
            { id: "stat.overdue", kind: "stat", label: "Overdue", value: "3" },
            { id: "stat.total", kind: "stat", label: "This month", value: "$18,420" },
          ],
        },
        {
          id: "filters.queue",
          kind: "filters",
          fields: [
            { id: "filter.status", label: "Status", type: "select", value: "Awaiting approval", options: ["Awaiting approval", "Approved", "Rejected"] },
            { id: "filter.team", label: "Team", type: "select", value: "All teams", options: ["All teams", "Platform", "Sales"] },
            { id: "filter.overdue", label: "Overdue only", type: "switch" },
          ],
        },
        {
          id: "queue.expenses",
          kind: "task-queue",
          title: "Expenses",
          columns: ["Employee", "Amount", "Category", "Submitted", "Status"],
          onRow: { kind: "navigate", screenId: "screen.detail" },
          rows: [
            { id: "expense.1042", values: { Employee: "Maya Fernando", Amount: "$148.20", Category: "Travel", Submitted: "2 days ago", Status: "Awaiting approval" } },
            { id: "expense.1041", values: { Employee: "Dilan Perera", Amount: "$1,250.00", Category: "Conference", Submitted: "3 days ago", Status: "Awaiting approval" }, tone: "warning" },
            { id: "expense.1039", values: { Employee: "Sara Kim", Amount: "$32.90", Category: "Meals", Submitted: "6 days ago", Status: "Overdue" }, tone: "error" },
            { id: "expense.1038", values: { Employee: "Ravi Nair", Amount: "$410.00", Category: "Equipment", Submitted: "1 week ago", Status: "Overdue" }, tone: "error" },
          ],
        },
        {
          id: "empty.queue",
          kind: "empty-state",
          title: "Nothing waiting",
          text: "Every expense has been decided. New submissions land here.",
          showIn: ["state.empty"],
        },
      ],
      overlays: [
        {
          kind: "drawer",
          id: "drawer.export",
          title: "Export queue",
          content: [
            { id: "text.export", kind: "text", text: "Choose a format. The export is prepared in the background and emailed to you." },
            { id: "form.export", kind: "form", fields: [{ id: "field.format", label: "Format", type: "select", value: "CSV", options: ["CSV", "XLSX"] }], actions: [{ id: "btn.export.go", label: "Prepare export", primary: true, action: { kind: "close-overlay" } }] },
          ],
        },
      ],
    },
    {
      id: "screen.detail",
      name: "Expense detail",
      roleIds: ["approver", "finance"],
      navigationId: "nav.main",
      content: [
        { id: "crumbs.detail", kind: "breadcrumbs", items: [{ id: "crumb.queue", label: "Approval queue", action: { kind: "navigate", screenId: "screen.queue" } }, { id: "crumb.detail", label: "#1042" }] },
        { id: "heading.detail", kind: "heading", text: "Expense #1042 · Maya Fernando" },
        {
          id: "split.detail",
          kind: "split",
          ratio: 7,
          left: [
            {
              id: "detail.expense",
              kind: "detail",
              title: "Claim",
              fields: [
                { label: "Amount", value: "$148.20" },
                { label: "Category", value: "Travel" },
                { label: "Cost centre", value: "PLAT-220" },
                { label: "Receipt", value: "taxi-2026-09-18.pdf" },
                { label: "Note", value: "Airport taxi after the customer workshop." },
              ],
            },
            {
              id: "timeline.expense",
              kind: "timeline",
              entries: [
                { id: "tl.1", when: "Sep 18, 17:40", who: "Maya Fernando", text: "Submitted the claim" },
                { id: "tl.2", when: "Sep 18, 17:41", who: "Platform", text: "Policy check passed" },
                { id: "tl.3", when: "Sep 19, 09:02", who: "You", text: "Opened for review" },
              ],
            },
          ],
          right: [
            {
              id: "approval.expense",
              kind: "approval-panel",
              title: "Decision",
              summary: "Within policy. No duplicate found in the last 90 days.",
              actions: [
                { id: "btn.approve", label: "Approve", primary: true, action: { kind: "show-dialog", dialogId: "dialog.approve" } },
                { id: "btn.reject", label: "Reject", danger: true, action: { kind: "show-dialog", dialogId: "dialog.reject" } },
              ],
            },
          ],
        },
      ],
      overlays: [
        {
          kind: "dialog",
          id: "dialog.approve",
          title: "Approve expense #1042?",
          content: [{ id: "text.approve", kind: "text", text: "$148.20 will be scheduled for the next payroll run. This preview changes nothing." }],
          actions: [
            { id: "btn.approve.cancel", label: "Cancel", action: { kind: "close-overlay" } },
            { id: "btn.approve.confirm", label: "Approve", primary: true, action: { kind: "close-overlay" } },
          ],
        },
        {
          kind: "dialog",
          id: "dialog.reject",
          title: "Reject expense #1042",
          content: [{ id: "form.reject", kind: "form", fields: [{ id: "field.reason", label: "Reason", type: "textarea" }], actions: [] }],
          actions: [
            { id: "btn.reject.cancel", label: "Cancel", action: { kind: "close-overlay" } },
            { id: "btn.reject.confirm", label: "Reject", danger: true, action: { kind: "close-overlay" } },
          ],
        },
      ],
    },
    {
      id: "screen.new",
      name: "New expense",
      roleIds: ["employee"],
      navigationId: "nav.main",
      content: [
        { id: "heading.new", kind: "heading", text: "New expense" },
        { id: "validation.new", kind: "validation-summary", issues: ["Amount must be greater than zero", "A receipt is required over $25"], showIn: ["state.invalid"] },
        {
          id: "stepper.new",
          kind: "stepper",
          steps: [
            {
              id: "step.details",
              label: "Details",
              content: [
                {
                  id: "form.details",
                  kind: "form",
                  fields: [
                    { id: "field.amount", label: "Amount", value: "0.00", error: "Must be greater than zero", errorIn: ["state.invalid"] },
                    { id: "field.category", label: "Category", type: "select", value: "Travel", options: ["Travel", "Meals", "Equipment", "Conference"] },
                    { id: "field.date", label: "Date", type: "date", value: "2026-09-18" },
                    { id: "field.note", label: "Note", type: "textarea" },
                  ],
                  actions: [{ id: "btn.next", label: "Next", primary: true, action: { kind: "set-step", stepperId: "stepper.new", stepId: "step.receipt" } }],
                },
              ],
            },
            {
              id: "step.receipt",
              label: "Receipt",
              content: [
                { id: "text.receipt", kind: "text", text: "Attach the receipt. Photos and PDFs are accepted." },
                { id: "form.receipt", kind: "form", fields: [{ id: "field.receipt", label: "Receipt file", value: "taxi-2026-09-18.pdf", error: "A receipt is required over $25", errorIn: ["state.invalid"] }], actions: [
                  { id: "btn.back", label: "Back", action: { kind: "set-step", stepperId: "stepper.new", stepId: "step.details" } },
                  { id: "btn.review", label: "Review", primary: true, action: { kind: "set-step", stepperId: "stepper.new", stepId: "step.review" } },
                ] },
              ],
            },
            {
              id: "step.review",
              label: "Review",
              content: [
                { id: "detail.review", kind: "detail", title: "Summary", fields: [{ label: "Amount", value: "$148.20" }, { label: "Category", value: "Travel" }, { label: "Receipt", value: "taxi-2026-09-18.pdf" }] },
                { id: "btn.submit", kind: "button", label: "Submit for approval", primary: true, action: { kind: "show-dialog", dialogId: "dialog.submitted" } },
              ],
            },
          ],
        },
      ],
      overlays: [
        {
          kind: "dialog",
          id: "dialog.submitted",
          title: "Submitted",
          content: [{ id: "text.submitted", kind: "text", text: "Your claim is with your approver. Nothing was actually sent in this preview." }],
          actions: [{ id: "btn.submitted.ok", label: "Go to my expenses", primary: true, action: { kind: "navigate", screenId: "screen.mine" } }],
        },
      ],
    },
    {
      id: "screen.mine",
      name: "My expenses",
      roleIds: ["employee"],
      navigationId: "nav.main",
      content: [
        { id: "heading.mine", kind: "heading", text: "My expenses", actions: [{ id: "btn.new", label: "New expense", primary: true, action: { kind: "navigate", screenId: "screen.new" } }] },
        {
          id: "tabs.mine",
          kind: "tabs",
          tabs: [
            {
              id: "tab.open",
              label: "Open",
              content: [
                { id: "table.mine.open", kind: "table", columns: ["Reference", "Amount", "Category", "Status"], rows: [
                  { id: "mine.1042", values: { Reference: "#1042", Amount: "$148.20", Category: "Travel", Status: "Awaiting approval" } },
                  { id: "mine.1030", values: { Reference: "#1030", Amount: "$65.00", Category: "Meals", Status: "Needs receipt" }, tone: "warning" },
                ] },
              ],
            },
            {
              id: "tab.paid",
              label: "Paid",
              content: [
                { id: "table.mine.paid", kind: "table", columns: ["Reference", "Amount", "Paid on"], rows: [
                  { id: "mine.1001", values: { Reference: "#1001", Amount: "$220.00", "Paid on": "Aug 28" } },
                ] },
              ],
            },
          ],
        },
      ],
    },
    {
      id: "screen.reports",
      name: "Reports",
      roleIds: ["finance"],
      navigationId: "nav.main",
      content: [
        { id: "heading.reports", kind: "heading", text: "Month-end report" },
        { id: "alert.reports.failed", kind: "alert", tone: "warning", text: "Ledger sync is delayed. Totals exclude the last 4 hours.", showIn: ["state.failed"] },
        { id: "grid.reports", kind: "grid", columns: 3, content: [
          { id: "stat.approved", kind: "stat", label: "Approved", value: "$14,980" },
          { id: "stat.rejected", kind: "stat", label: "Rejected", value: "$1,120" },
          { id: "stat.pending2", kind: "stat", label: "Still pending", value: "$2,320" },
        ] },
        { id: "table.byteam", kind: "table", title: "By team", columns: ["Team", "Claims", "Approved", "Rejected"], onRow: { kind: "navigate", screenId: "screen.detail" }, rows: [
          { id: "team.platform", values: { Team: "Platform", Claims: "24", Approved: "$8,410", Rejected: "$320" } },
          { id: "team.sales", values: { Team: "Sales", Claims: "31", Approved: "$6,570", Rejected: "$800" } },
        ] },
        { id: "empty.reports", kind: "empty-state", title: "No claims this month", text: "Reports fill in as claims are decided.", showIn: ["state.empty"] },
        { id: "link.policy", kind: "link", label: "Expense policy (opens the queue in this preview)", action: { kind: "navigate", screenId: "screen.queue" } },
      ],
    },
  ],
};
