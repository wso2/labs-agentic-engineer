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

// "What is wrong here, and what do I do about it" — one presentation, wherever
// it is asked (#575 retest).
//
// Two surfaces ask it: the rail's alert button on an amber section, and Build
// refusing to run. They are the same KIND of thing — a list of unmet conditions
// the user can act on — so they read the same way rather than one being a strip
// under the header and the other a set of rows in a sidebar.
//
// It is a dialog rather than an inline strip because these lists run long: a
// build refusal can name several components each missing something, which a
// 280px column cannot hold and a header strip pushes the workspace down.

import {
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  List,
  ListItem,
  ListItemIcon,
  ListItemText,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { TriangleAlert } from "@wso2/oxygen-ui-icons-react";

/** One thing that is wrong, and optionally the way to fix it. */
export interface Problem {
  key: string;
  /** What is wrong, in the user's terms. */
  label: string;
  /** Where the fix is. Absent for a problem the user resolves by editing. */
  fix?: { label: string; run: () => void } | undefined;
}

export function ProblemsDialog({
  open,
  title,
  intro,
  problems,
  proceed,
  resolve,
  onClose,
}: {
  open: boolean;
  title: string;
  /** One line of context above the list, when the title alone is not enough. */
  intro?: string | undefined;
  problems: Problem[];
  /**
   * The way past, for a dialog that WARNS rather than refuses.
   *
   * Absent means the platform said no and the list is what the user must deal
   * with first. Present means the user may go on knowing what stands — which
   * is the difference between the build gate, which enforces, and unsettled
   * requirements, which are deliberately not a gate: the document arrives full
   * of the agent's own judgments and is refined in place, so a design run
   * against some of them is ordinary use, not a mistake.
   */
  proceed?: { label: string; run: () => void } | undefined;
  /**
   * What declining means, when it is more than dismissing.
   *
   * Kept off `onClose` deliberately: Escape and the backdrop close this too,
   * and dismissing a dialog should dismiss it, not send the user somewhere
   * they did not ask to go.
   */
  resolve?: { label: string; run: () => void } | undefined;
  onClose: () => void;
}) {
  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>{title}</DialogTitle>
      <DialogContent dividers>
        {intro && (
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
            {intro}
          </Typography>
        )}
        <List dense disablePadding>
          {problems.map((problem) => (
            <ListItem
              key={problem.key}
              disableGutters
              secondaryAction={
                problem.fix ? (
                  // Acting CLOSES the dialog and goes — so it never becomes a
                  // thing the user dismisses twice, once to read and once to
                  // get past.
                  <Button
                    size="small"
                    onClick={() => {
                      onClose();
                      problem.fix?.run();
                    }}
                  >
                    {problem.fix.label}
                  </Button>
                ) : undefined
              }
            >
              {/* Themed through currentColor: this app's theme defines none of
                  MUI's palette CSS variables, so an icon `color` naming one
                  resolves to nothing and renders unstyled. */}
              <ListItemIcon sx={{ minWidth: 32, color: "warning.main" }}>
                <TriangleAlert size={16} />
              </ListItemIcon>
              <ListItemText
                primary={problem.label}
                slotProps={{ primary: { variant: "body2" } }}
              />
            </ListItem>
          ))}
          {problems.length === 0 && (
            <Stack sx={{ py: 1 }}>
              <Typography variant="body2" color="text.secondary">
                Nothing to resolve.
              </Typography>
            </Stack>
          )}
        </List>
      </DialogContent>
      <DialogActions>
        {/* A warning keeps the way past on the right, where a confirm lives;
            a refusal has none, and Close is the only thing to do. */}
        <Button
          onClick={() => {
            onClose();
            resolve?.run();
          }}
        >
          {resolve?.label ?? "Close"}
        </Button>
        {proceed && (
          <Button
            variant="contained"
            onClick={() => {
              onClose();
              proceed.run();
            }}
          >
            {proceed.label}
          </Button>
        )}
      </DialogActions>
    </Dialog>
  );
}
