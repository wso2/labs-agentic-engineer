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

// Tiny DSL → Excalidraw scene converter (@aep/excalidraw-dsl — the single
// workspace copy; the legacy per-project duplicates are gone).
//
// The `wireframes` dialect targets DESKTOP webapp wireframes in the gray,
// structural style: screens default to 1280×800, web primitives (navbar,
// sidebar, table, input, card, image) render as grayscale boxes — layout
// first, no visual polish. The agent writes the DSL; this compiler owns
// every Excalidraw-format concern (required fields, ids, styling).

export type DslKind = 'wireframes' | 'domain-model';

// ---------- Wireframes DSL ----------

type WireframeKind =
  | 'rect'
  | 'ellipse'
  | 'button'
  | 'text'
  | 'heading'
  | 'input'
  | 'card'
  | 'image'
  | 'table'
  | 'navbar'
  | 'sidebar'
  // richer webapp primitives
  | 'tabs'
  | 'list'
  | 'select'
  | 'search'
  | 'textarea'
  | 'checkbox'
  | 'radio'
  | 'toggle'
  | 'badge'
  | 'avatar'
  | 'progress'
  | 'divider'
  | 'breadcrumb'
  | 'chart'
  | 'icon'
  | 'link';

/**
 * Semantic emphasis. Wireframes stay grayscale by default; a variant opts a
 * single element into meaningful color (a primary CTA, a destructive action,
 * a status badge, an AI step). Kept rare so color always *means* something.
 */
type WireframeVariant =
  | 'primary'
  | 'secondary'
  | 'danger'
  | 'success'
  | 'warning'
  | 'info'
  | 'ai'
  | 'active'
  | 'muted';

interface WireframeElement {
  kind: WireframeKind;
  label: string;
  x: number;
  y: number;
  width: number;
  height: number;
  variant?: WireframeVariant;
  /** Body rows for a `table`, collected from nested `row "a | b"` lines. */
  rows?: string[][];
  /** Screen this element navigates to, from a trailing `-> ScreenName`. */
  navTo?: string;
  /** True when the author gave an explicit `WxH` (layout keeps it). */
  wSet?: boolean;
  hSet?: boolean;
}

interface WireframeScreen {
  name: string;
  /** Optional subtitle explaining what the view is for / which role it serves. */
  description?: string;
  width: number;
  height: number;
  elements: WireframeElement[];
}

interface WireframeFlow {
  from: string;
  to: string;
}

/**
 * A named `flow "…"` block: one persona's walkthrough, REFERENCING screen
 * names rather than defining screens — so a screen shared by two personas
 * (Login, a sign-out landing) is listed by each flow and still compiled once.
 * `screens` holds canonical names, entry point first.
 */
interface WireframeNamedFlow {
  name: string;
  /** Optional `role "…"` keyword line: the persona who walks this flow. */
  role?: string;
  /** Optional `description "…"` keyword line, mirroring a screen's subtitle. */
  description?: string;
  screens: string[];
}

interface WireframeAst {
  screens: WireframeScreen[];
  /** Legacy unnamed `flow` block edges — parsed, never rendered. */
  flows: WireframeFlow[];
  namedFlows: WireframeNamedFlow[];
}

// ---------- Domain Model DSL ----------

interface DomainAttribute {
  name: string;
  type: string;
}

interface DomainEntity {
  name: string;
  attrs: DomainAttribute[];
}

interface DomainRelation {
  from: string;
  to: string;
  cardinality: string;
  label: string;
}

interface DomainAst {
  entities: DomainEntity[];
  relations: DomainRelation[];
}

// ---------- Excalidraw scene shape ----------

export interface ExcalidrawScene {
  type: 'excalidraw';
  version: number;
  source: string;
  elements: ExcalidrawElement[];
  appState: { viewBackgroundColor: string };
  files: Record<string, never>;
}

interface ExcalidrawElementBase {
  id: string;
  type: string;
  x: number;
  y: number;
  width: number;
  height: number;
  angle: number;
  strokeColor: string;
  backgroundColor: string;
  fillStyle: string;
  strokeWidth: number;
  strokeStyle: string;
  roughness: number;
  opacity: number;
  groupIds: string[];
  frameId: null;
  roundness: { type: number } | null;
  seed: number;
  versionNonce: number;
  version: number;
  isDeleted: boolean;
  boundElements: { id: string; type: string }[] | null;
  updated: number;
  link: null;
  locked: boolean;
  /** Which `screen` emitted this element; set on every element of the grid scene. */
  customData?: { screen: string };
}

interface RectElement extends ExcalidrawElementBase {
  type: 'rectangle' | 'ellipse' | 'diamond';
}

interface TextElement extends ExcalidrawElementBase {
  type: 'text';
  text: string;
  fontSize: number;
  fontFamily: number;
  textAlign: 'left' | 'center' | 'right';
  verticalAlign: 'top' | 'middle' | 'bottom';
  baseline: number;
  containerId: string | null;
  originalText: string;
  lineHeight: number;
  autoResize: boolean;
}

interface PointBinding {
  elementId: string;
  focus: number;
  gap: number;
  /** Normalized [x, y] in [0, 1] relative to the bound element. Required
   *  for elbow arrows (FixedPointBinding); harmless on straight arrows. */
  fixedPoint?: [number, number];
}

interface ArrowElement extends ExcalidrawElementBase {
  type: 'arrow' | 'line';
  points: [number, number][];
  lastCommittedPoint: null;
  startBinding: PointBinding | null;
  endBinding: PointBinding | null;
  startArrowhead: null | 'arrow';
  endArrowhead: null | 'arrow';
  elbowed?: boolean;
  /** Elbow-arrow specific. Null lets Excalidraw compute segments itself. */
  fixedSegments?: null;
  startIsSpecial?: null;
  endIsSpecial?: null;
}

export type ExcalidrawElement = RectElement | TextElement | ArrowElement;

// ---------- Public API ----------

export function dslToExcalidraw(kind: DslKind, dsl: string): string {
  const elements =
    kind === 'wireframes'
      ? renderWireframes(parseWireframesDsl(dsl))
      : renderDomainModel(parseDomainModelDsl(dsl));
  const scene: ExcalidrawScene = {
    type: 'excalidraw',
    version: 2,
    source: 'aep-generator',
    elements,
    appState: { viewBackgroundColor: '#ffffff' },
    files: {},
  };
  return JSON.stringify(scene, null, 2);
}

// ---------- Wireframe compile that reports what changed ----------

/**
 * A wireframe compile plus the compiler's own account of which screens
 * changed since `previous`. Hold the result and pass it back on the next
 * compile; the compiler remembers nothing between calls.
 */
export type WireframeCompile =
  | {
      ok: true;
      json: string;
      /**
       * Screens whose rendered content differs from `previous`, in canvas
       * order (topmost first). Empty when nothing changed or when there was
       * no `previous` to compare against. A screen that merely moved because
       * one above it grew is NOT listed — comparison is relative to each
       * screen's own origin.
       */
      changedScreens: string[];
      /** Per-screen fingerprints, carried so the NEXT compile can compare. */
      fingerprints: Record<string, string>;
      /** Screen names in canvas order (topmost first); `[0]` is where a reader starts. */
      screenOrder: string[];
    }
  | { ok: false; error: string };

/**
 * A screen's rendered content as one comparable string, positions taken
 * relative to the screen's own top-left. Screens stack in one column, so a
 * taller screen above shifts everything below it; comparing absolute
 * coordinates would call every shifted screen "changed". Element ids are
 * already screen-relative, and everything else is compared verbatim.
 */
function screenFingerprint(elements: readonly ExcalidrawElement[]): string {
  let ox = Number.POSITIVE_INFINITY;
  let oy = Number.POSITIVE_INFINITY;
  for (const e of elements) {
    if (e.x < ox) ox = e.x;
    if (e.y < oy) oy = e.y;
  }
  return elements
    .map((e) => JSON.stringify({ ...e, x: e.x - ox, y: e.y - oy }))
    .sort()
    .join('\n');
}

/**
 * Compile a wireframes DSL and report which screens changed versus the
 * previous compile. This is what lets a viewer follow an edit without
 * guessing: the compiler KNOWS the screen structure, so it — not a diff over
 * a flat scene — says what moved. `previous` is the prior result from this
 * function (or null on first compile / a different file); the caller keeps
 * it, the compiler stays pure.
 *
 * A source that does not compile returns `ok: false` exactly like
 * `tryDslToExcalidraw`, and reports no change — so a half-written stream
 * frame never produces a focus signal.
 */
export function compileWireframes(dsl: string, previous: WireframeCompile | null): WireframeCompile {
  const base = tryDslToExcalidraw('wireframes', dsl);
  if (!base.ok) return base;
  const elements = (JSON.parse(base.json) as ExcalidrawScene).elements;

  // Bucket by the screen tag every element carries, remembering each screen's
  // topmost y so the report can be ordered the way the canvas is.
  const byScreen = new Map<string, ExcalidrawElement[]>();
  const topOf = new Map<string, number>();
  for (const e of elements) {
    const name = e.customData?.screen;
    if (!name) continue;
    const bucket = byScreen.get(name);
    if (bucket) bucket.push(e);
    else byScreen.set(name, [e]);
    const t = topOf.get(name);
    if (t === undefined || e.y < t) topOf.set(name, e.y);
  }

  const fingerprints: Record<string, string> = {};
  for (const [name, els] of byScreen) fingerprints[name] = screenFingerprint(els);

  const prior = previous?.ok ? previous.fingerprints : null;
  const changedScreens: string[] = [];
  if (prior) {
    for (const name of Object.keys(fingerprints)) {
      if (prior[name] !== fingerprints[name]) changedScreens.push(name);
    }
    // A removed screen has nothing left to show, but the gap it left is worth
    // reporting; it is ordered by where it USED to sit, which only the previous
    // compile knows.
    for (const name of Object.keys(prior)) {
      if (!(name in fingerprints)) changedScreens.push(name);
    }
    // Rank on the previous canvas — the one the reader was actually looking at
    // — placing each screen by its old index and falling back to the new
    // ordering for screens that did not exist before. Ranking a removed screen
    // by the CURRENT canvas is impossible (it has no position there), and
    // defaulting it to last claimed the screen below it changed first.
    const priorRank = new Map(
      (previous?.ok ? previous.screenOrder : []).map((name, i) => [name, i] as const),
    );
    const currentRank = new Map(
      [...topOf.entries()].sort((a, b) => a[1] - b[1]).map(([name], i) => [name, i] as const),
    );
    const rankOf = (name: string): number =>
      priorRank.get(name) ?? (currentRank.get(name) ?? 0) + priorRank.size;
    changedScreens.sort((a, b) => rankOf(a) - rankOf(b));
  }

  const screenOrder = [...topOf.entries()].sort((a, b) => a[1] - b[1]).map(([name]) => name);
  return { ok: true, json: base.json, changedScreens, fingerprints, screenOrder };
}

export function tryDslToExcalidraw(
  kind: DslKind,
  dsl: string,
): { ok: true; json: string } | { ok: false; error: string } {
  if (!dsl || dsl.trim().length === 0) return { ok: false, error: 'empty DSL source' };
  try {
    const json = dslToExcalidraw(kind, dsl);
    const parsed = JSON.parse(json) as ExcalidrawScene;
    if (parsed.elements.length === 0) {
      return {
        ok: false,
        error:
          kind === 'wireframes'
            ? 'no screens parsed — expected `screen <Name>` blocks'
            : 'no entities parsed — expected `entity <Name>` blocks',
      };
    }
    return { ok: true, json };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) };
  }
}

// ---------- Prototype (click-through) compile mode ----------

export interface PrototypeHotspot {
  x: number;
  y: number;
  width: number;
  height: number;
  target: string;
}

export interface PrototypeScreen {
  name: string;
  description?: string;
  width: number;
  height: number;
  /** Serialized ExcalidrawScene: this ONE screen, frame at origin (0,0). */
  sceneJson: string;
  hotspots: PrototypeHotspot[];
}

/**
 * One persona's walkthrough. `screens` are canonical `PrototypeScreen` names,
 * entry point first — membership lives on the flow, not the screen, so one
 * screen can belong to several flows without being compiled twice.
 */
export interface PrototypeFlow {
  name: string;
  /** The persona who walks this flow, from the `role "…"` keyword line. */
  role?: string;
  /** What the journey is, from the `description "…"` keyword line. */
  description?: string;
  screens: string[];
}

