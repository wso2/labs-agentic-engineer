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

import { useState, type ReactNode } from "react";
import { Box, Button, TextField, Typography } from "@wso2/oxygen-ui";
import type { DesignComment } from "../api/designModel";
import { canReply, canResolve } from "../model/comments";
import { soft } from "../../spec/components/Tag";

const STATUS_WORDS: Record<DesignComment["status"], string> = {
  open: "open",
  addressed: "addressed, check it",
  resolved: "resolved",
};

export function PinNumber({ n, status }: { n: number | string; status: DesignComment["status"] | "draft" }) {
  return (
    <Box
      component="span"
      sx={{
        display: "inline-grid",
        placeItems: "center",
        minWidth: 20,
        height: 20,
        px: 0.5,
        borderRadius: "10px 10px 10px 2px",
        bgcolor: status === "addressed" ? "success.main" : "info.main",
        color: "background.paper",
        fontFamily: "monospace",
        fontSize: "0.6875rem",
        fontWeight: 600,
      }}
    >
      {n}
    </Box>
  );
}

/** Writing a comment or a reply: what it is about, the words, and its two buttons. */
function Draft({
  label,
  placeholder,
  submit,
  busy,
  onSubmit,
  onCancel,
}: {
  label: ReactNode;
  placeholder: string;
  submit: string;
  busy: boolean;
  onSubmit: (text: string) => void;
  onCancel: () => void;
}) {
  const [text, setText] = useState("");
  return (
    <Box sx={{ border: 1, borderColor: "info.main", borderRadius: 2.5, px: 1.5, py: 1.25, display: "flex", flexDirection: "column", gap: 1 }}>
      <Typography variant="body2">{label}</Typography>
      <TextField
        autoFocus
        multiline
        minRows={2}
        size="small"
        placeholder={placeholder}
        value={text}
        onChange={(e) => setText(e.target.value)}
        slotProps={{ htmlInput: { "aria-label": placeholder } }}
      />
      <Box sx={{ display: "flex", gap: 1 }}>
        <Button size="small" variant="contained" disabled={!text.trim() || busy} onClick={() => onSubmit(text)}>
          {submit}
        </Button>
        <Button size="small" onClick={onCancel}>
          Cancel
        </Button>
      </Box>
    </Box>
  );
}

