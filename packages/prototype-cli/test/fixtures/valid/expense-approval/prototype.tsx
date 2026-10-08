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

// Fixture (ported from the template-react snapshot): an expense approval
// portal. Three roles, four display states, and between its screens nearly
// every kit component; decisions and new claims change the shared collection.

import { useState } from "react";
import {
  Alert,
  Badge,
  Breadcrumbs,
  Button,
  Detail,
  Dialog,
  Drawer,
  EmptyState,
  Field,
  Filters,
  Form,
  Grid,
  Heading,
  Link,
  Navigation,
  Screen,
  Split,
  Stack,
  Stat,
  Stepper,
  Table,
  Tabs,
  Text,
  Timeline,
  ValidationSummary,
  defineApp,
  useCollection,
  useDisplayState,
  useNav,
  useParams,
  useRole,
} from "@wso2/prototype-kit";

type Status = "Awaiting approval" | "Overdue" | "Approved" | "Rejected";

interface Expense {
  id: string;
  employee: string;
  amount: string;
  category: string;
  submitted: string;
  status: Status;
}

const expenses: Expense[] = [
  { id: "1042", employee: "Maya Fernando", amount: "$148.20", category: "Travel", submitted: "2 days ago", status: "Awaiting approval" },
  { id: "1041", employee: "Dilan Perera", amount: "$1,250.00", category: "Conference", submitted: "3 days ago", status: "Awaiting approval" },
  { id: "1039", employee: "Sara Kim", amount: "$32.90", category: "Meals", submitted: "6 days ago", status: "Overdue" },
  { id: "1038", employee: "Ravi Nair", amount: "$410.00", category: "Equipment", submitted: "1 week ago", status: "Overdue" },
];

const tone = (status: Status) =>
  status === "Overdue" ? "error" : status === "Approved" ? "success" : status === "Rejected" ? "default" : "warning";

const nav = (
  <Navigation
    id="nav.main"
    layout="side"
    items={[
      { id: "nav.queue", label: "Approval queue", to: "screen.queue" },
      { id: "nav.mine", label: "My expenses", to: "screen.mine" },
      { id: "nav.new", label: "New expense", to: "screen.new" },
      { id: "nav.reports", label: "Reports", to: "screen.reports" },
    ]}
  />
);

function Queue() {
  const state = useDisplayState();
  const role = useRole();
  const all = useCollection<Expense>("expenses");
  const [exporting, setExporting] = useState(false);
  const [status, setStatus] = useState("Awaiting approval");
  const shown = state === "state.empty" ? [] : all.items.filter((e) => e.status !== "Approved" && e.status !== "Rejected");
  return (
    <Screen nav={nav}>
      <Heading id="heading.queue" text="Approval queue" actions={<Button id="btn.export" label="Export" onPress={() => setExporting(true)} />} />
      {state === "state.failed" && (
        <Alert id="alert.sync-failed" tone="error" title="Expense feed unavailable" text="The ERP integration has not responded since 08:12. Amounts may be stale." />
      )}
      <Grid columns={3}>
        <Stat id="stat.pending" label="Awaiting approval" value={String(shown.length)} />
        <Stat id="stat.overdue" label="Overdue" value={String(shown.filter((e) => e.status === "Overdue").length)} />
        <Stat id="stat.total" label="This month" value="$18,420" />
      </Grid>
      <Filters id="filters.queue">
        <Field id="filter.status" label="Status" type="select" value={status} options={["Awaiting approval", "Approved", "Rejected"]} onChange={setStatus} />
        <Field id="filter.team" label="Team" type="select" value="All teams" options={["All teams", "Platform", "Sales"]} />
        <Field id="filter.overdue" label="Overdue only" type="switch" value="off" />
      </Filters>
      <Table
        id="queue.expenses"
        title="Expenses"
        columns={["Employee", "Amount", "Category", "Submitted", "Status"]}
        rows={shown.map((e) => ({
          id: `expense.${e.id}`,
          cells: [e.employee, e.amount, e.category, e.submitted, e.status],
          tone: tone(e.status),
          to: "screen.detail",
          params: { expense: e.id },
        }))}
        empty={
          <EmptyState
            id="empty.queue"
            title="Nothing waiting"
            text="Every expense has been decided. New submissions land here."
            actions={role === "finance" ? <Button id="btn.empty.reports" label="Open reports" to="screen.reports" /> : undefined}
          />
        }
      />
      <Link id="link.oldest" label="Jump to the oldest overdue claim" to="screen.detail" params={{ expense: "1038" }} />
      <Drawer id="drawer.export" title="Export queue" open={exporting} onClose={() => setExporting(false)}>
        <Text id="text.export" text="Choose a format. The export is prepared in the background and emailed to you." />
        <Form id="form.export" actions={<Button id="btn.export.go" label="Prepare export" emphasis="primary" onPress={() => setExporting(false)} />}>
          <Field id="field.format" label="Format" type="select" defaultValue="CSV" options={["CSV", "XLSX"]} />
        </Form>
      </Drawer>
    </Screen>
  );
}

