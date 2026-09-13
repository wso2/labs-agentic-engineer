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

import { useMemo, useRef, useState } from "react";
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Alert,
  Box,
  Chip,
  Collapse,
  IconButton,
  InputAdornment,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tab,
  Tabs,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { ChevronDown, ChevronRight, Globe, Lock, Search, X } from "@wso2/oxygen-ui-icons-react";
import {
  parseOpenApi,
  type Method,
  type ParsedOpenApi,
  type Operation,
  type Param,
  type Protection,
  type Response,
  type Schema,
  type SchemaField,
  type TagSection,
} from "./parse.js";

type ChipColor =
  | "default"
  | "primary"
  | "secondary"
  | "error"
  | "info"
  | "success"
  | "warning";

// Solid background color for each method's badge — the exact Swagger UI /
// petstore.swagger.io hues, so the viewer reads as a standard OpenAPI doc
// (GET blue, POST green, ...). Not theme-driven: these are the canonical
// method colors and are identical in light and dark. The badge below computes
// its text color with getContrastText, so labels stay readable in both themes
// (an improvement over Swagger's own low-contrast white-on-teal PATCH).
const METHOD_COLOR: Record<Method, string> = {
  GET: "#61affe",
  POST: "#49cc90",
  PUT: "#fca130",
  PATCH: "#50e3c2",
  DELETE: "#f93e3e",
  HEAD: "#9012fe",
  OPTIONS: "#0d5aa7",
};

// Response status class → color: 2xx ok, 4xx client, 5xx server, else neutral.
function statusColor(code: string): ChipColor {
  const n = Number(code);
  if (n >= 200 && n < 300) return "success";
  if (n >= 400 && n < 500) return "warning";
  if (n >= 500) return "error";
  return "info";
}

// Shared monospace styling for paths / field names.
const mono = { fontFamily: "monospace", fontSize: "0.8125rem" } as const;

// Tiny uppercase red "required" marker (matches the prototype's .param-req) —
// deliberately not a Chip, so it reads as a subtle annotation next to the name.
function RequiredTag() {
  return (
    <Typography
      component="span"
      sx={{
        fontSize: "0.6rem",
        fontWeight: 600,
        letterSpacing: "0.08em",
        textTransform: "uppercase",
        color: "error.main",
        lineHeight: 1,
      }}
    >
      required
    </Typography>
  );
}

// Small subtle monospace pill for a type token (matches the prototype's
// .sch-type) — quieter than a full Chip so it doesn't dominate the row.
function TypeTag({ label }: { label: string }) {
  return (
    <Box
      component="span"
      sx={{
        display: "inline-flex",
        alignItems: "center",
        px: 0.75,
        py: "1px",
        borderRadius: 999,
        border: 1,
        borderColor: "divider",
        bgcolor: "action.hover",
        color: "text.secondary",
        fontFamily: "monospace",
        fontSize: "0.6875rem",
        lineHeight: 1.4,
        whiteSpace: "nowrap",
      }}
    >
      {label}
    </Box>
  );
}

// Who may reach the operation, in words: "Employee, Approver · own rows" for a
// scope, and fixed copy for the other two states — they are constants of the
// model, not data a caller could vary. Empty when the caller passed no map, so
// a view without one renders exactly as it did before.
function grantLine(protection: Protection, roles: ScopeRoles | undefined): string {
  if (!roles) return "";
  if (protection.kind === "public") return "anyone · no token needed";
  if (protection.kind === "signedIn") return "everyone";
  const grant = roles[protection.scope];
  if (!grant) return "";
  const names = Array.isArray(grant) ? grant : grant.roles;
  const note = Array.isArray(grant) ? undefined : grant.note;
  if (!names?.length) return "";
  return note ? `${names.join(", ")} · ${note}` : names.join(", ");
}

