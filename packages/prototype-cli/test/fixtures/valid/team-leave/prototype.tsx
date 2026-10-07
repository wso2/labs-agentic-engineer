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

// Fixture: a real agent-generated prototype (Team Leave) with My Leave and the
// manager's queue rewritten to the kit's grouped vocabulary — a <StatGroup> of
// <Stat>s with icons and hints, <Section>s that own their actions, and tables
// with a status column and per-row actions. The other screens are as the agent
// wrote them, so Team History still colours its last cell with the older `tone`.

import { useState, type ReactNode } from "react";
import {
  Alert,
  AppShell,
  Button,
  Detail,
  Dialog,
  EmptyState,
  Field,
  Form,
  Heading,
  Screen,
  Section,
  Stack,
  Stat,
  StatGroup,
  Table,
  ValidationSummary,
  defineApp,
  useCollection,
  useDisplayState,
  useNav,
  useParams,
  useRole,
} from "@wso2/prototype-kit";

interface LeaveRequest {
  id: string;
  employeeId: string;
  employeeName: string;
  type: "Vacation" | "Sick" | "Personal";
  startDate: string;
  endDate: string;
  reason: string;
  days: number;
  status: "pending" | "approved" | "rejected";
  comment?: string;
}

interface LeaveBalance {
  id: string;
  employeeId: string;
  employeeName: string;
  type: "Vacation" | "Sick" | "Personal";
  balance: number;
  allowance: number;
}

const leaveRequests: LeaveRequest[] = [
  { id: "req-1001", employeeId: "emp-priya", employeeName: "Priya Nair", type: "Vacation", startDate: "2026-11-02", endDate: "2026-11-04", reason: "Family trip", days: 3, status: "pending" },
  { id: "req-1002", employeeId: "emp-priya", employeeName: "Priya Nair", type: "Sick", startDate: "2026-09-02", endDate: "2026-09-02", reason: "Flu", days: 1, status: "approved" },
  { id: "req-1003", employeeId: "emp-priya", employeeName: "Priya Nair", type: "Personal", startDate: "2026-08-01", endDate: "2026-08-03", reason: "Moving", days: 3, status: "rejected", comment: "No coverage available" },
  { id: "req-2001", employeeId: "emp-alex", employeeName: "Alex Doe", type: "Vacation", startDate: "2026-10-10", endDate: "2026-10-12", reason: "Family trip", days: 3, status: "pending" },
  { id: "req-2002", employeeId: "emp-sam", employeeName: "Sam Lee", type: "Sick", startDate: "2026-10-15", endDate: "2026-10-16", reason: "Doctor visit", days: 2, status: "pending" },
  { id: "req-2003", employeeId: "emp-alex", employeeName: "Alex Doe", type: "Sick", startDate: "2026-09-02", endDate: "2026-09-02", reason: "Flu", days: 1, status: "approved" },
  { id: "req-2004", employeeId: "emp-sam", employeeName: "Sam Lee", type: "Personal", startDate: "2026-08-01", endDate: "2026-08-03", reason: "Moving", days: 3, status: "rejected", comment: "No coverage available" },
];

const balances: LeaveBalance[] = [
  { id: "bal-priya-vacation", employeeId: "emp-priya", employeeName: "Priya Nair", type: "Vacation", balance: 12, allowance: 20 },
  { id: "bal-priya-sick", employeeId: "emp-priya", employeeName: "Priya Nair", type: "Sick", balance: 6, allowance: 10 },
  { id: "bal-priya-personal", employeeId: "emp-priya", employeeName: "Priya Nair", type: "Personal", balance: 3, allowance: 5 },
  { id: "bal-alex-vacation", employeeId: "emp-alex", employeeName: "Alex Doe", type: "Vacation", balance: 12, allowance: 20 },
  { id: "bal-alex-sick", employeeId: "emp-alex", employeeName: "Alex Doe", type: "Sick", balance: 6, allowance: 10 },
  { id: "bal-alex-personal", employeeId: "emp-alex", employeeName: "Alex Doe", type: "Personal", balance: 3, allowance: 5 },
  { id: "bal-sam-vacation", employeeId: "emp-sam", employeeName: "Sam Lee", type: "Vacation", balance: 8, allowance: 20 },
  { id: "bal-sam-sick", employeeId: "emp-sam", employeeName: "Sam Lee", type: "Sick", balance: 10, allowance: 10 },
  { id: "bal-sam-personal", employeeId: "emp-sam", employeeName: "Sam Lee", type: "Personal", balance: 2, allowance: 5 },
];

