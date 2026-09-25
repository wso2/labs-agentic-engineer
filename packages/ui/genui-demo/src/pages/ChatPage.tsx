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

import { useMemo, useState } from "react";
import {
  Box,
  Button,
  CodeBlock,
  Paper,
  Stack,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import {
  GenUiActionError,
  type GenUiActionHandlers,
  type GenUiDispatchOutcome,
} from "@aep/ui-genui";
import { GenUiView } from "@aep/ui-genui-oxygen";
import {
  confirmationMessage,
  openingMessage,
  userMessage,
  type ChatMessage,
} from "../chat/agent.js";
import { checkAnswers, describeAnswers, fieldRules } from "../chat/formRules.js";
import { createHandlers } from "../handlers.js";

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

function AgentCard({
  message,
  handlers,
  onOutcome,
}: {
  message: ChatMessage;
  handlers: GenUiActionHandlers;
  onOutcome: (outcome: GenUiDispatchOutcome) => void;
}) {
  const [showJson, setShowJson] = useState(false);
  if (!message.spec) return null;
  return (
    <Stack spacing={1}>
      <Paper variant="outlined">
        <Box sx={{ p: 2 }}>
          <GenUiView spec={message.spec} handlers={handlers} onActionOutcome={onOutcome} />
        </Box>
      </Paper>
      <Box>
        <Button size="small" variant="text" onClick={() => setShowJson((v) => !v)}>
          {showJson ? "Hide JSON" : "Show the JSON the agent sent"}
        </Button>
      </Box>
      {showJson ? <CodeBlock code={JSON.stringify(message.spec, null, 2)} language="json" /> : null}
    </Stack>
  );
}

/**
 * A chat with a simulated agent. The agent asks with a form card, the user's
 * answers go back as replyToAgent, the agent checks them against the form it
 * sent, and answers with a confirmation card.
 */
export function ChatPage() {
  const [messages, setMessages] = useState<ChatMessage[]>(() => [openingMessage()]);
  const [typing, setTyping] = useState(false);
  const [log, setLog] = useState<GenUiDispatchOutcome[]>([]);
  const base = useMemo(() => createHandlers(), []);
  const onOutcome = (outcome: GenUiDispatchOutcome) => setLog((prev) => [outcome, ...prev]);

  // Each card gets handlers bound to its own message, so a reply is checked
  // against the form that card showed.
  const handlersFor = (message: ChatMessage): GenUiActionHandlers => ({
    ...base,
    replyToAgent: async ({ answers }) => {
      if (!message.spec) return;
      const rules = fieldRules(message.spec);
      const errors = checkAnswers(rules, answers);
      if (Object.keys(errors).length > 0) {
        throw new GenUiActionError("Some answers are missing or invalid.", errors);
      }
      const facts = describeAnswers(rules, answers);
      setMessages((prev) => [
        ...prev,
        userMessage(facts.map((f) => `${f.label}: ${f.value}`).join(" · ")),
      ]);
      setTyping(true);
      await sleep(900);
      setMessages((prev) => [...prev, confirmationMessage(facts)]);
      setTyping(false);
    },
  });

  const startOver = () => {
    setMessages([openingMessage()]);
    setTyping(false);
    setLog([]);
  };

  return (
    <Box
      sx={{
        display: "grid",
        gap: 3,
        gridTemplateColumns: { md: "minmax(0, 760px) 1fr" },
        alignItems: "start",
      }}
    >
      <Stack spacing={2}>
        <Typography variant="body2" color="text.secondary">
          A simulated agent. Every card is a GenUI spec the agent sent; open its JSON to see
          what it wrote. Your answers go back as the replyToAgent action, and the agent checks
          them against the form it asked with.
        </Typography>
        {messages.map((message) =>
          message.from === "agent" ? (
            <Stack key={message.id} spacing={1}>
              <Typography variant="overline" color="text.secondary">
                Agent
              </Typography>
              <Typography variant="body1">{message.text}</Typography>
              <AgentCard
                message={message}
                handlers={handlersFor(message)}
                onOutcome={onOutcome}
              />
            </Stack>
          ) : (
            <Stack key={message.id} spacing={1} sx={{ alignItems: "flex-end" }}>
              <Typography variant="overline" color="text.secondary">
                You
              </Typography>
              <Paper variant="outlined">
                <Box sx={{ px: 2, py: 1 }}>
                  <Typography variant="body2">{message.text}</Typography>
                </Box>
              </Paper>
            </Stack>
          ),
        )}
        {typing ? (
          <Typography variant="body2" color="text.secondary">
            Agent is typing…
          </Typography>
        ) : null}
        <Stack direction="row" spacing={1}>
          <TextField
            fullWidth
            size="small"
            disabled
            placeholder="This agent is scripted: answer through its cards."
          />
          <Button variant="outlined" onClick={startOver}>
            Start over
          </Button>
        </Stack>
      </Stack>
      <Stack spacing={1}>
        <Typography variant="subtitle2">Action log</Typography>
        {log.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            Nothing sent yet.
          </Typography>
        ) : (
          log.map((outcome, index) => (
            <Typography key={index} variant="body2" sx={{ fontFamily: "monospace" }}>
              {outcome.status} · {outcome.action}
            </Typography>
          ))
        )}
      </Stack>
    </Box>
  );
}
