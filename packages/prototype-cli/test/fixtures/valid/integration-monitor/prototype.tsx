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

// Fixture (ported from the template-react snapshot): an integration monitor
// with a top navigation, display states that change what every screen says,
// and an admin-only area whose edits change the shared collection.

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
  Table,
  Tabs,
  Text,
  Timeline,
  defineApp,
  useCollection,
  useDisplayState,
  useNav,
  useParams,
  useRole,
} from "@wso2/prototype-kit";

interface Connection {
  id: string;
  name: string;
  schedule: string;
  lastRun: string;
  status: "Failing" | "Healthy";
  alertEmail: string;
  alertAfter: string;
}

const connections: Connection[] = [
  { id: "sf-erp", name: "Salesforce → ERP", schedule: "Every 10 minutes", lastRun: "09:40", status: "Failing", alertEmail: "integrations@example.com", alertAfter: "3" },
  { id: "erp-wh", name: "ERP → Warehouse", schedule: "Every 10 minutes", lastRun: "09:30", status: "Healthy", alertEmail: "integrations@example.com", alertAfter: "3" },
  { id: "hr-dir", name: "HR → Directory", schedule: "Hourly", lastRun: "09:00", status: "Healthy", alertEmail: "people-ops@example.com", alertAfter: "5" },
];

const nav = (
  <Navigation
    id="nav.top"
    layout="top"
    items={[
      { id: "nav.overview", label: "Overview", to: "screen.overview" },
      { id: "nav.connections", label: "Connections", to: "screen.connections" },
    ]}
  />
);

function Overview() {
  const state = useDisplayState();
  const role = useRole();
  const all = useCollection<Connection>("connections");
  const [connection, setConnection] = useState("All connections");
  return (
    <Screen nav={nav}>
      <Heading id="heading.overview" text="Integration health" />
      {state === "state.healthy" && <Alert id="alert.healthy" tone="success" text="All 6 connections synced within their schedule." />}
      {state === "state.delayed" && (
        <Alert id="alert.delayed" tone="warning" title="Salesforce → ERP is behind" text="The last run started 42 minutes late; 318 messages are queued." />
      )}
      {state === "state.failed" && (
        <Alert id="alert.failed" tone="error" title="Order sync failed" text="ERP returned 503 for 3 consecutive runs. No orders have synced since 06:00." />
      )}
      <Grid columns={4}>
        <Stat id="stat.connections" label="Connections" value={String(all.items.length)} />
        <Stat id="stat.runs" label="Runs today" value={state === "state.empty" ? "0" : "142"} />
        <Stat id="stat.failures" label="Failures today" value={state === "state.failed" ? "3" : "0"} />
        <Stat id="stat.backlog" label="Queued messages" value={state === "state.delayed" ? "318" : "0"} />
      </Grid>
      {state === "state.empty" ? (
        <EmptyState
          id="empty.runs"
          title="No runs yet"
          text="Runs appear here once a connection's first schedule fires."
          actions={role === "integration-admin" ? <Button id="btn.empty.connections" label="Review connections" to="screen.connections" /> : undefined}
        />
      ) : (
        <>
          <Filters id="filters.runs">
            <Field
              id="filter.connection"
              label="Connection"
              type="select"
              value={connection}
              options={["All connections", ...all.items.map((c) => c.name)]}
              onChange={setConnection}
            />
            <Field id="filter.outcome" label="Outcome" type="select" value="Any" options={["Any", "Succeeded", "Failed", "Delayed"]} />
          </Filters>
          <Table
            id="table.runs"
            title="Recent runs"
            columns={["Run", "Connection", "Started", "Duration", "Outcome"]}
            rows={[
              { id: "run.8812", cells: ["#8812", "Salesforce → ERP", "09:40", "—", "Failed"], tone: "error" as const, to: "screen.run" },
              { id: "run.8811", cells: ["#8811", "ERP → Warehouse", "09:30", "48s", "Succeeded"], tone: "success" as const },
              { id: "run.8810", cells: ["#8810", "Salesforce → ERP", "09:20", "6m 12s", "Delayed"], tone: "warning" as const, to: "screen.run" },
            ].filter((r) => connection === "All connections" || r.cells[1] === connection)}
          />
          {state === "state.failed" && <Button id="btn.latest-failure" label="Show the latest failure" to="screen.run" />}
        </>
      )}
    </Screen>
  );
}