// Protection pill — "public", "signed in", or the scope handle the gateway
// enforces. Same quiet pill as TypeTag so it annotates the row rather than
// competing with the method badge; the scope handle is the only one that gets
// full-strength text, because it is the one the reader is looking for.
function ProtectionTag({ protection }: { protection: Protection }) {
  const isScope = protection.kind === "scope";
  const label =
    protection.kind === "public"
      ? "public"
      : protection.kind === "signedIn"
        ? "signed in"
        : protection.scope;
  const Icon = protection.kind === "public" ? Globe : Lock;
  return (
    <Box
      component="span"
      sx={{
        display: "inline-flex",
        alignItems: "center",
        gap: 0.5,
        px: 0.75,
        py: "1px",
        borderRadius: 999,
        border: 1,
        borderColor: "divider",
        bgcolor: "action.hover",
        color: isScope ? "text.primary" : "text.secondary",
        fontFamily: isScope ? "monospace" : "inherit",
        fontSize: "0.6875rem",
        lineHeight: 1.4,
        whiteSpace: "nowrap",
        flexShrink: 0,
      }}
    >
      <Icon size={11} />
      {label}
    </Box>
  );
}

// Solid method badge with guaranteed-contrast text (getContrastText), sized
// like the prototype's .op-method — always readable regardless of theme.
function MethodBadge({ method }: { method: Method }) {
  const bg = METHOD_COLOR[method];
  return (
    <Box
      component="span"
      sx={(theme) => ({
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        minWidth: 58,
        px: 1,
        py: 0.5,
        borderRadius: 1,
        flexShrink: 0,
        fontFamily: "monospace",
        fontSize: "0.6875rem",
        fontWeight: 700,
        letterSpacing: "0.06em",
        textTransform: "uppercase",
        bgcolor: bg,
        color: theme.palette.getContrastText(bg),
      })}
    >
      {method}
    </Box>
  );
}

// ── Example JSON block ───────────────────────────────────────────────────────
function JsonBlock({ data }: { data: unknown }) {
  return (
    <Box
      component="pre"
      sx={{
        m: 0,
        p: 1.5,
        borderRadius: 1,
        bgcolor: "action.hover",
        color: "text.primary",
        fontFamily: "monospace",
        fontSize: "0.8125rem",
        overflow: "auto",
        whiteSpace: "pre",
      }}
    >
      {JSON.stringify(data, null, 2)}
    </Box>
  );
}

// ── Schema tree (recursive) ──────────────────────────────────────────────────
function SchemaFieldRow({ field, depth }: { field: SchemaField; depth: number }) {
  const hasChildren = !!field.children?.length;
  const [open, setOpen] = useState(depth < 1);
  return (
    <Box sx={{ pl: depth * 2 }}>
      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.5, py: 0.5 }}>
        <Box
          sx={{
            width: 24,
            flexShrink: 0,
            display: "flex",
            justifyContent: "center",
            pt: 0.25,
          }}
        >
          {hasChildren ? (
            <IconButton
              size="small"
              onClick={() => setOpen(!open)}
              aria-label={open ? "Collapse" : "Expand"}
              sx={{ p: 0 }}
            >
              {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            </IconButton>
          ) : (
            <Box
              sx={{
                width: 4,
                height: 4,
                borderRadius: "50%",
                bgcolor: "text.disabled",
                mt: 0.75,
              }}
            />
          )}
        </Box>
        <Box sx={{ minWidth: 0, flex: 1 }}>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
            <Typography component="code" sx={mono}>
              {field.name}
            </Typography>
            <TypeTag label={field.type} />
            {field.required && <RequiredTag />}
          </Box>
          {field.desc && (
            <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
              {field.desc}
            </Typography>
          )}
          {field.enumValues && field.enumValues.length > 0 && (
            <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5, mt: 0.5 }}>
              {field.enumValues.map((v) => (
                <TypeTag key={v} label={v} />
              ))}
            </Box>
          )}
        </Box>
      </Box>
      {hasChildren && (
        <Collapse in={open} unmountOnExit>
          {field.children!.map((c) => (
            <SchemaFieldRow key={c.name} field={c} depth={depth + 1} />
          ))}
        </Collapse>
      )}
    </Box>
  );
}

