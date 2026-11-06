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

import { useRef, useState, type ReactNode } from "react";
import { Box, Button, Checkbox, Popover, Typography } from "@wso2/oxygen-ui";
import { MessageSquare } from "@wso2/oxygen-ui-icons-react";
import type { ReviewComment } from "@aep/agent-stream";
import { CommentComposer, CommentThreads, type ThreadActions } from "./CommentThreads";

// An artifact's title line, as a file's header in GitHub's pull request
// review (D13, L23): its name and file, then at the right a plain "Checked"
// box (GitHub's Viewed: ticking it records the artifact's hash, and a changed
// artifact shows unticked again, "Changed since you checked it"; it does not
// collapse anything) and a 💬 with the count of its comments, which drops
// down its threads and a text box for a new one. Acceptance tests take
// comments but no Checked. The title line names the artifact's revision once
// a design run has changed it ("Revision 2"). The file's body can open the 💬
// on a comment about one line (`quote`: a technical decision's Change…).

export interface CheckState {
  checked: boolean;
  /** Ticked once, and the artifact changed since. */
  changed: boolean;
}

export function ArtifactFrame({
  title,
  path,
  revision,
  check,
  onCheck,
  comments,
  actions,
  onComment,
  place,
  ready,
  quote = null,
  onQuoteDone,
  children,
}: {
  title: string;
  path: string;
  /** The design runs that changed it (the first design is 1); absent when not known. */
  revision?: number | undefined;
  /** Null: not checkable (reference only). */
  check: CheckState | null;
  onCheck: (checked: boolean) => void;
  comments: readonly ReviewComment[];
  actions: ThreadActions;
  onComment: (text: string) => void;
  place?: (comment: ReviewComment) => string | null;
  /** Whether the review can be written now. */
  ready: boolean;
  /** A line of the file to comment on: the 💬 opens with it quoted above the text box. */
  quote?: string | null;
  /** The 💬 closed, or the comment on the quoted line was added. */
  onQuoteDone?: () => void;
  children: ReactNode;
}) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const button = useRef<HTMLButtonElement>(null);
  const open = anchor !== null || quote !== null;
  return (
    <Box component="section" aria-label={title} sx={{ display: "flex", flexDirection: "column", gap: 2, minWidth: 0 }}>
      <Box
        component="header"
        sx={{ display: "flex", alignItems: "center", gap: 1, px: 1.5, py: 0.75, bgcolor: "action.hover", border: 1, borderColor: "divider", borderRadius: 2, flexWrap: "wrap" }}
      >
        <Box sx={{ minWidth: 0, flex: 1, display: "flex", alignItems: "baseline", columnGap: 1, flexWrap: "wrap" }}>
          <Typography component="h2" sx={{ fontSize: "0.9375rem", fontWeight: 600 }}>
            {title}
          </Typography>
          <Typography variant="caption" color="text.secondary" sx={{ fontFamily: "monospace", overflowWrap: "anywhere" }}>
            {path}
          </Typography>
          {revision !== undefined && (
            <Typography variant="caption" color="text.secondary">
              Revision {revision}
            </Typography>
          )}
          {check?.changed && (
            <Typography variant="caption" color="warning.main">
              {revision !== undefined ? "· " : ""}Changed since you checked it
            </Typography>
          )}
        </Box>
        {check && (
          <Box component="label" sx={{ display: "flex", alignItems: "center", gap: 0.25, pr: 1, cursor: "pointer", fontSize: "0.8125rem" }}>
            <Checkbox
              size="small"
              checked={check.checked}
              disabled={!ready}
              onChange={(e) => onCheck(e.target.checked)}
              sx={{ p: 0.5 }}
              inputProps={{ "aria-label": `Checked: ${title}` }}
            />
            Checked
          </Box>
        )}
        <Button
          ref={button}
          size="small"
          variant="text"
          onClick={(e) => setAnchor(anchor ? null : e.currentTarget)}
          aria-label={`Comments on ${title}: ${comments.length}`}
          aria-expanded={open}
          startIcon={<MessageSquare size={15} />}
          sx={{ minWidth: 0, px: 1 }}
        >
          {comments.length > 0 ? comments.length : ""}
        </Button>
        <Popover
          open={open}
          anchorEl={anchor ?? button.current}
          onClose={() => {
            setAnchor(null);
            onQuoteDone?.();
          }}
          anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
          transformOrigin={{ vertical: "top", horizontal: "right" }}
          slotProps={{ paper: { sx: { width: 440, maxWidth: "calc(100vw - 32px)", p: 1.5, display: "flex", flexDirection: "column", gap: 1.25 } } }}
        >
          <Typography variant="body2" sx={{ fontWeight: 600 }}>
            Comments on {title}
          </Typography>
          {comments.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              No comments yet.
            </Typography>
          ) : (
            <Box sx={{ maxHeight: 360, overflowY: "auto" }}>
              <CommentThreads comments={comments} actions={actions} {...(place ? { place } : {})} />
            </Box>
          )}
          <CommentComposer
            key={quote ?? ""}
            label={`Your comment on ${title}`}
            disabled={!ready}
            quote={quote}
            onAdd={(text) => {
              onComment(text);
              // Stay open on the threads, as after any comment.
              if (quote !== null) {
                setAnchor(button.current);
                onQuoteDone?.();
              }
            }}
          />
        </Popover>
      </Box>
      <Box sx={{ minWidth: 0 }}>{children}</Box>
    </Box>
  );
}
