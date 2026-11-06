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

import {
  Background,
  EdgeLabelRenderer,
  Handle,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useInternalNode,
  useReactFlow,
  useStoreApi,
  useViewport,
  applyNodeChanges,
  type Edge,
  type EdgeProps,
  type InternalNode,
  type Node,
  type NodeChange,
  type NodeProps,
  getBezierPath,
  getSmoothStepPath
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import "./diagram.css";
import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ProjectModel } from "../domain/cellModel";
import { FitScreenIcon, ZoomInIcon, ZoomOutIcon } from "../ui/ControlIcons";
// TODO: re-enable when the PNG/SVG image export feature is complete.
// import { exportPng, exportSvg } from "./exportImage";
import { applyCustomLayout, captureCustomPosition, type CustomLayout } from "./customLayout";
import { classifyDiagramMotion, type DiagramMotionSnapshot } from "./diagramMotion";
import { getFloatingAnchors, shapeForNodeType, type NodeRect } from "./floatingGeometry";
import { toReactFlow } from "./flowLayout";
import { connectionIdsForNode, edgeConnectionId, highlightedNodeIdsForConnections } from "./highlightModel";

function CellBoundaryNode({ data }: NodeProps) {
  const width = Number(data.width);
  const height = Number(data.height);
  const title = typeof data.title === "string" && data.title.trim() ? data.title : undefined;
  const version = typeof data.version === "string" && data.version.trim() ? data.version : undefined;

  return (
    <div className="cell-boundary" data-cell-shape="octagon" style={{ width, height }}>
      <svg className="cell-boundary__outline" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
        <polygon
          data-cell-outline="octagon"
          points="30,0 70,0 100,30 100,70 70,100 30,100 0,70 0,30"
          fill="none"
          stroke="currentColor"
          strokeWidth="3.5"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
      {title || version ? (
        <div className="cell-boundary__title" data-cell-title-placement="northwest-outside">
          {title ? <span>{title}</span> : null}
          {version ? <small>{version}</small> : null}
        </div>
      ) : null}
      <div className="cell-gate-label cell-gate-label--north" data-gate-label="north" data-gate-placement="outside">
        North
      </div>
      <div className="cell-gate-label cell-gate-label--east" data-gate-label="east" data-gate-placement="outside">
        East
      </div>
      <div className="cell-gate-label cell-gate-label--south" data-gate-label="south" data-gate-placement="outside">
        South
      </div>
      <div className="cell-gate-label cell-gate-label--west" data-gate-label="west" data-gate-placement="outside">
        West
      </div>
    </div>
  );
}

function ComponentNode({ data }: NodeProps) {
  const nodeId = String(data.nodeId);
  const componentType =
    typeof data.componentType === "string" && data.componentType.trim() ? data.componentType : undefined;

  return (
    <div className="component-node" data-node-shape="circle" data-diagram-node-id={nodeId}>
      <Handle id="component-top-target" type="target" position={Position.Top} />
      <Handle id="component-top-source" type="source" position={Position.Top} />
      <Handle id="component-left-target" type="target" position={Position.Left} />
      <Handle id="component-left-source" type="source" position={Position.Left} />
      <span>{String(data.label)}</span>
      {componentType ? <small>{componentType}</small> : null}
      <Handle id="component-right-source" type="source" position={Position.Right} />
      <Handle id="component-bottom-source" type="source" position={Position.Bottom} />
    </div>
  );
}

function ExternalNode({ data }: NodeProps) {
  const direction = String(data.direction);
  const nodeId = String(data.nodeId);

  return (
    <div className={`external-node external-node--${direction}`} data-external-shape="circle" data-diagram-node-id={nodeId}>
      <Handle id="external-left-target" type="target" position={Position.Left} />
      <Handle id="external-top-target" type="target" position={Position.Top} />
      <span>{String(data.label)}</span>
      {data.externalType ? <small>{String(data.externalType)}</small> : null}
      <Handle id="external-right-source" type="source" position={Position.Right} />
      <Handle id="external-bottom-source" type="source" position={Position.Bottom} />
    </div>
  );
}