function SchemaTree({ schema }: { schema: Schema }) {
  if (schema.fields.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ p: 1.5 }}>
        No fields declared.
      </Typography>
    );
  }
  return (
    <Box>
      {schema.fields.map((f) => (
        <SchemaFieldRow key={f.name} field={f} depth={0} />
      ))}
    </Box>
  );
}

// ── Parameters table ─────────────────────────────────────────────────────────
function ParamsTable({ params }: { params: Param[] }) {
  if (!params.length) {
    return (
      <Typography variant="body2" color="text.secondary">
        No parameters.
      </Typography>
    );
  }
  return (
    <Table size="small">
      <TableHead>
        <TableRow>
          <TableCell>Name</TableCell>
          <TableCell>Type</TableCell>
          <TableCell>Description</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {params.map((p, i) => (
          <TableRow key={`${p.in}-${p.name}-${i}`}>
            <TableCell>
              <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
                <Typography component="code" sx={mono}>
                  {p.name}
                </Typography>
                {p.required && <RequiredTag />}
              </Box>
            </TableCell>
            <TableCell>
              <TypeTag label={p.type} />
            </TableCell>
            <TableCell>
              <Typography variant="body2" color="text.secondary">
                {p.desc}
              </Typography>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

// ── Response row ─────────────────────────────────────────────────────────────
function ResponseRow({ code, description, schema, schemaName, example }: Response) {
  const [tab, setTab] = useState<"schema" | "example">(schema ? "schema" : "example");
  const hasContent = !!(schema || example !== undefined);
  return (
    <Accordion disableGutters elevation={0} sx={{ "&:before": { display: "none" } }}>
      <AccordionSummary expandIcon={<ChevronDown size={16} />}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1, minWidth: 0, flex: 1 }}>
          <Chip label={code} size="small" color={statusColor(code)} />
          <Typography variant="body2" sx={{ flex: 1, minWidth: 0 }} noWrap>
            {description || (schemaName ? `Returns ${schemaName}` : "")}
          </Typography>
          {schemaName && (
            <Typography component="code" sx={mono} color="text.secondary">
              {schemaName}
            </Typography>
          )}
        </Box>
      </AccordionSummary>
      {hasContent && (
        <AccordionDetails>
          <Tabs
            value={tab}
            onChange={(_, v) => setTab(v as "schema" | "example")}
            sx={{ mb: 1 }}
          >
            {schema && <Tab label="Schema" value="schema" />}
            {example !== undefined && <Tab label="Example" value="example" />}
          </Tabs>
          {tab === "schema" && schema && <SchemaTree schema={schema} />}
          {tab === "example" && example !== undefined && <JsonBlock data={example} />}
        </AccordionDetails>
      )}
    </Accordion>
  );
}

// ── Operation row ────────────────────────────────────────────────────────────
function OperationRow({ op, roles }: { op: Operation; roles: ScopeRoles | undefined }) {
  const grants = grantLine(op.protection, roles);
  return (
    <Accordion disableGutters sx={{ "&:before": { display: "none" } }}>
      <AccordionSummary expandIcon={<ChevronDown size={18} />}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1.5, minWidth: 0, flex: 1 }}>
          <MethodBadge method={op.method} />
          <Typography component="code" sx={mono}>
            {op.path}
          </Typography>
          <Typography
            variant="body2"
            color="text.secondary"
            sx={{ flex: 1, minWidth: 0 }}
            noWrap
          >
            {op.name}
          </Typography>
          <ProtectionTag protection={op.protection} />
          {grants && (
            // `title` carries the full line, since the row truncates it.
            <Typography
              variant="caption"
              color="text.secondary"
              title={grants}
              sx={{ maxWidth: 260, flexShrink: 0 }}
              noWrap
            >
              {grants}
            </Typography>
          )}
        </Box>
      </AccordionSummary>
      <AccordionDetails>
        {op.summary && (
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {op.summary}
          </Typography>
        )}
        <Typography variant="subtitle2" sx={{ mb: 1 }}>
          Parameters
        </Typography>
        <Box sx={{ mb: 2 }}>
          <ParamsTable params={op.params} />
        </Box>
        <Typography variant="subtitle2" sx={{ mb: 1 }}>
          Responses
        </Typography>
        {op.responses.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            No responses declared.
          </Typography>
        ) : (
          <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1 }}>
            {op.responses.map((r) => (
              <ResponseRow key={r.code} {...r} />
            ))}
          </Box>
        )}
      </AccordionDetails>
    </Accordion>
  );
}

