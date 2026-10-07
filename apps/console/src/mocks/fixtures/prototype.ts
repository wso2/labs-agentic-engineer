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

import { parsePrototypeCommand } from "@aep/contracts/commands";
import type * as Y from "yjs";
import { readDocFile } from "@aep/collab-doc";
import { manifestPath, sourcePath } from "../../features/prototype/model/prototypes";
import type { PrototypeFeedback } from "../../features/agent-chat/turnScope";
import type { components } from "../../generated/aep-api";
import type { ScriptFrame } from "../chatServer";
import { Script } from "./interview";

type ConversationMessage = components["schemas"]["ConversationMessage"];

// The mock agent's `/prototype` turns (spec #860). Acme Expenses' design has
// one web application, expense-web; its prototype is the sample below, a
// prototype the kit's checks pass on the Oxygen theme (prototype.test.ts).
//
//  - Make prototype (`/prototype expense-web`, or a bare `/prototype`): writes
//    the manifest, then the source, as the agent does (the gate wants the
//    manifest first); once it exists, the turn says it is up to date.
//  - A review's Send all (the same command with `prototypeFeedback`): answers
//    each request by number, applied or declined. A few requests the sample
//    knows how to apply (a tweak, an editFile on the source); anything else is
//    declined, as the mock cannot write code.
//
// The files land in the local doc from the turn's stream, as every agent
// write does in mock mode; a finished turn's files also seed the doc after a
// reload (chatServer.ts `prototypeFileWrites`).

/** The web application the sample is for. */
export const SAMPLE_COMPONENT = "expense-web";

export const SAMPLE_MANIFEST = `{
  "schemaVersion": 3,
  "name": "Acme Expenses",
  "entryScreen": "screen.my-claims",
  "roles": [
    { "id": "employee", "name": "Employee" },
    { "id": "manager", "name": "Manager" },
    { "id": "finance", "name": "Finance" }
  ],
  "states": [
    { "id": "state.default", "name": "Default" },
    { "id": "state.empty", "name": "Nothing to show" }
  ],
  "screens": [
    { "id": "screen.my-claims", "name": "My claims", "roleIds": ["employee", "manager", "finance"] },
    { "id": "screen.new-claim", "name": "New claim", "roleIds": ["employee", "manager", "finance"] },
    { "id": "screen.pending", "name": "Pending approvals", "roleIds": ["manager", "finance"] },
    { "id": "screen.claim", "name": "Claim", "roleIds": ["manager", "finance"] },
    { "id": "screen.account", "name": "Account", "roleIds": ["employee", "manager", "finance"] },
    { "id": "screen.settings", "name": "Settings", "roleIds": ["employee", "manager", "finance"] },
    { "id": "screen.signed-out", "name": "Signed out", "roleIds": ["employee", "manager", "finance"] }
  ],
  "flows": [
    { "id": "flow.submit", "name": "Submit a claim", "roleId": "employee", "screenIds": ["screen.new-claim", "screen.my-claims"] },
    { "id": "flow.approve", "name": "Approve a claim", "roleId": "manager", "screenIds": ["screen.pending", "screen.claim"] }
  ]
}
`;

