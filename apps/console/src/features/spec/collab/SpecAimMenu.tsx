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

import { useCallback, useEffect, useRef, useState } from "react";
import { Box, Button, Chip, Paper, Stack, TextField, Tooltip } from "@wso2/oxygen-ui";
import { Crosshair } from "@wso2/oxygen-ui-icons-react";
import type { Editor } from "@tiptap/react";
import { docBlocks } from "./docBlocks";
import { anchorFor, type Anchor } from "../lib/anchor";
import { setAimHighlight } from "./aimHighlightPlugin";

// Aiming the agent at part of a markdown document (#666).
//
// The rule this component exists to keep is console ADR-0023: a SELECTION MAY
// ONLY OFFER. Selecting text puts a chip on screen and nothing else — no box
// opens, no focus moves, the caret is untouched and the range is never cleared.
// The document is a real editor, and drag-select is overwhelmingly how a person
// retypes, deletes or copies; an input that claimed the keyboard on selection
// would make all three impossible.
//
// The box opens on an EXPLICIT act — the chip, or ⌘K — and only then takes
// focus.
//
// Positioning is computed from `coordsAtPos` rather than delegated to Tiptap's
// BubbleMenu (used above for agent-suggestion review). BubbleMenu hides itself
// when the editor loses focus, which is exactly what opening our box does.

/** What the editor needs to let a selection reach the agent. */
export interface SpecAimBinding {
  /** The document being edited — the anchor's `file`. */
  path: string;
  /**
   * Dispatch. `change` rewrites the selection in place and leaves the chat
   * panel shut; `discuss` opens the same selection as a grilling.
   */
  send: (instruction: string, anchor: Anchor, intent: "change" | "discuss") => Promise<boolean>;
  /** Why a send would be refused right now — an agent already holds the turn —
   *  or `""` when it is live. Doubles as the disabled tooltip. */
  busyReason: string;
}

/**
 * The box's width. Wide enough that a sentence fits on one line and a
 * paragraph reads as welcome — a narrow box tells the user to keep it short,
 * and an aimed instruction is exactly where they should not have to.
 */
const BOX_WIDTH = 560;

/**
 * A request to open the box the user did not drag for — a lens asked (#652's
 * Discuss aims it at a block; #666's add-lenses open it to collect a command's
 * subject). `seq` is what makes two requests for the same spot two events.
 */
export type AimRequest =
  | { kind: "block"; from: number; to: number; intent: "change" | "discuss"; seq: number }
  | {
      kind: "command";
      command: string;
      placeholder: string;
      cta: string;
      at: number;
      seq: number;
    };

interface Placement {
  top: number;
  left: number;
}

/**
 * Where the floating surface sits: under the END of the selection, clamped into
 * the editor's own box so a selection at the right edge stays reachable.
 *
 * The end, not the start: anchored at the start the box lands ON the passage it
 * is about, hiding the text the user is writing an instruction for — which
 * defeats the wash it sits beside. The end is also where the mouse let go.
 */
function placementFor(editor: Editor, host: HTMLElement): Placement | null {
  const { to } = editor.state.selection;
  let coords: { top: number; bottom: number; left: number };
  try {
    coords = editor.view.coordsAtPos(to);
  } catch {
    // The position no longer resolves — an agent rewrote the document out from
    // under a stale selection. Nothing to point at.
    return null;
  }
  const box = host.getBoundingClientRect();
  return {
    top: coords.bottom - box.top + 6,
    left: Math.min(Math.max(coords.left - box.left, 8), Math.max(box.width - BOX_WIDTH, 8)),
  };
}