export interface PrototypeModel {
  screens: PrototypeScreen[];
  /** Empty when the DSL declares no `flow "…"` blocks. */
  flows: PrototypeFlow[];
}

export function tryDslToPrototype(
  dsl: string,
): { ok: true; model: PrototypeModel } | { ok: false; error: string } {
  if (!dsl || dsl.trim().length === 0) return { ok: false, error: 'empty DSL source' };
  try {
    const ast = parseWireframesDsl(dsl);
    if (ast.screens.length === 0) {
      return { ok: false, error: 'no screens parsed — expected `screen <Name>` blocks' };
    }
    const validTargets = new Map(ast.screens.map((s) => [s.name.toLowerCase(), s.name]));
    const screens: PrototypeScreen[] = ast.screens.map((screen) => {
      const chromeHotspots: PrototypeHotspot[] = [];
      const elements = renderWireframes(
        { screens: [screen], flows: [], namedFlows: [] },
        { prototype: { validTargets, chromeHotspots } },
      );
      const scene: ExcalidrawScene = {
        type: 'excalidraw',
        version: 2,
        source: 'aep-generator',
        elements,
        appState: { viewBackgroundColor: '#ffffff' },
        files: {},
      };
      // A hotspot promises a screen change, so it is claimed only by a control
      // whose target EXISTS and is a DIFFERENT screen. A `-> ThisScreen` says
      // "go to where you already are": agents write it meaning "acts in place"
      // (an Add beside a search box that appends a row), and honouring it would
      // render a control that highlights, invites a click, and cannot change
      // anything — which reads as a broken prototype. Dropped here so it looks
      // exactly like the un-annotated controls it belongs with.
      const bodyHotspots: PrototypeHotspot[] = screen.elements
        .filter(
          (el): el is WireframeElement & { navTo: string } =>
            Boolean(el.navTo) && el.kind !== 'navbar' && el.kind !== 'sidebar' &&
            validTargets.has(el.navTo!.toLowerCase()) &&
            validTargets.get(el.navTo!.toLowerCase()) !== screen.name,
        )
        .map((el) => ({
          x: el.x, y: el.y, width: el.width, height: el.height,
          target: validTargets.get(el.navTo.toLowerCase())!,
        }));
      const hotspots: PrototypeHotspot[] = [...chromeHotspots, ...bodyHotspots];
      const out: PrototypeScreen = {
        name: screen.name,
        width: screen.width,
        height: screen.height, // post-layout: layout may have grown it
        sceneJson: JSON.stringify(scene),
        hotspots,
      };
      if (screen.description) out.description = screen.description;
      return out;
    });
    const flows: PrototypeFlow[] = ast.namedFlows.map((f) => ({
      name: f.name,
      ...(f.role !== undefined ? { role: f.role } : {}),
      ...(f.description !== undefined ? { description: f.description } : {}),
      screens: [...f.screens],
    }));
    return { ok: true, model: { screens, flows } };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) };
  }
}

// ---------- Wireframes layout validation ----------

/**
 * Element kinds with a meaningful box (explicit or defaulted W×H). Free text
 * (`text`/`heading`/`link`/`breadcrumb`) is EXCLUDED from overlap checks — its
 * width is an estimate of the rendered glyphs, so checking it would flag
 * perfectly fine layouts. Chrome (`navbar`/`sidebar`) is compiler-placed and
 * covered by the dedicated chrome rules instead.
 */
const BOX_KINDS = new Set<WireframeKind>([
  'rect', 'ellipse', 'button', 'input', 'card', 'image', 'table', 'tabs',
  'list', 'select', 'search', 'textarea', 'checkbox', 'radio', 'toggle',
  'badge', 'avatar', 'progress', 'chart', 'icon',
]);

/**
 * Check a wireframes DSL source for layout mistakes the compiler would render
 * verbatim: elements outside the screen frame, under the navbar/sidebar
 * chrome, or PARTIALLY overlapping each other. Full containment is layering
 * (a badge inside a card) and allowed; a partial overlap is always a
 * collision. Returns one human/model-readable issue per problem — empty when
 * the layout is clean. Unparseable DSL returns [] (syntax is the compile
 * gate's job, not this one's).
 */
export function validateWireframeLayout(dsl: string): string[] {
  let ast: WireframeAst;
  try {
    ast = parseWireframesDsl(dsl);
  } catch {
    return [];
  }
  const issues: string[] = [];
  const box = (el: WireframeElement) =>
    `(x${el.x}..${el.x + el.width}, y${el.y}..${el.y + el.height})`;

  for (const screen of ast.screens) {
    const hasNavbar = screen.elements.some((e) => e.kind === 'navbar');
    const hasSidebar = screen.elements.some((e) => e.kind === 'sidebar');
    const content = screen.elements.filter(
      (e) => e.kind !== 'navbar' && e.kind !== 'sidebar',
    );

    for (const el of content) {
      // Frame bounds — the compiler draws exactly here, so past-the-edge
      // coordinates render outside the screen outline.
      if (el.x < 0 || el.y < 0 || el.x + el.width > screen.width || el.y + el.height > screen.height) {
        issues.push(
          `screen ${screen.name}: ${el.kind} "${el.label}" ${box(el)} extends past the ${screen.width}x${screen.height} frame — keep x+width <= ${screen.width} and y+height <= ${screen.height}.`,
        );
        continue; // out-of-frame already explains itself; skip chrome noise
      }
      // Chrome bands: the navbar fills y 0..56 and the sidebar x 0..240.
      if (hasNavbar && el.y < NAVBAR_H) {
        issues.push(
          `screen ${screen.name}: ${el.kind} "${el.label}" ${box(el)} sits under the navbar (y 0..${NAVBAR_H}) — start content at y >= 72.`,
        );
      }
      if (hasSidebar && el.x < SIDEBAR_W) {
        issues.push(
          `screen ${screen.name}: ${el.kind} "${el.label}" ${box(el)} sits under the sidebar rail (x 0..${SIDEBAR_W}) — start content at x >= 264.`,
        );
      }
    }

    // Pairwise partial overlaps between box-like elements.
    const boxes = content.filter((e) => BOX_KINDS.has(e.kind));
    for (let a = 0; a < boxes.length; a++) {
      for (let b = a + 1; b < boxes.length; b++) {
        const A = boxes[a]!;
        const B = boxes[b]!;
        const ix = Math.min(A.x + A.width, B.x + B.width) - Math.max(A.x, B.x);
        const iy = Math.min(A.y + A.height, B.y + B.height) - Math.max(A.y, B.y);
        if (ix <= 0 || iy <= 0) continue; // disjoint or edge-sharing
        const aInB = A.x >= B.x && A.y >= B.y && A.x + A.width <= B.x + B.width && A.y + A.height <= B.y + B.height;
        const bInA = B.x >= A.x && B.y >= A.y && B.x + B.width <= A.x + A.width && B.y + B.height <= A.y + A.height;
        if (aInB || bInA) continue; // full containment = intentional layering
        issues.push(
          `screen ${screen.name}: ${A.kind} "${A.label}" ${box(A)} partially overlaps ${B.kind} "${B.label}" ${box(B)} — move or resize so they either nest fully (layering inside a card) or don't touch.`,
        );
      }
    }
  }
  return issues;
}

// ---------- Wireframes parser ----------

const QUOTED = /"((?:[^"\\]|\\.)*)"/;
const SIZE = /(\d+)\s*x\s*(\d+)/;
/** The retired coordinate dialect's `x,y` token — its presence means "regenerate". */
const LEGACY_COORDS = /(?:^|\s)\d+\s*,\s*\d+(?:\s|$)/;

// ---------- Flow tree (parse output, pre-layout) ----------
//
// The DSL carries STRUCTURE (stack / row / split / card nesting); the layout
// pass below computes every pixel. Overlap and out-of-frame are inexpressible.

interface ElNode {
  type: 'el';
  el: WireframeElement;
  /** Nested children — only a `card` container carries them (elements or rows). */
  children: Array<ElNode | RowNode>;
}
interface RowNode {
  type: 'row';
  children: ElNode[];
  /** Index in `children` where right-edge packing starts; -1 = none. */
  rightFrom: number;
}
interface SplitNode {
  type: 'split';
  leftPct: number;
  left: FlowNode[];
  right: FlowNode[];
}
type FlowNode = ElNode | RowNode | SplitNode;

interface FlowScreen extends WireframeScreen {
  chrome: WireframeElement[];
  tree: FlowNode[];
  /** True when the screen declared an explicit height (it stays a minimum). */
  hDeclared: boolean;
}

function parseWireframesDsl(dsl: string): WireframeAst {
  const { ast, legacy } = buildWireframes(dsl, null);
  if (legacy) {
    throw new Error(
      'this file uses the retired coordinate dialect (absolute x,y positions) — regenerate the wireframes to get the flow dialect',
    );
  }
  return ast;
}

/**
 * Strict-syntax check for the write-gate: unknown keywords, misplaced
 * `left`/`right`/table-`row` lines, and retired-dialect coordinates are
 * reported with line numbers. The compile path stays tolerant (bad lines are
 * skipped) so streamed prefixes keep previewing.
 */
export function validateWireframeSyntax(dsl: string): string[] {
  const errors: string[] = [];
  buildWireframes(dsl, errors);
  return errors;
}

interface Ctx {
  level: number;
  kind: 'root' | 'row' | 'split' | 'col' | 'card' | 'table';
  nodes?: FlowNode[]; // root / col
  row?: RowNode;
  split?: SplitNode;
  card?: ElNode;
  table?: WireframeElement;
}