export const SAMPLE_SOURCE = `// Acme Expenses: employees submit claims, managers approve them, and Finance
// gives a second approval over $1,000. Claims are one collection, so a claim
// submitted here waits in the manager's queue.

import { useState, type ReactNode } from "react";
import {
  AppShell,
  Badge,
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
  Text,
  defineApp,
  useCollection,
  useDisplayState,
  useNav,
  useParams,
  useRole,
} from "@wso2/prototype-kit";

type Status = "Waiting for manager" | "Waiting for Finance" | "Approved" | "Rejected";

interface Claim {
  id: string;
  employee: string;
  title: string;
  total: string;
  submitted: string;
  status: Status;
}

const claims: Claim[] = [
  { id: "42", employee: "Priya Shah", title: "Client visit, Denver", total: "$1,200.00", submitted: "Mon 22 Sep", status: "Waiting for manager" },
  { id: "43", employee: "Dev Patel", title: "Team lunch", total: "$84.00", submitted: "Tue 23 Sep", status: "Waiting for manager" },
  { id: "39", employee: "Priya Shah", title: "Taxi to the airport", total: "$64.00", submitted: "Thu 11 Sep", status: "Approved" },
];

const tone = (status: Status) => (status === "Approved" ? "success" : status === "Rejected" ? "error" : "warning");

/** Who each role signs in as. */
const users: Record<string, { name: string; email: string }> = {
  employee: { name: "Priya Shah", email: "priya@acme.example" },
  manager: { name: "Sam Ortiz", email: "sam@acme.example" },
  finance: { name: "Lena Park", email: "lena@acme.example" },
};

function useUser() {
  return users[useRole()] ?? users.employee!;
}

function Shell({ children }: { children: ReactNode }) {
  return (
    <AppShell
      id="shell"
      user={useUser()}
      nav={[
        { id: "nav.my-claims", label: "My claims", to: "screen.my-claims" },
        { id: "nav.pending", label: "Approvals", to: "screen.pending" },
      ]}
      account="screen.account"
      settings="screen.settings"
      signOut="screen.signed-out"
    >
      {children}
    </AppShell>
  );
}

function MyClaims() {
  const state = useDisplayState();
  const all = useCollection<Claim>("claims");
  const mine = state === "state.empty" ? [] : all.items.filter((c) => c.employee === "Priya Shah");
  return (
    <Shell>
      <Heading id="heading.my-claims" text="My claims" />
      <Section
        id="section.my-claims"
        title="Claims"
        count={mine.length}
        actions={<Button id="btn.new-claim" label="New claim" emphasis="primary" to="screen.new-claim" />}
      >
        <Table
          id="table.my-claims"
          columns={["Claim", { label: "Total", kind: "number" }, "Submitted", { label: "Status", kind: "status" }]}
          rows={mine.map((c) => ({ id: \`mine.\${c.id}\`, cells: [c.title, c.total, c.submitted], status: { text: c.status, tone: tone(c.status) } }))}
          empty={<EmptyState id="empty.my-claims" title="No claims yet" text="Claims you submit show here with their status." />}
        />
      </Section>
    </Shell>
  );
}

function NewClaim() {
  const nav_ = useNav();
  const all = useCollection<Claim>("claims");
  const [title, setTitle] = useState("Hotel, Denver");
  const [total, setTotal] = useState("780.00");
  const submit = () => {
    all.create({ employee: "Priya Shah", title, total: \`$\${total}\`, submitted: "Today", status: "Waiting for manager" });
    nav_.go("screen.my-claims");
  };
  return (
    <Shell>
      <Heading id="heading.new-claim" text="New claim" />
      <Form id="form.claim" actions={<Button id="btn.submit" label="Submit claim" emphasis="primary" onPress={submit} />}>
        <Field id="field.title" label="What it was for" value={title} onChange={setTitle} required />
        <Field id="field.total" label="Total" type="number" value={total} onChange={setTotal} required />
        <Field id="field.category" label="Category" type="select" defaultValue="Travel" options={["Travel", "Meals", "Equipment"]} />
      </Form>
    </Shell>
  );
}

function Pending() {
  const state = useDisplayState();
  const all = useCollection<Claim>("claims");
  const waiting = state === "state.empty" ? [] : all.items.filter((c) => c.status.startsWith("Waiting"));
  return (
    <Shell>
      <Heading id="heading.pending" text="Pending approvals" />
      <StatGroup>
        <Stat id="stat.waiting" label="Waiting for you" value={String(waiting.length)} hint="claims to decide" icon="Inbox" tone="warning" />
        <Stat id="stat.oldest" label="Oldest" value={waiting[0]?.submitted ?? "None"} hint="submitted, still waiting" icon="Clock" />
      </StatGroup>
      <Section id="section.pending" title="To decide" count={waiting.length}>
        <Table
          id="table.pending"
          columns={["Employee", "Claim", { label: "Total", kind: "number" }, "Submitted"]}
          rows={waiting.map((c) => ({ id: \`pending.\${c.id}\`, cells: [c.employee, c.title, c.total, c.submitted], to: "screen.claim", params: { claim: c.id } }))}
          empty={<EmptyState id="empty.pending" title="Nothing waiting" text="Claims your team submits land here." />}
        />
      </Section>
    </Shell>
  );
}

function ClaimDetail() {
  const { claim: id = "42" } = useParams();
  const role = useRole();
  const nav_ = useNav();
  const all = useCollection<Claim>("claims");
  const [dialog, setDialog] = useState<"approve" | "reject" | null>(null);
  const [reason, setReason] = useState("");
  const claim = all.get(id) ?? all.items[0]!;
  const decide = (status: Status) => {
    all.update(claim.id, { status });
    setDialog(null);
    nav_.go("screen.pending");
  };
  return (
    <Shell>
      <Stack direction="row">
        <Heading id="heading.claim" text={\`Claim \${claim.id} · \${claim.employee}\`} />
        <Badge id="badge.status" label={claim.status} tone={tone(claim.status)} />
      </Stack>
      <Text id="text.total" text={\`Total \${claim.total}\`} />
      <Detail
        id="detail.claim"
        title="Claim"
        fields={[
          { label: "For", value: claim.title },
          { label: "Submitted", value: claim.submitted },
          { label: "Policy", value: "Over $1,000 also needs Finance" },
        ]}
      />
      {role === "manager" ? (
        <Stack direction="row">
          <Button id="btn.approve" label="Approve" emphasis="primary" onPress={() => setDialog("approve")} />
          <Button id="btn.reject" label="Reject" emphasis="danger" onPress={() => setDialog("reject")} />
        </Stack>
      ) : (
        <Text id="text.finance" text="Finance gives the second approval once the manager has approved." />
      )}
      <Dialog
        id="dialog.approve"
        title={\`Approve claim \${claim.id}?\`}
        open={dialog === "approve"}
        onClose={() => setDialog(null)}
        actions={<Button id="btn.approve.confirm" label="Approve" emphasis="primary" onPress={() => decide("Approved")} />}
      >
        <Text id="text.approve" text={\`\${claim.total} goes to payroll once approved.\`} />
      </Dialog>
      <Dialog
        id="dialog.reject"
        title={\`Reject claim \${claim.id}\`}
        open={dialog === "reject"}
        onClose={() => setDialog(null)}
        actions={<Button id="btn.reject.confirm" label="Reject" emphasis="danger" onPress={() => decide("Rejected")} />}
      >
        <Form id="form.reject">
          <Field id="field.reason" label="Reason" type="textarea" value={reason} onChange={setReason} required />
        </Form>
      </Dialog>
    </Shell>
  );
}

function Account() {
  const user = useUser();
  return (
    <Shell>
      <Heading id="heading.account" text="Account" />
      <Detail
        id="detail.account"
        fields={[
          { label: "Name", value: user.name },
          { label: "Email", value: user.email },
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
        onSubmit={() => nav_.go("screen.my-claims")}
        actions={<Button id="btn.save-settings" label="Save settings" emphasis="primary" submit />}
      >
        <Field id="field.currency" label="Currency" type="select" defaultValue="USD" options={["USD", "EUR", "LKR"]} />
        <Field id="field.notify" label="Email me when a claim is decided" type="switch" defaultValue="on" />
      </Form>
    </Shell>
  );
}

function SignedOut() {
  return (
    <Screen>
      <Heading id="heading.signed-out" text="You are signed out" />
      <Text id="text.signed-out" text="Sign in again to see your claims." />
      <Button id="btn.sign-in" label="Sign in" emphasis="primary" to="screen.my-claims" />
    </Screen>
  );
}

export default defineApp({
  screens: {
    "screen.my-claims": MyClaims,
    "screen.new-claim": NewClaim,
    "screen.pending": Pending,
    "screen.claim": ClaimDetail,
    "screen.account": Account,
    "screen.settings": Settings,
    "screen.signed-out": SignedOut,
  },
  data: { claims },
});
`;

