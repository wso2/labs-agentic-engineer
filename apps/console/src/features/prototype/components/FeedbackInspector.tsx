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

/**
 * The Annotate inspector (review shell variant D, #817): the composer — what
 * the request is about (the selection, or the whole screen), the request text,
 * Add request — above the queued requests and the one Send all.
 *
 * Presentation only. The shell owns the queue's contents and what adding,
 * removing and sending mean; this renders them and reports the gestures.
 */

import { useState, type Dispatch } from "react";
import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  Chip,
  CircularProgress,
  IconButton,
  Paper,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { Send, X } from "@wso2/oxygen-ui-icons-react";
import type { PrototypeModelV1 } from "@aep/prototype-model";
import { MAX_QUEUED_REQUESTS, type PrototypeAnnotation } from "../model/annotations";
import { labelOf } from "../model/labels";
import type { PrototypeViewEvent, PrototypeViewState } from "../model/viewState";

export const INSPECTOR_WIDTH = 380;

export interface FeedbackInspectorProps {
  model: PrototypeModelV1;
  view: PrototypeViewState;
  dispatch: Dispatch<PrototypeViewEvent>;
  annotations: readonly PrototypeAnnotation[];
  /** Queue a request about the current selection (or the whole screen). */
  onAdd: (request: string) => void;
  onRemove: (id: string) => void;
  onSendAll: () => void;
  /** This batch is on its way. */
  sending: boolean;
  /** An agent turn is running on the project: nothing may be queued or sent. */
  locked: boolean;
  error: string | null;
}

export function FeedbackInspector(props: FeedbackInspectorProps) {
  const { model, view, annotations, sending, locked, error } = props;
  const screen = model.screens.find((s) => s.id === view.screenId);
  return (
    <Paper
      square
      elevation={0}
      component="aside"
      aria-label="Feedback"
      sx={{ width: INSPECTOR_WIDTH, flexShrink: 0, borderLeft: 1, borderColor: "divider", display: "flex", flexDirection: "column", minHeight: 0 }}
    >
      <Stack direction="row" alignItems="center" spacing={1} sx={{ px: 2, py: 1.5, borderBottom: 1, borderColor: "divider" }}>
        <Typography variant="subtitle2" component="h2" sx={{ flex: 1 }}>
          Feedback
        </Typography>
        <Button
          variant="contained"
          size="small"
          startIcon={sending ? <CircularProgress size={14} color="inherit" /> : <Send size={14} />}
          disabled={annotations.length === 0 || locked}
          onClick={props.onSendAll}
        >
          {sending ? "Sending…" : annotations.length === 0 ? "Send all" : `Send all (${annotations.length})`}
        </Button>
      </Stack>
      {sending ? (
        <Alert severity="info" sx={{ borderRadius: 0 }}>
          The agent is revising this prototype. It refreshes here when the agent finishes.
        </Alert>
      ) : locked ? (
        <Alert severity="info" sx={{ borderRadius: 0 }}>
          An agent is working on this project. You can queue and send requests once it finishes.
        </Alert>
      ) : null}
      {error && !sending && (
        <Alert severity="error" sx={{ borderRadius: 0 }}>
          {error}
        </Alert>
      )}
      <Box sx={{ p: 2, borderBottom: 1, borderColor: "divider" }}>
        <Stack spacing={1}>
          <Typography variant="caption" color="text.secondary">
            On {screen?.name ?? view.screenId}
          </Typography>
          <Composer {...props} full={annotations.length >= MAX_QUEUED_REQUESTS} />
        </Stack>
      </Box>
      <Box sx={{ flex: 1, overflow: "auto", p: 2 }}>
        {annotations.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            Click anything in the app to select it, then describe the change. With nothing selected, the request is about
            the whole screen.
          </Typography>
        ) : (
          <Stack spacing={1.5} component="ol" aria-label="Queued requests" sx={{ m: 0, p: 0, listStyle: "none" }}>
            {annotations.map((a, i) => (
              <QueuedRequest key={a.id} number={i + 1} annotation={a} model={model} onRemove={props.onRemove} />
            ))}
          </Stack>
        )}
      </Box>
    </Paper>
  );
}

