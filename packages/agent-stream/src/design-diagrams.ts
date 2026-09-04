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
 * Write-gate behavior for the design's diagram documents — the domain model
 * (`specs/design/domain-model.md`, exactly one mermaid `erDiagram`) and each
 * key flow (`specs/design/flows/<slug>.md`, exactly one mermaid
 * `sequenceDiagram` whose participants are nodes `design.cell` declares or
 * actors the PRD names). ADR-0020 made one diagram per file the contract so a
 * generated diagram can be judged alone; this is the judge.
 *
 * The grammar is deliberately OURS, for the two shapes the design skill
 * prescribes, not mermaid's: mermaid's own parser for these diagram kinds needs
 * a DOM, and the subset the skill asks the agent to write is small. Anything
 * outside the subset is rejected with the line that broke it, which steers the
 * agent toward mermaid the console is guaranteed to render. The console's live
 * render remains the fidelity backstop.
 *
 * Same seam as the other gates — a pure check that returns the first problem,
 * except that a flow's participants are resolved against two OTHER bundle
 * files, so the check takes a reader for the cell and the PRD.
 */

export interface DesignDiagramProblem {
  code: "INVALID_DIAGRAM" | "UNKNOWN_PARTICIPANT";
  message: string;
}

/** The two bundle files a flow's participants resolve against. */
export interface DiagramBundleReader {
  read(path: string): string | undefined;
}

export const DOMAIN_MODEL_PATH = "specs/design/domain-model.md";
export const DESIGN_CELL_PATH = "specs/design/design.cell";
const PRD_PATH = "specs/requirements/prd.md";
const FLOW_RE = /^specs\/design\/flows\/[^/]+\.md$/;

type DiagramKind = "erDiagram" | "sequenceDiagram";

function diagramKindFor(path: string): DiagramKind | null {
  if (path === DOMAIN_MODEL_PATH) return "erDiagram";
  if (FLOW_RE.test(path)) return "sequenceDiagram";
  return null;
}

interface Diagnostic {
  /** 1-based line in the FILE, not the block. */
  line: number;
  message: string;
}

interface MermaidBlock {
  /** 1-based line of the opening fence. */
  fenceLine: number;
  /** Body lines paired with their 1-based file line numbers. */
  lines: Array<{ line: number; text: string }>;
}

// -------------------------------------------------------------------------
// Fences
// -------------------------------------------------------------------------

interface MermaidScan {
  blocks: MermaidBlock[];
  /** Line of a ```mermaid fence that never closes, if any. */
  unterminated: number | null;
}

/** Every ```mermaid … ``` block in a markdown document. */
function mermaidBlocks(content: string): MermaidScan {
  const out: MermaidBlock[] = [];
  let open: MermaidBlock | null = null;
  let inOtherFence = false;
  content.split("\n").forEach((raw, i) => {
    const line = i + 1;
    const text = raw.trim();
    if (open) {
      if (/^```\s*$/.test(text)) {
        out.push(open);
        open = null;
      } else {
        open.lines.push({ line, text });
      }
      return;
    }
    if (inOtherFence) {
      if (/^```\s*$/.test(text)) inOtherFence = false;
      return;
    }
    if (/^```mermaid\s*$/.test(text)) {
      open = { fenceLine: line, lines: [] };
    } else if (text.startsWith("```")) {
      inOtherFence = true;
    }
  });
  return { blocks: out, unterminated: open === null ? null : (open as MermaidBlock).fenceLine };
}

// -------------------------------------------------------------------------
// sequenceDiagram — the subset the design skill prescribes
// -------------------------------------------------------------------------

const ARROWS = "<<-->>|<<->>|-->>|->>|-->|->|--x|-x|--\\)|-\\)";
const MESSAGE_RE = new RegExp(`^(\\S+?)\\s*(${ARROWS})\\s*([+-])?\\s*(\\S+?)\\s*:(.*)$`);
const DECLARE_RE = /^(?:create\s+)?(participant|actor)\s+([^\s:]+)(?:\s+as\s+(.+))?$/;
const NOTE_RE = /^note\s+(?:left of|right of|over)\s+([^\s:,]+)(?:\s*,\s*([^\s:,]+))?\s*:/i;
const OPENERS = /^(alt|opt|loop|par|critical|break|rect|box)(\s|$)/;
// The two shapes the evidence says models actually write when a line fails:
// a multi-word name in a declaration, or in a message endpoint. Mermaid
// wants one-word ids with the label carried by `as`, so the error must
// TEACH that — a generic "not a sequence statement" earns a byte-identical
// retry (seen live: attempt two of a rejected flow was unchanged).
const SPACED_DECLARE_RE = /^((?:create\s+)?(?:participant|actor))\s+\S+(?:\s+\S+)+$/;
const SPACED_MESSAGE_RE = new RegExp(`^(.+?)\\s*(?:${ARROWS})\\s*[+-]?\\s*(.+?)\\s*:`);
const CONTINUERS = /^(else|and|option)(\s|$)/;

