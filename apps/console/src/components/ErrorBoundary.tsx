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

import { Component, useState, type ErrorInfo, type ReactNode } from "react";
import { Box, Button, Collapse, Typography, type SxProps, type Theme } from "@wso2/oxygen-ui";
import { TriangleAlert } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "./EmptyState";

// A render-time throw anywhere in the console used to reach TanStack Router's
// top-level catch, which swaps the WHOLE shell (nav, header, chat) for its
// default "Something went wrong / Show Error" page. Nothing recorded the
// stack, and only a page reload brought the app back.
//
// This boundary contains such a throw to the section it wraps — a page, the
// chat panel — so the rest of the workspace keeps working, and it records
// what happened so the next occurrence can be fixed at its source. It is
// containment, not a fix: the root cause of a throw stays a bug to chase
// with the stack this component now surfaces.
//
// Recovery, in order:
//   1. New input. The parent passes `resetKey` — the scene a canvas draws,
//      the pathname a page renders — and a change clears the error.
//   2. Bounded automatic retry. The observed failures behave like timing
//      races (a reload of the SAME content renders fine), so the boundary
//      retries the same children after a short wait. It stops after
//      RETRY_DELAYS_MS runs out: a deterministic throw must not become a
//      fallback that flashes forever and hides the bug.
//   3. The "Try again" button, which also gives the automatic attempts back.

/** Waits before each automatic retry; the list's length is the attempt cap. */
const RETRY_DELAYS_MS: readonly number[] = [2_000, 5_000];
/**
 * How long a recovered section must stay up before its automatic attempts
 * are given back. Without this a boundary that recovered once would meet
 * the next, unrelated failure with fewer attempts, and eventually none.
 */
const SETTLE_MS = 10_000;

interface ErrorBoundaryProps {
  children: ReactNode;
  /** What the reader was looking at, in a sentence fragment: "The chat panel". */
  label: string;
  /** A change here clears the error and resets the automatic attempts. */
  resetKey?: unknown;
  /** Fallback stretches to fill a flex column (the canvas, the chat panel). */
  fill?: boolean;
  /** Extra sizing for the fallback when the children set their own (the chat panel's width). */
  fallbackSx?: SxProps<Theme>;
  /**
   * What the fallback says once the automatic attempts are spent. A section
   * defaults to advice; the app-level boundary passes the "contact your
   * administrator" line, since nothing above it can recover.
   */
  exhaustedMessage?: string;
  /**
   * Also offer "Reload page". For the app-level boundary: the commonest
   * whole-app failure is a stale bundle after a deploy, which only a reload fixes.
   */
  offerReload?: boolean;
}

const DEFAULT_EXHAUSTED_MESSAGE = "Try again, or reload the page if it keeps happening.";

interface ErrorBoundaryState {
  error: Error | null;
  /** Automatic retries spent on the current run of failures. */
  attempts: number;
}