const CURRENT_EMPLOYEE_ID = "emp-priya";
const CURRENT_EMPLOYEE_NAME = "Priya Nair";
const CURRENT_EMPLOYEE_EMAIL = "priya.nair@example.com";
const CURRENT_MANAGER_NAME = "Dinesh Shah";
const CURRENT_MANAGER_EMAIL = "dinesh.shah@example.com";

function toneForStatus(status: LeaveRequest["status"]) {
  if (status === "pending") return "warning" as const;
  if (status === "approved") return "success" as const;
  return "error" as const;
}

const STATUS_LABEL: Record<LeaveRequest["status"], string> = { pending: "Pending", approved: "Approved", rejected: "Rejected" };

const LEAVE_ICON = { Vacation: "Plane", Sick: "HeartPulse", Personal: "User" } as const;

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** The calendar days from start to end, both included. */
function daysBetween(start: string, end: string) {
  return Math.round((Date.parse(end) - Date.parse(start)) / 86_400_000) + 1;
}

/** "2 Nov – 4 Nov 2026", or "2 Sep 2026" for one day. */
function dateRange(start: string, end: string) {
  const day = (iso: string) => {
    const [, month, date] = iso.split("-");
    return `${Number(date)} ${MONTHS[Number(month) - 1]}`;
  };
  const year = end.slice(0, 4);
  return start === end ? `${day(start)} ${year}` : `${day(start)} – ${day(end)} ${year}`;
}

function Shell({ children }: { children: ReactNode }) {
  const role = useRole();
  const isManager = role === "Manager";
  const user = isManager
    ? { name: CURRENT_MANAGER_NAME, email: CURRENT_MANAGER_EMAIL, role: "Manager" }
    : { name: CURRENT_EMPLOYEE_NAME, email: CURRENT_EMPLOYEE_EMAIL, role: "Employee" };
  const navItems = isManager
    ? [
        { id: "nav.team-queue", label: "Pending Requests", to: "screen.team-queue" },
        { id: "nav.team-history", label: "Team History", to: "screen.team-history" },
        { id: "nav.team-balances", label: "Team Balances", to: "screen.team-balances" },
      ]
    : [
        { id: "nav.my-leave", label: "My Leave", to: "screen.my-leave" },
        { id: "nav.new-request", label: "New Request", to: "screen.new-request" },
      ];
  return (
    <AppShell
      id="shell"
      user={user}
      nav={navItems}
      account="screen.account"
      settings="screen.settings"
      signOut="screen.signed-out"
    >
      {children}
    </AppShell>
  );
}