function ExpenseDetail() {
  const { expense: id = "1042" } = useParams();
  const state = useDisplayState();
  const role = useRole();
  const nav_ = useNav();
  const all = useCollection<Expense>("expenses");
  const [dialog, setDialog] = useState<"approve" | "reject" | null>(null);
  const [reason, setReason] = useState("");
  const expense = all.get(id) ?? all.items[0]!;
  const decide = (status: Status) => {
    all.update(expense.id, { status });
    setDialog(null);
    nav_.go("screen.queue");
  };
  return (
    <Screen nav={nav}>
      <Breadcrumbs
        id="crumbs.detail"
        items={[
          { id: "crumb.queue", label: "Approval queue", to: "screen.queue" },
          { id: "crumb.detail", label: `#${expense.id}` },
        ]}
      />
      <Stack direction="row">
        <Heading id="heading.detail" text={`Expense #${expense.id} · ${expense.employee}`} />
        <Badge id="badge.status" label={expense.status} tone={tone(expense.status)} />
      </Stack>
      <Split
        ratio={7}
        left={
          <>
            <Detail
              id="detail.expense"
              title="Claim"
              fields={[
                { label: "Amount", value: expense.amount },
                { label: "Category", value: expense.category },
                { label: "Cost centre", value: "PLAT-204" },
                { label: "Submitted", value: expense.submitted },
              ]}
            />
            <Timeline
              id="timeline.expense"
              entries={[
                { when: "Sep 18, 17:40", who: expense.employee, text: "Submitted the claim" },
                { when: "Sep 18, 17:41", who: "Policy check", text: "Within policy" },
              ]}
            />
          </>
        }
        right={
          role === "approver" ? (
            <Stack>
              <Heading id="heading.decision" text="Decision" level="section" />
              <Text id="text.decision" text="Within policy. No duplicate found in the last 90 days." />
              <Stack direction="row">
                <Button id="btn.approve" label="Approve" emphasis="primary" onPress={() => setDialog("approve")} />
                <Button id="btn.reject" label="Reject" emphasis="danger" onPress={() => setDialog("reject")} />
              </Stack>
            </Stack>
          ) : (
            <Text id="text.finance" text="Finance admins review decided claims in the month-end report." />
          )
        }
      />
      <Dialog
        id="dialog.approve"
        title={`Approve expense #${expense.id}?`}
        open={dialog === "approve"}
        onClose={() => setDialog(null)}
        actions={
          <>
            <Button id="btn.approve.cancel" label="Cancel" onPress={() => setDialog(null)} />
            <Button id="btn.approve.confirm" label="Approve" emphasis="primary" onPress={() => decide("Approved")} />
          </>
        }
      >
        <Text id="text.approve" text={`${expense.amount} will be scheduled for the next payroll run.`} />
      </Dialog>
      <Dialog
        id="dialog.reject"
        title={`Reject expense #${expense.id}`}
        open={dialog === "reject"}
        onClose={() => setDialog(null)}
        actions={
          <>
            <Button id="btn.reject.cancel" label="Cancel" onPress={() => setDialog(null)} />
            <Button id="btn.reject.confirm" label="Reject" emphasis="danger" onPress={() => decide("Rejected")} />
          </>
        }
      >
        <Form id="form.reject">
          <Field
            id="field.reason"
            label="Reason"
            type="textarea"
            value={reason}
            onChange={setReason}
            required
            error={state === "state.invalid" ? "Give the employee a reason" : undefined}
          />
        </Form>
      </Dialog>
    </Screen>
  );
}