/** A change a request can make to the sample, as one edit of its source. */
interface Tweak {
  match: RegExp;
  from: string;
  to: string;
  applied: string;
}

const TWEAKS: Tweak[] = [
  {
    match: /\b(right|left|swap|order)\b/i,
    from: `          <Button id="btn.approve" label="Approve" emphasis="primary" onPress={() => setDialog("approve")} />
          <Button id="btn.reject" label="Reject" emphasis="danger" onPress={() => setDialog("reject")} />`,
    to: `          <Button id="btn.reject" label="Reject" emphasis="danger" onPress={() => setDialog("reject")} />
          <Button id="btn.approve" label="Approve" emphasis="primary" onPress={() => setDialog("approve")} />`,
    applied: "Approve is now on the right, Reject on the left; their ids are unchanged.",
  },
  {
    match: /\b(total|bigger|larger|prominent)\b/i,
    from: '      <Text id="text.total" text={`Total ${claim.total}`} />',
    to: '      <Heading id="text.total" level="section" text={`Total ${claim.total}`} />',
    applied: "The total is now a heading, so it stands out on the claim; its id is unchanged.",
  },
];

export interface PrototypeTurn {
  display: string;
  frames: ScriptFrame[];
  reply: ConversationMessage[];
  /** The prototype's files as the turn leaves them, by room path; absent when it wrote nothing. */
  files?: Record<string, string>;
}