// ── Tag section ──────────────────────────────────────────────────────────────
function TagSectionView({ section, roles }: { section: TagSection; roles: ScopeRoles | undefined }) {
  return (
    <Box component="section" sx={{ mb: 4 }}>
      <Typography variant="h6" sx={{ fontWeight: 700 }}>
        {section.title}
      </Typography>
      {section.blurb && (
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
          {section.blurb}
        </Typography>
      )}
      <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, overflow: "hidden" }}>
        {section.endpoints.map((ep) => (
          <OperationRow key={ep.id} op={ep} roles={roles} />
        ))}
      </Box>
    </Box>
  );
}

// ── Schemas (models) section ─────────────────────────────────────────────────
function SchemasSection({ schemas }: { schemas: Record<string, Schema> }) {
  const names = Object.keys(schemas);
  if (names.length === 0) return null;
  return (
    <Box component="section" sx={{ mb: 4 }}>
      <Typography variant="h6" sx={{ fontWeight: 700 }}>
        Schemas
      </Typography>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
        Reusable object definitions referenced throughout the API.
      </Typography>
      <Box sx={{ border: 1, borderColor: "divider", borderRadius: 1, overflow: "hidden" }}>
        {names.map((name) => {
          const schema = schemas[name]!;
          return (
            <Accordion key={name} disableGutters sx={{ "&:before": { display: "none" } }}>
              <AccordionSummary expandIcon={<ChevronDown size={18} />}>
                <Box
                  sx={{ display: "flex", alignItems: "center", gap: 1.5, minWidth: 0, flex: 1 }}
                >
                  <Typography component="code" sx={mono}>
                    {name}
                  </Typography>
                  <TypeTag label={schema.type} />
                  <Typography variant="caption" color="text.secondary" sx={{ ml: "auto" }}>
                    {schema.fields.length} field{schema.fields.length === 1 ? "" : "s"}
                  </Typography>
                </Box>
              </AccordionSummary>
              <AccordionDetails>
                <SchemaTree schema={schema} />
              </AccordionDetails>
            </Accordion>
          );
        })}
      </Box>
    </Box>
  );
}

// ── Public component ─────────────────────────────────────────────────────────
/**
 * Who may reach an operation, resolved by the caller from the permission
 * catalog (`specs/design/security.json`): scope handle → the roles that grant
 * it. The view resolves nothing itself — it holds the contract, not the
 * catalog — so an unmapped handle simply shows no roles.
 *
 * A plain array is the common case (`{ "claims:read": ["Employee", "Approver"] }`).
 * The object form adds the qualifier the Security page shows beside a grant,
 * e.g. `{ roles: ["Employee", "Approver"], note: "own rows" }` renders as
 * "Employee, Approver · own rows".
 */
export interface ScopeGrant {
  roles: string[];
  note?: string;
}

export type ScopeRoles = Record<string, string[] | ScopeGrant>;