function GatewayNode({ data }: NodeProps) {
  const direction = String(data.direction);
  const nodeId = String(data.nodeId);

  return (
    <div className={`gateway-node gateway-node--${direction}`} data-gateway-bound={direction} data-diagram-node-id={nodeId}>
      <Handle id="gateway-top-target" type="target" position={Position.Top} />
      <Handle id="gateway-left-target" type="target" position={Position.Left} />
      <Handle id="gateway-right-source" type="source" position={Position.Right} />
      <Handle id="gateway-bottom-source" type="source" position={Position.Bottom} />
    </div>
  );
}

function EdgeLabel({
  label,
  x,
  y,
  entering = false
}: {
  label: EdgeProps["label"];
  x: number;
  y: number;
  entering?: boolean;
}) {
  if (!label) {
    return null;
  }
  return (
    <EdgeLabelRenderer>
      <div
        className={entering ? "edge-label edge-label--entering" : "edge-label"}
        style={{ transform: `translate(-50%, -50%) translate(${x}px,${y}px)` }}
      >
        {label}
      </div>
    </EdgeLabelRenderer>
  );
}

function makePathEdge(computePath: (props: EdgeProps) => [string, number, number, ...unknown[]]) {
  return function PathEdge(props: EdgeProps) {
    const [edgePath, labelX, labelY] = computePath(props);
    const isEntering = props.data?.motionStatus === "entering";
    return (
      <>
        <path
          className="react-flow__edge-path"
          d={edgePath}
          markerEnd={props.markerEnd}
          pathLength={isEntering ? 1 : undefined}
        />
        <EdgeLabel label={props.label} x={labelX} y={labelY} entering={isEntering} />
      </>
    );
  };
}

const LabeledEdge = makePathEdge((props) => getBezierPath(props));
const StepEdge = makePathEdge((props) => getSmoothStepPath({ ...props, borderRadius: 0 }));

const FALLBACK_NODE_SIZE: Record<string, number> = {
  component: 112,
  external: 106,
  gateway: 34
};

function toNodeRect(node: InternalNode): NodeRect {
  const fallback = FALLBACK_NODE_SIZE[node.type ?? ""] ?? 40;
  const { x, y } = node.internals.positionAbsolute;
  return {
    x,
    y,
    width: node.measured?.width ?? fallback,
    height: node.measured?.height ?? fallback,
    shape: shapeForNodeType(node.type)
  };
}

function dominantPositions(dx: number, dy: number): { source: Position; target: Position } {
  if (Math.abs(dx) >= Math.abs(dy)) {
    return dx >= 0
      ? { source: Position.Right, target: Position.Left }
      : { source: Position.Left, target: Position.Right };
  }
  return dy >= 0
    ? { source: Position.Bottom, target: Position.Top }
    : { source: Position.Top, target: Position.Bottom };
}

// Floating edge: attaches to each node's perimeter at the point facing the other node,
// so component circles no longer snap to four fixed handles.
function FloatingEdge(props: EdgeProps) {
  const sourceNode = useInternalNode(props.source);
  const targetNode = useInternalNode(props.target);

  if (!sourceNode || !targetNode) {
    return null;
  }

  const { sx, sy, tx, ty } = getFloatingAnchors(toNodeRect(sourceNode), toNodeRect(targetNode));
  const positions = dominantPositions(tx - sx, ty - sy);
  const [edgePath, labelX, labelY] = getBezierPath({
    sourceX: sx,
    sourceY: sy,
    sourcePosition: positions.source,
    targetX: tx,
    targetY: ty,
    targetPosition: positions.target
  });
  const isEntering = props.data?.motionStatus === "entering";

  return (
    <>
      <path
        className="react-flow__edge-path"
        d={edgePath}
        markerEnd={props.markerEnd}
        pathLength={isEntering ? 1 : undefined}
      />
      <EdgeLabel label={props.label} x={labelX} y={labelY} entering={isEntering} />
    </>
  );
}