function buildWireframes(
  dsl: string,
  errors: string[] | null,
): { ast: WireframeAst; legacy: boolean } {
  const ast: WireframeAst = { screens: [], flows: [], namedFlows: [] };
  let screen: FlowScreen | null = null;
  let stack: Ctx[] = [];
  let inFlow = false;
  let legacy = false;
  const err = (no: number, msg: string) => errors?.push(`line ${no}: ${msg}`);
  // The named `flow "…"` block currently open (null inside a legacy unnamed
  // block). Screen references are collected with their line numbers and
  // resolved AFTER the parse — a flow may list a screen declared further down.
  let currentFlow: WireframeNamedFlow | null = null;
  const flowRefs: Array<{ flow: WireframeNamedFlow; raw: string; line: number }> = [];
  // Header line per named flow, so an empty flow can be rejected at ITS line.
  const namedFlowLines: Array<{ flow: WireframeNamedFlow; line: number }> = [];

  const lines = dsl.split(/\r?\n/);
  for (let no = 1; no <= lines.length; no++) {
    const raw = lines[no - 1]!.replace(/\s+$/, '');
    const trimmed = raw.trim();
    if (trimmed.length === 0 || trimmed.startsWith('//') || trimmed.startsWith('#')) continue;
    const level = Math.floor((raw.length - raw.replace(/^[ \t]+/, '').length + 1) / 2);

    if (level === 0) {
      const screenMatch =
        /^screen\s+([\w-]+)(?:\s+"((?:[^"\\]|\\.)*)")?(?:\s+(\d+)\s*x\s*(\d+))?\s*$/i.exec(trimmed);
      if (screenMatch) {
        screen = {
          name: screenMatch[1]!.trim(),
          width: screenMatch[3] ? parseInt(screenMatch[3], 10) : DEFAULT_SCREEN_W,
          height: screenMatch[4] ? parseInt(screenMatch[4], 10) : DEFAULT_SCREEN_H,
          hDeclared: Boolean(screenMatch[4]),
          elements: [],
          chrome: [],
          tree: [],
        };
        if (screenMatch[2]) screen.description = unescapeQuoted(screenMatch[2]);
        // Screen names are identity: element ids are screen-relative, the
        // per-screen fingerprints that drive the viewer's focus are keyed by
        // name, and `-> Target` resolves by name (case-insensitively). Two
        // screens sharing one name merge into a single fingerprint and emit
        // colliding element ids, so it is always an authoring bug.
        if (ast.screens.some((s) => s.name.toLowerCase() === screen!.name.toLowerCase())) {
          err(no, `duplicate screen ${JSON.stringify(screen.name)} — declare each screen once`);
        }
        ast.screens.push(screen);
        stack = [{ level: 0, kind: 'root', nodes: screen.tree }];
        inFlow = false;
        currentFlow = null;
        continue;
      }
      const flowHead = /^flow(?:\s+"((?:[^"\\]|\\.)*)")?\s*$/i.exec(trimmed);
      if (flowHead) {
        screen = null;
        inFlow = true;
        currentFlow = null;
        if (flowHead[1] !== undefined) {
          const name = unescapeQuoted(flowHead[1]);
          if (ast.namedFlows.some((f) => f.name === name)) {
            err(no, `duplicate flow ${JSON.stringify(name)} — declare each flow once`);
          } else {
            currentFlow = { name, screens: [] };
            ast.namedFlows.push(currentFlow);
            namedFlowLines.push({ flow: currentFlow, line: no });
          }
        }
        continue;
      }
      screen = null;
      inFlow = false;
      err(no, `unknown top-level line ${JSON.stringify(trimmed.slice(0, 40))} — expected \`screen <Name>\``);
      continue;
    }

    if (inFlow) {
      const edge = /^([\w-]+)\s*->\s*([\w-]+)$/.exec(trimmed);
      if (edge) {
        ast.flows.push({ from: edge[1]!, to: edge[2]! });
        continue;
      }
      if (currentFlow) {
        // Keyword metadata (`role "…"`, `description "…"`) is grammar-distinct
        // from a screen reference: a reference is a BARE name, so a screen
        // literally named `role` still resolves — only keyword + quoted string
        // reads as metadata.
        const kw = /^(role|description)\s+"((?:[^"\\]|\\.)*)"$/i.exec(trimmed);
        if (kw) {
          const key = kw[1]!.toLowerCase() as 'role' | 'description';
          if (currentFlow[key] !== undefined) {
            err(no, `duplicate ${key} for flow ${JSON.stringify(currentFlow.name)} — declare it once`);
          } else {
            currentFlow[key] = unescapeQuoted(kw[2]!);
          }
          continue;
        }
        const ref = /^([\w-]+)$/.exec(trimmed);
        if (ref) {
          flowRefs.push({ flow: currentFlow, raw: ref[1]!, line: no });
          continue;
        }
        err(no, `unknown flow line ${JSON.stringify(trimmed.slice(0, 40))} — expected a screen name`);
      }
      continue;
    }
    if (!screen) continue;

    // Find the container this line nests under.
    while (stack.length > 1 && stack[stack.length - 1]!.level >= level) stack.pop();
    const parent = stack[stack.length - 1]!;

    // Where a plain stacked node would land in this context.
    const stackTarget = (): FlowNode[] | null =>
      parent.kind === 'root' || parent.kind === 'col' ? parent.nodes! : null;

    // --- table body row: `row "cell | cell"` ------------------------------
    if (/^row\s+"/i.test(trimmed)) {
      if (parent.kind === 'table' && parent.table) {
        const q = QUOTED.exec(trimmed);
        if (q) (parent.table.rows ??= []).push(splitItems(unescapeQuoted(q[1]!)));
      } else {
        err(no, 'a quoted `row "…"` is table data and must nest under a `table`');
      }
      continue;
    }
    // --- layout row --------------------------------------------------------
    if (/^row\s*$/i.test(trimmed)) {
      const target = stackTarget();
      const rowNode: RowNode = { type: 'row', children: [], rightFrom: -1 };
      if (target) {
        target.push(rowNode);
      } else if (parent.kind === 'card' && parent.card) {
        // Side-by-side content INSIDE a card (two stats, label+value) — a
        // natural entity-card shape, laid out within the card's inner width.
        parent.card.children.push(rowNode);
      } else {
        err(no, 'a layout `row` can only sit in a screen stack, a split column, or inside a `card`');
        continue;
      }
      stack.push({ level, kind: 'row', row: rowNode });
      continue;
    }
    // --- split + its columns ----------------------------------------------
    const splitMatch = /^split\s+(\d+)\s*\/\s*(\d+)\s*$/i.exec(trimmed);
    if (splitMatch) {
      const target = stackTarget();
      if (!target) {
        err(no, '`split` can only sit in a screen stack');
        continue;
      }
      const node: SplitNode = {
        type: 'split',
        leftPct: parseInt(splitMatch[1]!, 10),
        left: [],
        right: [],
      };
      target.push(node);
      stack.push({ level, kind: 'split', split: node });
      continue;
    }
    if (/^(left|right)\s*$/i.test(trimmed)) {
      const word = trimmed.toLowerCase() as 'left' | 'right';
      if (parent.kind === 'split' && parent.split) {
        stack.push({
          level,
          kind: 'col',
          nodes: word === 'left' ? parent.split.left : parent.split.right,
        });
      } else if (word === 'right' && parent.kind === 'row' && parent.row) {
        parent.row.rightFrom = parent.row.children.length;
      } else {
        err(no, `\`${word}\` only makes sense under a \`split\` (column group) or inside a \`row\` (right-packing marker)`);
      }
      continue;
    }
    // --- element ------------------------------------------------------------
    const parsed = parseWireframeElement(trimmed);
    if (!parsed) {
      err(no, `unknown element ${JSON.stringify(trimmed.split(/\s/)[0])} — not a DSL keyword or element kind`);
      continue;
    }
    if (parsed.legacy) {
      legacy = true;
      err(no, 'absolute x,y coordinates are retired — the layout is computed from structure (stack / row / split); remove the coordinates');
      continue;
    }
    const el = parsed.el;
    if (el.kind === 'navbar' || el.kind === 'sidebar') {
      screen.chrome.push(el);
      continue;
    }
    const elNode: ElNode = { type: 'el', el, children: [] };
    if (parent.kind === 'row' && parent.row) {
      parent.row.children.push(elNode);
    } else if (parent.kind === 'card' && parent.card) {
      parent.card.children.push(elNode);
    } else {
      const target = stackTarget();
      if (!target) {
        err(no, `an element cannot nest under a \`${parent.kind}\` here`);
        continue;
      }
      target.push(elNode);
    }
    // Containers accept children: a card layers its children inside itself;
    // a table takes `row "…"` data lines.
    if (el.kind === 'card') stack.push({ level, kind: 'card', card: elNode });
    else if (el.kind === 'table') stack.push({ level, kind: 'table', table: el });
  }

  for (const s of ast.screens) layoutScreen(s as FlowScreen);

  // Resolved here, not inline, because a flow may list a screen declared
  // further down the file. An unknown name is always an authoring bug, so it
  // is reported with its line; a repeat within one flow is the author drawing
  // a return trip (sign out → Login) that the `-> Target` arrows already
  // carry, so the first position wins and the duplicate is dropped.
  const canonicalByLower = new Map(ast.screens.map((s) => [s.name.toLowerCase(), s.name]));
  for (const ref of flowRefs) {
    const canonical = canonicalByLower.get(ref.raw.toLowerCase());
    if (!canonical) {
      err(ref.line, `flow references unknown screen ${JSON.stringify(ref.raw)}`);
      continue;
    }
    if (!ref.flow.screens.includes(canonical)) ref.flow.screens.push(canonical);
  }
  // A flow with no screen references cannot start anywhere — the picker would
  // offer a journey with no entry. Always an authoring bug (a role/description
  // shell, or legacy `A -> B` edge lines that a named flow does not read), so
  // it is rejected at the flow's own header line.
  for (const { flow: f, line } of namedFlowLines) {
    if (f.screens.length === 0) {
      err(line, `flow ${JSON.stringify(f.name)} lists no screens — list at least one screen reference`);
    }
  }

  return { ast, legacy };
}

// ---------- Flow layout engine ----------
//
// Pure geometry: walks the flow tree computing every {x,y,w,h} in
// screen-local coordinates. Deterministic; unit-tested via the rendered
// scene and the validateWireframeLayout oracle.

const STACK_GAP = 24;
const ROW_GAP = 16;
const CARD_PAD = 16;
const CARD_CHILD_GAP = 12;
const SPLIT_GUTTER = 40;
const MARGIN = 40;
const CONTENT_TOP = 76; // below the 56px navbar band

/** Kinds that share a row's remaining width (everything else keeps intrinsic). */
const FLEX_KINDS = new Set<WireframeKind>([
  'card', 'input', 'select', 'search', 'textarea', 'chart', 'image', 'list',
  'tabs', 'progress', 'table', 'rect',
]);
/** Kinds that take the full container width when stacked. */
const FILL_KINDS = new Set<WireframeKind>(['table', 'chart', 'divider']);

function resolveStackSize(el: WireframeElement, containerW: number): void {
  if (!el.wSet && FILL_KINDS.has(el.kind)) el.width = containerW;
  if (el.kind === 'table' && !el.hSet) {
    el.height = 42 + Math.max(1, el.rows?.length ?? 1) * 38;
  }
  el.width = Math.min(el.width, containerW);
}

function layoutScreen(screen: FlowScreen): void {
  const hasNavbar = screen.chrome.some((c) => c.kind === 'navbar');
  const hasSidebar = screen.chrome.some((c) => c.kind === 'sidebar');
  const x0 = hasSidebar ? 264 : MARGIN;
  const w = screen.width - MARGIN - x0;
  const y0 = hasNavbar ? CONTENT_TOP : MARGIN;
  const out: WireframeElement[] = [];
  const nextY = layoutStack(screen.tree, x0, w, y0, out);
  const bottom = nextY - STACK_GAP; // drop the trailing gap
  if (!screen.hDeclared || bottom + MARGIN > screen.height) {
    screen.height = Math.max(screen.height, bottom + MARGIN);
  }
  screen.elements = [...screen.chrome, ...out];
}

/** Stack `nodes` top-to-bottom at x within width w; returns the next y cursor. */
function layoutStack(
  nodes: FlowNode[],
  x: number,
  w: number,
  y: number,
  out: WireframeElement[],
): number {
  for (const node of nodes) {
    if (node.type === 'el') {
      if (node.el.kind === 'card' && node.children.length > 0) {
        resolveStackSize(node.el, w);
        y += layoutCard(node, x, y, node.el.wSet ? node.el.width : w, out) + STACK_GAP;
      } else {
        resolveStackSize(node.el, w);
        node.el.x = x;
        node.el.y = y;
        out.push(node.el);
        y += node.el.height + STACK_GAP;
      }
    } else if (node.type === 'row') {
      y += layoutRow(node, x, w, y, out) + STACK_GAP;
    } else {
      y += layoutSplit(node, x, w, y, out) + STACK_GAP;
    }
  }
  return y;
}

/** Lay a row's children side by side within w; returns the row height. */
function layoutRow(row: RowNode, x: number, w: number, y: number, out: WireframeElement[]): number {
  const kids = row.children;
  if (kids.length === 0) return 0;
  const gaps = ROW_GAP * (kids.length - 1);

  // Width distribution: explicit → keep; auto kinds → intrinsic; flexible
  // kinds share the remainder equally; everything scales down if it can't fit.
  const isFlex = (n: ElNode) => !n.el.wSet && FLEX_KINDS.has(n.el.kind);
  const fixedSum = kids.filter((k) => !isFlex(k)).reduce((s, k) => s + k.el.width, 0);
  const flexKids = kids.filter(isFlex);
  if (flexKids.length > 0) {
    const share = Math.max(60, Math.floor((w - gaps - fixedSum) / flexKids.length));
    for (const k of flexKids) k.el.width = share;
  }
  const total = kids.reduce((s, k) => s + k.el.width, 0) + gaps;
  if (total > w) {
    const scale = (w - gaps) / (total - gaps);
    for (const k of kids) k.el.width = Math.max(24, Math.floor(k.el.width * scale));
  }

  // Place: left group flows from x; the right group packs against x+w.
  const rightFrom = row.rightFrom < 0 ? kids.length : row.rightFrom;
  const heights: number[] = [];
  const place = (k: ElNode, kx: number): void => {
    if (k.el.kind === 'card' && k.children.length > 0) {
      heights.push(layoutCard(k, kx, y, k.el.width, out));
    } else {
      if (k.el.kind === 'table' && !k.el.hSet) {
        k.el.height = 42 + Math.max(1, k.el.rows?.length ?? 1) * 38;
      }
      k.el.x = kx;
      k.el.y = y;
      out.push(k.el);
      heights.push(k.el.height);
    }
  };
  let cx = x;
  for (let i = 0; i < rightFrom; i++) {
    place(kids[i]!, cx);
    cx += kids[i]!.el.width + ROW_GAP;
  }
  let rx = x + w;
  for (let i = kids.length - 1; i >= rightFrom; i--) {
    rx -= kids[i]!.el.width;
    place(kids[i]!, rx);
    rx -= ROW_GAP;
  }
  return Math.max(...heights);
}

