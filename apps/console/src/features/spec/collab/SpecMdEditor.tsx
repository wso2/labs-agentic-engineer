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

import { useEffect, useRef, useState } from "react";
import { alpha, Box, Button, Paper, Stack, Typography } from "@wso2/oxygen-ui";
import { Check, Sparkles, X } from "@wso2/oxygen-ui-icons-react";
import { EditorContent, useEditor } from "@tiptap/react";
import { BubbleMenu } from "@tiptap/react/menus";
import StarterKit from "@tiptap/starter-kit";
import { AgentStreamingContext, MermaidCodeBlock } from "./MermaidCodeBlock";
import { Markdown } from "@tiptap/markdown";
import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import { useStickToBottom } from "use-stick-to-bottom";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import type * as Y from "yjs";
import { AgentInsertion } from "@aep/collab-doc";
import {
  acceptAll,
  acceptRange,
  agentRanges,
  rangeAt,
  rejectAll,
  rejectRange,
} from "./agentReview";
import { SpecMdToolbar } from "./SpecMdToolbar";
import { PrdLenses, refreshPrdLenses, type PrdLensBinding } from "./prdLensPlugin";
import { SpecLinks, refreshSpecLinks, type SpecLinkBinding } from "./specLinkPlugin";

// Collaborative WYSIWYG editor for markdown spec files (#86 phase 6).
// The shared source of truth is the file's Y.XmlFragment (seeded server-side
// from the repo markdown; serialized back to markdown by the committer,
// phase 3). Yjs owns history, so StarterKit's undo/redo is disabled.
// Agent insertions arrive marked (agentInsertion) — highlighted for review;
// accept keeps the text, reject deletes it; the committer holds marked files
// out of interim flushes until reviewed.
export function SpecMdEditor({
  fragment,
  provider,
  self,
  agentStreaming,
  lenses,
  links,
}: {
  fragment: Y.XmlFragment;
  provider: HocuspocusProvider;
  self: { name: string; color: string };
  // True while an agent peer is writing into the room. Drives tail-following:
  // the document area follows the streamed text only while this is set.
  agentStreaming: boolean;
  /** The PRD's code lenses (#579) — passed for that file only, absent elsewhere. */
  lenses?: PrdLensBinding | undefined;
  /** Cross-references to sibling spec documents; every markdown file carries them. */
  links?: SpecLinkBinding | undefined;
}) {
  // The extension list is built once per (fragment, provider) — a file swap
  // remounts this component under a new key — so the plugin reaches the CURRENT
  // binding through a ref rather than the closure it was configured with.
  const lensRef = useRef(lenses);
  const linkRef = useRef(links);
  useEffect(() => {
    lensRef.current = lenses;
    linkRef.current = links;
  });
  const editor = useEditor(
    {
      extensions: [
        // codeBlock is disabled in StarterKit and re-added mermaid-aware:
        // ```mermaid blocks render as live diagrams, click to edit source.
        StarterKit.configure({ undoRedo: false, codeBlock: false }),
        MermaidCodeBlock,
        Markdown,
        AgentInsertion,
        Collaboration.configure({ fragment }),
        CollaborationCaret.configure({ provider, user: self }),
        SpecLinks.configure({ binding: () => linkRef.current }),
        ...(lenses
          ? [
              PrdLenses.configure({
                run: (command: string) => lensRef.current?.run(command),
                isBusy: () => Boolean(lensRef.current?.busyReason),
                busyReason: () => lensRef.current?.busyReason ?? "",
              }),
            ]
          : []),
      ],
    },
    [fragment, provider],
  );

  // Whether a lens is clickable is console state, not document state, so a
  // change to it has to ask ProseMirror to re-render the widgets. The ref
  // effect above is declared first, so it has already landed by the time this
  // rebuild reads the binding.
  const busyReason = lenses?.busyReason ?? "";
  useEffect(() => {
    if (editor && lensRef.current) refreshPrdLenses(editor.view);
  }, [editor, busyReason]);

  // Same for the project's file list: which references resolve is console
  // state, and a feature doc the agent just wrote makes one more of them live.
  // Compared by value — the caller rebuilds the array on every render, and a
  // rebuild per keystroke would be a decoration sweep for nothing.
  const knownPaths = (links?.knownPaths ?? []).join("\n");
  useEffect(() => {
    if (editor && linkRef.current) refreshSpecLinks(editor.view);
  }, [editor, knownPaths]);

  // Pending-review count, refreshed on every document change.
  const [pending, setPending] = useState(0);
  useEffect(() => {
    if (!editor) return;
    const update = () => setPending(agentRanges(editor.state.doc).length);
    update();
    editor.on("transaction", update);
    return () => {
      editor.off("transaction", update);
    };
  }, [editor]);

  // Follow the tail while an agent streams markdown into this doc. The content
  // grows via Yjs/ProseMirror (outside React's render), so a `useEffect` on
  // React state can't see it — use-stick-to-bottom drives the scroll from a
  // ResizeObserver on the content element instead. The content ref is only
  // attached while `agentStreaming`, so ordinary human editing never triggers
  // auto-scroll; scrolling up mid-stream breaks the lock (the library tells
  // user scroll apart from its own animation). `initial: false` keeps an
  // already-written file scrolled to its top when opened.
  const { scrollRef, contentRef, scrollToBottom } = useStickToBottom({
    initial: false,
  });
  useEffect(() => {
    if (agentStreaming) void scrollToBottom();
  }, [agentStreaming, scrollToBottom]);

  return (
    <Box
      sx={{
        // The editor is a single framed card that fills the pane: toolbar
        // (and review bar) docked as header rows, only the document area
        // below them scrolls — text can never pass the toolbar (#206 rework).
        flexGrow: 1,
        minHeight: 0,
        display: "flex",
        flexDirection: "column",
        border: 1,
        borderColor: "divider",
        borderRadius: 1,
        overflow: "hidden",
        // Plain paper token, same as AgentChatPanel — it re-resolves per
        // color scheme (a JS-resolved gradient froze the light paper into
        // dark mode and rendered gray; #206 D10 revision, 2026-07-12).
        bgcolor: "background.paper",
        "&:focus-within": { borderColor: "primary.main" },
      }}
    >
      {editor && (
        <>
          <SpecMdToolbar editor={editor} />
          {pending > 0 && (
            <Stack
              direction="row"
              alignItems="center"
              spacing={1}
              sx={{
                flexShrink: 0,
                px: 1.5,
                py: 0.75,
                borderBottom: 1,
                borderColor: "divider",
                backgroundImage: (theme) =>
                  `linear-gradient(${alpha(theme.palette.primary.main, 0.08)}, ${alpha(
                    theme.palette.primary.main,
                    0.08,
                  )})`,
              }}
            >
              <Sparkles size={16} />
              <Typography variant="body2" sx={{ flexGrow: 1 }}>
                {pending} agent suggestion{pending === 1 ? "" : "s"} pending review
              </Typography>
              <Button
                size="small"
                color="inherit"
                startIcon={<X size={14} />}
                onClick={() => rejectAll(editor)}
              >
                Reject all
              </Button>
              <Button
                size="small"
                variant="contained"
                startIcon={<Check size={14} />}
                onClick={() => acceptAll(editor)}
              >
                Accept all
              </Button>
            </Stack>
          )}
        </>
      )}
      {editor && (
        <BubbleMenu
          editor={editor}
          shouldShow={({ state }) =>
            rangeAt(state.doc, state.selection.from) !== null
          }
        >
          <Paper elevation={3} sx={{ px: 1, py: 0.5 }}>
            <Stack direction="row" alignItems="center" spacing={0.5}>
              <Typography variant="caption" color="text.secondary" sx={{ pr: 0.5 }}>
                {rangeAt(editor.state.doc, editor.state.selection.from)?.agent ||
                  "agent"}
              </Typography>
              <Button
                size="small"
                startIcon={<Check size={14} />}
                onClick={() => {
                  const r = rangeAt(editor.state.doc, editor.state.selection.from);
                  if (r) acceptRange(editor, r);
                }}
              >
                Accept
              </Button>
              <Button
                size="small"
                color="error"
                startIcon={<X size={14} />}
                onClick={() => {
                  const r = rangeAt(editor.state.doc, editor.state.selection.from);
                  if (r) rejectRange(editor, r);
                }}
              >
                Reject
              </Button>
            </Stack>
          </Paper>
        </BubbleMenu>
      )}
      <Box
        ref={scrollRef}
        sx={{
          flexGrow: 1,
          minHeight: 0,
          overflow: "auto",
          px: 2,
          py: 1,
          cursor: "text",
          "& .tiptap": { outline: "none" },
          "& .tiptap p": { my: 1 },
          "& .tiptap h1, & .tiptap h2, & .tiptap h3": { mt: 2, mb: 1 },
          "& .tiptap ul, & .tiptap ol": { pl: 3 },
          "& .tiptap table": { borderCollapse: "collapse" },
          "& .tiptap th, & .tiptap td": {
            border: 1,
            borderColor: "divider",
            px: 1,
            py: 0.5,
          },
          // The PRD's code lenses (#579). A SECTION lens is always on show —
          // it is how the command is discovered at all — while a LINE lens
          // appears on its entry's hover (or focus, for the keyboard), so a
          // twenty-story list carries three visible controls rather than
          // twenty-three.
          "& .prd-lens": {
            all: "unset",
            cursor: "pointer",
            userSelect: "none",
            whiteSpace: "nowrap",
            verticalAlign: "baseline",
            display: "inline-block",
            ml: 1,
            px: 0.75,
            py: 0.125,
            // Same size as the remote-caret label below: the smallest thing
            // this editor already asks anyone to read.
            fontSize: "0.7rem",
            lineHeight: 1.6,
            fontStyle: "normal",
            fontWeight: 500,
            borderRadius: "999px",
            border: "1px solid",
            borderColor: "divider",
            color: "text.secondary",
            transition: "opacity 120ms, color 120ms, border-color 120ms",
          },
          "& .prd-lens:hover:not(:disabled)": {
            color: "primary.main",
            borderColor: "primary.main",
            bgcolor: (theme) => alpha(theme.palette.primary.main, 0.08),
          },
          "& .prd-lens:disabled": { cursor: "default", opacity: 0.4 },
          "& .prd-lens--line": { opacity: 0 },
          "& .tiptap li:hover .prd-lens--line, & .tiptap p:hover .prd-lens--line, & .prd-lens--line:focus-visible":
            { opacity: 1 },
          // A reference to a sibling spec document reads as a link and acts
          // like one; an ordinary external link keeps the editor's default.
          "& .spec-link": {
            cursor: "pointer",
            color: "primary.main",
            textDecoration: "underline",
            textUnderlineOffset: "2px",
          },
          // The two kinds of unsettled read differently because they ARE
          // different: an assumption is one word of an otherwise-settled
          // decision, an open question is a whole entry nobody has answered.
          // A deferred one keeps its entry but drops to a resting grey.
          "& .prd-flag--assumed": {
            borderRadius: "2px",
            px: 0.25,
            bgcolor: (theme) => alpha(theme.palette.warning.main, 0.16),
            borderBottom: "1px dashed",
            borderBottomColor: "warning.main",
          },
          "& .tiptap .prd-flag--question, & .tiptap .prd-flag--deferred": {
            pl: 1,
            borderLeft: "3px solid",
          },
          "& .tiptap .prd-flag--question": { borderLeftColor: "info.main" },
          "& .tiptap .prd-flag--deferred": {
            borderLeftColor: "divider",
            color: "text.secondary",
          },
          // Unreviewed agent insertions (#86 ph6) — soft primary wash until
          // accepted (mark removed) or rejected (range deleted).
          "& .tiptap span.agent-insertion": {
            bgcolor: (theme) => alpha(theme.palette.primary.main, 0.12),
            borderBottom: "2px solid",
            borderBottomColor: (theme) => alpha(theme.palette.primary.main, 0.4),
            borderRadius: "2px",
          },
          // Remote carets (CollaborationCaret): colored bar + name label.
          "& .collaboration-carets__caret": {
            borderLeft: "1px solid",
            borderRight: "1px solid",
            mx: "-1px",
            position: "relative",
            wordBreak: "normal",
          },
          "& .collaboration-carets__label": {
            borderRadius: "3px 3px 3px 0",
            color: "common.white",
            fontSize: "0.7rem",
            fontWeight: 600,
            left: "-1px",
            lineHeight: 1.2,
            px: 0.5,
            position: "absolute",
            top: "-1.3em",
            userSelect: "none",
            whiteSpace: "nowrap",
          },
        }}
        onClick={() => editor?.commands.focus()}
      >
        {/* The content wrapper is what use-stick-to-bottom measures. It is
            only attached to the ResizeObserver while an agent is streaming, so
            the tail-follow engages for agent writes and stays out of the way
            of normal editing. */}
        <Box ref={agentStreaming ? contentRef : undefined}>
          {/* Node views render through portals into this tree, so the mermaid
              blocks read the streaming flag from here to hide their control. */}
          <AgentStreamingContext.Provider value={agentStreaming}>
            <EditorContent editor={editor} />
          </AgentStreamingContext.Provider>
        </Box>
      </Box>
    </Box>
  );
}