function Run() {
  const state = useDisplayState();
  const [replaying, setReplaying] = useState(false);
  const [payload, setPayload] = useState(false);
  const [tab, setTab] = useState("tab.run.summary");
  return (
    <Screen nav={nav}>
      <Breadcrumbs
        id="crumbs.run"
        items={[
          { id: "crumb.overview", label: "Overview", to: "screen.overview" },
          { id: "crumb.run", label: "Run #8812" },
        ]}
      />
      <Stack direction="row">
        <Heading
          id="heading.run"
          text="Run #8812 · Salesforce → ERP"
          actions={
            <>
              <Button id="btn.replay" label="Replay run" emphasis="primary" onPress={() => setReplaying(true)} />
              <Button id="btn.payload" label="View payload" onPress={() => setPayload(true)} />
            </>
          }
        />
        {state === "state.failed" && <Badge id="badge.run.failed" label="Failed" tone="error" />}
        {state === "state.delayed" && <Badge id="badge.run.delayed" label="Delayed" tone="warning" />}
        {state === "state.healthy" && <Badge id="badge.run.healthy" label="Succeeded" tone="success" />}
      </Stack>
      <Tabs
        id="tabs.run"
        active={tab}
        onChange={setTab}
        tabs={[
          {
            id: "tab.run.summary",
            label: "Summary",
            content: (
              <Split
                ratio={8}
                left={
                  <Detail
                    id="detail.run"
                    title="Run"
                    fields={[
                      { label: "Connection", value: "Salesforce → ERP" },
                      { label: "Trigger", value: "Schedule, every 10 minutes" },
                      { label: "Messages", value: "112" },
                    ]}
                  />
                }
                right={
                  <>
                    {state === "state.failed" && <Text id="text.run.error" text="ERP responded 503 Service Unavailable on POST /orders." />}
                    <Link id="link.run.log" label="Open the run log" onPress={() => setTab("tab.run.log")} />
                  </>
                }
              />
            ),
          },
          {
            id: "tab.run.log",
            label: "Log",
            content: (
              <Timeline
                id="timeline.run"
                entries={[
                  { when: "09:40:00", who: "Scheduler", text: "Run started" },
                  { when: "09:40:03", who: "Salesforce", text: "Read 112 changed orders" },
                  { when: "09:40:09", who: "ERP", text: "503 Service Unavailable" },
                ]}
              />
            ),
          },
        ]}
      />
      <Dialog
        id="dialog.replay"
        title="Replay run #8812?"
        open={replaying}
        onClose={() => setReplaying(false)}
        actions={
          <>
            <Button id="btn.replay.cancel" label="Cancel" onPress={() => setReplaying(false)} />
            <Button id="btn.replay.confirm" label="Replay" emphasis="primary" onPress={() => setReplaying(false)} />
          </>
        }
      >
        <Text id="text.replay" text="The 112 orders would be sent to ERP again." />
      </Dialog>
      <Drawer id="drawer.payload" title="First failed message" open={payload} onClose={() => setPayload(false)}>
        <Detail
          id="detail.payload"
          fields={[
            { label: "Order", value: "SO-20931" },
            { label: "Account", value: "Northwind Traders" },
            { label: "Total", value: "$4,210.00" },
          ]}
        />
      </Drawer>
    </Screen>
  );
}

function Connections() {
  const state = useDisplayState();
  const all = useCollection<Connection>("connections");
  return (
    <Screen nav={nav}>
      <Heading id="heading.connections" text="Connections" />
      {(state === "state.delayed" || state === "state.failed") && (
        <Table
          id="queue.attention"
          title="Needs attention"
          columns={["Connection", "Issue", "Since"]}
          rows={[
            { id: "attention.sf-erp", cells: ["Salesforce → ERP", "Target unavailable", "06:00"], tone: "error", to: "screen.connection", params: { connection: "sf-erp" } },
            { id: "attention.erp-wh", cells: ["ERP → Warehouse", "Credential expires in 5 days", "Sep 20"], tone: "warning", to: "screen.connection", params: { connection: "erp-wh" } },
          ]}
        />
      )}
      <Table
        id="table.connections"
        title="All connections"
        columns={["Connection", "Schedule", "Last run", "Status"]}
        rows={all.items.map((c) => ({
          id: `conn.${c.id}`,
          cells: [c.name, c.schedule, c.lastRun, c.status],
          tone: c.status === "Failing" ? ("error" as const) : undefined,
          to: "screen.connection",
          params: { connection: c.id },
        }))}
      />
    </Screen>
  );
}

function ConnectionDetail() {
  const { connection: id } = useParams();
  const nav_ = useNav();
  const all = useCollection<Connection>("connections");
  const connection = (id === undefined ? undefined : all.get(id)) ?? all.items[0]!;
  const save = (values: Record<string, string>) => {
    all.update(connection.id, { alertEmail: values["alertEmail"] ?? connection.alertEmail, alertAfter: values["alertAfter"] ?? connection.alertAfter });
    nav_.go("screen.connections");
  };
  return (
    <Screen nav={nav}>
      <Breadcrumbs
        id="crumbs.connection"
        items={[
          { id: "crumb.connections", label: "Connections", to: "screen.connections" },
          { id: "crumb.connection", label: connection.name },
        ]}
      />
      <Heading id="heading.connection" text={connection.name} />
      <Detail
        id="detail.connection"
        title="Configuration"
        fields={[
          { label: "Source", value: "Salesforce · Orders" },
          { label: "Target", value: "ERP · POST /orders" },
          { label: "Schedule", value: connection.schedule },
        ]}
      />
      <Form id="form.connection" title="Alerting" onSubmit={save} actions={<Button id="btn.alert.save" label="Save and return" emphasis="primary" submit />}>
        <Field id="field.alert-email" name="alertEmail" label="Alert email" defaultValue={connection.alertEmail} required />
        <Field id="field.alert-after" name="alertAfter" label="Alert after consecutive failures" type="number" defaultValue={connection.alertAfter} required pattern="[1-9][0-9]*" />
      </Form>
    </Screen>
  );
}

export default defineApp({
  screens: {
    "screen.overview": Overview,
    "screen.run": Run,
    "screen.connections": Connections,
    "screen.connection": ConnectionDetail,
  },
  data: { connections },
});
