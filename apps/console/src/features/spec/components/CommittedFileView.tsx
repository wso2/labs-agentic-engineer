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

import { Alert, Box, Button, CircularProgress, TextField } from "@wso2/oxygen-ui";
import { MarkdownView } from "../../../components/MarkdownView";
import { EmptyState } from "../../../components/EmptyState";
import { fileLabel } from "../api/labels";

/**
 * A document as GIT has it — the view the spec workspace falls back to whenever
 * the live room is not the source (#586).
 *
 * It exists because the fallback used to be dishonest in both directions. A
 * room that failed to seed rendered a blank, editable pane over a document that
 * exists in git; and when the committed copy DID render, it rendered into a
 * `TextField` whose keystrokes went nowhere ("edits aren't saved yet"). Both
 * offered to take work that nothing could commit.
 *
 * So: read-only, always. Markdown is rendered rather than shown as source,
 * because the user is reading a document, not a file. When the room is offline
 * the pane says so and says what it is showing instead — the banner names the
 * user's situation, never the service that failed (lexicon naming rule 6), and
 * names the recovery, because the provider really is reconnecting.
 */
export function CommittedFileView({
  path,
  content,
  errorMessage,
  onRetry,
  offline,
}: {
  path: string;
  /** The committed bytes, or null while they are loading / failed to load. */
  content: string | null;
  /** Set when the committed read failed — the pane has nothing to show. */
  errorMessage?: string | undefined;
  onRetry: () => void;
  /** The room is unreachable, as opposed to simply not holding this file. */
  offline: boolean;
}) {
  if (content !== null) {
    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 2, height: "100%" }}>
        {offline && (
          <Alert severity="warning">
            Live editing is unavailable. Showing the last committed version.
            Reconnecting…
          </Alert>
        )}
        {path.endsWith(".md") ? (
          <MarkdownView>{content}</MarkdownView>
        ) : (
          <TextField
            fullWidth
            multiline
            minRows={20}
            value={content}
            aria-label={`${fileLabel(path)}, read only`}
            slotProps={{
              input: {
                readOnly: true,
                sx: { fontFamily: "monospace", fontSize: "0.875rem" },
              },
            }}
          />
        )}
      </Box>
    );
  }

  // Nothing to show. Same shape as the pane's other whole-view states (the
  // empty workspace, the agent mid-turn) rather than a bar pinned above an
  // empty document — and it carries the message and the one Retry, the way a
  // failed repository clone does.
  if (errorMessage !== undefined) {
    return (
      <EmptyState
        title={`${fileLabel(path)} couldn't be loaded`}
        description={errorMessage}
        action={
          <Button variant="contained" onClick={onRetry}>
            Retry
          </Button>
        }
      />
    );
  }

  return (
    <Box
      sx={{
        height: "100%",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
      }}
    >
      <CircularProgress aria-label={`Loading ${fileLabel(path)}`} />
    </Box>
  );
}