function Composer({ model, view, dispatch, onAdd, locked, full }: FeedbackInspectorProps & { full: boolean }) {
  const [text, setText] = useState("");
  const selected = view.selectedComponentIds;
  const canAdd = text.trim() !== "" && !locked && !full;
  const add = () => {
    if (!canAdd) return;
    onAdd(text.trim());
    setText("");
  };
  return (
    <Stack spacing={1.5}>
      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap aria-label="Request is about">
        {selected.length === 0 ? (
          <Chip size="small" variant="outlined" label="Whole screen" />
        ) : (
          selected.map((id) => (
            <Chip
              key={id}
              size="small"
              color="primary"
              label={labelOf(model, view.screenId, id)}
              onDelete={() => dispatch({ type: "TOGGLE_SELECTION", componentId: id })}
            />
          ))
        )}
      </Stack>
      <TextField
        multiline
        minRows={2}
        size="small"
        label="Request"
        slotProps={{ inputLabel: { shrink: true } }}
        placeholder={selected.length > 0 ? "What should change here?" : "What should change on this screen?"}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) add();
        }}
      />
      <Stack direction="row" justifyContent="flex-end" spacing={1}>
        {selected.length > 0 && (
          <Button size="small" onClick={() => dispatch({ type: "CLEAR_SELECTION" })}>
            Clear selection
          </Button>
        )}
        <Button size="small" variant="outlined" disabled={!canAdd} onClick={add}>
          Add request
        </Button>
      </Stack>
      {full && (
        <Typography variant="caption" color="text.secondary">
          A batch holds at most {MAX_QUEUED_REQUESTS} requests — send these first.
        </Typography>
      )}
    </Stack>
  );
}

function QueuedRequest({
  number,
  annotation,
  model,
  onRemove,
}: {
  number: number;
  annotation: PrototypeAnnotation;
  model: PrototypeModelV1;
  onRemove: (id: string) => void;
}) {
  const screen = model.screens.find((s) => s.id === annotation.screenId)?.name ?? annotation.screenId;
  const state = model.states.find((s) => s.id === annotation.stateId)?.name ?? annotation.stateId;
  return (
    <Card variant="outlined" component="li" aria-label={`Request ${number}`}>
      <CardContent sx={{ py: 1.5, "&:last-child": { pb: 1.5 } }}>
        <Stack direction="row" alignItems="flex-start" spacing={1}>
          <Chip size="small" color="warning" label={number} sx={{ minWidth: 24 }} />
          <Stack spacing={0.75} sx={{ flex: 1, minWidth: 0 }}>
            <Stack direction="row" spacing={0.5} alignItems="center" flexWrap="wrap" useFlexGap>
              <Typography variant="caption" fontWeight="fontWeightMedium">
                {screen}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                · {state}
              </Typography>
            </Stack>
            <Stack direction="row" spacing={0.5} flexWrap="wrap" useFlexGap>
              {annotation.componentIds.length === 0 ? (
                <Chip size="small" variant="outlined" label="Whole screen" />
              ) : (
                annotation.componentIds.map((id) => (
                  <Chip key={id} size="small" label={labelOf(model, annotation.screenId, id)} />
                ))
              )}
            </Stack>
            <Typography variant="body2" sx={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
              {annotation.request}
            </Typography>
          </Stack>
          <Tooltip title="Remove request">
            <IconButton size="small" aria-label={`Remove request ${number}`} onClick={() => onRemove(annotation.id)}>
              <X size={14} />
            </IconButton>
          </Tooltip>
        </Stack>
      </CardContent>
    </Card>
  );
}
