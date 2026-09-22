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

// PROTOTYPE (throwaway, issue #813). Oxygen renderer for the v1 registry,
// shared by all three shell variants. Annotate mode: every wrapper swallows
// the click and toggles selection; Preview mode: leaves act.

import { createContext, useContext, type ReactNode } from "react";
import {
  Alert,
  AlertTitle,
  Box,
  Breadcrumbs,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Drawer,
  FormControlLabel,
  Grid,
  Link,
  ListingTable,
  MenuItem,
  Sidebar,
  Stack,
  StatCard,
  Step,
  StepLabel,
  Stepper,
  Switch,
  Tab,
  Tabs,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { ClipboardList, FileText, Inbox, PlusCircle } from "@wso2/oxygen-ui-icons-react";
import { labelOf as labelFor, type ActionSpec, type ButtonSpec, type FieldSpec, type NodeSpec, type OverlaySpec, type PrototypeModelV1, type RowSpec, type ScreenSpec, type Tone } from "./model";
import type { ViewEvent, ViewState } from "./viewState";

interface Ctx {
  model: PrototypeModelV1;
  state: ViewState;
  dispatch: (e: ViewEvent) => void;
}

const RenderCtx = createContext<Ctx | null>(null);
const useCtx = () => useContext(RenderCtx)!;

export function RenderProvider({ value, children }: { value: Ctx; children: ReactNode }) {
  return <RenderCtx.Provider value={value}>{children}</RenderCtx.Provider>;
}

// ---------- Annotatable wrapper ----------

function Annotatable({ id, children, inline }: { id: string; children: ReactNode; inline?: boolean | undefined }) {
  const { state, dispatch, model } = useCtx();
  const annotate = state.mode === "annotate";
  const selected = state.selectedComponentIds.includes(id);
  const label = labelFor(model, state.screenId, id);
  const pin = state.queue.findIndex((a) => a.screenId === state.screenId && a.componentIds.includes(id));
  return (
    <Box
      data-prototype-component-id={id}
      role={annotate ? "button" : undefined}
      aria-pressed={annotate ? selected : undefined}
      onClick={
        annotate
          ? (e) => {
              e.stopPropagation();
              dispatch({ type: "TOGGLE_SELECTION", componentId: id });
            }
          : undefined
      }
      sx={{
        position: "relative",
        display: inline ? "inline-block" : "block",
        minWidth: 0,
        ...(annotate && {
          pointerEvents: "auto",
          cursor: "crosshair",
          outline: "2px solid",
          outlineOffset: 2,
          outlineColor: selected ? "primary.main" : "transparent",
          borderRadius: 1,
          "&:hover": { outlineColor: selected ? "primary.main" : "primary.light" },
        }),
      }}
    >
      {selected && (
        <Chip
          size="small"
          color="primary"
          label={label}
          sx={{ position: "absolute", top: -12, left: 4, zIndex: 2, pointerEvents: "none" }}
        />
      )}
      {pin >= 0 && annotate && !selected && (
        <Chip size="small" color="warning" label={pin + 1} sx={{ position: "absolute", top: -10, right: -6, zIndex: 2, pointerEvents: "none", minWidth: 24 }} />
      )}
      <Box sx={annotate ? { pointerEvents: "none" } : undefined}>{children}</Box>
    </Box>
  );
}


// ---------- Screen ----------

export function Screen({ screen }: { screen: ScreenSpec }) {
  return (
    <Stack spacing={3}>
      {screen.content.map((n) => (
        <Node key={n.id} node={n} />
      ))}
      {(screen.overlays ?? []).map((o) => (
        <Overlay key={o.id} overlay={o} />
      ))}
    </Stack>
  );
}

export function SideNav({ navigationId }: { navigationId: string }) {
  const { model, state, dispatch } = useCtx();
  const nav = model.navigation.find((n) => n.id === navigationId);
  if (!nav) return null;
  const items = nav.items.filter((i) => !i.roleIds || i.roleIds.includes(state.roleId));
  const activeId = items.find((i) => i.action.kind === "navigate" && i.action.screenId === state.screenId)?.id;
  const icons: Record<string, ReactNode> = {
    "nav.queue": <Inbox size={18} />,
    "nav.mine": <ClipboardList size={18} />,
    "nav.new": <PlusCircle size={18} />,
    "nav.reports": <FileText size={18} />,
  };
  return (
    <Sidebar activeItem={activeId ?? ""} onSelect={(id) => {
      const item = items.find((i) => i.id === id);
      if (item) dispatch({ type: "ACTIVATE", componentId: id, action: item.action });
    }}>
      <Sidebar.Nav>
        <Sidebar.Category>
          <Sidebar.CategoryLabel>{model.name}</Sidebar.CategoryLabel>
          {items.map((i) => (
            <Annotatable key={i.id} id={i.id}>
              <Sidebar.Item id={i.id}>
                <Sidebar.ItemIcon>{icons[i.id]}</Sidebar.ItemIcon>
                <Sidebar.ItemLabel>{i.label}</Sidebar.ItemLabel>
              </Sidebar.Item>
            </Annotatable>
          ))}
        </Sidebar.Category>
      </Sidebar.Nav>
    </Sidebar>
  );
}

// ---------- Nodes ----------

function visible(n: { showIn?: string[] }, stateId: string) {
  return !n.showIn || n.showIn.includes(stateId);
}

function Node({ node }: { node: NodeSpec }) {
  const { state } = useCtx();
  if (!visible(node, state.stateId)) return null;
  return (
    <Annotatable id={node.id}>
      <NodeBody node={node} />
    </Annotatable>
  );
}

function NodeBody({ node }: { node: NodeSpec }) {
  const { state, dispatch } = useCtx();
  switch (node.kind) {
    case "stack":
      return (
        <Stack direction={node.direction ?? "column"} spacing={2}>
          {node.content.map((n) => <Node key={n.id} node={n} />)}
        </Stack>
      );
    case "grid":
      return (
        <Grid container spacing={2}>
          {node.content.map((n) => (
            <Grid key={n.id} size={{ xs: 12, md: 12 / node.columns }}>
              <Node node={n} />
            </Grid>
          ))}
        </Grid>
      );
    case "split":
      return (
        <Grid container spacing={3}>
          <Grid size={{ xs: 12, md: node.ratio ?? 6 }}>
            <Stack spacing={3}>{node.left.map((n) => <Node key={n.id} node={n} />)}</Stack>
          </Grid>
          <Grid size={{ xs: 12, md: 12 - (node.ratio ?? 6) }}>
            <Stack spacing={3}>{node.right.map((n) => <Node key={n.id} node={n} />)}</Stack>
          </Grid>
        </Grid>
      );
    case "breadcrumbs":
      return (
        <Breadcrumbs>
          {node.items.map((i) =>
            i.action ? (
              <Annotatable key={i.id} id={i.id} inline>
                <Link component="button" underline="hover" onClick={() => dispatch({ type: "ACTIVATE", componentId: i.id, action: i.action! })}>
                  {i.label}
                </Link>
              </Annotatable>
            ) : (
              <Typography key={i.id} color="text.primary">{i.label}</Typography>
            ),
          )}
        </Breadcrumbs>
      );
    case "tabs": {
      const active = state.activeTabIds[node.id] ?? node.tabs[0]!.id;
      return (
        <Stack spacing={2}>
          <Tabs value={active} onChange={(_, v: string) => dispatch({ type: "ACTIVATE", componentId: v, action: { kind: "set-tab", tabsId: node.id, tabId: v } })}>
            {node.tabs.map((t) => <Tab key={t.id} value={t.id} label={t.label} />)}
          </Tabs>
          {node.tabs.find((t) => t.id === active)!.content.map((n) => <Node key={n.id} node={n} />)}
        </Stack>
      );
    }
    case "stepper": {
      const active = state.activeStepIds[node.id] ?? node.steps[0]!.id;
      const idx = node.steps.findIndex((s) => s.id === active);
      return (
        <Stack spacing={3}>
          <Stepper activeStep={idx}>
            {node.steps.map((s) => (
              <Step key={s.id}>
                <StepLabel>{s.label}</StepLabel>
              </Step>
            ))}
          </Stepper>
          {node.steps[idx]!.content.map((n) => <Node key={n.id} node={n} />)}
        </Stack>
      );
    }
    case "text":
      return <Typography variant="body2" color="text.secondary">{node.text}</Typography>;
    case "heading":
      return (
        <Stack direction="row" alignItems="center" justifyContent="space-between">
          <Typography variant="h5">{node.text}</Typography>
          {node.actions && <Stack direction="row" spacing={1}>{node.actions.map((b) => <Btn key={b.id} b={b} />)}</Stack>}
        </Stack>
      );
    case "badge":
      return <Chip size="small" label={node.label} color={chipColor(node.tone)} />;
    case "stat":
      return <StatCard label={node.label} value={node.value} />;
    case "alert":
      return (
        <Alert severity={node.tone === "default" ? "info" : node.tone}>
          {node.title && <AlertTitle>{node.title}</AlertTitle>}
          {node.text}
        </Alert>
      );
    case "empty-state":
      return (
        <Card variant="outlined">
          <CardContent>
            <Stack alignItems="center" spacing={1} sx={{ py: 4 }}>
              <Typography variant="h6">{node.title}</Typography>
              <Typography variant="body2" color="text.secondary">{node.text}</Typography>
              {node.action && <Btn b={node.action} />}
            </Stack>
          </CardContent>
        </Card>
      );
    case "button":
      return <Btn b={node} bare />;
    case "link":
      return (
        <Link component="button" onClick={() => dispatch({ type: "ACTIVATE", componentId: node.id, action: node.action })}>
          {node.label}
        </Link>
      );
    case "form":
      return (
        <Card variant="outlined">
          {node.title && <CardHeader title={node.title} />}
          <CardContent>
            <Stack spacing={2}>
              {node.fields.map((f) => <Field key={f.id} f={f} />)}
              {node.actions.length > 0 && (
                <Stack direction="row" spacing={1} justifyContent="flex-end">
                  {node.actions.map((b) => <Btn key={b.id} b={b} />)}
                </Stack>
              )}
            </Stack>
          </CardContent>
        </Card>
      );
    case "validation-summary":
      return (
        <Alert severity="error">
          <AlertTitle>Fix {node.issues.length} problems before submitting</AlertTitle>
          <Stack component="ul" sx={{ m: 0, pl: 2 }}>
            {node.issues.map((i) => <Typography key={i} component="li" variant="body2">{i}</Typography>)}
          </Stack>
        </Alert>
      );
    case "filters":
      return (
        <Stack direction="row" spacing={2} alignItems="center" flexWrap="wrap">
          {node.fields.map((f) => <Field key={f.id} f={f} compact />)}
        </Stack>
      );
    case "table":
    case "task-queue":
      return <Table title={node.title} columns={node.columns} rows={node.rows} onRow={node.onRow} />;
    case "detail":
      return (
        <Card variant="outlined">
          {node.title && <CardHeader title={node.title} />}
          <CardContent>
            <Grid container spacing={2}>
              {node.fields.map((f) => (
                <Grid key={f.label} size={{ xs: 12, sm: 6 }}>
                  <Typography variant="overline" color="text.secondary">{f.label}</Typography>
                  <Typography variant="body1">{f.value}</Typography>
                </Grid>
              ))}
            </Grid>
          </CardContent>
        </Card>
      );
    case "timeline":
      return (
        <Card variant="outlined">
          <CardHeader title="Activity" />
          <CardContent>
            <Stack spacing={2}>
              {node.entries.map((e) => (
                <Stack key={e.id} direction="row" spacing={2}>
                  <Typography variant="caption" color="text.secondary" sx={{ minWidth: 110 }}>{e.when}</Typography>
                  <Stack>
                    <Typography variant="body2">{e.text}</Typography>
                    <Typography variant="caption" color="text.secondary">{e.who}</Typography>
                  </Stack>
                </Stack>
              ))}
            </Stack>
          </CardContent>
        </Card>
      );
    case "approval-panel":
      return (
        <Card>
          <CardHeader title={node.title} />
          <CardContent>
            <Stack spacing={2}>
              <Typography variant="body2" color="text.secondary">{node.summary}</Typography>
              <Stack spacing={1}>{node.actions.map((b) => <Btn key={b.id} b={b} full />)}</Stack>
            </Stack>
          </CardContent>
        </Card>
      );
  }
}

function chipColor(t: Tone | undefined) {
  return t === "default" || !t ? "default" : t;
}

function Btn({ b, bare, full }: { b: ButtonSpec; bare?: boolean; full?: boolean }) {
  const { dispatch } = useCtx();
  const button = (
    <Button
      variant={b.primary ? "contained" : "outlined"}
      color={b.danger ? "error" : "primary"}
      fullWidth={!!full}
      onClick={() => dispatch({ type: "ACTIVATE", componentId: b.id, action: b.action })}
    >
      {b.label}
    </Button>
  );
  // A `button` node is already wrapped by Node; nested buttons wrap themselves.
  return bare ? button : <Annotatable id={b.id} inline={!full}>{button}</Annotatable>;
}

function Field({ f, compact }: { f: FieldSpec; compact?: boolean }) {
  const { state } = useCtx();
  const showError = !!f.error && (!f.errorIn || f.errorIn.includes(state.stateId));
  let input: ReactNode;
  if (f.type === "switch") {
    input = <FormControlLabel control={<Switch defaultChecked={false} />} label={f.label} />;
  } else if (f.type === "select") {
    input = (
      <TextField select size={compact ? "small" : "medium"} label={f.label} defaultValue={f.value ?? ""} sx={{ minWidth: 180 }}>
        {(f.options ?? []).map((o) => <MenuItem key={o} value={o}>{o}</MenuItem>)}
      </TextField>
    );
  } else {
    input = (
      <TextField
        size={compact ? "small" : "medium"}
        label={f.label}
        type={f.type === "date" ? "date" : "text"}
        multiline={f.type === "textarea"}
        {...(f.type === "textarea" ? { minRows: 3 } : {})}
        defaultValue={f.value ?? ""}
        error={showError}
        {...(showError ? { helperText: f.error } : {})}
        slotProps={{ input: { readOnly: true }, ...(f.type === "date" ? { inputLabel: { shrink: true } } : {}) }}
        fullWidth={!compact}
      />
    );
  }
  return <Annotatable id={f.id} inline={compact}>{input}</Annotatable>;
}

function Table({ title, columns, rows, onRow }: { title?: string | undefined; columns: string[]; rows: RowSpec[]; onRow?: ActionSpec | undefined }) {
  const { state, dispatch } = useCtx();
  const annotate = state.mode === "annotate";
  return (
    <ListingTable.Container>
      {title && <Typography variant="subtitle1" sx={{ px: 2, pt: 1.5 }}>{title}</Typography>}
      <ListingTable>
        <ListingTable.Head>
          <ListingTable.Row>
            {columns.map((c) => <ListingTable.Cell key={c}>{c}</ListingTable.Cell>)}
          </ListingTable.Row>
        </ListingTable.Head>
        <ListingTable.Body>
          {rows.map((r) => {
            const selected = state.selectedComponentIds.includes(r.id);
            return (
              <ListingTable.Row
                key={r.id}
                clickable={annotate || !!onRow}
                selected={selected}
                data-prototype-component-id={r.id}
                onClick={(e) => { e.stopPropagation(); dispatch({ type: "ACTIVATE", componentId: r.id, action: onRow ?? { kind: "select-row", tableId: "", rowId: r.id } }); }}
                {...(annotate ? { sx: { pointerEvents: "auto", cursor: "crosshair", ...(selected ? { outline: "2px solid", outlineColor: "primary.main" } : {}) } } : {})}
              >
                {columns.map((c, i) => (
                  <ListingTable.Cell key={c}>
                    {i === columns.length - 1 && r.tone ? <Chip size="small" label={r.values[c]} color={chipColor(r.tone)} /> : r.values[c]}
                  </ListingTable.Cell>
                ))}
              </ListingTable.Row>
            );
          })}
        </ListingTable.Body>
      </ListingTable>
    </ListingTable.Container>
  );
}

// ---------- Overlays ----------

function Overlay({ overlay }: { overlay: OverlaySpec }) {
  const { state, dispatch } = useCtx();
  const open = state.openOverlayId === overlay.id;
  const close = () => dispatch({ type: "CLOSE_OVERLAY" });
  if (overlay.kind === "dialog") {
    return (
      <Dialog open={open} onClose={close} fullWidth maxWidth="sm">
        <DialogTitle>{overlay.title}</DialogTitle>
        <DialogContent>
          <Annotatable id={overlay.id}>
            <Stack spacing={2} sx={{ pt: 1 }}>{overlay.content.map((n) => <Node key={n.id} node={n} />)}</Stack>
          </Annotatable>
        </DialogContent>
        <DialogActions>{overlay.actions.map((b) => <Btn key={b.id} b={b} />)}</DialogActions>
      </Dialog>
    );
  }
  return (
    <Drawer anchor="right" open={open} onClose={close}>
      <Box sx={{ width: 380, p: 3 }}>
        <Typography variant="h6" sx={{ mb: 2 }}>{overlay.title}</Typography>
        <Annotatable id={overlay.id}>
          <Stack spacing={2}>{overlay.content.map((n) => <Node key={n.id} node={n} />)}</Stack>
        </Annotatable>
      </Box>
    </Drawer>
  );
}
