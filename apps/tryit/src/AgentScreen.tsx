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

import { useState } from "react";
import { Alert, Avatar, Box, Button, Chip, CircularProgress, Divider, IconButton, Paper, Stack, TextField, Typography } from "@wso2/oxygen-ui";
import { Bot, LogOut, RefreshCw, Send, User, Wrench } from "@wso2/oxygen-ui-icons-react";
import { sendTurn, type ToolCall, type TurnResult } from "./chat";
import type { Launch } from "./launch";

type Entry =
  | { kind: "user"; text: string }
  | { kind: "assistant"; text: string; toolCalls: ToolCall[] }
  | { kind: "notice"; result: Exclude<TurnResult, { kind: "reply" }> };

/**
 * One conversation with the agent, as the signed-in test user. The transcript
 * is this tab's rendering only; the agent holds the history and the
 * conversation id is the one piece of state carried between turns.
 */
export function AgentScreen({
  launch,
  token,
  username,
  onSignOut,
}: {
  launch: Launch;
  token: string;
  username: string | null;
  onSignOut: () => Promise<void>;
}) {
  const [entries, setEntries] = useState<Entry[]>([]);
  const [conversationId, setConversationId] = useState<string | undefined>(undefined);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);

  const send = async () => {
    const message = draft.trim();
    if (!message || sending) return;
    setDraft("");
    setSending(true);
    setEntries((prior) => [...prior, { kind: "user", text: message }]);
    const result = await sendTurn(launch.endpoint, token, conversationId ? { message, conversationId } : { message });
    if (result.kind === "reply") {
      setConversationId(result.conversationId || undefined);
      setEntries((prior) => [...prior, { kind: "assistant", text: result.text, toolCalls: result.toolCalls }]);
    } else {
      setEntries((prior) => [...prior, { kind: "notice", result }]);
    }
    setSending(false);
  };

  const reset = () => {
    setEntries([]);
    setConversationId(undefined);
  };

  return (
    <Box sx={{ minHeight: "100vh", display: "flex", flexDirection: "column", bgcolor: "background.default" }}>
      <Stack
        direction="row"
        spacing={1.5}
        sx={{ alignItems: "center", px: 2, py: 1.5, borderBottom: 1, borderColor: "divider", bgcolor: "background.paper", flexWrap: "wrap", rowGap: 1 }}
      >
        <Avatar sx={{ width: 32, height: 32, bgcolor: "action.hover", color: "text.primary" }}>
          <Bot size={18} aria-hidden />
        </Avatar>
        <Stack spacing={0} sx={{ minWidth: 0 }}>
          <Typography variant="subtitle1" component="h1" sx={{ fontWeight: 600, lineHeight: 1.2 }}>
            {launch.component}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {launch.project}
            {username ? ` · signed in as ${username}` : " · signed in as a test user"}
          </Typography>
        </Stack>
        <Box sx={{ flexGrow: 1 }} />
        <Button size="small" variant="outlined" startIcon={<RefreshCw size={14} aria-hidden />} onClick={reset} disabled={sending}>
          New conversation
        </Button>
        <Button size="small" variant="text" startIcon={<LogOut size={14} aria-hidden />} onClick={() => void onSignOut()}>
          Sign out
        </Button>
      </Stack>

      <Box component="main" sx={{ flexGrow: 1, overflowY: "auto", px: 2, py: 3 }}>
        <Stack spacing={2} sx={{ maxWidth: 760, mx: "auto" }} role="log" aria-label="Conversation">
          {entries.length === 0 && (
            <Typography variant="body2" color="text.secondary" sx={{ textAlign: "center", py: 6 }}>
              Each turn runs the real agent and its tools, as {username ?? "the test user"}.
            </Typography>
          )}
          {entries.map((entry, index) => (
            <EntryView key={index} entry={entry} endpoint={launch.endpoint} />
          ))}
          {sending && (
            <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }} aria-live="polite">
              <CircularProgress size={16} />
              <Typography variant="body2" color="text.secondary">
                Thinking…
              </Typography>
            </Stack>
          )}
        </Stack>
      </Box>

      <Divider />
      <Box
        component="form"
        onSubmit={(event) => {
          event.preventDefault();
          void send();
        }}
        sx={{ px: 2, py: 2, bgcolor: "background.paper" }}
      >
        <Paper
          variant="outlined"
          sx={{
            maxWidth: 760,
            mx: "auto",
            display: "flex",
            alignItems: "flex-end",
            gap: 1,
            pl: 2,
            pr: 1,
            py: 0.75,
            borderRadius: 3,
            borderColor: "divider",
            "&:focus-within": { borderColor: "primary.main" },
          }}
        >
          <TextField
            id="tryit-message"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                void send();
              }
            }}
            placeholder={`Message ${launch.component}`}
            multiline
            maxRows={8}
            fullWidth
            variant="standard"
            disabled={sending}
            slotProps={{
              htmlInput: { "aria-label": "Message" },
              input: { disableUnderline: true, sx: { py: 0.75, fontSize: 15 } },
            }}
          />
          <IconButton
            type="submit"
            aria-label="Send"
            disabled={sending || draft.trim() === ""}
            sx={{
              bgcolor: "primary.main",
              color: "primary.contrastText",
              "&:hover": { bgcolor: "primary.dark" },
              "&.Mui-disabled": { bgcolor: "action.disabledBackground", color: "action.disabled" },
              width: 36,
              height: 36,
            }}
          >
            <Send size={16} aria-hidden />
          </IconButton>
        </Paper>
      </Box>
    </Box>
  );
}