const nodeTypes = {
  cellBoundary: memo(CellBoundaryNode),
  component: memo(ComponentNode),
  external: memo(ExternalNode),
  gateway: memo(GatewayNode)
};

const edgeTypes = {
  smoothstep: memo(LabeledEdge),
  step: memo(StepEdge),
  floating: memo(FloatingEdge)
};

function sameConnectionIds(left: string[], right: string[]) {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

export interface DiagramCanvasInsets {
  left: number;
  right: number;
}

const DEFAULT_INSETS: DiagramCanvasInsets = { left: 0, right: 0 };
const FIT_VIEW_LEFT_PADDING = 112;

/**
 * Vertical fit padding, per side, for a canvas that shows the floating chrome.
 *
 * 112px clears the zoom controls and the canvas notification on a full
 * workspace — about 12% of a 900px pane. The same constant on a short pane is
 * pathological: 112 top and bottom on a 414px spec view leaves 190px for a
 * diagram nearly a thousand pixels tall, and fit-to-view clamps to minZoom.
 * Scale with the pane height instead, so a short pane degrades: at most 112px,
 * at least 16px, and never more than a fifth of the pane.
 */
const FIT_VIEW_VERTICAL_PADDING_MAX = 112;
const FIT_VIEW_VERTICAL_PADDING_MIN = 16;
const FIT_VIEW_VERTICAL_PADDING_RATIO = 0.12;
const FIT_VIEW_VERTICAL_PADDING_SHARE = 0.2;

/**
 * A compact embed hides the chrome, so it keeps a fixed sliver of breathing
 * room rather than the scaled clearance above. 112 top and bottom is 224px of
 * a 340px panel, leaving a third of the box for the diagram it exists to show.
 */
const FIT_VIEW_COMPACT_PADDING: `${number}px` = "16px";
const MOTION_SETTLE_MS = 420;

interface FitPadding {
  top: `${number}px`;
  bottom: `${number}px`;
  left: `${number}px`;
  right: `${number}px`;
}

function verticalFitPadding(paneHeight: number, compact: boolean): `${number}px` {
  if (compact) {
    return FIT_VIEW_COMPACT_PADDING;
  }
  // Unknown until the pane is measured. The post-paint fit reads the real height.
  if (!(paneHeight > 0)) {
    return `${FIT_VIEW_VERTICAL_PADDING_MAX}px`;
  }

  const proportional = paneHeight * FIT_VIEW_VERTICAL_PADDING_RATIO;
  const clamped = Math.min(FIT_VIEW_VERTICAL_PADDING_MAX, Math.max(FIT_VIEW_VERTICAL_PADDING_MIN, proportional));
  // The pixel minimum must not eat a tiny pane.
  const capped = Math.min(clamped, paneHeight * FIT_VIEW_VERTICAL_PADDING_SHARE);
  return `${Math.round(capped)}px`;
}

export function buildFitPadding(insets: DiagramCanvasInsets, compact = false, paneHeight = 0): FitPadding {
  const leftPadding = insets.left > 0 ? insets.left + FIT_VIEW_LEFT_PADDING : 0;
  const vertical = verticalFitPadding(paneHeight, compact);

  return {
    top: vertical,
    bottom: vertical,
    left: `${leftPadding}px`,
    right: `${insets.right}px`
  };
}

function FitViewController({
  insets,
  model,
  fitKey,
  compact
}: {
  insets: DiagramCanvasInsets;
  model: ProjectModel;
  fitKey?: string;
  compact?: boolean;
}) {
  const { fitView } = useReactFlow();
  const store = useStoreApi();
  const { left, right } = insets;

  useEffect(() => {
    // Read the pane at call time. The resize handler may measure between this
    // effect and the post-paint fit; subscribing would also refit on every
    // resize and throw away a zoom the user had chosen.
    const fitOptions = () => ({
      padding: buildFitPadding({ left, right }, compact, store.getState().height),
      duration: 200
    });
    fitView(fitOptions());
    const postPaintFit = window.requestAnimationFrame(() => fitView(fitOptions()));

    // model is only used to re-trigger the fit when the diagram data itself changes
    // (switching documents), not on every re-render (e.g. focus-click highlighting).
    return () => window.cancelAnimationFrame(postPaintFit);
  }, [left, right, fitView, model, fitKey, compact, store]);

  return null;
}

// AEP divergence from upstream: the "Auto arrange" button is dropped (a moved
// component is a transient viewing aid here — the layout resets whenever the
// DSL changes, so a manual reset control isn't worth the chrome), and the
// remaining controls carry `title` tooltips.
function ZoomControls({ insets, compact }: { insets: DiagramCanvasInsets; compact?: boolean }) {
  const { zoomIn, zoomOut, fitView } = useReactFlow();
  const { zoom } = useViewport();
  const store = useStoreApi();

  return (
    <div className="zoom-controls">
      <button
        type="button"
        className="zoom-controls__button"
        aria-label="Zoom out"
        title="Zoom out"
        onClick={() => zoomOut()}
      >
        <ZoomOutIcon size={16} />
      </button>
      <span className="zoom-controls__level">{Math.round(zoom * 100)}%</span>
      <button
        type="button"
        className="zoom-controls__button"
        aria-label="Zoom in"
        title="Zoom in"
        onClick={() => zoomIn()}
      >
        <ZoomInIcon size={16} />
      </button>
      <button
        type="button"
        className="zoom-controls__button zoom-controls__button--fit"
        aria-label="Fit diagram to view"
        title="Fit diagram to view"
        onClick={() =>
          fitView({ padding: buildFitPadding(insets, compact, store.getState().height), duration: 200 })
        }
      >
        <FitScreenIcon size={16} />
      </button>
    </div>
  );
}

// TODO: re-enable when the PNG/SVG image export feature is complete.
// function ExportControls({ filename }: { filename: string }) {
//   const { getNodes } = useReactFlow();
//   function viewportElement(): HTMLElement | null {
//     return document.querySelector(".react-flow__viewport");
//   }
//   async function handleExport(kind: "png" | "svg") {
//     const viewport = viewportElement();
//     if (!viewport) { return; }
//     try {
//       const args = { nodes: getNodes(), viewport, filename };
//       if (kind === "png") { await exportPng(args); } else { await exportSvg(args); }
//     } catch (error) {
//       console.error(`Diagram ${kind.toUpperCase()} export failed`, error);
//     }
//   }
//   return (
//     <div className="export-controls">
//       <button type="button" className="export-controls__button" aria-label="Export PNG" onClick={() => handleExport("png")}>PNG</button>
//       <button type="button" className="export-controls__button" aria-label="Export SVG" onClick={() => handleExport("svg")}>SVG</button>
//     </div>
//   );
// }

/** Color theme applied to the rendered diagram. */
export type DiagramTheme = "light" | "dark";

export interface DiagramCanvasProps {
  model: ProjectModel | null;
  insets?: DiagramCanvasInsets;
  fitKey?: string;
  motionContextKey?: string;
  source?: string;
  customLayout?: CustomLayout | null;
  onCustomLayoutChange?: (layout: CustomLayout) => void;
  canvasMessage?: CanvasMessage | null;
  /** Light or dark color theme. Defaults to `"light"`. */
  theme?: DiagramTheme;
  /**
   * Fit the graph for an embed that hides the floating chrome, trading the
   * room reserved for zoom controls and notifications for the diagram itself.
   */
  compact?: boolean;
  /**
   * A diagram to look at, not to operate: no dragging, no selection, and no
   * keyboard focus on the nodes.
   *
   * A host cannot achieve this with `pointer-events: none` on a wrapper, which
   * is what the overview panel tried. React Flow's own stylesheet sets
   * `pointer-events: all` on `.react-flow__node`, so the nodes re-enable
   * themselves and stay draggable; and pointer events never governed the
   * keyboard anyway — every node carries `tabindex="0"`, so a keyboard user
   * tabbed through eleven of them on a panel that is not supposed to respond.
   */
  readOnly?: boolean;
}

export interface CanvasMessage {
  id: number;
  tone: "warning" | "info";
  text: string;
}

export function DiagramCanvas({
  model,
  insets = DEFAULT_INSETS,
  fitKey,
  motionContextKey = "default",
  source = "",
  customLayout = null,
  onCustomLayoutChange,
  canvasMessage = null,
  theme = "light",
  compact = false,
  readOnly = false
}: DiagramCanvasProps) {
  const [activeConnectionIds, setActiveConnectionIds] = useState<string[]>([]);
  const [, setMotionVersion] = useState(0);
  const previousMotionSnapshot = useRef<DiagramMotionSnapshot | null>(null);
  const flow = useMemo<ReturnType<typeof toReactFlow>>(
    () => (model ? toReactFlow(model) : { nodes: [], edges: [], cellSize: { width: 0, height: 0 } }),
    [model]
  );
  const laidOutNodes = useMemo(() => applyCustomLayout(flow.nodes, customLayout), [customLayout, flow.nodes]);
  // Per-node `draggable` BEATS the canvas-wide `nodesDraggable`, so a read-only
  // canvas has to clear the flag `flowLayout` sets on the kinds a user may
  // normally arrange. Without this the nodes keep their drag handlers and the
  // "read-only" preview is still something you can rearrange with a mouse.
  const positionedNodes = useMemo(
    () => (readOnly ? laidOutNodes.map((n) => ({ ...n, draggable: false })) : laidOutNodes),
    [laidOutNodes, readOnly],
  );
  const [dragNodes, setDragNodes] = useState<Node[] | null>(null);
  const liveNodes = dragNodes ?? positionedNodes;
  const motion = classifyDiagramMotion(
    previousMotionSnapshot.current,
    positionedNodes,
    flow.edges,
    motionContextKey
  );
  const activeConnectionIdSet = useMemo(() => new Set(activeConnectionIds), [activeConnectionIds]);
  const highlightedNodeIds = useMemo(() => {
    if (activeConnectionIdSet.size === 0) {
      return new Set<string>();
    }

    return highlightedNodeIdsForConnections(flow.edges, activeConnectionIdSet);
  }, [activeConnectionIdSet, flow.edges]);
  const getConnectionIdsForNode = useCallback(
    (nodeId: string) => connectionIdsForNode(flow.edges, nodeId),
    [flow.edges]
  );
  const setActiveConnections = useCallback((connectionIds: string[]) => {
    setActiveConnectionIds((current) => (sameConnectionIds(current, connectionIds) ? current : connectionIds));
  }, []);
  const isFocusView = activeConnectionIdSet.size > 0;
  const nodes = useMemo<Node[]>(
    () =>
      liveNodes.map((node) => ({
        ...node,
        className: [
          node.className,
          motion.enteringNodeIds.has(node.id) ? "diagram-node--entering" : "",
          motion.movingNodeIds.has(node.id) ? "diagram-node--position-animated" : "",
          isFocusView
            ? highlightedNodeIds.has(node.id)
              ? "connection-highlight-node"
              : "connection-dimmed-node"
            : ""
        ]
          .filter(Boolean)
          .join(" ")
      })),
    [highlightedNodeIds, isFocusView, liveNodes, motion.enteringNodeIds, motion.movingNodeIds]
  );
  const edges = useMemo<Edge[]>(
    () =>
      flow.edges.map((edge) => ({
        ...edge,
        data: {
          ...edge.data,
          motionStatus: motion.enteringEdgeIds.has(edge.id) ? "entering" : "idle"
        },
        className: [
          edge.className,
          motion.enteringEdgeIds.has(edge.id) ? "diagram-edge--entering" : "",
          isFocusView
            ? activeConnectionIdSet.has(edgeConnectionId(edge))
              ? "connection-highlight-edge"
              : "connection-dimmed-edge"
            : ""
        ]
          .filter(Boolean)
          .join(" ")
      })),
    [activeConnectionIdSet, flow.edges, isFocusView, motion.enteringEdgeIds]
  );

  useEffect(() => {
    previousMotionSnapshot.current = motion.snapshot;
  }, [flow.edges, motion.snapshot, motionContextKey, positionedNodes]);

  useEffect(() => {
    if (motion.enteringNodeIds.size === 0 && motion.enteringEdgeIds.size === 0) {
      return;
    }

    const settleTimer = window.setTimeout(() => setMotionVersion((current) => current + 1), MOTION_SETTLE_MS);
    return () => window.clearTimeout(settleTimer);
  }, [motion.enteringEdgeIds, motion.enteringNodeIds]);

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setActiveConnections([]);
      }
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [setActiveConnections]);

  const handleNodesChange = useCallback(
    (changes: NodeChange[]) => {
      const positionChanges = changes.filter((change) => change.type === "position");
      if (positionChanges.length === 0) {
        return;
      }
      setDragNodes((current) => applyNodeChanges(positionChanges, current ?? positionedNodes));
    },
    [positionedNodes]
  );

  const handleNodeDragStop = useCallback(
    (_event: MouseEvent | TouchEvent, node: Node) => {
      const nextLayout = captureCustomPosition(customLayout, source, node, flow.nodes);
      if (!nextLayout) {
        setDragNodes(null);
        return;
      }
      setDragNodes(null);
      onCustomLayoutChange?.(nextLayout);
    },
    [customLayout, flow.nodes, onCustomLayoutChange, source]
  );

  if (!model) {
    return (
      <div className="cell-diagram-root empty-canvas" data-cd-theme={theme}>
        <span>Fix the DSL errors to render the diagram.</span>
      </div>
    );
  }

  return (
    <div className="cell-diagram-root" data-cd-theme={theme}>
      <ReactFlowProvider key={motionContextKey}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        minZoom={0.25}
        maxZoom={1.35}
        nodesDraggable={!readOnly}
        nodesConnectable={false}
        elementsSelectable={!readOnly}
        nodesFocusable={!readOnly}
        edgesFocusable={!readOnly}
        disableKeyboardA11y={readOnly}
        proOptions={{ hideAttribution: true }}
        onNodesChange={handleNodesChange}
        onNodeDragStop={handleNodeDragStop}
        onNodeClick={(_, node) => setActiveConnections(getConnectionIdsForNode(node.id))}
        onPaneClick={() => setActiveConnections([])}
      >
        <FitViewController
          insets={insets}
          model={model}
          fitKey={fitKey}
          compact={compact}
        />
        <Background color={theme === "dark" ? "#334155" : "#cbd5e1"} gap={22} />
        <ZoomControls insets={insets} compact={compact} />
        {/* TODO: re-enable when the PNG/SVG image export feature is complete.
        <ExportControls filename={model.title?.trim() || "cell-diagram"} /> */}
        <div
          key={canvasMessage?.id ?? "focus-hint"}
          className="canvas-notification"
          data-mode={canvasMessage ? "message" : "hint"}
          data-tone={canvasMessage?.tone ?? "neutral"}
          data-focus-mode={isFocusView ? "active" : "idle"}
          role={canvasMessage ? "status" : undefined}
        >
          {canvasMessage?.text ??
            (isFocusView
              ? "Focus view: click outside or press Esc to return to the full diagram."
              : "Click a component to focus its connections.")}
        </div>
      </ReactFlow>
      </ReactFlowProvider>
    </div>
  );
}
