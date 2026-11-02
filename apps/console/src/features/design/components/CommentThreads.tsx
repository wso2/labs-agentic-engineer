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
import { Box, Button, Link, TextField, Typography } from "@wso2/oxygen-ui";
import type { ReviewComment } from "@aep/agent-stream";
import { stamp } from "../../../lib/stamp";
import { soft } from "../../spec/components/Tag";

// One artifact's comment threads, as GitHub's pull request review keeps them
// (D13): a draft is Pending, with Edit and Delete, until Address comments
// sends it; a sent one is with the agent; an addressed one carries the
// agent's one-line reply, with Reopen, which puts it in the next batch. A
// comment the agent found ambiguous shows its question (answered from the
// Overview's Answer N questions) and, once answered, the answer. Used
// in an artifact's 💬 panel and on the Comments item. No spot comments: a
// prototype comment names the screen it was made on (`place`).

export interface ThreadActions {
  /** The signed-in user's id: only their own drafts can be edited or deleted. */
  me: string;
  onEdit: (id: string, text: string) => void;
  onDelete: (id: string) => void;
  onReopen: (id: string) => void;
}

function Badge({ tone, children }: { tone: "warning" | "info"; children: string }) {
  return (
    <Box
      component="span"
      sx={{ fontSize: "0.6875rem", fontWeight: 600, px: 0.75, borderRadius: 999, border: 1, borderColor: `${tone}.main`, color: `${tone}.main`, whiteSpace: "nowrap" }}
    >
      {children}
    </Box>
  );
}

/** A comment that quotes a line: the quote as markdown, then what the user wrote. */
function quotedComment(quote: string, text: string): string {
  return `${quote
    .split("\n")
    .map((l) => `> ${l}`)
    .join("\n")}\n\n${text}`;
}

/** A comment's quoted line and what was written under it; the quote is null when it quotes nothing. */
function splitQuote(text: string): { quote: string | null; words: string } {
  const m = /^((?:> .*\n)+)\n([\s\S]*)$/.exec(text);
  if (!m) return { quote: null, words: text };
  const quote = m[1]!
    .trimEnd()
    .split("\n")
    .map((l) => l.slice(2))
    .join("\n");
  return { quote, words: m[2]! };
}

/** The line a comment is about, read-only. */
function Quote({ children }: { children: string }) {
  return (
    <Typography
      component="blockquote"
      variant="body2"
      aria-label="The decision this comment changes"
      sx={{ m: 0, pl: 1.25, borderLeft: 3, borderColor: "divider", color: "text.secondary", whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}
    >
      {children}
    </Typography>
  );
}

/** A draft's text, to edit: what was typed; a quote stays as it was, above the box. */
function Editor({ initial, label, onSave, onCancel }: { initial: string; label: string; onSave: (text: string) => void; onCancel: () => void }) {
  const { quote, words } = splitQuote(initial);
  const [text, setText] = useState(words);
  const save = () => onSave(quote === null ? text.trim() : quotedComment(quote, text.trim()));
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
      {quote !== null && <Quote>{quote}</Quote>}
      <TextField size="small" fullWidth multiline minRows={2} autoFocus value={text} onChange={(e) => setText(e.target.value)} inputProps={{ "aria-label": label }} />
      <Box sx={{ display: "flex", gap: 1, justifyContent: "flex-end" }}>
        <Button size="small" onClick={onCancel}>
          Cancel
        </Button>
        <Button size="small" variant="outlined" disabled={!text.trim()} onClick={save}>
          Save
        </Button>
      </Box>
    </Box>
  );
}