function CommentCard({
  comment,
  unanchored,
  busy,
  onResolve,
  onReply,
}: {
  comment: DesignComment;
  /** Its element is gone from the artifact: it has no pin. */
  unanchored: boolean;
  busy: boolean;
  onResolve: () => void;
  onReply: (text: string) => void;
}) {
  const [replying, setReplying] = useState(false);
  const addressed = comment.status === "addressed";
  return (
    <Box
      component="li"
      sx={{
        border: 1,
        borderColor: addressed ? "success.main" : "divider",
        borderRadius: 2.5,
        px: 1.5,
        py: 1.25,
        display: "flex",
        flexDirection: "column",
        gap: 0.75,
        fontSize: "0.8125rem",
      }}
    >
      <Box sx={{ display: "flex", alignItems: "center", gap: 0.75, minWidth: 0 }}>
        <PinNumber n={comment.n} status={comment.status} />
        <Typography component="b" sx={{ fontSize: "0.8125rem", fontWeight: 600, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {comment.anchor.label}
        </Typography>
        <Typography component="span" sx={{ ml: "auto", fontSize: "0.72rem", fontWeight: 600, whiteSpace: "nowrap", color: addressed ? "success.main" : "info.main" }}>
          {STATUS_WORDS[comment.status]}
        </Typography>
      </Box>
      {unanchored && (
        <Typography variant="caption" color="text.secondary">
          Unanchored: the element this was on is gone.
        </Typography>
      )}
      {comment.earlier.map((said, i) => (
        <Typography key={i} variant="body2" color="text.secondary" sx={{ fontSize: "0.8125rem" }}>
          {said}
        </Typography>
      ))}
      <Typography variant="body2" sx={{ fontSize: "0.8125rem" }}>
        {comment.text}
      </Typography>
      {comment.reply && (
        <Typography variant="body2" sx={{ fontSize: "0.8125rem", bgcolor: "action.hover", borderRadius: 2, px: 1.25, py: 0.75 }}>
          <Box component="span" sx={{ fontWeight: 600, color: "primary.main", mr: 0.5 }}>
            Agent
          </Box>
          {comment.reply}
          {comment.specLine && (
            <Box component="span" sx={{ display: "block", mt: 0.5, color: "text.secondary" }}>
              Changed in the spec: {comment.specLine}
            </Box>
          )}
        </Typography>
      )}
      {replying ? (
        <Draft
          label="What's still not right?"
          placeholder="Your reply"
          submit="Reply"
          busy={busy}
          onSubmit={(text) => {
            onReply(text);
            setReplying(false);
          }}
          onCancel={() => setReplying(false)}
        />
      ) : (
        (canResolve(comment) || canReply(comment)) && (
          <Box sx={{ display: "flex", gap: 1 }}>
            {canResolve(comment) && (
              <Button size="small" variant="outlined" color="success" disabled={busy} onClick={onResolve}>
                Resolve
              </Button>
            )}
            {canReply(comment) && (
              <Button size="small" disabled={busy} onClick={() => setReplying(true)}>
                Reply
              </Button>
            )}
          </Box>
        )
      )}
    </Box>
  );
}

/**
 * The comments on the open artifact, under it: the one being written, then
 * each unresolved one with the agent's reply once addressed (and, when the
 * element it was on is gone, saying so: it has no pin), and how many are
 * resolved.
 */
export function CommentsPanel({
  comments,
  gone,
  draft,
  busy,
  onPin,
  onCancelDraft,
  onResolve,
  onReply,
}: {
  comments: DesignComment[];
  /** The comments whose element is gone from the artifact. */
  gone: ReadonlySet<string>;
  /** The element a comment is being written about. */
  draft: { label: string } | null;
  busy: boolean;
  onPin: (text: string) => void;
  onCancelDraft: () => void;
  onResolve: (id: string) => void;
  onReply: (id: string, text: string) => void;
}) {
  const unresolved = comments.filter((c) => c.status !== "resolved");
  const resolved = comments.length - unresolved.length;
  if (!draft && unresolved.length === 0 && resolved === 0) return null;
  return (
    <Box
      component="section"
      aria-label="Comments on this artifact"
      sx={{ borderTop: 1, borderColor: "divider", pt: 1.75, mt: 2.5, display: "flex", flexDirection: "column", gap: 1.25, maxWidth: "72ch" }}
    >
      {(draft || unresolved.length > 0) && (
        <Typography component="h3" sx={{ fontSize: "0.6875rem", letterSpacing: "0.08em", textTransform: "uppercase", fontWeight: 600, color: "text.secondary" }}>
          Comments on this artifact
        </Typography>
      )}
      {draft && (
        <Draft
          label={
            <>
              Comment on <b>{draft.label}</b>
            </>
          }
          placeholder="What should change?"
          submit="Pin comment"
          busy={busy}
          onSubmit={onPin}
          onCancel={onCancelDraft}
        />
      )}
      {unresolved.length > 0 && (
        <Box component="ul" sx={{ listStyle: "none", m: 0, p: 0, display: "flex", flexDirection: "column", gap: 1.25 }}>
          {unresolved.map((c) => (
            <CommentCard
              key={c.id}
              comment={c}
              unanchored={gone.has(c.id)}
              busy={busy}
              onResolve={() => onResolve(c.id)}
              onReply={(text) => onReply(c.id, text)}
            />
          ))}
        </Box>
      )}
      {resolved > 0 && (
        <Typography variant="caption" color="text.secondary" sx={{ bgcolor: soft("success", 0.06), alignSelf: "flex-start", px: 1, borderRadius: 1 }}>
          {resolved} resolved comment{resolved === 1 ? "" : "s"}.
        </Typography>
      )}
    </Box>
  );
}