function MyLeave() {
  const state = useDisplayState();
  const requestsCol = useCollection<LeaveRequest>("leaveRequests");
  const balancesCol = useCollection<LeaveBalance>("balances");
  const [cancelingId, setCancelingId] = useState<string | null>(null);

  const myBalances = balancesCol.items.filter((b) => b.employeeId === CURRENT_EMPLOYEE_ID);
  const myRequests = state === "state.empty"
    ? []
    : requestsCol.items.filter((r) => r.employeeId === CURRENT_EMPLOYEE_ID);

  const cancelingRequest = cancelingId ? requestsCol.get(cancelingId) : undefined;

  const confirmCancel = () => {
    if (cancelingId) {
      requestsCol.remove(cancelingId);
    }
    setCancelingId(null);
  };

  return (
    <Shell>
      <Heading id="heading.my-leave" text="My Leave" />
      {state === "state.failed" && (
        <Alert id="alert.my-leave-failed" tone="error" title="Could not load your leave data" text="Try again in a few minutes." />
      )}
      <StatGroup>
        {myBalances.map((b) => (
          <Stat
            key={b.id}
            id={`stat.balance.${b.id}`}
            label={b.type}
            value={`${b.balance} days`}
            hint={`of ${b.allowance} left this year`}
            icon={LEAVE_ICON[b.type]}
          />
        ))}
      </StatGroup>
      <Section
        id="section.my-requests"
        title="My Requests"
        count={myRequests.length}
        subtitle="You can cancel a request until your manager decides on it."
        actions={<Button id="btn.new-request" label="New request" emphasis="primary" to="screen.new-request" />}
      >
        <Table
          id="table.my-requests"
          columns={["Dates", "Type", "Reason", { label: "Days", kind: "number" }, { label: "Status", kind: "status" }]}
          rows={myRequests.map((r) => ({
            id: `row.my-request.${r.id}`,
            cells: [dateRange(r.startDate, r.endDate), r.type, r.reason, String(r.days)],
            status: { text: STATUS_LABEL[r.status], tone: toneForStatus(r.status) },
            actions:
              r.status === "pending"
                ? [{ id: `row.my-request.${r.id}.cancel`, label: "Cancel", emphasis: "danger" as const, onPress: () => setCancelingId(r.id) }]
                : [],
          }))}
          empty={
            <EmptyState
              id="empty.my-requests"
              title="No leave requests yet"
              text="Requests you submit show here with their status."
              actions={<Button id="btn.empty.new-request" label="New request" emphasis="primary" to="screen.new-request" />}
            />
          }
        />
      </Section>
      <Dialog
        id="dialog.cancel-request"
        title="Cancel this request?"
        open={Boolean(cancelingRequest)}
        onClose={() => setCancelingId(null)}
        actions={
          <Stack direction="row">
            <Button id="btn.cancel-request.keep" label="Keep it" onPress={() => setCancelingId(null)} />
            <Button id="btn.cancel-request.confirm" label="Cancel request" emphasis="danger" onPress={confirmCancel} />
          </Stack>
        }
      >
        {cancelingRequest && (
          <Detail
            id="detail.cancel-request"
            fields={[
              { label: "Type", value: cancelingRequest.type },
              { label: "Dates", value: dateRange(cancelingRequest.startDate, cancelingRequest.endDate) },
            ]}
          />
        )}
      </Dialog>
    </Shell>
  );
}

function NewRequestScreen() {
  const state = useDisplayState();
  const nav_ = useNav();
  const requestsCol = useCollection<LeaveRequest>("leaveRequests");

  const handleSubmit = (values: Record<string, string>) => {
    requestsCol.create({
      employeeId: CURRENT_EMPLOYEE_ID,
      employeeName: CURRENT_EMPLOYEE_NAME,
      type: values.type as LeaveRequest["type"],
      startDate: values.startDate,
      endDate: values.endDate,
      days: daysBetween(values.startDate, values.endDate),
      reason: values.reason,
      status: "pending",
    });
    nav_.go("screen.my-leave");
  };

  return (
    <Shell>
      <Heading id="heading.new-request" text="New Leave Request" />
      {state === "state.validation-error" && (
        <ValidationSummary
          id="validation.new-request"
          issues={["Choose a leave type", "Give a reason for the request"]}
        />
      )}
      <Form
        id="form.new-request"
        onSubmit={handleSubmit}
        actions={
          <Stack direction="row">
            <Button id="btn.new-request.cancel" label="Cancel" to="screen.my-leave" />
            <Button id="btn.new-request.submit" label="Submit" emphasis="primary" submit />
          </Stack>
        }
      >
        <Field
          id="field.type"
          name="type"
          label="Leave type"
          type="select"
          options={["Vacation", "Sick", "Personal"]}
          required
          error={state === "state.validation-error" ? "Choose a leave type" : undefined}
        />
        <Field id="field.start-date" name="startDate" label="Start date" type="date" required />
        <Field id="field.end-date" name="endDate" label="End date" type="date" required />
        <Field
          id="field.reason"
          name="reason"
          label="Reason"
          type="textarea"
          required
          error={state === "state.validation-error" ? "Give a reason for the request" : undefined}
        />
      </Form>
    </Shell>
  );
}