export function SpecAimMenu({
  editor,
  aim,
  request,
  runCommand,
}: {
  editor: Editor;
  aim: SpecAimBinding;
  /** A lens opening the box on its block; null between requests. */
  request?: AimRequest | null | undefined;
  /** Sends a composed `/command subject` line the way the lens itself would. */
  runCommand?: ((line: string) => void) | undefined;
}) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  const boxRef = useRef<HTMLDivElement | null>(null);
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  // Why the last send did not go — shown UNDER the field. Without this a
  // refused send (an agent already holds the turn, a race on the thread) left
  // the box open and mute: the reason landed as an error row in a chat panel
  // that a quiet Change never opens.
  const [sendError, setSendError] = useState<string | null>(null);
  const [placement, setPlacement] = useState<Placement | null>(null);
  const [hasRange, setHasRange] = useState(false);
  // The aimed range as two NUMBERS, not an object. The wash is painted by
  // dispatching a transaction, and a transaction re-runs `measure` — so an
  // effect keyed on a freshly-built object would paint, re-measure, rebuild the
  // object, and paint again forever. Numbers settle on the second pass.
  const [from, setFrom] = useState(0);
  const [to, setTo] = useState(0);
  // What Enter sends. Change for a selection; a Discuss lens opens the box
  // with Enter sending Discuss — a lens called Discuss whose Enter changed the
  // document would be a lie. Both buttons stay either way.
  const [enterIntent, setEnterIntent] = useState<"change" | "discuss">("change");
  // Command mode (#666): the box is collecting a subject for `/actor` or
  // `/feature` rather than aiming at a selection. No anchor, no wash, one CTA.
  const [compose, setCompose] = useState<{ command: string; placeholder: string; cta: string } | null>(null);

  // Re-measured on every transaction, so the surface follows the text it points
  // at while an agent streams into the same document. Positions are held LIVE
  // (ProseMirror maps them through every edit) and only become an anchor at
  // send — see `handleSend`.
  const measure = useCallback(() => {
    const host = hostRef.current;
    if (!host) return;
    const selection = editor.state.selection;
    setHasRange(!selection.empty);
    setFrom(selection.from);
    setTo(selection.to);
    setPlacement(placementFor(editor, host));
  }, [editor]);

  useEffect(() => {
    measure();
    editor.on("transaction", measure);
    return () => {
      editor.off("transaction", measure);
    };
  }, [editor, measure]);

  // The wash follows whatever is aimed at. It has to survive the box taking
  // focus: the browser stops painting the native selection the moment the
  // editor loses it, so without this the user is typing an instruction about a
  // passage they can no longer see.
  useEffect(() => {
    setAimHighlight(editor.view, !compose && (hasRange || open) ? { from, to } : null);
  }, [editor, hasRange, open, from, to, compose]);

  // Scrolling moves the text without changing the document, so the transaction
  // hook above never fires for it.
  useEffect(() => {
    const scroller = editor.view.dom.closest("[data-aim-scroll]");
    if (!scroller) return;
    scroller.addEventListener("scroll", measure, { passive: true });
    return () => scroller.removeEventListener("scroll", measure);
  }, [editor, measure]);

  // ⌘K / Ctrl-K opens the box on whatever is selected — or, with a bare caret,
  // on the block it sits in, which is what "aim at this" means when nothing is
  // dragged.
  useEffect(() => {
    const dom = editor.view.dom;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "k" || !(e.metaKey || e.ctrlKey)) return;
      e.preventDefault();
      setOpen(true);
    };
    dom.addEventListener("keydown", onKeyDown);
    return () => dom.removeEventListener("keydown", onKeyDown);
  }, [editor]);

  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  // A lens aimed the box for the user. The block becomes the selection — the
  // same state a drag would have left — so everything downstream (the wash,
  // the anchor at send) is the one path, not a second one.
  const seq = request?.seq ?? 0;
  useEffect(() => {
    if (!request || seq === 0) return;
    if (request.kind === "command") {
      // Park the caret at the lens so the box opens where the click was; the
      // command needs no selection, and none is taken.
      editor.commands.setTextSelection({ from: request.at, to: request.at });
      setCompose({ command: request.command, placeholder: request.placeholder, cta: request.cta });
    } else {
      editor.commands.setTextSelection({ from: request.from + 1, to: request.to - 1 });
      setEnterIntent(request.intent);
      setCompose(null);
    }
    setOpen(true);
    // `request` is read once per seq; the fields never change under a seq.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editor, seq]);

  const dismiss = useCallback(() => {
    setOpen(false);
    setText("");
    setEnterIntent("change");
    setCompose(null);
    setSendError(null);
  }, []);

  // Escape: back to the document, caret exactly where it was. Dropping the
  // selection because a suggestion was waved away would be the editor losing
  // the user's place (ADR-0023) — and they may well want to retype over it.
  const close = useCallback(() => {
    dismiss();
    editor.view.focus();
  }, [dismiss, editor]);

  // A click anywhere else dismisses the box — the document, the file rail, the
  // toolbar. Deliberately NOT followed by refocusing the editor: the click is
  // about to put focus (and, in the document, the caret) wherever the user
  // aimed it, and fighting that is exactly the kind of interference ADR-0023
  // exists to forbid. Found on the local setup: without this the box stayed
  // open over a caret that had moved on, and the wash followed the caret, so
  // the box sat there claiming to be about a passage the user never selected.
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (boxRef.current?.contains(e.target as Node)) return;
      dismiss();
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    return () => document.removeEventListener("pointerdown", onPointerDown, true);
  }, [open, dismiss]);

  const handleSend = useCallback(
    (intent: "change" | "discuss") => {
      if (aim.busyReason !== "" || !text.trim()) return;
      const { from, to } = editor.state.selection;
      // Snapshot HERE, not when the selection was made: ProseMirror has been
      // mapping these positions through every concurrent edit, so the excerpt
      // describes what the block says at the moment the user actually asks.
      const anchor = anchorFor(aim.path, docBlocks(editor.state.doc), from, to);
      if (!anchor) {
        setSendError("The selected passage is no longer in the document — reselect and try again.");
        return;
      }
      // The box closes NOW. The message row is already in the log the moment
      // the send starts, so the box has nothing left to say — waiting on the
      // dispatch made Enter read as dead for the seconds it takes. The rare
      // refused dispatch surfaces in the log, and the sender opens the panel
      // onto it so the failure is seen, not filed.
      const line = text.trim();
      close();
      void aim.send(line, anchor, intent);
    },
    [editor, aim, text, close],
  );

  // A composed command rides the lens's own send path — seedChat, which opens
  // the panel and speaks as the user: `/feature manager approvals`. The line is
  // command plus their words, the same shape the per-line lenses compose from
  // the entry they sit on. An empty send is the bare command, which is exactly
  // what the lens did before it learned to ask.
  const handleCompose = useCallback(() => {
    if (!compose || aim.busyReason !== "") return;
    const subject = text.trim();
    runCommand?.(subject ? `${compose.command} ${subject}` : compose.command);
    close();
  }, [compose, aim.busyReason, text, runCommand, close]);

  const busy = aim.busyReason !== "";
  const disabled = busy || (!compose && text.trim() === "");

  return (
    <Box ref={hostRef} sx={{ position: "absolute", inset: 0, pointerEvents: "none" }}>
      {placement && (hasRange || open) && (
        <Box
          sx={{ position: "absolute", top: placement.top, left: placement.left, pointerEvents: "auto", zIndex: 5 }}
        >
          {open ? (
            <Paper
              ref={boxRef}
              elevation={8}
              data-testid="aim-box"
              sx={{
                width: BOX_WIDTH,
                maxWidth: "calc(100% - 16px)",
                p: 1.5,
                // OPAQUE, on purpose. The theme's `background.paper` is glass
                // — a translucent black, blurred — and over a page of prose the
                // text underneath reads as noise behind the words the user is
                // typing. The box sits ON the document; it should not look like
                // it is trying to be part of it. `background.default` is the
                // opaque token; see CatalogTypeDrawer for the same forcing.
                bgcolor: "background.default",
                backgroundImage: "none",
                backdropFilter: "none",
                border: 1,
                borderColor: "divider",
              }}
            >
              <TextField
                inputRef={inputRef}
                fullWidth
                multiline
                maxRows={4}
                size="small"
                placeholder={compose?.placeholder ?? "What should change here?"}
                value={text}
                error={sendError !== null}
                helperText={sendError}
                onChange={(e) => {
                  setText(e.target.value);
                  setSendError(null);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Escape") {
                    e.preventDefault();
                    close();
                  }
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    if (compose) handleCompose();
                    else handleSend(enterIntent);
                  }
                }}
              />
              {compose ? (
                <Stack direction="row" spacing={1} sx={{ mt: 1 }} justifyContent="flex-end">
                  <Tooltip title={busy ? aim.busyReason : compose.command}>
                    <span>
                      <Button size="small" variant="contained" disabled={disabled} onClick={handleCompose}>
                        {compose.cta}
                      </Button>
                    </span>
                  </Tooltip>
                </Stack>
              ) : (
              <Stack direction="row" spacing={1} sx={{ mt: 1 }} justifyContent="flex-end">
                <Tooltip title={busy ? aim.busyReason : "Talk it through before anything changes"}>
                  <span>
                    <Button size="small" disabled={disabled} onClick={() => handleSend("discuss")}>
                      Discuss
                    </Button>
                  </span>
                </Tooltip>
                <Tooltip title={busy ? aim.busyReason : "Rewrite the selection"}>
                  <span>
                    <Button
                      size="small"
                      variant="contained"
                      disabled={disabled}
                      onClick={() => handleSend("change")}
                    >
                      Change
                    </Button>
                  </span>
                </Tooltip>
              </Stack>
              )}
            </Paper>
          ) : (
            <Chip
              data-testid="aim-chip"
              icon={<Crosshair size={14} />}
              label="Ask agent  ⌘K"
              size="small"
              // The chip alone swallows its mousedown, so pressing it does not
              // blur the editor and lose the native selection before the box
              // has taken over. Only the chip: on the box this same handler
              // stopped the textarea taking focus from a click, and keystrokes
              // went into the document instead — found on the local setup.
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => setOpen(true)}
              sx={{
                cursor: "pointer",
                // The chip floats over running text, so it has to separate
                // itself from it: solid paper, a real border, and a shadow
                // that reads as lift rather than tint. A translucent chip over
                // a paragraph was invisible — found in use. `background.default`
                // is the opaque token; `paper` is glass in this theme.
                bgcolor: "background.default",
                backgroundImage: "none",
                backdropFilter: "none",
                border: 1,
                borderColor: "primary.main",
                color: "primary.main",
                fontWeight: 600,
                boxShadow: 6,
                "& .MuiChip-icon": { color: "primary.main" },
                "&:hover": { bgcolor: "background.default", boxShadow: 8 },
              }}
            />
          )}
        </Box>
      )}
    </Box>
  );
}