interface SequenceParse {
  diagnostics: Diagnostic[];
  participants: Set<string>;
  /** The subset declared with the `actor` keyword — people, not nodes. */
  actors: Set<string>;
}

function parseSequence(block: MermaidBlock): SequenceParse {
  const diagnostics: Diagnostic[] = [];
  const participants = new Set<string>();
  const actors = new Set<string>();
  const openBlocks: number[] = [];
  let sawHeader = false;

  for (const { line, text } of block.lines) {
    if (text === "" || text.startsWith("%%")) continue;
    if (!sawHeader) {
      if (text !== "sequenceDiagram") {
        diagnostics.push({ line, message: `the block must open with \`sequenceDiagram\`, not \`${text}\`` });
        return { diagnostics, participants, actors };
      }
      sawHeader = true;
      continue;
    }
    if (/^autonumber(\s|$)/.test(text) || /^(title|accTitle|accDescr)\b/.test(text)) continue;
    const decl = DECLARE_RE.exec(text);
    if (decl) {
      participants.add(decl[2] as string);
      if (decl[1] === "actor") actors.add(decl[2] as string);
      continue;
    }
    const destroy = /^destroy\s+([^\s:]+)$/.exec(text);
    if (destroy) {
      participants.add(destroy[1] as string);
      continue;
    }
    const act = /^(?:activate|deactivate)\s+([^\s:]+)$/.exec(text);
    if (act) {
      participants.add(act[1] as string);
      continue;
    }
    const note = NOTE_RE.exec(text);
    if (note) {
      participants.add(note[1] as string);
      if (note[2]) participants.add(note[2]);
      continue;
    }
    if (/^links?\s+[^\s:]+\s*:/.test(text) || /^properties\s+[^\s:]+\s*:/.test(text)) continue;
    if (OPENERS.test(text)) {
      openBlocks.push(line);
      continue;
    }
    if (CONTINUERS.test(text)) {
      if (openBlocks.length === 0) {
        diagnostics.push({ line, message: `\`${text.split(/\s/)[0]}\` outside an alt/par/critical block` });
      }
      continue;
    }
    if (text === "end") {
      if (openBlocks.length === 0) {
        diagnostics.push({ line, message: "`end` with no open block" });
      } else {
        openBlocks.pop();
      }
      continue;
    }
    const msg = MESSAGE_RE.exec(text);
    if (msg) {
      participants.add(msg[1] as string);
      participants.add(msg[4] as string);
      continue;
    }
    const spacedDecl = SPACED_DECLARE_RE.exec(text);
    if (spacedDecl && !/\sas\s/.test(text)) {
      const keyword = spacedDecl[1] as string;
      const words = text.slice(keyword.length).trim();
      const oneWord = words.replace(/[^\p{L}\p{N}]+/gu, "");
      diagnostics.push({
        line,
        message: `\`${text}\` — a name is ONE word; declare \`${keyword} ${oneWord} as ${words}\` and use \`${oneWord}\` in every message`,
      });
      continue;
    }
    const spaced = SPACED_MESSAGE_RE.exec(text);
    if (spaced && (/\s/.test(spaced[1] as string) || /\s/.test(spaced[2] as string))) {
      diagnostics.push({
        line,
        message: `\`${text}\` — a message's endpoints are one-word ids (\`WarehouseStaff->>ops-console: …\`); a multi-word name gets its words from the declaration's \`as\` alias, never from the message line`,
      });
      continue;
    }
    diagnostics.push({
      line,
      message: `\`${text}\` is not a sequence statement — expected a participant/actor declaration, a message (A->>B: text), a Note, or an alt/opt/loop/par block`,
    });
  }
  if (!sawHeader) {
    diagnostics.push({ line: block.fenceLine, message: "the mermaid block is empty" });
  }
  for (const line of openBlocks) {
    diagnostics.push({ line, message: "block opened here is never closed with `end`" });
  }
  return { diagnostics, participants, actors };
}

// -------------------------------------------------------------------------
// erDiagram — the subset the design skill prescribes
// -------------------------------------------------------------------------

