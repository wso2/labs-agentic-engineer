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

import type { ReactNode } from "react";
import { Stack, Typography } from "@wso2/oxygen-ui";

import type { AgentGuardrailStatus, AgentModelConnection } from "./AgentView.js";
import type { AgentAttachments, AgentGuardrail, AgentSpec } from "./parse.js";
import { mono, Panel, Row } from "./parts.js";

/** Plain-language gloss for the interface types AFM defines. */
function interfaceNote(type: string): string | undefined {
  switch (type) {
    case "webchat":
      return "An HTTP endpoint a web app calls, one request per turn.";
    case "webhook":
      return "Invoked by an external system's callback rather than a user.";
    case "platformchat":
      return "Reached through a chat platform's own integration.";
    default:
      return undefined;
  }
}

/** Memory, said as who remembers the conversation rather than as `client` / `server`. */
function memoryRow(memory: string): { value: string; note?: string } {
  if (memory === "server") {
    return { value: "The agent remembers the conversation", note: "Saved in its own database." };
  }
  if (memory === "client") {
    return { value: "The web app remembers the conversation", note: "Sent back with every message." };
  }
  return { value: memory };
}

/** "application/pdf" → "PDF", "image/jpeg" → "JPEG": the subtype is the name people know. */
function typeLabel(mediaType: string): string {
  return (mediaType.split("/")[1] ?? mediaType).toUpperCase();
}

/** `x-aep.attachments`, or that the agent takes text alone (the default). */
function AttachmentsPanel({ attachments }: { attachments: AgentAttachments | undefined }) {
  return (
    <Panel title="Attachments">
      {attachments ? (
        <>
          <Row label="Types" value={attachments.types.map(typeLabel).join(", ")} />
          <Row
            label="Limits"
            value={`Up to ${attachments.maxFiles} ${attachments.maxFiles === 1 ? "file" : "files"}, ${attachments.maxFileSizeMB} MB each`}
          />
        </>
      ) : (
        <Row label="Files" value="Text only, no files" />
      )}
    </Panel>
  );
}

/**
 * The model the agent runs on is the organisation's connection from Settings,
 * not the AFM's `model:` block: that block holds only `${env:}` placeholders
 * the deploy fills in from the same connection, so showing it would say
 * nothing. The key never appears; it reaches the agent through the gateway.
 */
function ModelPanel({
  connection,
  settingsLink,
}: {
  connection: AgentModelConnection | null | "loading";
  settingsLink: ReactNode;
}) {
  return (
    <Panel title="Model">
      {connection === "loading" ? (
        <Typography variant="body2" color="text.secondary">
          Loading the organisation&apos;s model connection…
        </Typography>
      ) : connection === null ? (
        <Typography variant="body2" color="text.secondary">
          No model connected. {settingsLink}
        </Typography>
      ) : (
        <>
          <Row label="Model" value={<span style={mono}>{connection.model}</span>} />
          <Row label="Format" value={connection.format} />
          <Row label="Host" value={<span style={mono}>{connection.host}</span>} />
          <Row label="Access" value="Through the environment's AI gateway (Agent Manager)" />
          {settingsLink ? (
            <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 1 }}>
              The organisation&apos;s model connection. {settingsLink}
            </Typography>
          ) : null}
        </>
      )}
    </Panel>
  );
}

/**
 * The AI-gateway guardrails the spec declares, each with why the agent has it
 * and — when the caller knows — what the last deploy did with it in each
 * environment. Anything but "applied" carries its reason, because a guardrail
 * that did not land is a protection the agent does not have. Rendered only
 * when there is at least one: rules the agent follows itself are its
 * instructions, not this panel's.
 */
function GuardrailsPanel({
  guardrails,
  status,
}: {
  guardrails: AgentGuardrail[];
  status: Record<string, AgentGuardrailStatus[]> | undefined;
}) {
  return (
    <Panel title="Guardrails">
      {/* Keyed by position too: a live draft reaches this view before the write
          gate that refuses a repeated policy. */}
      {guardrails.map((g, i) => {
          const outcomes = status?.[g.policy];
          return (
            <Row
              key={`${g.policy}:${i}`}
              label="Policy"
              value={
                <Stack spacing={0.25}>
                  <span style={mono}>{g.policy}</span>
                  {g.why ? (
                    <Typography variant="caption" color="text.secondary">
                      {g.why}
                    </Typography>
                  ) : null}
                  {status === undefined ? null : outcomes && outcomes.length > 0 ? (
                    outcomes.map((o) => (
                      <Stack key={o.environment} spacing={0}>
                        <Typography variant="caption" color={o.status === "applied" ? "success.main" : "warning.main"}>
                          {`${o.environment}: ${o.status}`}
                        </Typography>
                        {o.reason ? (
                          <Typography variant="caption" color="text.secondary">
                            {o.reason}
                          </Typography>
                        ) : null}
                      </Stack>
                    ))
                  ) : (
                    <Typography variant="caption" color="text.secondary">
                      Not deployed yet
                    </Typography>
                  )}
                </Stack>
              }
            />
          );
        })}
    </Panel>
  );
}

export function ConfigurationTab({
  spec,
  modelConnection,
  settingsLink,
  guardrailStatus,
}: {
  spec: AgentSpec;
  modelConnection: AgentModelConnection | null | "loading" | undefined;
  settingsLink: ReactNode;
  guardrailStatus: Record<string, AgentGuardrailStatus[]> | undefined;
}) {
  const memory = spec.memory ? memoryRow(spec.memory) : null;
  return (
    <Stack spacing={2}>
      {modelConnection !== undefined ? (
        <ModelPanel connection={modelConnection} settingsLink={settingsLink} />
      ) : null}
      {spec.interfaces.length > 0 ? (
        <Panel title="Exposure">
          {spec.interfaces.map((iface) => (
            <Row
              key={`${iface.type}:${iface.path ?? ""}`}
              label={iface.type}
              value={<span style={mono}>{iface.path ? `POST ${iface.path}` : "—"}</span>}
              note={interfaceNote(iface.type)}
            />
          ))}
        </Panel>
      ) : null}
      <AttachmentsPanel attachments={spec.attachments} />
      {spec.guardrails.length > 0 ? <GuardrailsPanel guardrails={spec.guardrails} status={guardrailStatus} /> : null}
      {memory ? (
        <Panel title="Memory">
          <Row label="Conversation" value={memory.value} note={memory.note} />
        </Panel>
      ) : null}
    </Stack>
  );
}