function NewExpense() {
  const state = useDisplayState();
  const nav_ = useNav();
  const all = useCollection<Expense>("expenses");
  const [step, setStep] = useState("step.details");
  const [amount, setAmount] = useState("148.20");
  const [category, setCategory] = useState("Travel");
  const [submitted, setSubmitted] = useState(false);
  const invalid = state === "state.invalid";
  const submit = () => {
    all.create({ employee: "You", amount: `$${amount}`, category, submitted: "just now", status: "Awaiting approval" });
    setSubmitted(true);
  };
  return (
    <Screen nav={nav}>
      <Heading id="heading.new" text="New expense" />
      {invalid && <ValidationSummary id="validation.new" issues={["Amount must be greater than zero", "A receipt is required over $25"]} />}
      <Stepper
        id="stepper.new"
        active={step}
        onChange={setStep}
        steps={[
          {
            id: "step.details",
            label: "Details",
            content: (
              <Form id="form.details" actions={<Button id="btn.next" label="Next" emphasis="primary" onPress={() => setStep("step.receipt")} />}>
                <Field id="field.amount" label="Amount" type="number" value={invalid ? "0.00" : amount} onChange={setAmount} error={invalid ? "Must be greater than zero" : undefined} />
                <Field id="field.category" label="Category" type="select" value={category} options={["Travel", "Meals", "Equipment", "Conference"]} onChange={setCategory} />
                <Field id="field.date" label="Date" type="date" value="2026-09-18" />
                <Field id="field.note" label="Note" type="textarea" />
              </Form>
            ),
          },
          {
            id: "step.receipt",
            label: "Receipt",
            content: (
              <>
                <Text id="text.receipt" text="Attach the receipt. Photos and PDFs are accepted." />
                <Form
                  id="form.receipt"
                  actions={
                    <>
                      <Button id="btn.back" label="Back" onPress={() => setStep("step.details")} />
                      <Button id="btn.review" label="Review" emphasis="primary" onPress={() => setStep("step.review")} />
                    </>
                  }
                >
                  <Field id="field.receipt" label="Receipt file" value="taxi-2026-09-18.pdf" error={invalid ? "A receipt is required over $25" : undefined} />
                </Form>
              </>
            ),
          },
          {
            id: "step.review",
            label: "Review",
            content: (
              <>
                <Detail
                  id="detail.review"
                  title="Summary"
                  fields={[
                    { label: "Amount", value: `$${amount}` },
                    { label: "Category", value: category },
                    { label: "Receipt", value: "taxi-2026-09-18.pdf" },
                  ]}
                />
                <Button id="btn.submit" label="Submit for approval" emphasis="primary" onPress={submit} />
              </>
            ),
          },
        ]}
      />
      <Dialog
        id="dialog.submitted"
        title="Submitted"
        open={submitted}
        onClose={() => setSubmitted(false)}
        actions={<Button id="btn.submitted.ok" label="Go to my expenses" emphasis="primary" onPress={() => nav_.go("screen.mine")} />}
      >
        <Text id="text.submitted" text="Your claim is with your approver." />
      </Dialog>
    </Screen>
  );
}

function Mine() {
  const state = useDisplayState();
  const all = useCollection<Expense>("expenses");
  const open = state === "state.empty" ? [] : all.items.filter((e) => e.employee === "You" || e.id === "1042");
  return (
    <Screen nav={nav}>
      <Heading id="heading.mine" text="My expenses" actions={<Button id="btn.new" label="New expense" emphasis="primary" to="screen.new" />} />
      <Tabs
        id="tabs.mine"
        tabs={[
          {
            id: "tab.open",
            label: "Open",
            content: (
              <Table
                id="table.mine.open"
                columns={["Reference", "Amount", "Category", "Status"]}
                rows={open.map((e) => ({ id: `mine.${e.id}`, cells: [`#${e.id}`, e.amount, e.category, e.status], tone: tone(e.status) }))}
                empty={<EmptyState id="empty.mine" title="No open claims" text="Claims you submit appear here until they are paid." />}
              />
            ),
          },
          {
            id: "tab.paid",
            label: "Paid",
            content: <Table id="table.mine.paid" columns={["Reference", "Amount", "Paid on"]} rows={[{ id: "mine.1001", cells: ["#1001", "$220.00", "Aug 28"] }]} />,
          },
        ]}
      />
    </Screen>
  );
}

function Reports() {
  const state = useDisplayState();
  return (
    <Screen nav={nav}>
      <Heading id="heading.reports" text="Month-end report" />
      {state === "state.failed" && <Alert id="alert.reports.failed" tone="warning" text="Ledger sync is delayed. Totals exclude the last 4 hours." />}
      {state === "state.empty" ? (
        <EmptyState id="empty.reports" title="No claims this month" text="Reports fill in as claims are decided." />
      ) : (
        <>
          <Grid columns={3}>
            <Stat id="stat.approved" label="Approved" value="$14,980" />
            <Stat id="stat.rejected" label="Rejected" value="$1,120" />
            <Stat id="stat.still-pending" label="Still pending" value="$2,320" />
          </Grid>
          <Table
            id="table.by-team"
            title="By team"
            columns={["Team", "Claims", "Approved", "Rejected"]}
            rows={[
              { id: "team.platform", cells: ["Platform", "24", "$8,410", "$320"] },
              { id: "team.sales", cells: ["Sales", "31", "$6,570", "$800"] },
            ]}
          />
        </>
      )}
      <Link id="link.policy" label="Review the approval queue" to="screen.queue" />
    </Screen>
  );
}

export default defineApp({
  screens: {
    "screen.queue": Queue,
    "screen.detail": ExpenseDetail,
    "screen.new": NewExpense,
    "screen.mine": Mine,
    "screen.reports": Reports,
  },
  data: { expenses },
});