const ENTITY = String.raw`(?:"[^"]+"|[A-Za-z_][\w-]*)`;
const ALIAS = String.raw`(?:\[[^\]]*\])?`;
const RELATION_RE = new RegExp(
  `^${ENTITY}${ALIAS}\\s+(?:\\|o|\\|\\||\\}o|\\}\\|)(?:--|\\.\\.)(?:o\\||\\|\\||o\\{|\\|\\{)\\s+${ENTITY}${ALIAS}\\s*:\\s*(?:"[^"]*"|\\S.*)$`,
);
const BLOCK_OPEN_RE = new RegExp(`^${ENTITY}${ALIAS}\\s*\\{$`);
const BARE_ENTITY_RE = new RegExp(`^${ENTITY}${ALIAS}$`);
const ATTRIBUTE_RE = /^[A-Za-z_][\w[\]()<>,.-]*\s+[A-Za-z_][\w-]*(?:\s+(?:PK|FK|UK)(?:\s*,\s*(?:PK|FK|UK))*)?(?:\s+"[^"]*")?$/;

function parseEr(block: MermaidBlock): Diagnostic[] {
  const diagnostics: Diagnostic[] = [];
  let sawHeader = false;
  let inEntity: number | null = null;

  for (const { line, text } of block.lines) {
    if (text === "" || text.startsWith("%%")) continue;
    if (!sawHeader) {
      if (text !== "erDiagram") {
        diagnostics.push({ line, message: `the block must open with \`erDiagram\`, not \`${text}\`` });
        return diagnostics;
      }
      sawHeader = true;
      continue;
    }
    if (inEntity !== null) {
      if (text === "}") {
        inEntity = null;
      } else if (!ATTRIBUTE_RE.test(text)) {
        diagnostics.push({
          line,
          message: 'not an attribute — expected `type name`, optionally followed by PK/FK/UK and a "comment"',
        });
      }
      continue;
    }
    if (/^(title|accTitle|accDescr)\b/.test(text) || /^direction\s+(TB|BT|LR|RL)$/.test(text)) continue;
    if (BLOCK_OPEN_RE.test(text)) {
      inEntity = line;
      continue;
    }
    if (RELATION_RE.test(text) || BARE_ENTITY_RE.test(text)) continue;
    diagnostics.push({
      line,
      message: `\`${text}\` is not an ER statement — expected an entity block (NAME { … }) or a relation (A ||--o{ B : label)`,
    });
  }
  if (!sawHeader) diagnostics.push({ line: block.fenceLine, message: "the mermaid block is empty" });
  if (inEntity !== null) diagnostics.push({ line: inEntity, message: "entity block opened here is never closed with `}`" });
  return diagnostics;
}

// -------------------------------------------------------------------------
// What a flow's participants resolve against
// -------------------------------------------------------------------------

export interface CellNodes {
  components: string[];
  externals: string[];
}

const BOUNDARIES = new Set(["north", "east", "south", "west"]);

/** Quoted runs stay one token — the cell grammar's tokenizer. */
function tokenize(statement: string): string[] {
  return [...statement.matchAll(/"([^"]*)"|(\S+)/g)].map((m) => (m[1] !== undefined ? m[1] : (m[2] as string)));
}

/**
 * The node ids a design.cell declares — components inside the cell and the
 * boundary externals on its edges. Facts only (the same slice the platform's
 * scaffold reads), never the diagram's semantics: an edge, a title, a cell
 * block brace or the platform's frontmatter are all skipped.
 */
export function cellNodeIds(cellSource: string): CellNodes {
  const components: string[] = [];
  const externals: string[] = [];
  const lines = cellSource.split("\n");
  let i = 0;
  if (lines[0]?.trim() === "---") {
    const close = lines.findIndex((l, idx) => idx > 0 && l.startsWith("---"));
    if (close > 0) i = close + 1;
  }
  for (; i < lines.length; i++) {
    const text = (lines[i] ?? "").trim();
    if (text === "" || text.startsWith("#") || text.startsWith("//") || text.includes("->")) continue;
    if (text === "}" || /^cell\s/.test(text)) continue;
    const tokens = tokenize(text);
    const head = tokens[0];
    const id = tokens[1];
    if (!head || !id) continue;
    if (head === "component") components.push(id);
    else if (BOUNDARIES.has(head)) externals.push(id);
  }
  return { components, externals };
}

/**
 * The actors a PRD names: every bullet under its Actors (or legacy Personas)
 * heading, plus the subject of every "As a(n) X," story — the two places the
 * PRD contract puts an actor, so a PRD written before the Actors section
 * existed still resolves its own stories' actors.
 */