export interface OpenApiViewProps {
  /** Raw OpenAPI YAML or JSON text. */
  spec: string;
  /**
   * Optional: scope handle → the roles that grant it. When given, every row
   * names who may reach it — the granting roles beside a scope handle, and the
   * fixed copy for the public and signed-in states. Absent, the view renders
   * exactly as it did before, protection pill included.
   */
  roles?: ScopeRoles;
}

export function OpenApiView({ spec, roles }: OpenApiViewProps) {
  // A STREAMED spec grows line by line; YAML's line-boundary prefixes almost
  // always parse, but the odd one doesn't (e.g. cut inside a quoted scalar).
  // Hold the last GOOD parse so a bad intermediate never flashes the error
  // alert over already-rendered endpoints — the next flush repairs it.
  const attempt = useMemo(() => parseOpenApi(spec), [spec]);
  const lastGood = useRef<ParsedOpenApi | null>(null);
  if (!("kind" in attempt)) lastGood.current = attempt;
  const parsed = "kind" in attempt ? (lastGood.current ?? attempt) : attempt;
  const [query, setQuery] = useState("");

  if ("kind" in parsed) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">
          Couldn't parse the OpenAPI document: {parsed.message}
        </Alert>
      </Box>
    );
  }

  // Live filter — keep an operation if its path / method / summary or the
  // section title matches the query.
  const q = query.trim().toLowerCase();
  const filtered = !q
    ? parsed.sections
    : parsed.sections
        .map((s) => {
          if (s.title.toLowerCase().includes(q)) return s;
          const eps = s.endpoints.filter(
            (ep) =>
              ep.path.toLowerCase().includes(q) ||
              ep.name.toLowerCase().includes(q) ||
              ep.method.toLowerCase().includes(q),
          );
          return eps.length ? { ...s, endpoints: eps } : null;
        })
        .filter((s): s is TagSection => !!s);

  return (
    <Box sx={{ height: "100%", overflow: "auto", p: 3 }}>
      <Box sx={{ maxWidth: 960, mx: "auto" }}>
        {/* Hero — eyebrow row (format badge + version) above a bold title,
            mirroring the design.json overview header. */}
        <Box sx={{ mb: 3 }}>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 1 }}>
            <Box
              component="span"
              sx={(theme) => ({
                display: "inline-flex",
                alignItems: "center",
                px: 1,
                py: 0.5,
                borderRadius: 1,
                fontFamily: "monospace",
                fontSize: "0.6875rem",
                fontWeight: 700,
                letterSpacing: "0.06em",
                textTransform: "uppercase",
                bgcolor: theme.palette.primary.main,
                color: theme.palette.getContrastText(theme.palette.primary.main),
              })}
            >
              OpenAPI
            </Box>
            {parsed.info.version && (
              <Chip label={parsed.info.version} size="small" variant="outlined" />
            )}
          </Box>
          <Typography variant="h4" sx={{ fontWeight: 700, lineHeight: 1.2 }}>
            {parsed.info.title}
          </Typography>
          {parsed.info.description && (
            <Typography variant="body1" color="text.secondary" sx={{ mt: 1 }}>
              {parsed.info.description}
            </Typography>
          )}
        </Box>

        {/* Filter */}
        <TextField
          fullWidth
          size="small"
          placeholder="Filter operations · path, method, or summary"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          sx={{ mb: 3 }}
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <Search size={16} />
                </InputAdornment>
              ),
              endAdornment: query ? (
                <InputAdornment position="end">
                  <IconButton
                    size="small"
                    aria-label="Clear filter"
                    onClick={() => setQuery("")}
                  >
                    <X size={16} />
                  </IconButton>
                </InputAdornment>
              ) : undefined,
            },
          }}
        />

        {filtered.map((s) => (
          <TagSectionView key={s.id} section={s} roles={roles} />
        ))}
        {filtered.length === 0 && (
          <Typography variant="body2" color="text.secondary">
            No operations match “{query}”.
          </Typography>
        )}

        <SchemasSection schemas={parsed.schemas} />
      </Box>
    </Box>
  );
}