/** A comment as written: its quote, if it has one, set apart from the words under it. */
function Body({ text }: { text: string }) {
  const { quote, words } = splitQuote(text);
  return (
    <>
      {quote !== null && <Quote>{quote}</Quote>}
      <Typography variant="body2" sx={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
        {words}
      </Typography>
    </>
  );
}

function Thread({ comment: c, place, actions }: { comment: ReviewComment; place: string | null; actions: ThreadActions }) {
  const [editing, setEditing] = useState(false);
  return (
    <Box component="li" aria-label={`Comment by ${c.author.name}`} sx={{ border: 1, borderColor: "divider", borderRadius: 1.5, bgcolor: "background.paper", overflow: "hidden" }}>
      <Box sx={{ px: 1.5, py: 1, display: "flex", flexDirection: "column", gap: 0.25 }}>
        <Box sx={{ display: "flex", gap: 1, alignItems: "baseline", flexWrap: "wrap" }}>
          <Typography variant="body2" sx={{ fontWeight: 600 }}>
            {c.author.name}
          </Typography>
          {c.status === "draft" && <Badge tone="warning">Pending</Badge>}
          {c.status === "reopened" && <Badge tone="warning">Reopened</Badge>}
          {c.status === "sent" && (
            <Typography variant="caption" color={c.question && !c.answer ? "warning.main" : "text.secondary"}>
              sent{c.sentAt ? ` ${stamp(c.sentAt)}` : ""}, {c.question && !c.answer ? "the agent has a question" : "with the agent"}
            </Typography>
          )}
          {place && (
            <Typography variant="caption" color="text.secondary">
              on {place}
            </Typography>
          )}
          <Box sx={{ flex: 1 }} />
          {c.status === "draft" && c.author.id === actions.me && !editing && (
            <>
              <Link component="button" variant="caption" onClick={() => setEditing(true)}>
                Edit
              </Link>
              <Link component="button" variant="caption" onClick={() => actions.onDelete(c.id)}>
                Delete
              </Link>
            </>
          )}
        </Box>
        {editing ? (
          <Editor
            initial={c.text}
            label={`Edit your comment: ${c.text}`}
            onCancel={() => setEditing(false)}
            onSave={(text) => {
              actions.onEdit(c.id, text);
              setEditing(false);
            }}
          />
        ) : (
          <Body text={c.text} />
        )}
      </Box>
      {c.question && (
        <Box sx={{ px: 1.5, py: 1, borderTop: 1, borderColor: "divider", bgcolor: soft("warning"), display: "flex", flexDirection: "column", gap: 0.25 }}>
          <Typography variant="body2">
            <Box component="span" sx={{ fontWeight: 600 }}>
              Agent asks:
            </Box>{" "}
            {c.question}
          </Typography>
          <Typography variant="body2" color={c.answer ? "text.primary" : "text.secondary"}>
            {c.answer ? (
              <>
                <Box component="span" sx={{ fontWeight: 600 }}>
                  Answer:
                </Box>{" "}
                {c.answer}
              </>
            ) : (
              "Answer it from the Overview's Answer button."
            )}
          </Typography>
        </Box>
      )}
      {c.reply && (
        <Box sx={{ px: 1.5, py: 1, borderTop: 1, borderColor: "divider", bgcolor: soft("success"), display: "flex", gap: 1, alignItems: "baseline", flexWrap: "wrap" }}>
          <Typography variant="body2" sx={{ fontWeight: 600 }}>
            Agent
          </Typography>
          <Typography variant="body2" sx={{ flex: 1, minWidth: 200 }}>
            <Box component="span" sx={{ color: "success.main", fontWeight: 600 }}>
              ✓ Addressed:
            </Box>{" "}
            {c.reply}
          </Typography>
          {c.status === "addressed" && (
            <Link component="button" variant="caption" onClick={() => actions.onReopen(c.id)}>
              Reopen
            </Link>
          )}
        </Box>
      )}
    </Box>
  );
}

/** Comments, oldest first, each a thread. */
export function CommentThreads({
  comments,
  actions,
  place = () => null,
}: {
  comments: readonly ReviewComment[];
  actions: ThreadActions;
  /** Where on the artifact a comment was made, in words; null for the artifact as a whole. */
  place?: (comment: ReviewComment) => string | null;
}) {
  if (comments.length === 0) return null;
  return (
    <Box component="ul" sx={{ m: 0, p: 0, listStyle: "none", display: "flex", flexDirection: "column", gap: 0.75 }}>
      {comments.map((c) => (
        <Thread key={c.id} comment={c} place={place(c)} actions={actions} />
      ))}
    </Box>
  );
}

/**
 * The text box a comment is written in: it stays Pending until Address
 * comments on the Overview sends it. A comment about one line (a technical
 * decision's Change…) shows that line above the box, read-only, so it always
 * names what it is about; the comment is the quote, then what was typed.
 */
export function CommentComposer({
  label,
  disabled,
  quote = null,
  onAdd,
}: {
  label: string;
  disabled: boolean;
  /** The line the comment is about. */
  quote?: string | null;
  onAdd: (text: string) => void;
}) {
  const [text, setText] = useState("");
  const add = () => {
    if (!text.trim() || disabled) return;
    onAdd(quote === null ? text.trim() : quotedComment(quote, text.trim()));
    setText("");
  };
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
      {quote !== null && <Quote>{quote}</Quote>}
      <TextField
        size="small"
        fullWidth
        multiline
        minRows={2}
        autoFocus
        placeholder="Leave a comment"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) add();
        }}
        inputProps={{ "aria-label": label }}
      />
      {quote !== null && (
        <Typography variant="caption" color="text.secondary">
          Write what you want instead under the quote.
        </Typography>
      )}
      <Box sx={{ display: "flex", gap: 1, alignItems: "center" }}>
        <Typography variant="caption" color="text.secondary" sx={{ flex: 1 }}>
          Stays Pending until you address your comments from the Overview.
        </Typography>
        <Button size="small" variant="outlined" disabled={!text.trim() || disabled} onClick={add}>
          Add comment
        </Button>
      </Box>
    </Box>
  );
}