export function prdActors(prd: string): string[] {
  const out = new Set<string>();
  const lines = prd.split("\n");
  let inActors = false;
  for (const raw of lines) {
    const text = raw.trim();
    const heading = /^#{1,6}\s+(.*)$/.exec(text);
    if (heading) {
      inActors = /^(actors|personas)\b/i.test(heading[1] as string);
      continue;
    }
    if (!inActors) continue;
    const bullet = /^[-*+]\s+(.+)$/.exec(text);
    if (!bullet) continue;
    const name = (bullet[1] as string)
      .replace(/[*_`]/g, "")
      .split(/\s+[—–-]\s+|:\s|\s\(/)[0]
      ?.trim();
    if (name) out.add(name);
  }
  for (const m of prd.matchAll(/\bAs an?\s+([^,\n]+?),/gi)) out.add((m[1] as string).trim());
  return [...out];
}

/** Names compare with case, spaces, hyphens and underscores flattened. */
function normalize(name: string): string {
  return name.toLowerCase().replace(/[^a-z0-9]/g, "");
}

// -------------------------------------------------------------------------
// The gate
// -------------------------------------------------------------------------

const MAX_LISTED = 5;

function reject(path: string, code: DesignDiagramProblem["code"], detail: string): DesignDiagramProblem {
  return {
    code,
    message: `${path} rejected — ${detail} The file is unchanged; fix it and re-emit the WHOLE file in ONE retry.`,
  };
}

function listDiagnostics(diagnostics: Diagnostic[]): string {
  const shown = diagnostics.slice(0, MAX_LISTED).map((d) => `line ${d.line}: ${d.message}`);
  const more = diagnostics.length - shown.length;
  return shown.join("; ") + (more > 0 ? `; +${more} more` : "") + ".";
}

/**
 * Judge a design diagram document. `null` for every path that is not one of
 * the two diagram documents, and for a document that satisfies its contract.
 */
export function checkDesignDiagram(
  path: string,
  content: string,
  bundle: DiagramBundleReader,
): DesignDiagramProblem | null {
  const kind = diagramKindFor(path);
  if (kind === null) return null;

  const { blocks, unterminated } = mermaidBlocks(content);
  if (unterminated !== null) {
    return reject(
      path,
      "INVALID_DIAGRAM",
      `the \`\`\`mermaid fence opened at line ${unterminated} is never closed — close it with \`\`\` on its own line.`,
    );
  }
  if (blocks.length === 0) {
    return reject(path, "INVALID_DIAGRAM", `it holds no \`\`\`mermaid block — it must hold exactly one ${kind}.`);
  }
  if (blocks.length > 1) {
    const where = blocks.map((b) => b.fenceLine).join(", ");
    const hint =
      kind === "sequenceDiagram"
        ? "one flow per file — put the extra diagram in its own specs/design/flows/<slug>.md."
        : "one domain model — merge the entities into a single erDiagram.";
    return reject(
      path,
      "INVALID_DIAGRAM",
      `it holds ${blocks.length} mermaid blocks (lines ${where}) but must hold exactly one ${kind}: ${hint}`,
    );
  }
  const block = blocks[0] as MermaidBlock;

  if (kind === "erDiagram") {
    const diagnostics = parseEr(block);
    return diagnostics.length === 0 ? null : reject(path, "INVALID_DIAGRAM", listDiagnostics(diagnostics));
  }

  const parsed = parseSequence(block);
  if (parsed.diagnostics.length > 0) {
    return reject(path, "INVALID_DIAGRAM", listDiagnostics(parsed.diagnostics));
  }

  const cellSource = bundle.read(DESIGN_CELL_PATH);
  if (cellSource === undefined || cellSource.trim() === "") {
    return reject(
      path,
      "UNKNOWN_PARTICIPANT",
      `${DESIGN_CELL_PATH} is not in the bundle yet, so its participants cannot be resolved — write the cell first; a flow's participants must be nodes the cell declares or actors the PRD names.`,
    );
  }
  const nodes = cellNodeIds(cellSource);
  const actors = prdActors(bundle.read(PRD_PATH) ?? "");
  const allowed = new Set([...nodes.components, ...nodes.externals, ...actors].map(normalize));
  // A PRD written before its contract had an Actors section names nobody the
  // gate can check against; then a participant declared as an `actor` is
  // taken at its word — the cell's nodes stay enforced for everything else.
  const actorsUncheckable = actors.length === 0;
  const unknown = [...parsed.participants].filter(
    (p) => !allowed.has(normalize(p)) && !(actorsUncheckable && parsed.actors.has(p)),
  );
  if (unknown.length === 0) return null;

  const list = (xs: string[]) => (xs.length ? xs.join(", ") : "none");
  return reject(
    path,
    "UNKNOWN_PARTICIPANT",
    `participant${unknown.length > 1 ? "s" : ""} ${unknown.map((u) => `\`${u}\``).join(", ")} ${unknown.length > 1 ? "are" : "is"} neither a node design.cell declares (components: ${list(nodes.components)}; boundary externals: ${list(nodes.externals)}) nor an actor the PRD names (${list(actors)}). Use one of those ids, add the node to design.cell first, or have the actor named in the PRD's Actors section.`,
  );
}