/** Two independent column stacks + the vertical divider; returns the height. */
function layoutSplit(node: SplitNode, x: number, w: number, y: number, out: WireframeElement[]): number {
  const pct = Math.min(90, Math.max(10, node.leftPct));
  const leftW = Math.round(((w - SPLIT_GUTTER) * pct) / 100);
  const rightW = w - SPLIT_GUTTER - leftW;
  const leftBottom = layoutStack(node.left, x, leftW, y, out) - STACK_GAP;
  const rightBottom = layoutStack(node.right, x + leftW + SPLIT_GUTTER, rightW, y, out) - STACK_GAP;
  const h = Math.max(leftBottom, rightBottom, y + 40) - y;
  out.push({
    kind: 'divider',
    label: '',
    x: x + leftW + SPLIT_GUTTER / 2,
    y,
    width: 1,
    height: h,
  });
  return h;
}

/**
 * A card with nested children: the children stack inside the card's padding
 * (below its title), `badge` children dock to the top-right corner, and the
 * card grows around its content. Returns the card's height.
 */
function layoutCard(node: ElNode, x: number, y: number, w: number, out: WireframeElement[]): number {
  const el = node.el;
  el.x = x;
  el.y = y;
  el.width = w;
  out.push(el);
  const badges = node.children.filter(
    (c): c is ElNode => c.type === 'el' && c.el.kind === 'badge',
  );
  const rest = node.children.filter((c) => !(c.type === 'el' && c.el.kind === 'badge'));
  const innerX = x + CARD_PAD;
  const innerW = w - 2 * CARD_PAD;
  let cy = y + (el.label ? 44 : CARD_PAD);
  for (const c of rest) {
    if (c.type === 'row') {
      cy += layoutRow(c, innerX, innerW, cy, out) + CARD_CHILD_GAP;
      continue;
    }
    resolveStackSize(c.el, innerW);
    c.el.x = innerX;
    c.el.y = cy;
    out.push(c.el);
    cy += c.el.height + CARD_CHILD_GAP;
  }
  const contentBottom = rest.length > 0 ? cy - CARD_CHILD_GAP : cy;
  el.height = Math.max(el.hSet ? el.height : 0, contentBottom + CARD_PAD - y, 72);
  for (const b of badges) {
    b.el.x = x + w - CARD_PAD - b.el.width;
    b.el.y = y + 12;
    out.push(b.el);
  }
  return el.height;
}

/** Per-kind default sizes when no `WxH` is given. Texts auto-size to label. */
function defaultSize(kind: WireframeKind, label: string): { width: number; height: number } {
  switch (kind) {
    case 'text':
    case 'link':
      return { width: Math.max(60, label.length * 9), height: 24 };
    case 'heading':
      return { width: Math.max(80, label.length * 12), height: 30 };
    case 'breadcrumb':
      return { width: Math.max(120, label.length * 8), height: 20 };
    case 'input':
    case 'select':
    case 'search':
      return { width: 320, height: 36 };
    case 'textarea':
      return { width: 320, height: 96 };
    case 'button':
      return { width: 140, height: 40 };
    case 'table':
      return { width: 640, height: 240 };
    case 'card':
      return { width: 300, height: 160 };
    case 'image':
      return { width: 240, height: 140 };
    case 'chart':
      return { width: 320, height: 180 };
    case 'tabs':
      return { width: 480, height: 40 };
    case 'list':
      // one row per item (`|`-separated); fall back to a single row.
      return { width: 320, height: Math.max(1, splitItems(label).length) * 40 };
    case 'badge':
      return { width: Math.max(56, label.length * 8 + 20), height: 24 };
    case 'avatar':
      return { width: 40, height: 40 };
    case 'progress':
      return { width: 240, height: 10 };
    case 'toggle':
      return { width: 44, height: 24 };
    case 'checkbox':
    case 'radio':
      return { width: Math.max(120, label.length * 8 + 28), height: 20 };
    case 'icon':
      return { width: 24, height: 24 };
    case 'divider':
      return { width: 320, height: 1 };
    default:
      return { width: 160, height: 32 };
  }
}

const KIND_RE =
  /^(rect|ellipse|button|text|heading|input|card|image|table|navbar|sidebar|tabs|list|select|search|textarea|checkbox|radio|toggle|badge|avatar|progress|divider|breadcrumb|chart|icon|link)\b/i;
const VARIANT_RE = /\b(primary|secondary|danger|success|warning|info|ai|active|muted)\b/i;

const NAV_RE = /\s*->\s*([\w-]+)\s*$/;

function parseWireframeElement(
  line: string,
): { el: WireframeElement; legacy: boolean } | null {
  const kindMatch = KIND_RE.exec(line);
  if (!kindMatch) return null;
  const kind = kindMatch[1]!.toLowerCase() as WireframeKind;
  const afterKind = line.slice(kindMatch[0].length).trim();

  // Pull off an optional trailing `-> ScreenName` (element-level navigation)
  // before parsing the rest, so the target can't be mistaken for a variant.
  const navMatch = NAV_RE.exec(afterKind);
  const navTo = navMatch ? navMatch[1] : undefined;
  const rest = navMatch ? afterKind.slice(0, navMatch.index).trim() : afterKind;

  const labelMatch = QUOTED.exec(rest);
  const label = labelMatch ? unescapeQuoted(labelMatch[1]!) : '';
  const afterLabel = labelMatch ? rest.slice(labelMatch.index + labelMatch[0].length).trim() : rest;

  // Positions come from the layout pass, never from the line. A bare `x,y`
  // after the label is the retired coordinate dialect — flag it so the caller
  // can steer regeneration instead of silently mis-rendering an old file.
  const legacy = LEGACY_COORDS.test(` ${afterLabel} `);

  let { width, height } = defaultSize(kind, label);
  let wSet = false;
  let hSet = false;
  const sizeMatch = SIZE.exec(afterLabel);
  if (sizeMatch) {
    width = parseInt(sizeMatch[1]!, 10);
    height = parseInt(sizeMatch[2]!, 10);
    wSet = true;
    hSet = true;
  }

  // An optional trailing bareword opts the element into semantic color.
  const variantMatch = VARIANT_RE.exec(afterLabel);
  const el: WireframeElement = { kind, label, x: 0, y: 0, width, height };
  if (wSet) el.wSet = true;
  if (hSet) el.hSet = true;
  if (variantMatch) el.variant = variantMatch[1]!.toLowerCase() as WireframeVariant;
  if (navTo) el.navTo = navTo;
  return { el, legacy };
}