function TeamQueue() {
  const state = useDisplayState();
  const requestsCol = useCollection<LeaveRequest>("leaveRequests");
  const pending = state === "state.empty"
    ? []
    : requestsCol.items.filter((r) => r.employeeId !== CURRENT_EMPLOYEE_ID && r.status === "pending");
  const daysRequested = pending.reduce((sum, r) => sum + r.days, 0);
  const people = new Set(pending.map((r) => r.employeeId)).size;

  return (
    <Shell>
      <Heading id="heading.team-queue" text="Pending Requests" />
      {state === "state.failed" && (
        <Alert id="alert.team-queue-failed" tone="error" title="Could not load the team's requests" text="Try again in a few minutes." />
      )}
      <StatGroup>
        <Stat id="stat.queue.pending" label="Awaiting you" value={String(pending.length)} hint="requests to decide" icon="Inbox" tone="warning" />
        <Stat id="stat.queue.days" label="Days requested" value={String(daysRequested)} hint="across pending requests" icon="CalendarDays" />
        <Stat id="stat.queue.people" label="People asking" value={String(people)} hint="on your team" icon="Users" />
      </StatGroup>
      <Section id="section.team-queue" title="To decide" count={pending.length} subtitle="Approve here, or open a request to reject it with a comment.">
        <Table
          id="table.team-queue"
          columns={["Employee", "Dates", "Type", "Reason", { label: "Days", kind: "number" }]}
          rows={pending.map((r) => ({
            id: `row.team-queue.${r.id}`,
            cells: [r.employeeName, dateRange(r.startDate, r.endDate), r.type, r.reason, String(r.days)],
            to: "screen.request-detail",
            params: { requestId: r.id },
            actions: [
              { id: `row.team-queue.${r.id}.approve`, label: "Approve", onPress: () => requestsCol.update(r.id, { status: "approved" }) },
              { id: `row.team-queue.${r.id}.reject`, label: "Reject", emphasis: "danger" as const, to: "screen.request-detail", params: { requestId: r.id } },
            ],
          }))}
          empty={<EmptyState id="empty.team-queue" title="Nothing to decide" text="New requests from your team show here." />}
        />
      </Section>
    </Shell>
  );
}

function RequestDetail() {
  const { requestId } = useParams();
  const state = useDisplayState();
  const nav_ = useNav();
  const requestsCol = useCollection<LeaveRequest>("leaveRequests");
  const [comment, setComment] = useState("");

  const fallback = requestsCol.items.find((r) => r.status === "pending" && r.employeeId !== CURRENT_EMPLOYEE_ID);
  const request = (requestId ? requestsCol.get(requestId) : undefined) ?? fallback ?? requestsCol.items[0]!;

  const approve = () => {
    requestsCol.update(request.id, { status: "approved" });
    nav_.go("screen.team-queue");
  };

  const reject = () => {
    if (!comment.trim()) return;
    requestsCol.update(request.id, { status: "rejected", comment });
    nav_.go("screen.team-queue");
  };

  return (
    <Shell>
      <Heading id="heading.request-detail" text={`Request from ${request.employeeName}`} />
      <Detail
        id="detail.request"
        fields={[
          { label: "Type", value: request.type },
          { label: "Dates", value: `${request.startDate} - ${request.endDate}` },
          { label: "Reason", value: request.reason },
        ]}
      />
      <Field
        id="field.comment"
        label="Comment (required to reject)"
        type="textarea"
        value={comment}
        onChange={setComment}
        error={state === "state.validation-error" ? "A comment is required to reject a request" : undefined}
      />
      <Stack direction="row">
        <Button id="btn.reject" label="Reject" emphasis="danger" onPress={reject} />
        <Button id="btn.approve" label="Approve" emphasis="primary" onPress={approve} />
      </Stack>
    </Shell>
  );
}

function TeamHistory() {
  const state = useDisplayState();
  const requestsCol = useCollection<LeaveRequest>("leaveRequests");
  const decided = state === "state.empty"
    ? []
    : requestsCol.items.filter((r) => r.employeeId !== CURRENT_EMPLOYEE_ID && r.status !== "pending");

  return (
    <Shell>
      <Heading id="heading.team-history" text="Team History" />
      <Table
        id="table.team-history"
        columns={["Employee", "Dates", "Type", "Comment", "Status"]}
        rows={decided.map((r) => ({
          id: `row.team-history.${r.id}`,
          cells: [r.employeeName, `${r.startDate} - ${r.endDate}`, r.type, r.comment ?? "", r.status],
          tone: toneForStatus(r.status),
        }))}
        empty={<EmptyState id="empty.team-history" title="No decided requests yet" text="Approved and rejected requests appear here." />}
      />
    </Shell>
  );
}