function EntryView({ entry, endpoint }: { entry: Entry; endpoint: string }) {
  if (entry.kind === "notice") return <Notice result={entry.result} endpoint={endpoint} />;
  const mine = entry.kind === "user";
  return (
    <Stack direction="row" spacing={1.5} sx={{ justifyContent: mine ? "flex-end" : "flex-start" }}>
      {!mine && (
        <Avatar sx={{ width: 28, height: 28, bgcolor: "action.hover", color: "text.primary" }}>
          <Bot size={15} aria-hidden />
        </Avatar>
      )}
      <Stack spacing={0.75} sx={{ maxWidth: "80%" }}>
        <Paper
          variant={mine ? "elevation" : "outlined"}
          elevation={0}
          sx={{ px: 1.75, py: 1.25, whiteSpace: "pre-wrap", ...(mine && { bgcolor: "primary.main", color: "primary.contrastText" }) }}
        >
          <Typography variant="body2">{entry.text}</Typography>
        </Paper>
        {entry.kind === "assistant" && entry.toolCalls.length > 0 && (
          <Stack direction="row" spacing={0.75} sx={{ flexWrap: "wrap", rowGap: 0.75 }} aria-label="Tools the agent called">
            {entry.toolCalls.map((call, index) => (
              <Chip key={index} size="small" variant="outlined" icon={<Wrench size={12} aria-hidden />} label={`called ${call.toolName}`} />
            ))}
          </Stack>
        )}
      </Stack>
      {mine && (
        <Avatar sx={{ width: 28, height: 28, bgcolor: "action.hover", color: "text.primary" }}>
          <User size={15} aria-hidden />
        </Avatar>
      )}
    </Stack>
  );
}

function Notice({ result, endpoint }: { result: Exclude<TurnResult, { kind: "reply" }>; endpoint: string }) {
  switch (result.kind) {
    case "refused":
      return (
        <Alert severity="warning">
          The agent refused the test user's token. Sign out and in again; if it still refuses, rotate the
          test user's password in the console (Deployments → Try it out) and sign in with the new one.
        </Alert>
      );
    case "forbidden":
      return (
        <Alert severity="warning">
          This test user is not allowed to use the agent: its roles do not grant a scope the agent
          requires. Check the role's grants in specs/design/security.json and rebuild, or sign in as a
          different test user.
        </Alert>
      );
    case "upstream":
      return (
        <Alert severity="error">
          The agent answered {result.status}.
          {result.body && (
            <Typography component="pre" variant="caption" sx={{ m: 0, mt: 1, whiteSpace: "pre-wrap", fontFamily: "monospace" }}>
              {result.body.slice(0, 2000)}
            </Typography>
          )}
        </Alert>
      );
    case "unreachable":
      return (
        <Alert severity="error">
          Can't reach the agent at {endpoint}. {result.message}
        </Alert>
      );
  }
}