function unescapeQuoted(s: string): string {
  // Single left-to-right pass so each `\X` is consumed once: `\\`→`\`, `\"`→`"`,
  // and `\n`/`\t` become real line breaks / tabs (agents write `\n` in a label
  // to get a title + subtitle, e.g. `card "Name\n$28 · In stock"`).
  return s.replace(/\\([\\"nt])/g, (_m, c: string) =>
    c === 'n' ? '\n' : c === 't' ? '\t' : c,
  );
}

// ---------- Wireframes renderer ----------

const DEFAULT_SCREEN_W = 1280; // desktop webapp frame (override: `screen Name WxH`)
const DEFAULT_SCREEN_H = 800;
const SCREEN_GAP_X = 120;
const SCREEN_GAP_Y = 120;
// Screens stack in one column: side-by-side rows shrank every screen to an
// illegible size once the viewer fitted the whole board, and a single column
// is what lets the viewer land on the FIRST screen with the next one's top
// edge peeking below as the cue that there is more.
const COLUMNS = 1;
const TITLE_H = 32; // screen-name row drawn ABOVE the outline
const DESC_H = 22; // extra headroom for a screen description subtitle
const NAVBAR_H = 56;
const SIDEBAR_W = 240;
const TABLE_HEADER_H = 36;
const TABLE_ROW_H = 36;

// Oxygen UI-derived palette — light surfaces, neutral borders, and the WSO2
// brand orange for primary actions / active navigation, so a wireframe reads
// like the real product instead of a wall of gray. Structure is still low-fi
// (hand-drawn boxes); only the colors follow Oxygen.
const BRAND = '#fa7b3f'; // Oxygen primary.main (WSO2 orange)
const BRAND_DARK = '#e74420'; // gradient dark end
const BRAND_TINT = '#fff0e8'; // pale orange for active/hover surfaces
const STROKE = '#c4c9d2'; // element borders (neutral)
const SCREEN_STROKE = '#b3b9c4'; // screen frame border
const FILL_NAV = '#ffffff'; // top bar surface
const FILL_CHROME = '#f5f4f2'; // sidebar, table header (warm near-white)
const FILL_SOFT = '#f4f5f7'; // generic rect / ellipse
const FILL_CARD = '#ffffff'; // cards/panels are white with a border
const FILL_BUTTON = '#f4f5f7'; // secondary (non-primary) button

/** Split a pipe-separated label ("Home | Risks | Reports") into items. */
function splitItems(label: string): string[] {
  return label
    .split('|')
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

/**
 * Split a chrome (navbar/sidebar) item into the text it draws and its optional
 * `-> Screen` target. Chrome is how a real webapp reaches its top-level views,
 * so each item navigates independently — a target on the whole element could
 * only ever name one destination. Applies to navbar/sidebar ONLY: `splitItems`
 * is shared with lists, tabs, table columns and card stat tiles, whose text is
 * content rather than navigation.
 */
function parseChromeItem(item: string): { text: string; target?: string } {
  const m = /^(.*?)\s*->\s*([\w-]+)$/.exec(item);
  if (!m) return { text: item };
  const text = m[1]!.trim();
  // An arrow with nothing in front of it ("-> Screen") has no label — draw the
  // clause literally rather than producing an invisible full-row hotspot.
  if (!text) return { text: item };
  return { text, target: m[2]! };
}

/**
 * Read a progress fraction from a label — accepts "60%", "0.6", or "3/4".
 * Falls back to 0.5 so a bare `progress` still draws something sensible.
 */
function parseFraction(label: string): number {
  const pct = /(\d+(?:\.\d+)?)\s*%/.exec(label);
  if (pct) return clamp01(parseFloat(pct[1]!) / 100);
  const frac = /(\d+(?:\.\d+)?)\s*\/\s*(\d+(?:\.\d+)?)/.exec(label);
  if (frac) {
    const d = parseFloat(frac[2]!);
    return d ? clamp01(parseFloat(frac[1]!) / d) : 0.5;
  }
  const dec = /^\s*(0?\.\d+|1(?:\.0+)?)\s*$/.exec(label);
  if (dec) return clamp01(parseFloat(dec[1]!));
  return 0.5;
}

function clamp01(n: number): number {
  return Math.max(0, Math.min(1, n));
}

// Accent color for flow markers + screen number badges. Picked to read
// against the standard wireframe palette without competing with element
// fills.
const FLOW_ACCENT = '#1971c2';

// Semantic accents — used ONLY when an element opts in via a variant. The
// stroke/text color carries the meaning; the fill is a pale tint of it so a
// badge or primary button reads as colored without shouting. Everything
// unvarianted stays on the grayscale palette above.
const ACCENT_STROKE: Record<WireframeVariant, string> = {
  primary: BRAND,
  secondary: '#5f6b7a',
  danger: '#d92d20',
  success: '#2e7d32',
  warning: '#ed6c02',
  info: '#0288d1',
  ai: '#7048e8',
  active: BRAND,
  muted: '#6c757d',
};
const ACCENT_FILL: Record<WireframeVariant, string> = {
  primary: BRAND_TINT,
  secondary: FILL_SOFT,
  danger: '#fdecea',
  success: '#e8f5e9',
  warning: '#fff4e5',
  info: '#e5f6fd',
  ai: '#f3f0ff',
  active: BRAND_TINT,
  muted: '#f1f3f5',
};

interface WireframeRenderOpts {
  /** Prototype mode: one-screen scene with the canvas decorations suppressed —
   *  navigation lives in the model's hotspots, not on the elements. Maps
   *  lowercased screen name → canonical name. `chromeHotspots` is an out-param:
   *  navbar/sidebar item geometry is computed HERE during rendering (the layout
   *  pass never positions chrome), so the renderer is the only place that can
   *  report those bounds. */
  prototype?: {
    validTargets: Map<string, string>;
    chromeHotspots?: PrototypeHotspot[];
  };
}

function renderWireframes(ast: WireframeAst, opts?: WireframeRenderOpts): ExcalidrawElement[] {
  const out: ExcalidrawElement[] = [];
  // Screen-name → 1-based number, shown as "Screen N" in the corner and in
  // each element's `→ Screen N · Name` navigation marker. Precomputed so an
  // element can point at a screen that appears later in the file.
  const screenNumber = new Map<string, number>();
  ast.screens.forEach((s, i) => screenNumber.set(s.name.toLowerCase(), i + 1));
  // Lowercase screen name → canonical name, so a chrome item's `-> Screen`
  // target can be compared against the screen currently being rendered
  // (case-insensitively) to decide which rail entry is "active".
  const screenNameByLower = new Map<string, string>();
  ast.screens.forEach((s) => screenNameByLower.set(s.name.toLowerCase(), s.name));

  // Flow membership, shown on the canvas so the grid answers "whose screen is
  // this?" without entering the prototype. A screen listed by several flows
  // reads "Common" rather than a list — the names are the personas' walkthrough
  // labels, and stacking them makes the marker unreadable at grid zoom.
  const flowLabelByLower = new Map<string, string>();
  for (const s of ast.screens) {
    const owning = ast.namedFlows.filter((f) => f.screens.includes(s.name));
    if (owning.length === 1) flowLabelByLower.set(s.name.toLowerCase(), owning[0]!.name);
    else if (owning.length > 1) flowLabelByLower.set(s.name.toLowerCase(), 'Common');
  }

  // Variable-size screens flow left-to-right, COLUMNS per row; each row is as
  // tall as its tallest screen.
  let curX = 0;
  let curY = 0;
  let rowMaxH = 0;
  ast.screens.forEach((screen, idx) => {
    const firstEl = out.length;
    const number = idx + 1;
    if (idx > 0 && idx % COLUMNS === 0) {
      curX = 0;
      curY += rowMaxH + SCREEN_GAP_Y;
      rowMaxH = 0;
    }
    const sx = curX;
    const sy = curY;
    curX += screen.width + SCREEN_GAP_X;
    // Title block above the frame: a prominent screen name, plus an optional
    // description subtitle explaining what the view is for (or which role it
    // serves). Taller when a description is present.
    const titleH = opts?.prototype ? 0 : TITLE_H + (screen.description ? DESC_H : 0);
    rowMaxH = Math.max(rowMaxH, screen.height + titleH);
    const screenId = stableId(`screen:${screen.name}:${idx}`);

    // Screen name + number badge sit ABOVE the outline so chrome (navbar)
    // can occupy the screen's full interior.
    if (!opts?.prototype) {
      out.push(
        makeText(
          stableId(`screen-label:${screen.name}:${idx}`),
          sx,
          sy,
          screen.width - 60,
          24,
          screen.name,
          20,
          'left',
        ),
      );
      // Right-aligned, so widening the box for the flow label moves nothing:
      // the text still ends at the screen's right edge and the title block
      // keeps its height.
      const flowLabel = flowLabelByLower.get(screen.name.toLowerCase());
      const marker = flowLabel ? `${flowLabel} · Screen ${number}` : `Screen ${number}`;
      const markerW = 300;
      out.push(
        withColor(
          makeText(
            stableId(`screen-num:${screen.name}:${idx}`),
            sx + screen.width - (markerW + 12),
            sy,
            markerW,
            18,
            marker,
            14,
            'right',
          ),
          FLOW_ACCENT,
        ),
      );
      if (screen.description) {
        out.push(
          withColor(
            makeText(
              stableId(`screen-desc:${screen.name}:${idx}`),
              sx,
              sy + 26,
              screen.width - 20,
              18,
              screen.description,
              14,
              'left',
            ),
            '#868e96',
          ),
        );
      }
    }
    const frameY = sy + titleH;
    out.push(makeRect(screenId, sx, frameY, screen.width, screen.height, SCREEN_STROKE, '#ffffff', null));

    // Claim a hotspot for a chrome item that names a real, different screen.
    const claimChromeItem = (
      target: string | undefined,
      x: number, y: number, width: number, height: number,
    ): void => {
      const proto = opts?.prototype;
      if (!target || !proto?.chromeHotspots) return;
      const canonical = proto.validTargets.get(target.toLowerCase());
      if (!canonical || canonical === screen.name) return;
      proto.chromeHotspots.push({ x, y, width, height, target: canonical });
    };

    for (const el of screen.elements) {
      // Coordinates are screen-local from the outline's top-left corner —
      // what the author writes is where it lands, no hidden padding.
      const ex = sx + el.x;
      const ey = frameY + el.y;
      // Left-aligned text auto-sizes to its content and never wraps, so a long
      // line (e.g. a rail comment) would bleed past the screen's right edge.
      // Clip it to the frame's inner right edge with an ellipsis — the same
      // treatment table cells get — so nothing renders outside the screen.
      const clipText = (label: string, fontSize: number): string =>
        truncateLabel(label, fitChars(sx + screen.width - 16 - ex, fontSize));
      // Identity is SCREEN-relative (el.x/el.y), never the canvas position
      // (ex/ey). Screens stack in one column, so growing one screen shifts
      // every screen below it; keying identity on canvas coordinates gave
      // every one of those elements a new id, which made a viewer diff read
      // an untouched screen as wholly replaced. The screen name still keeps
      // ids unique across screens.
      const eid = stableId(`el:${screen.name}:${el.kind}:${el.label}:${el.x}:${el.y}`);
      // A `-> ScreenName` on this element draws a navigation marker right
      // beside it, so the reader sees exactly which control goes where.
      if (!opts?.prototype && el.navTo) {
        const num = screenNumber.get(el.navTo.toLowerCase());
        if (num !== undefined) {
          const label = `→ Screen ${num} · ${el.navTo}`;
          const markerW = Math.max(120, label.length * 8);
          const markerH = 16;
          // Beside the control when there is room; below it when there is not.
          // "Room" means the marker would neither cross the screen's right edge
          // nor land on a sibling laid out to the right on the same row — the
          // common case being two buttons side by side, where a marker drawn
          // across the neighbour made BOTH unreadable.
          const besideX = ex + el.width + 10;
          const besideY = ey + Math.max(0, (el.height - markerH) / 2);
          const screenRight = sx + screen.width - 16;
          const crossesEdge = besideX + markerW > screenRight;
          const hitsSibling = screen.elements.some((other) => {
            if (other === el) return false;
            const ox = sx + other.x;
            const oy = frameY + other.y;
            return (
              ox < besideX + markerW &&
              ox + other.width > besideX &&
              oy < besideY + markerH &&
              oy + other.height > besideY
            );
          });
          const below = crossesEdge || hitsSibling;
          const mx = below ? Math.min(ex, screenRight - markerW) : besideX;
          const my = below ? ey + el.height + 4 : besideY;
          out.push(
            withColor(
              makeText(
                stableId(`nav:${screen.name}:${el.label}:${el.navTo}:${el.x}:${el.y}`),
                mx,
                my,
                markerW,
                markerH,
                label,
                13,
                'left',
              ),
              FLOW_ACCENT,
            ),
          );
        }
      }
      switch (el.kind) {
        case 'navbar': {
          // White top bar laid out like a real webapp header: the first item
          // (the app/brand name) sits on the LEFT in the brand color; the
          // remaining nav links are grouped on the RIGHT, ending at a
          // notification bell + account avatar in the top-right corner.
          out.push(makeRect(eid, sx, frameY, screen.width, NAVBAR_H, STROKE, FILL_NAV, null));
          const items = splitItems(el.label).map(parseChromeItem);
          const textY = frameY + (NAVBAR_H - 18) / 2;
          if (items[0]) {
            out.push(
              withColor(
                makeText(stableId(`${eid}:brand`), sx + 24, textY, Math.max(60, items[0].text.length * 9), 18, items[0].text, 14, 'left'),
                BRAND,
              ),
            );
          }
          const AV = 30; // avatar diameter
          const avx = sx + screen.width - 24 - AV;
          const avy = frameY + (NAVBAR_H - AV) / 2;
          // Notification button: a plain circle with a small brand "unread"
          // badge at its top-right, set a clear gap left of the avatar.
          const BELL = 24;
          const bx = avx - 16 - BELL;
          const by = frameY + (NAVBAR_H - BELL) / 2;
          // Nav links right-to-left, ending well left of the bell.
          let cursor = bx - 28;
          for (let i = items.length - 1; i >= 1; i--) {
            const item = items[i]!;
            const w = Math.max(40, item.text.length * 8.5);
            cursor -= w;
            out.push(
              makeText(stableId(`${eid}:item:${item.text}:${i}`), cursor, textY, w, 18, item.text, 14, 'left'),
            );
            claimChromeItem(item.target, cursor, textY, w, 18);
            cursor -= 32;
          }
          out.push({ ...makeRect(stableId(`${eid}:bell`), bx, by, BELL, BELL, STROKE, FILL_NAV, { type: 3 }), type: 'ellipse' });
          out.push({ ...makeRect(stableId(`${eid}:bell:dot`), bx + BELL - 7, by + 1, 6, 6, BRAND, BRAND, { type: 3 }), type: 'ellipse' });
          // Account avatar: head + shoulders inside a circle — the signed-in user.
          out.push({ ...makeRect(stableId(`${eid}:user`), avx, avy, AV, AV, STROKE, FILL_SOFT, { type: 3 }), type: 'ellipse' });
          out.push({ ...makeRect(stableId(`${eid}:user:head`), avx + AV / 2 - 5, avy + 6, 10, 10, STROKE, '#ffffff', { type: 3 }), type: 'ellipse' });
          out.push({ ...makeRect(stableId(`${eid}:user:body`), avx + AV / 2 - 8, avy + AV - 8, 16, 12, STROKE, '#ffffff', { type: 3 }), type: 'ellipse' });
          break;
        }
        case 'sidebar': {
          out.push(
            makeRect(eid, sx, frameY + NAVBAR_H, SIDEBAR_W, screen.height - NAVBAR_H, STROKE, FILL_CHROME, null),
          );
          const sidebarItems = splitItems(el.label).map(parseChromeItem);
          // The active row is the one whose target names the screen currently
          // being rendered — that's the entry a real rail would highlight
          // after navigating here. Unannotated chrome has no targets, so
          // nothing matches and the old "first item" default still applies,
          // keeping canvas output byte-identical for unannotated sidebars.
          const activeIndex = sidebarItems.findIndex((item) => {
            if (!item.target) return false;
            return screenNameByLower.get(item.target.toLowerCase()) === screen.name;
          });
          sidebarItems.forEach((item, i) => {
            const iy = frameY + NAVBAR_H + 12 + i * 40;
            const active = activeIndex === -1 ? i === 0 : i === activeIndex;
            if (active) {
              // A brand-tinted pill behind the active item, Oxygen-style.
              out.push(makeRect(stableId(`${eid}:active:${i}`), sx + 8, iy - 4, SIDEBAR_W - 16, 32, BRAND_TINT, BRAND_TINT, { type: 3 }));
            }
            // The whole row is the click target, as in a real sidebar — not
            // just the glyphs.
            claimChromeItem(item.target, sx + 8, iy - 4, SIDEBAR_W - 16, 32);
            out.push(
              withColor(
                makeText(
                  stableId(`${eid}:item:${item.text}:${i}`),
                  sx + 20,
                  iy + 4,
                  SIDEBAR_W - 40,
                  18,
                  item.text,
                  14,
                  'left',
                ),
                active ? BRAND_DARK : '#1e1e1e',
              ),
            );
          });
          break;
        }
        case 'table': {
          const cols = splitItems(el.label);
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, 'transparent', null));
          out.push(makeRect(stableId(`${eid}:hdr`), ex, ey, el.width, TABLE_HEADER_H, STROKE, FILL_CHROME, null));
          const colW = el.width / Math.max(1, cols.length);
          cols.forEach((col, i) => {
            out.push(
              makeText(
                stableId(`${eid}:col:${col}:${i}`),
                ex + 12 + i * colW,
                ey + (TABLE_HEADER_H - 18) / 2,
                Math.max(60, col.length * 9),
                18,
                truncateLabel(col, fitChars(colW - 16, 14)),
                14,
                'left',
              ),
            );
            if (i > 0) {
              out.push(makeLine(stableId(`${eid}:vline:${i}`), ex + i * colW, ey, 0, el.height));
            }
          });
          // Body: draw a divider under the header, then either the supplied
          // `row` data (realistic content) or empty ruled lines as a
          // placeholder. Cells are clipped to their column width.
          const rows = el.rows ?? [];
          const bodyTop = ey + TABLE_HEADER_H;
          const maxRows = Math.floor((el.height - TABLE_HEADER_H) / TABLE_ROW_H);
          const shown = rows.slice(0, Math.max(0, maxRows));
          shown.forEach((row, r) => {
            const ry = bodyTop + r * TABLE_ROW_H;
            if (r > 0) out.push(makeLine(stableId(`${eid}:hline:${r}`), ex, ry, el.width, 0));
            row.slice(0, cols.length).forEach((cell, c) => {
              if (!cell) return;
              out.push(
                makeText(
                  stableId(`${eid}:cell:${r}:${c}`),
                  ex + 12 + c * colW,
                  ry + (TABLE_ROW_H - 16) / 2,
                  Math.max(40, colW - 20),
                  16,
                  truncateLabel(cell, fitChars(colW - 20, 13)),
                  13,
                  'left',
                ),
              );
            });
          });
          // Ruled placeholder lines for the remaining empty rows.
          for (
            let r = Math.max(1, shown.length);
            r < maxRows;
            r++
          ) {
            const ry = bodyTop + r * TABLE_ROW_H;
            out.push(makeLine(stableId(`${eid}:rule:${r}`), ex, ry, el.width, 0));
          }
          break;
        }
        case 'image':
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, FILL_SOFT, null));
          out.push(makeLine(stableId(`${eid}:d1`), ex, ey, el.width, el.height));
          out.push(makeLine(stableId(`${eid}:d2`), ex, ey + el.height, el.width, -el.height));
          if (el.label) {
            out.push(
              makeText(
                stableId(`${eid}:label`),
                ex + 8,
                ey + el.height + 4,
                Math.max(60, el.label.length * 8),
                16,
                el.label,
                12,
                'left',
              ),
            );
          }
          break;
        case 'input':
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, '#ffffff'));
          out.push(
            makeText(
              stableId(`${eid}:label`),
              ex + 10,
              ey + Math.max(0, (el.height - 16) / 2),
              el.width - 20,
              16,
              el.label,
              14,
              'left',
            ),
          );
          break;
        case 'card': {
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, FILL_CARD));
          // A `|`-split label renders as a stat tile — small muted metric
          // label, BIG value, small caption ("Open items | 47 | across 5
          // audits"). A plain one-part label stays a simple panel title, so
          // container-style cards are unaffected.
          const parts = splitItems(el.label);
          if (parts.length >= 2) {
            out.push(
              withColor(
                makeText(stableId(`${eid}:metric`), ex + 14, ey + 14, el.width - 28, 16, parts[0]!, 12, 'left'),
                '#868e96',
              ),
            );
            out.push(makeText(stableId(`${eid}:value`), ex + 14, ey + 38, el.width - 28, 30, parts[1]!, 26, 'left'));
            if (parts[2]) {
              const capColor = el.variant ? ACCENT_STROKE[el.variant] : '#868e96';
              out.push(
                withColor(
                  makeText(stableId(`${eid}:cap`), ex + 14, ey + el.height - 28, el.width - 28, 16, parts[2]!, 12, 'left'),
                  capColor,
                ),
              );
            }
          } else {
            out.push(
              makeText(stableId(`${eid}:label`), ex + 12, ey + 12, el.width - 24, 18, el.label, 14, 'left'),
            );
          }
          break;
        }
        case 'heading': {
          // Section headings should read as headings — larger type, plus a
          // short brand rule under them so the eye finds the structure.
          out.push(makeText(eid, ex, ey, el.width, Math.max(el.height, 28), el.label, 22, 'left'));
          const ruleW = Math.min(el.width || 9999, Math.max(40, el.label.length * 12));
          out.push(withColor(makeLine(stableId(`${eid}:rule`), ex, ey + 30, ruleW, 0), BRAND));
          break;
        }
        case 'rect':
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, FILL_SOFT));
          out.push(
            makeText(
              stableId(`${eid}:label`),
              ex + 6,
              ey + 6,
              el.width - 12,
              Math.max(14, el.height - 12),
              el.label,
              14,
              'left',
            ),
          );
          break;
        case 'button': {
          // Default: gray outline button. A variant paints it: `primary`
          // fills solid ink with white text (the one dominant CTA); the
          // semantic variants (danger/success/…) tint the fill + border +
          // text so the action's intent reads at a glance.
          let bStroke = STROKE;
          let bFill = FILL_BUTTON;
          let bText = '#1e1e1e';
          if (el.variant === 'primary') {
            bStroke = BRAND;
            bFill = BRAND;
            bText = '#ffffff';
          } else if (el.variant && el.variant !== 'secondary') {
            bStroke = ACCENT_STROKE[el.variant];
            bFill = ACCENT_FILL[el.variant];
            bText = ACCENT_STROKE[el.variant];
          }
          out.push(makeRect(eid, ex, ey, el.width, el.height, bStroke, bFill, { type: 3 }));
          out.push(
            withColor(
              makeText(
                stableId(`${eid}:label`),
                ex,
                ey + Math.max(0, (el.height - 14) / 2),
                el.width,
                14,
                el.label,
                14,
                'center',
              ),
              bText,
            ),
          );
          break;
        }
        case 'ellipse':
          out.push({
            ...makeRect(eid, ex, ey, el.width, el.height, STROKE, FILL_SOFT),
            type: 'ellipse',
          });
          out.push(
            makeText(
              stableId(`${eid}:label`),
              ex,
              ey + Math.max(0, (el.height - 14) / 2),
              el.width,
              14,
              el.label,
              14,
              'center',
            ),
          );
          break;
        case 'text':
          out.push(
            makeText(eid, ex, ey, el.width, el.height, clipText(el.label, 14), 14, 'left'),
          );
          break;
        case 'divider':
          // Horizontal rule by default; a taller-than-wide divider draws a
          // VERTICAL rule — use one to separate the two columns of a detail
          // screen (`divider "" 760,180 1x420`).
          if (el.height > el.width) out.push(makeLine(eid, ex, ey, 0, el.height));
          else out.push(makeLine(eid, ex, ey, el.width, 0));
          break;
        case 'breadcrumb':
          out.push(withColor(makeText(eid, ex, ey, el.width, el.height, clipText(el.label, 13), 13, 'left'), '#868e96'));
          break;
        case 'link':
          out.push(withColor(makeText(eid, ex, ey, el.width, el.height, clipText(el.label, 14), 14, 'left'), ACCENT_STROKE.info));
          break;
        case 'icon':
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, FILL_SOFT, { type: 3 }));
          if (el.label) {
            out.push(makeText(stableId(`${eid}:g`), ex, ey + Math.max(0, (el.height - 12) / 2), el.width, 12, el.label.slice(0, 2), 12, 'center'));
          }
          break;
        case 'select':
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, '#ffffff'));
          out.push(makeText(stableId(`${eid}:lb`), ex + 10, ey + Math.max(0, (el.height - 16) / 2), el.width - 40, 16, el.label, 14, 'left'));
          out.push(withColor(makeText(stableId(`${eid}:ca`), ex + el.width - 24, ey + Math.max(0, (el.height - 16) / 2), 16, 16, '▾', 14, 'center'), '#868e96'));
          break;
        case 'search':
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, '#ffffff'));
          out.push(withColor(makeText(stableId(`${eid}:ic`), ex + 10, ey + Math.max(0, (el.height - 16) / 2), 16, 16, '⌕', 15, 'left'), '#868e96'));
          out.push(makeText(stableId(`${eid}:lb`), ex + 30, ey + Math.max(0, (el.height - 16) / 2), el.width - 40, 16, el.label, 14, 'left'));
          break;
        case 'textarea':
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, '#ffffff'));
          out.push(makeText(stableId(`${eid}:lb`), ex + 10, ey + 8, el.width - 20, 16, el.label, 14, 'left'));
          break;
        case 'tabs': {
          const items = splitItems(el.label);
          out.push(makeLine(stableId(`${eid}:base`), ex, ey + el.height - 1, el.width, 0));
          const tabW = el.width / Math.max(1, items.length);
          items.forEach((item, i) => {
            const active = i === 0; // first tab shown selected
            out.push(
              withColor(
                makeText(stableId(`${eid}:t:${i}`), ex + i * tabW, ey + Math.max(0, (el.height - 16) / 2) - 2, tabW, 16, item, 14, 'center'),
                active ? '#1e1e1e' : '#868e96',
              ),
            );
            if (active) {
              out.push(withColor(makeLine(stableId(`${eid}:u:${i}`), ex + i * tabW + 8, ey + el.height - 1, tabW - 16, 0), '#1e1e1e'));
            }
          });
          break;
        }
        case 'list': {
          const items = splitItems(el.label);
          const rowH = el.height / Math.max(1, items.length);
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, '#ffffff', { type: 3 }));
          items.forEach((item, i) => {
            const ry = ey + i * rowH;
            if (i > 0) out.push(makeLine(stableId(`${eid}:d:${i}`), ex, ry, el.width, 0));
            out.push(makeText(stableId(`${eid}:i:${i}`), ex + 12, ry + Math.max(0, (rowH - 16) / 2), el.width - 24, 16, item, 14, 'left'));
          });
          break;
        }
        case 'badge': {
          const v = el.variant ?? 'muted';
          out.push(makeRect(eid, ex, ey, el.width, el.height, ACCENT_STROKE[v], ACCENT_FILL[v], { type: 3 }));
          out.push(withColor(makeText(stableId(`${eid}:t`), ex, ey + Math.max(0, (el.height - 12) / 2), el.width, 12, el.label, 12, 'center'), ACCENT_STROKE[v]));
          break;
        }
        case 'avatar': {
          const stroke = el.variant ? ACCENT_STROKE[el.variant] : STROKE;
          const fill = el.variant ? ACCENT_FILL[el.variant] : FILL_SOFT;
          out.push({ ...makeRect(eid, ex, ey, el.width, el.height, stroke, fill, { type: 3 }), type: 'ellipse' });
          const initials = el.label.split(/\s+/).map((w) => w[0]).filter(Boolean).slice(0, 2).join('').toUpperCase();
          if (initials) {
            out.push(withColor(makeText(stableId(`${eid}:in`), ex, ey + Math.max(0, (el.height - 13) / 2), el.width, 13, initials, 13, 'center'), stroke));
          }
          break;
        }
        case 'progress': {
          const frac = parseFraction(el.label);
          const barFill = el.variant ? ACCENT_STROKE[el.variant] : ACCENT_STROKE.muted;
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, FILL_SOFT, { type: 3 }));
          out.push(makeRect(stableId(`${eid}:fill`), ex, ey, Math.max(2, Math.round(el.width * frac)), el.height, barFill, barFill, { type: 3 }));
          break;
        }
        case 'toggle': {
          const on = el.variant === 'active';
          const trackStroke = on ? ACCENT_STROKE.active : STROKE;
          const trackFill = on ? ACCENT_STROKE.active : FILL_BUTTON;
          out.push(makeRect(eid, ex, ey, el.width, el.height, trackStroke, trackFill, { type: 3 }));
          const knob = el.height - 6;
          const kx = on ? ex + el.width - knob - 3 : ex + 3;
          out.push({ ...makeRect(stableId(`${eid}:knob`), kx, ey + 3, knob, knob, trackStroke, '#ffffff', { type: 3 }), type: 'ellipse' });
          break;
        }
        case 'checkbox': {
          const on = el.variant === 'active';
          const box = 18;
          const by = ey + Math.max(0, (el.height - box) / 2);
          out.push(makeRect(eid, ex, by, box, box, on ? ACCENT_STROKE.active : STROKE, on ? ACCENT_STROKE.active : '#ffffff', null));
          if (on) out.push(withColor(makeText(stableId(`${eid}:ck`), ex, by + 1, box, 14, '✓', 13, 'center'), '#ffffff'));
          if (el.label) out.push(makeText(stableId(`${eid}:lb`), ex + box + 8, ey + Math.max(0, (el.height - 16) / 2), el.width - box - 8, 16, el.label, 14, 'left'));
          break;
        }
        case 'radio': {
          const on = el.variant === 'active';
          const d = 18;
          const by = ey + Math.max(0, (el.height - d) / 2);
          out.push({ ...makeRect(eid, ex, by, d, d, on ? ACCENT_STROKE.active : STROKE, '#ffffff', { type: 3 }), type: 'ellipse' });
          if (on) out.push({ ...makeRect(stableId(`${eid}:dot`), ex + 5, by + 5, d - 10, d - 10, ACCENT_STROKE.active, ACCENT_STROKE.active, { type: 3 }), type: 'ellipse' });
          if (el.label) out.push(makeText(stableId(`${eid}:lb`), ex + d + 8, ey + Math.max(0, (el.height - 16) / 2), el.width - d - 8, 16, el.label, 14, 'left'));
          break;
        }
        case 'chart': {
          out.push(makeRect(eid, ex, ey, el.width, el.height, STROKE, FILL_SOFT, { type: 3 }));
          out.push(makeLine(stableId(`${eid}:ax`), ex + 30, ey + 12, 0, el.height - 42));
          out.push(makeLine(stableId(`${eid}:ay`), ex + 30, ey + el.height - 30, el.width - 50, 0));
          const bars = 4;
          const slot = (el.width - 70) / bars;
          const bw = slot * 0.55;
          const span = Math.max(20, el.height - 70);
          for (let i = 0; i < bars; i++) {
            const bh = 24 + ((i * 41) % span);
            const bxp = ex + 40 + i * slot;
            out.push(makeRect(stableId(`${eid}:b:${i}`), bxp, ey + el.height - 30 - bh, bw, bh, STROKE, '#ced4da', { type: 3 }));
          }
          out.push(withColor(makeText(stableId(`${eid}:lb`), ex, ey + el.height - 16, el.width, 14, el.label || 'chart', 12, 'center'), '#868e96'));
          break;
        }
      }
    }
    // Every element a screen emitted — frame, title block, chrome, body — is
    // tagged with the screen it belongs to. Excalidraw preserves `customData`
    // and never renders it; it is what lets a viewer group elements per screen
    // (focus the first, follow the changed one) without geometry guesswork.
    for (let i = firstEl; i < out.length; i++) {
      out[i] = { ...out[i]!, customData: { screen: screen.name } };
    }
  });

  return out;
}