function TeamBalances() {
  const balancesCol = useCollection<LeaveBalance>("balances");
  const teamBalances = balancesCol.items.filter((b) => b.employeeId !== CURRENT_EMPLOYEE_ID);
  const employeeIds = Array.from(new Set(teamBalances.map((b) => b.employeeId)));

  const rows = employeeIds.map((employeeId) => {
    const rowsFor = teamBalances.filter((b) => b.employeeId === employeeId);
    const name = rowsFor[0]?.employeeName ?? employeeId;
    const of = (type: LeaveBalance["type"]) => rowsFor.find((b) => b.type === type)?.balance ?? 0;
    return { employeeId, name, vacation: of("Vacation"), sick: of("Sick"), personal: of("Personal") };
  });

  return (
    <Shell>
      <Heading id="heading.team-balances" text="Team Balances" />
      <Table
        id="table.team-balances"
        columns={["Employee", "Vacation", "Sick", "Personal"]}
        rows={rows.map((r) => ({
          id: `row.team-balances.${r.employeeId}`,
          cells: [r.name, String(r.vacation), String(r.sick), String(r.personal)],
          to: "screen.adjust-balance",
          params: { employeeId: r.employeeId },
        }))}
        empty={<EmptyState id="empty.team-balances" title="No team members yet" text="Team balances appear here." />}
      />
    </Shell>
  );
}

function AdjustBalance() {
  const { employeeId } = useParams();
  const state = useDisplayState();
  const nav_ = useNav();
  const balancesCol = useCollection<LeaveBalance>("balances");

  const targetId = employeeId ?? "emp-alex";
  const employeeBalances = balancesCol.items.filter((b) => b.employeeId === targetId);
  const employeeName = employeeBalances[0]?.employeeName ?? "Team Member";

  const handleSubmit = (values: Record<string, string>) => {
    const match = balancesCol.items.find((b) => b.employeeId === targetId && b.type === values.type);
    if (match) {
      balancesCol.update(match.id, { balance: Number(values.balance) });
    }
    nav_.go("screen.team-balances");
  };

  return (
    <Shell>
      <Heading id="heading.adjust-balance" text={`Adjust Balance — ${employeeName}`} />
      <Form
        id="form.adjust-balance"
        onSubmit={handleSubmit}
        actions={
          <Stack direction="row">
            <Button id="btn.adjust-balance.cancel" label="Cancel" to="screen.team-balances" />
            <Button id="btn.adjust-balance.save" label="Save" emphasis="primary" submit />
          </Stack>
        }
      >
        <Field
          id="field.adjust-type"
          name="type"
          label="Leave type"
          type="select"
          options={["Vacation", "Sick", "Personal"]}
          required
          error={state === "state.validation-error" ? "Choose a leave type" : undefined}
        />
        <Field id="field.adjust-balance" name="balance" label="New balance (days)" type="number" required />
      </Form>
    </Shell>
  );
}

function Account() {
  const role = useRole();
  const isManager = role === "Manager";
  const name = isManager ? CURRENT_MANAGER_NAME : CURRENT_EMPLOYEE_NAME;
  const email = isManager ? CURRENT_MANAGER_EMAIL : CURRENT_EMPLOYEE_EMAIL;
  return (
    <Shell>
      <Heading id="heading.account" text="Account" />
      <Detail
        id="detail.account"
        fields={[
          { label: "Name", value: name },
          { label: "Email", value: email },
          { label: "Role", value: role },
        ]}
      />
    </Shell>
  );
}

function Settings() {
  const nav_ = useNav();
  return (
    <Shell>
      <Heading id="heading.settings" text="Settings" />
      <Form
        id="form.settings"
        onSubmit={() => nav_.go("screen.my-leave")}
        actions={<Button id="btn.save-settings" label="Save settings" emphasis="primary" submit />}
      >
        <Field id="field.digest" label="Email me when a request is decided" type="switch" defaultValue="on" />
      </Form>
    </Shell>
  );
}

function SignedOut() {
  const role = useRole();
  const entry = role === "Manager" ? "screen.team-queue" : "screen.my-leave";
  return (
    <Screen>
      <Heading id="heading.signed-out" text="You are signed out" />
      <Button id="btn.sign-in" label="Sign in" emphasis="primary" to={entry} />
    </Screen>
  );
}

export default defineApp({
  screens: {
    "screen.my-leave": MyLeave,
    "screen.new-request": NewRequestScreen,
    "screen.team-queue": TeamQueue,
    "screen.request-detail": RequestDetail,
    "screen.team-history": TeamHistory,
    "screen.team-balances": TeamBalances,
    "screen.adjust-balance": AdjustBalance,
    "screen.account": Account,
    "screen.settings": Settings,
    "screen.signed-out": SignedOut,
  },
  data: { leaveRequests, balances },
});