/**
 * A `/prototype` turn, or null when the message is not one (the caller answers
 * it). `webApps` are the design's web applications; `doc` is the room as the
 * agent reads it.
 */
export function scriptPrototypeTurn(req: {
  instruction: string;
  feedback: PrototypeFeedback | undefined;
  webApps: readonly string[];
  doc: Y.Doc;
  turnKey: string;
}): PrototypeTurn | null {
  const command = parsePrototypeCommand(req.instruction);
  if (!command) return null;
  const display = req.instruction.trim();
  const component = req.feedback?.component ?? command.component ?? req.webApps[0];
  const s = new Script().pause(600);
  if (!component || !req.webApps.includes(component)) {
    s.say(
      req.webApps.length === 0
        ? "The design has no web application yet, so there is nothing to prototype. Design the product first."
        : `${component} is not a web application in the design.`,
    );
    return { display, ...s.end() };
  }
  if (component !== SAMPLE_COMPONENT) {
    s.say(`This mock can only prototype ${SAMPLE_COMPONENT}.`);
    return { display, ...s.end() };
  }
  const manifest = readDocFile(req.doc, manifestPath(component));
  const source = readDocFile(req.doc, sourcePath(component));
  if (req.feedback) return revise({ display, s, component, manifest, source, feedback: req.feedback, turnKey: req.turnKey });

  if (manifest !== undefined && source !== undefined) {
    s.say(`The prototype of ${component} is up to date with the design. Review it, and send me what you'd change.`);
    return { display, ...s.end() };
  }
  s.say(`Making the prototype of ${component} from the design: 7 screens in the app shell, the Employee, Manager and Finance roles, and the submit and approve flows.`);
  if (manifest === undefined) s.add(`${req.turnKey}-manifest`, manifestPath(component), SAMPLE_MANIFEST);
  if (source === undefined) s.add(`${req.turnKey}-source`, sourcePath(component), SAMPLE_SOURCE);
  s.pause(400).say("The prototype is ready. Open the Prototype tab and press Review to try it full screen; switch to Annotate to point at anything you'd change.");
  return {
    display,
    ...s.end(),
    files: { [manifestPath(component)]: manifest ?? SAMPLE_MANIFEST, [sourcePath(component)]: source ?? SAMPLE_SOURCE },
  };
}

function revise(req: {
  display: string;
  s: Script;
  component: string;
  manifest: string | undefined;
  source: string | undefined;
  feedback: PrototypeFeedback;
  turnKey: string;
}): PrototypeTurn {
  const { s, component, feedback } = req;
  if (req.manifest === undefined || req.source === undefined) {
    s.say(`There is no prototype of ${component} to revise yet. Make it first.`);
    return { display: req.display, ...s.end() };
  }
  const n = feedback.requests.length;
  s.say(`Revising the prototype of ${component} with your ${n === 1 ? "request" : `${n} requests`}.`);
  let source = req.source;
  const answers = feedback.requests.map((request, i) => {
    const tweak = TWEAKS.find((t) => t.match.test(request.text));
    if (!tweak) return `${i + 1}. Declined: this mock can't make that change. (${request.screenId})`;
    if (!source.includes(tweak.from)) return `${i + 1}. Already so: ${tweak.applied}`;
    s.edit(`${req.turnKey}-r${i + 1}`, sourcePath(component), tweak.from, tweak.to);
    source = source.replace(tweak.from, tweak.to);
    return `${i + 1}. Applied: ${tweak.applied}`;
  });
  s.pause(400).say(answers.join("\n"));
  const changed = source !== req.source;
  return {
    display: req.display,
    ...s.end(),
    ...(changed ? { files: { [manifestPath(component)]: req.manifest, [sourcePath(component)]: source } } : {}),
  };
}