const CLEAR: ErrorBoundaryState = { error: null, attempts: 0 };

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  override state: ErrorBoundaryState = CLEAR;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private settleTimer: ReturnType<typeof setTimeout> | null = null;
  // Kept off React state on purpose. componentDidCatch must not setState:
  // React DevTools' "force error" re-applies the error on EVERY render of the
  // boundary, so a catch that re-renders the boundary loops until React
  // throws "Maximum update depth exceeded" — past this boundary, into the
  // router's top-level catch. The stack is read lazily by the fallback's
  // Details toggle instead, which re-renders only the fallback.
  private componentStack: string | null = null;

  static getDerivedStateFromError(error: unknown): Partial<ErrorBoundaryState> {
    return { error: error instanceof Error ? error : new Error(String(error)) };
  }

  override componentDidCatch(error: Error, info: ErrorInfo) {
    // The one place the stack is recorded. Logged, not swallowed: without
    // this the fallback would hide the very evidence needed to fix the cause.
    console.error(`[console] ${this.props.label} failed to render`, error, info.componentStack);
    this.componentStack = info.componentStack ?? null;
    // A failure inside the settle window is the same run, not a new one.
    this.clearSettleTimer();
    // One retry timer at a time. A resetKey change that lands in the same
    // commit as a catch runs componentDidUpdate (and its reset) BEFORE this
    // hook, so the timer scheduled here would outlive that reset and the
    // catch on the re-rendered children would add a second one.
    this.clearTimer();
    const delay = RETRY_DELAYS_MS[this.state.attempts];
    if (delay === undefined) return;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.setState((s) => ({ error: null, attempts: s.attempts + 1 }));
    }, delay);
  }

  override componentDidUpdate(prev: ErrorBoundaryProps, prevState: ErrorBoundaryState) {
    // New input ends the run of failures whether or not one is showing: a
    // recovered section still inside its settle window would otherwise carry
    // the spent count into the next input. Guarded so a key that changes
    // every flush (the canvas scene) costs no commit when there is nothing
    // to reset.
    if (prev.resetKey !== this.props.resetKey && (this.state.error || this.state.attempts > 0)) this.reset();
    // A commit with no error after one with an error means the children
    // rendered. If they stay up for SETTLE_MS, the run of failures is over.
    if (prevState.error && !this.state.error && this.state.attempts > 0) {
      this.clearSettleTimer();
      this.settleTimer = setTimeout(() => {
        this.settleTimer = null;
        this.setState({ attempts: 0 });
      }, SETTLE_MS);
    }
  }

  override componentWillUnmount() {
    this.clearTimer();
    this.clearSettleTimer();
  }

  private clearTimer() {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
  }

  private clearSettleTimer() {
    if (this.settleTimer) clearTimeout(this.settleTimer);
    this.settleTimer = null;
  }

  private reset = () => {
    this.clearTimer();
    this.clearSettleTimer();
    this.componentStack = null;
    this.setState(CLEAR);
  };

  private readComponentStack = () => this.componentStack;

  override render() {
    const { error, attempts } = this.state;
    if (!error) return this.props.children;
    return (
      <ErrorFallback
        label={this.props.label}
        error={error}
        readComponentStack={this.readComponentStack}
        // Derived, not stored: componentDidCatch schedules a retry exactly
        // when an attempt is left (see RETRY_DELAYS_MS).
        retryPending={attempts < RETRY_DELAYS_MS.length}
        onRetry={this.reset}
        fill={this.props.fill ?? false}
        sx={this.props.fallbackSx}
        exhaustedMessage={this.props.exhaustedMessage ?? DEFAULT_EXHAUSTED_MESSAGE}
        offerReload={this.props.offerReload ?? false}
      />
    );
  }
}

function ErrorFallback({
  label,
  error,
  readComponentStack,
  retryPending,
  onRetry,
  fill,
  sx,
  exhaustedMessage,
  offerReload,
}: {
  label: string;
  error: Error;
  /** Read when Details opens: the stack lands after the first fallback render. */
  readComponentStack: () => string | null;
  retryPending: boolean;
  onRetry: () => void;
  fill: boolean;
  sx?: SxProps<Theme> | undefined;
  exhaustedMessage: string;
  offerReload: boolean;
}) {
  const [showDetails, setShowDetails] = useState(false);
  const details = [error.stack ?? `${error.name}: ${error.message}`, showDetails ? readComponentStack()?.trim() : null]
    .filter(Boolean)
    .join("\n\nComponent stack:\n");
  return (
    <Box
      role="alert"
      sx={[
        {
          ...(fill && { flex: 1, minHeight: 0, minWidth: 0, display: "flex", flexDirection: "column", justifyContent: "center" }),
          overflow: "auto",
        },
        ...(Array.isArray(sx) ? sx : [sx]),
      ]}
    >
      <EmptyState
        icon={<TriangleAlert size={48} />}
        title="Something went wrong"
        description={
          retryPending
            ? `${label} hit an error while rendering. Retrying automatically…`
            : `${label} hit an error while rendering. ${exhaustedMessage}`
        }
        action={
          <Box sx={{ display: "flex", gap: 1, justifyContent: "center", flexWrap: "wrap" }}>
            <Button variant="contained" onClick={onRetry}>
              Try again
            </Button>
            {offerReload && (
              <Button variant="outlined" onClick={() => window.location.reload()}>
                Reload page
              </Button>
            )}
            <Button variant="text" onClick={() => setShowDetails((v) => !v)} aria-expanded={showDetails}>
              {showDetails ? "Hide details" : "Details"}
            </Button>
          </Box>
        }
      />
      <Collapse in={showDetails} unmountOnExit>
        <Typography
          component="pre"
          variant="caption"
          sx={{
            mx: 2,
            mb: 2,
            p: 1.5,
            textAlign: "left",
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
            bgcolor: "action.hover",
            borderRadius: 1,
            maxHeight: 320,
            overflow: "auto",
            userSelect: "text",
          }}
        >
          {details}
        </Typography>
      </Collapse>
    </Box>
  );
}