/** Override an element's stroke colour without mutating the input. */
function withColor<T extends ExcalidrawElementBase>(el: T, color: string): T {
  return { ...el, strokeColor: color };
}

/**
 * An unbound straight line from (x, y) spanning (dx, dy) — table grid rules
 * and image-placeholder diagonals. Negative spans are fine (the points are
 * relative); width/height are normalized for the bounding box.
 */
function makeLine(id: string, x: number, y: number, dx: number, dy: number): ArrowElement {
  return {
    ...baseElement(id, x, y, Math.abs(dx), Math.abs(dy)),
    type: 'line',
    strokeColor: STROKE,
    roundness: null,
    points: [
      [0, 0],
      [dx, dy],
    ],
    lastCommittedPoint: null,
    startBinding: null,
    endBinding: null,
    startArrowhead: null,
    endArrowhead: null,
  };
}

// ---------- Domain Model parser ----------

function parseDomainModelDsl(dsl: string): DomainAst {
  const ast: DomainAst = { entities: [], relations: [] };
  let currentEntity: DomainEntity | null = null;

  for (const rawLine of dsl.split(/\r?\n/)) {
    const line = rawLine.replace(/\s+$/, '');
    if (line.trim().length === 0) continue;
    if (line.trim().startsWith('//') || line.trim().startsWith('#')) continue;

    const indented = /^\s+/.test(line);
    const trimmed = line.trim();

    if (!indented) {
      const entityMatch = /^entity\s+([\w-]+)\b/i.exec(trimmed);
      if (entityMatch) {
        currentEntity = { name: entityMatch[1]!, attrs: [] };
        ast.entities.push(currentEntity);
        continue;
      }
      const relMatch =
        /^relation\s+([\w-]+)\s*-\s*(?:\[([^\]]*)\])?\s*->\s*([\w-]+)(?:\s+"([^"]*)")?/i.exec(
          trimmed,
        );
      if (relMatch) {
        ast.relations.push({
          from: relMatch[1]!,
          to: relMatch[3]!,
          cardinality: (relMatch[2] ?? '').trim(),
          label: relMatch[4] ?? '',
        });
        currentEntity = null;
        continue;
      }
      const undirectedMatch = /^relation\s+([\w-]+)\s*--\s*([\w-]+)(?:\s+"([^"]*)")?/i.exec(trimmed);
      if (undirectedMatch) {
        ast.relations.push({
          from: undirectedMatch[1]!,
          to: undirectedMatch[2]!,
          cardinality: '',
          label: undirectedMatch[3] ?? '',
        });
        currentEntity = null;
        continue;
      }
      currentEntity = null;
      continue;
    }

    if (currentEntity) {
      const attrMatch = /^([\w-]+)\s*:\s*(.+)$/.exec(trimmed);
      if (attrMatch) {
        currentEntity.attrs.push({ name: attrMatch[1]!, type: attrMatch[2]!.trim() });
      }
    }
  }

  return ast;
}

// ---------- Domain Model renderer ----------

const ENTITY_W = 220;
const ENTITY_HEADER_H = 32;
const ATTR_LINE_H = 22;
const ENTITY_GAP_X = 100;
const ENTITY_GAP_Y = 80;

// ---------- Domain model layout (Sugiyama-style) ----------
//
// The renderer used to slot entities into a fixed 3-column grid and draw
// straight arrows edge-to-edge between them, which produced lines that
// crossed unrelated entity boxes whenever the graph topology didn't
// happen to match the grid. The layered layout below:
//
//   1. Breaks cycles by greedy DFS (back-edges flagged, not drawn straight)
//   2. Assigns layers via longest-path topological sort
//   3. Reorders within each layer using barycenter sweeps (up to 24)
//   4. Places coordinates with each layer centred horizontally
//
// Edges are then rendered as elbowed arrows so Excalidraw routes them
// between rows of entities. Re-adding an entity is automatically picked
// up because the layout is a pure function of the parsed AST.

const ENTITY_BACK_EDGE_COLOR = '#868e96';
const ENTITY_FWD_EDGE_COLOR = '#1e1e1e';

interface DomainLayoutNode {
  name: string;
  id: string;
  layer: number;
  order: number;
  x: number;
  y: number;
  w: number;
  h: number;
}

interface DomainLayoutEdge {
  from: string;
  to: string;
  kind: 'forward' | 'back';
  relIdx: number;
}

interface DomainLayout {
  nodes: Map<string, DomainLayoutNode>;
  edges: DomainLayoutEdge[];
}

function entityHeight(e: DomainEntity): number {
  return ENTITY_HEADER_H + Math.max(1, e.attrs.length) * ATTR_LINE_H + 12;
}

export function layoutDomainModel(ast: DomainAst): DomainLayout {
  const nodeNames = ast.entities.map((e) => e.name.toLowerCase());
  const nodeIndexByName = new Map(nodeNames.map((n, i) => [n, i]));
  const entityByName = new Map(ast.entities.map((e) => [e.name.toLowerCase(), e]));

  // Filter relations to those with known endpoints.
  const relList = ast.relations
    .map((rel, idx) => ({
      from: rel.from.toLowerCase(),
      to: rel.to.toLowerCase(),
      idx,
    }))
    .filter((r) => nodeIndexByName.has(r.from) && nodeIndexByName.has(r.to));

  // 1. Detect back edges via DFS (greedy cycle-break).
  const tempAdj = new Map<string, string[]>();
  for (const n of nodeNames) tempAdj.set(n, []);
  for (const r of relList) tempAdj.get(r.from)!.push(r.to);

  const VISITING = 1;
  const VISITED = 2;
  const dfsState = new Map<string, number>();
  const backEdgeKeys = new Set<string>();

  const dfs = (node: string): void => {
    dfsState.set(node, VISITING);
    for (const to of tempAdj.get(node)!) {
      const s = dfsState.get(to);
      if (s === VISITING) {
        backEdgeKeys.add(`${node}->${to}`);
      } else if (s !== VISITED) {
        dfs(to);
      }
    }
    dfsState.set(node, VISITED);
  };
  for (const n of nodeNames) if (!dfsState.has(n)) dfs(n);

  const forwardEdges: typeof relList = [];
  const backEdges: typeof relList = [];
  for (const r of relList) {
    if (backEdgeKeys.has(`${r.from}->${r.to}`)) backEdges.push(r);
    else forwardEdges.push(r);
  }

  // 2. Longest-path layering over forward edges only.
  const fwdAdj = new Map<string, string[]>();
  const fwdInDeg = new Map<string, number>();
  for (const n of nodeNames) {
    fwdAdj.set(n, []);
    fwdInDeg.set(n, 0);
  }
  for (const e of forwardEdges) {
    fwdAdj.get(e.from)!.push(e.to);
    fwdInDeg.set(e.to, (fwdInDeg.get(e.to) ?? 0) + 1);
  }
  const layer = new Map<string, number>();
  const remaining = new Map(fwdInDeg);
  const queue: string[] = [];
  for (const n of nodeNames) {
    if ((remaining.get(n) ?? 0) === 0) {
      layer.set(n, 0);
      queue.push(n);
    }
  }
  while (queue.length) {
    const node = queue.shift()!;
    const lvl = layer.get(node) ?? 0;
    for (const to of fwdAdj.get(node)!) {
      const next = Math.max(layer.get(to) ?? 0, lvl + 1);
      layer.set(to, next);
      const rem = (remaining.get(to) ?? 0) - 1;
      remaining.set(to, rem);
      if (rem === 0) queue.push(to);
    }
  }
  // Fallback for any node not yet placed (shouldn't happen after cycle
  // break, but stays robust against malformed AST).
  for (const n of nodeNames) if (!layer.has(n)) layer.set(n, 0);

  // 3. Bucket into layers, seed order by AST appearance.
  const maxLayer = Math.max(0, ...Array.from(layer.values()));
  const layerNodes: string[][] = Array.from({ length: maxLayer + 1 }, () => []);
  for (const n of nodeNames) layerNodes[layer.get(n)!]!.push(n);
  for (const ln of layerNodes) {
    ln.sort((a, b) => (nodeIndexByName.get(a)! - nodeIndexByName.get(b)!));
  }

  // 4. Barycenter sweeps — 24 iterations or until stable.
  const orderOf = new Map<string, number>();
  const refreshOrder = () => {
    for (const ln of layerNodes) ln.forEach((n, i) => orderOf.set(n, i));
  };
  refreshOrder();

  const arraysEqual = (a: string[], b: string[]) => {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
    return true;
  };

  for (let iter = 0; iter < 24; iter++) {
    let changed = false;
    // Down sweep — order each layer L (L>0) by avg position in layer L-1.
    for (let l = 1; l < layerNodes.length; l++) {
      const next = [...layerNodes[l]!];
      const baryFor = (node: string): number => {
        const orders: number[] = [];
        for (const e of forwardEdges) {
          if (e.to === node && layer.get(e.from) === l - 1) {
            orders.push(orderOf.get(e.from) ?? 0);
          }
        }
        if (orders.length === 0) return orderOf.get(node) ?? 0;
        return orders.reduce((a, b) => a + b, 0) / orders.length;
      };
      next.sort((a, b) => {
        const diff = baryFor(a) - baryFor(b);
        if (diff !== 0) return diff;
        return (orderOf.get(a) ?? 0) - (orderOf.get(b) ?? 0);
      });
      if (!arraysEqual(next, layerNodes[l]!)) {
        layerNodes[l] = next;
        changed = true;
      }
    }
    refreshOrder();
    // Up sweep.
    for (let l = layerNodes.length - 2; l >= 0; l--) {
      const next = [...layerNodes[l]!];
      const baryFor = (node: string): number => {
        const orders: number[] = [];
        for (const e of forwardEdges) {
          if (e.from === node && layer.get(e.to) === l + 1) {
            orders.push(orderOf.get(e.to) ?? 0);
          }
        }
        if (orders.length === 0) return orderOf.get(node) ?? 0;
        return orders.reduce((a, b) => a + b, 0) / orders.length;
      };
      next.sort((a, b) => {
        const diff = baryFor(a) - baryFor(b);
        if (diff !== 0) return diff;
        return (orderOf.get(a) ?? 0) - (orderOf.get(b) ?? 0);
      });
      if (!arraysEqual(next, layerNodes[l]!)) {
        layerNodes[l] = next;
        changed = true;
      }
    }
    refreshOrder();
    if (!changed) break;
  }

  // 5. Place coordinates — each layer is centred horizontally relative to
  // the widest layer.
  const layerHeights = layerNodes.map((ln) => {
    let h = 0;
    for (const n of ln) {
      const e = entityByName.get(n)!;
      h = Math.max(h, entityHeight(e));
    }
    return h;
  });
  const layerY: number[] = [];
  let yCursor = 0;
  for (let l = 0; l < layerNodes.length; l++) {
    layerY.push(yCursor);
    yCursor += layerHeights[l]! + ENTITY_GAP_Y;
  }
  const widthOfLayer = (count: number) =>
    count > 0 ? count * ENTITY_W + (count - 1) * ENTITY_GAP_X : 0;
  const maxWidth = Math.max(0, ...layerNodes.map((ln) => widthOfLayer(ln.length)));

  const nodes = new Map<string, DomainLayoutNode>();
  for (let l = 0; l < layerNodes.length; l++) {
    const ln = layerNodes[l]!;
    const offset = (maxWidth - widthOfLayer(ln.length)) / 2;
    for (let i = 0; i < ln.length; i++) {
      const name = ln[i]!;
      const e = entityByName.get(name)!;
      const h = entityHeight(e);
      const x = offset + i * (ENTITY_W + ENTITY_GAP_X);
      const y = layerY[l]! + (layerHeights[l]! - h) / 2;
      nodes.set(name, {
        name: e.name,
        id: stableId(`entity:${e.name}:${nodeIndexByName.get(name)}`),
        layer: l,
        order: i,
        x,
        y,
        w: ENTITY_W,
        h,
      });
    }
  }

  // 6. Edges list, preserving DSL declaration order.
  const edges: DomainLayoutEdge[] = relList.map((r) => ({
    from: r.from,
    to: r.to,
    kind: backEdgeKeys.has(`${r.from}->${r.to}`) ? 'back' : 'forward',
    relIdx: r.idx,
  }));

  return { nodes, edges };
}

function renderDomainModel(ast: DomainAst): ExcalidrawElement[] {
  const out: ExcalidrawElement[] = [];
  const layout = layoutDomainModel(ast);

  // Entities — same visual recipe as before, just positioned by the
  // layered layout instead of a fixed grid.
  for (const entity of ast.entities) {
    const node = layout.nodes.get(entity.name.toLowerCase());
    if (!node) continue;
    out.push(makeRect(node.id, node.x, node.y, node.w, node.h, '#1e1e1e', '#fff9db'));
    out.push(
      makeText(
        stableId(`entity-name:${entity.name}:${node.layer}-${node.order}`),
        node.x + 12,
        node.y + 8,
        node.w - 24,
        20,
        entity.name,
        16,
        'left',
      ),
    );
    entity.attrs.forEach((attr, ai) => {
      out.push(
        makeText(
          stableId(`attr:${entity.name}:${attr.name}:${ai}`),
          node.x + 12,
          node.y + ENTITY_HEADER_H + ai * ATTR_LINE_H + 4,
          node.w - 24,
          ATTR_LINE_H - 4,
          `${attr.name}: ${attr.type}`,
          13,
          'left',
        ),
      );
    });
  }

  // Relations — elbow-routed arrows. Each arrow chooses the closest pair
  // of faces (top/bottom/left/right) on source and target based on their
  // relative position, so the routing exits and enters whichever side
  // points roughly at the other entity. Forward and back edges share the
  // same routing; back edges only differ in colour.
  for (const edge of layout.edges) {
    const a = layout.nodes.get(edge.from);
    const b = layout.nodes.get(edge.to);
    if (!a || !b) continue;
    const arrowId = stableId(`rel:${edge.from}->${edge.to}:${edge.relIdx}`);
    const rel = ast.relations[edge.relIdx]!;
    const { start, end } = chooseFaces(a, b);
    const color = edge.kind === 'back' ? ENTITY_BACK_EDGE_COLOR : ENTITY_FWD_EDGE_COLOR;
    out.push(
      makeStraightArrow(arrowId, start.x, start.y, end.x, end.y, a.id, b.id, {
        strokeColor: color,
      }),
    );
    const labelText = relationLabel(rel);
    if (labelText) {
      const pos = labelPositionFor(start, end);
      out.push(
        withColor(
          makeText(
            stableId(`rel-label:${edge.from}->${edge.to}:${edge.relIdx}`),
            pos.x,
            pos.y,
            96,
            16,
            truncateLabel(labelText, 24),
            12,
            pos.align,
          ),
          color,
        ),
      );
    }
  }

  return out;
}

interface EdgeAnchor {
  x: number;
  y: number;
  /** Normalised attach point on the bound element ([x,y] in [0,1]). */
  fixedPoint: [number, number];
  /** Which face the anchor sits on. Used to position the label clear of
   *  the entity rectangle. */
  face: 'top' | 'bottom' | 'left' | 'right';
}

/**
 * Pick the closest pair of faces between two rectangles. Uses the gap
 * between rectangles (not the centre-to-centre vector) so two entities
 * sitting in different rows always exit through top/bottom even when
 * they're horizontally offset within their row — the row-to-row distance
 * is what reads as "vertical" to the eye, not the centre angle.
 */
function chooseFaces(
  a: { x: number; y: number; w: number; h: number },
  b: { x: number; y: number; w: number; h: number },
): { start: EdgeAnchor; end: EdgeAnchor } {
  const cax = a.x + a.w / 2;
  const cay = a.y + a.h / 2;
  const cbx = b.x + b.w / 2;
  const cby = b.y + b.h / 2;
  const dx = cbx - cax;
  const dy = cby - cay;
  // Edge-to-edge gaps. Positive when rectangles don't overlap on that
  // axis; negative when they overlap. We pick the axis with the larger
  // non-overlap so the arrow exits the face that's already pointing
  // toward empty space.
  const verticalGap =
    dy >= 0 ? b.y - (a.y + a.h) : a.y - (b.y + b.h);
  const horizontalGap =
    dx >= 0 ? b.x - (a.x + a.w) : a.x - (b.x + b.w);
  // If rectangles are stacked in different rows (verticalGap > 0) AND
  // not too far apart horizontally relative to the row gap, prefer
  // vertical exits — it reads as "Y depends on X" in the layout.
  const stacked = verticalGap > 0 && verticalGap >= horizontalGap * 0.3;
  if (stacked) {
    if (dy >= 0) {
      return {
        start: { x: cax, y: a.y + a.h, fixedPoint: [0.5, 1], face: 'bottom' },
        end: { x: cbx, y: b.y, fixedPoint: [0.5, 0], face: 'top' },
      };
    }
    return {
      start: { x: cax, y: a.y, fixedPoint: [0.5, 0], face: 'top' },
      end: { x: cbx, y: b.y + b.h, fixedPoint: [0.5, 1], face: 'bottom' },
    };
  }
  if (dx >= 0) {
    return {
      start: { x: a.x + a.w, y: cay, fixedPoint: [1, 0.5], face: 'right' },
      end: { x: b.x, y: cby, fixedPoint: [0, 0.5], face: 'left' },
    };
  }
  return {
    start: { x: a.x, y: cay, fixedPoint: [0, 0.5], face: 'left' },
    end: { x: b.x + b.w, y: cby, fixedPoint: [1, 0.5], face: 'right' },
  };
}

/**
 * Position the relation label clear of both entities. Sits beside the
 * source's exit face so it reads as "this entity is the origin of the
 * relation". For vertical exits the label drops below the start point;
 * for horizontal exits it sits to the right.
 */
function labelPositionFor(
  start: EdgeAnchor,
  end: EdgeAnchor,
): { x: number; y: number; align: 'left' | 'center' | 'right' } {
  if (start.face === 'bottom' || start.face === 'top') {
    // Vertical exit — place label just outside the start face, slightly
    // toward the target so it sits over the elbow's vertical leg.
    const x = start.x + (end.x - start.x) * 0.15 - 48;
    const yOffset = start.face === 'bottom' ? 6 : -22;
    return { x, y: start.y + yOffset, align: 'center' };
  }
  // Horizontal exit — place label above the start point, beside the
  // entity it belongs to.
  const xOffset = start.face === 'right' ? 8 : -104;
  return {
    x: start.x + xOffset,
    y: start.y - 22,
    align: start.face === 'right' ? 'left' : 'right',
  };
}

function relationLabel(rel: DomainRelation): string {
  if (rel.cardinality && rel.label) return `${rel.cardinality} ${rel.label}`;
  return rel.cardinality || rel.label || '';
}

function truncateLabel(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max - 1)}…` : text;
}

/**
 * How many characters of the hand-drawn font fit in `widthPx` at `fontSize`.
 * Used to clip table cells to their column so long content ("Client dinner —
 * Acme pitch") truncates with an ellipsis instead of bleeding into the next
 * column. The 0.52 factor is an average glyph-width ratio for Excalifont.
 */
function fitChars(widthPx: number, fontSize: number): number {
  return Math.max(3, Math.floor(widthPx / (fontSize * 0.52)));
}

// ---------- Element factories ----------

function baseElement(
  id: string,
  x: number,
  y: number,
  w: number,
  h: number,
): ExcalidrawElementBase {
  const seed = stableSeed(id);
  return {
    id,
    type: 'rectangle',
    x,
    y,
    width: w,
    height: h,
    angle: 0,
    strokeColor: '#1e1e1e',
    backgroundColor: 'transparent',
    fillStyle: 'solid',
    strokeWidth: 1,
    strokeStyle: 'solid',
    roughness: 1,
    opacity: 100,
    groupIds: [],
    frameId: null,
    roundness: { type: 3 },
    seed,
    versionNonce: seed ^ 0xa5a5,
    version: 1,
    isDeleted: false,
    boundElements: null,
    updated: 0,
    link: null,
    locked: false,
  };
}

function makeRect(
  id: string,
  x: number,
  y: number,
  w: number,
  h: number,
  stroke: string,
  fill: string,
  roundness: { type: number } | null = { type: 3 },
): RectElement {
  return {
    ...baseElement(id, x, y, w, h),
    type: 'rectangle',
    strokeColor: stroke,
    backgroundColor: fill,
    roundness,
  };
}

function makeText(
  id: string,
  x: number,
  y: number,
  w: number,
  h: number,
  text: string,
  fontSize: number,
  align: 'left' | 'center' | 'right',
): TextElement {
  return {
    ...baseElement(id, x, y, w, h),
    type: 'text',
    roundness: null,
    text,
    originalText: text,
    fontSize,
    fontFamily: 5, // Excalifont (default in Excalidraw 0.18)
    textAlign: align,
    verticalAlign: 'top',
    baseline: Math.round(fontSize * 0.85),
    containerId: null,
    lineHeight: 1.25,
    autoResize: false,
  };
}

/**
 * Build a straight arrow between two bound elements. We previously used
 * elbow arrows here, but Excalidraw's elbow router computes its bends
 * from the bound endpoints alone — it doesn't pathfind around other
 * entities, so for diagonally placed entities the Z-shape ended up
 * crossing whichever rectangle sat between source and target. A straight
 * line from the closest face on the source to the closest face on the
 * target reads more like a direct relation and follows the
 * "attach to the nearest edge" intent.
 */
function makeStraightArrow(
  id: string,
  x1: number,
  y1: number,
  x2: number,
  y2: number,
  startId: string | null,
  endId: string | null,
  options: { strokeColor?: string } = {},
): ArrowElement {
  const dx = x2 - x1;
  const dy = y2 - y1;
  const strokeColor = options.strokeColor ?? '#1e1e1e';
  return {
    ...baseElement(id, x1, y1, Math.max(1, Math.abs(dx)), Math.max(1, Math.abs(dy))),
    type: 'arrow',
    x: x1,
    y: y1,
    width: Math.max(1, Math.abs(dx)),
    height: Math.max(1, Math.abs(dy)),
    strokeColor,
    roundness: { type: 2 },
    points: [
      [0, 0],
      [dx, dy],
    ],
    lastCommittedPoint: null,
    startBinding: startId ? { elementId: startId, focus: 0, gap: 4 } : null,
    endBinding: endId ? { elementId: endId, focus: 0, gap: 4 } : null,
    startArrowhead: null,
    endArrowhead: 'arrow',
    elbowed: false,
  };
}

// ---------- Stable IDs / seeds ----------

function stableId(input: string): string {
  // Excalidraw element IDs are arbitrary strings; deterministic hashes keep
  // re-renders idempotent so the canvas doesn't spuriously animate.
  const h = fnv1a(input);
  return `gen-${h.toString(36)}`;
}

function stableSeed(input: string): number {
  return fnv1a(`seed:${input}`) >>> 0;
}

function fnv1a(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}
