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

import dagre from "@dagrejs/dagre";
import { MarkerType, type Edge, type Node } from "@xyflow/react";
import { CellModel, ProjectModel, type BoundaryDirection } from "../domain/cellModel";
import { clearBoundaryCorridors } from "./boundaryCorridor";

type FlowNodeData = Record<string, unknown>;

const componentSize = 112;
const externalSize = 106;
const gatewaySize = 34;
const componentWidth = componentSize;
const componentHeight = componentSize;
const cellPadding = 142;
// Free space a boundary edge keeps on either side of its line — see
// `clearBoundaryCorridors`. Half a component plus a little, so a nudged
// neighbour clears the line rather than grazing it.
const corridorClearance = 28;
const defaultCellSize = 640;
const externalGapByDirection: Record<BoundaryDirection, number> = {
  north: 220,
  east: 150,
  south: 220,
  west: 150
};
const externalStepByDirection: Record<BoundaryDirection, number> = {
  north: 240,
  east: 172,
  south: 240,
  west: 172
};

/**
 * The boundary directions each component's own edges leave or enter by. An
 * inbound edge occludes exactly like an outbound one — both run between the
 * component and the gateway — so both count.
 */
function boundaryDirectionsByComponent(cell: CellModel) {
  const componentIds = new Set(cell.components.map((component) => component.id));
  const directions = new Map<string, Set<BoundaryDirection>>();

  cell.edges.forEach((edge) => {
    const direction = edge.direction;
    if (edge.kind === "internal" || direction === "internal") {
      return;
    }

    const componentId = componentIds.has(edge.source) ? edge.source : edge.target;
    if (!componentIds.has(componentId)) {
      return;
    }

    const existing = directions.get(componentId) ?? new Set<BoundaryDirection>();
    existing.add(direction);
    directions.set(componentId, existing);
  });

  return directions;
}

export function layoutCell(cell: CellModel) {
  const graph = new dagre.graphlib.Graph();
  graph.setDefaultEdgeLabel(() => ({}));
  graph.setGraph({
    rankdir: "LR",
    ranksep: 72,
    nodesep: 44,
    marginx: 20,
    marginy: 20
  });

  cell.components.forEach((component) => {
    graph.setNode(component.id, { width: componentWidth, height: componentHeight });
  });

  cell.edges
    .filter((edge) => edge.kind === "internal")
    .forEach((edge) => {
      graph.setEdge(edge.source, edge.target);
    });

  dagre.layout(graph);

  const placed = clearBoundaryCorridors(
    cell.components.map((component) => {
      const position = graph.node(component.id) ?? { x: 0, y: 0 };
      return {
        id: component.id,
        x: position.x - componentWidth / 2,
        y: position.y - componentHeight / 2
      };
    }),
    boundaryDirectionsByComponent(cell),
    { width: componentWidth, height: componentHeight, clearance: corridorClearance }
  );

  const positions = new Map(placed.map((node) => [node.id, node]));
  const nodes = cell.components.map((component) => {
    const position = positions.get(component.id)!;
    return { component, x: position.x, y: position.y };
  });

  const bounds = nodes.reduce(
    (current, node) => ({
      minX: Math.min(current.minX, node.x),
      minY: Math.min(current.minY, node.y),
      maxX: Math.max(current.maxX, node.x + componentWidth),
      maxY: Math.max(current.maxY, node.y + componentHeight)
    }),
    {
      minX: Number.POSITIVE_INFINITY,
      minY: Number.POSITIVE_INFINITY,
      maxX: Number.NEGATIVE_INFINITY,
      maxY: Number.NEGATIVE_INFINITY
    }
  );

  if (nodes.length === 0) {
    return {
      nodes,
      width: defaultCellSize,
      height: defaultCellSize
    };
  }

  const contentWidth = bounds.maxX - bounds.minX + cellPadding * 2;
  const contentHeight = bounds.maxY - bounds.minY + cellPadding * 2;
  const cellSize = Math.max(defaultCellSize, contentWidth, contentHeight);
  const extraX = (cellSize - contentWidth) / 2;
  const extraY = (cellSize - contentHeight) / 2;

  return {
    nodes: nodes.map((node) => ({
      ...node,
      x: node.x - bounds.minX + cellPadding + extraX,
      y: node.y - bounds.minY + cellPadding + extraY
    })),
    width: cellSize,
    height: cellSize
  };
}

function externalPosition(
  direction: BoundaryDirection,
  index: number,
  count: number,
  cellWidth: number,
  cellHeight: number
) {
  const gap = externalGapByDirection[direction];
  const step = externalStepByDirection[direction];
  const offset = (index - (count - 1) / 2) * step;

  if (direction === "north") {
    return { x: cellWidth / 2 + offset - externalSize / 2, y: -gap };
  }

  if (direction === "south") {
    return { x: cellWidth / 2 + offset - externalSize / 2, y: cellHeight + gap - externalSize };
  }

  if (direction === "west") {
    return { x: -gap - externalSize, y: cellHeight / 2 + offset - externalSize / 2 };
  }

  return { x: cellWidth + gap, y: cellHeight / 2 + offset - externalSize / 2 };
}

function gatewayPosition(direction: BoundaryDirection, cellWidth: number, cellHeight: number) {
  if (direction === "north") {
    return { x: cellWidth / 2 - gatewaySize / 2, y: -gatewaySize / 2 };
  }

  if (direction === "south") {
    return { x: cellWidth / 2 - gatewaySize / 2, y: cellHeight - gatewaySize / 2 };
  }

  if (direction === "west") {
    return { x: -gatewaySize / 2, y: cellHeight / 2 - gatewaySize / 2 };
  }

  return { x: cellWidth - gatewaySize / 2, y: cellHeight / 2 - gatewaySize / 2 };
}

function componentHandle(direction: string, type: "source" | "target") {
  if (direction === "north") {
    return type === "source" ? "component-top-source" : "component-top-target";
  }

  if (direction === "south") {
    return type === "source" ? "component-bottom-source" : "component-top-target";
  }

  if (direction === "west") {
    return type === "source" ? "component-left-source" : "component-left-target";
  }

  return type === "source" ? "component-right-source" : "component-left-target";
}

function externalSourceHandle(direction: string) {
  if (direction === "north") {
    return "external-bottom-source";
  }

  if (direction === "west") {
    return "external-right-source";
  }

  return "external-right-source";
}

function externalTargetHandle(direction: string) {
  if (direction === "south") {
    return "external-top-target";
  }

  return "external-left-target";
}

function gatewaySourceHandle(direction: string) {
  if (direction === "north") {
    return "gateway-bottom-source";
  }

  if (direction === "south") {
    return "gateway-bottom-source";
  }

  if (direction === "west") {
    return "gateway-right-source";
  }

  return "gateway-right-source";
}

function gatewayTargetHandle(direction: string) {
  if (direction === "north") {
    return "gateway-top-target";
  }

  if (direction === "south") {
    return "gateway-top-target";
  }

  if (direction === "west") {
    return "gateway-left-target";
  }

  return "gateway-left-target";
}

function internalEdgeHandles() {
  return {
    sourceHandle: "component-right-source",
    targetHandle: "component-left-target"
  };
}

function connectionData(connectionId: string, connectedNodeIds: string[]) {
  return {
    connectionId,
    connectedNodeIds
  };
}

// Arrow-head colors reference the same theme tokens as the edge strokes in
// diagram.css. The markers render inside `.cell-diagram-root`, so these CSS
// variables resolve to the active (light/dark) theme automatically.
const EDGE_ARROW_COLORS: Record<string, string> = {
  north: "var(--cd-north)",
  east: "var(--cd-east)",
  south: "var(--cd-south)",
  west: "var(--cd-west)"
};
const DEFAULT_ARROW_COLOR = "var(--cd-edge)";
const CROSS_ARROW_COLOR = "var(--cd-cross)";

// A React Flow arrow marker (orient="auto" so it aligns to the path tangent),
// colored to match the edge. Used on the terminal segment of each logical edge.
// markerUnits "userSpaceOnUse" keeps the head a fixed size, so it does not balloon
// when a selected/highlighted edge thickens its stroke.
function arrowMarker(color: string) {
  return {
    type: MarkerType.Arrow,
    color,
    width: 25,
    height: 25,
    strokeWidth: 1.6,
    markerUnits: "userSpaceOnUse"
  };
}

function directionArrow(direction: string) {
  return arrowMarker(EDGE_ARROW_COLORS[direction] ?? DEFAULT_ARROW_COLOR);
}

function namespaced(cellId: string, id: string, multi: boolean) {
  return multi ? `${cellId}::${id}` : id;
}

function motionKeyForLine(cellId: string, kind: "component" | "external", line: number | undefined, id: string) {
  return typeof line === "number" ? `${cellId}:${kind}:${line}` : `${cellId}:${kind}:id:${id}`;
}

function componentSourceLine(cell: CellModel, componentId: string, declaredLine: number | undefined) {
  if (typeof declaredLine === "number") {
    return declaredLine;
  }

  return cell.edges.find((edge) => edge.source === componentId || edge.target === componentId)?.line;
}

function gatewayNodeId(cellId: string, direction: string, multi: boolean) {
  return multi ? `gateway-${cellId}-${direction}` : `gateway-${direction}`;
}

function externalNodeId(cellId: string, externalId: string, multi: boolean) {
  return multi ? `external-${cellId}-${externalId}` : `external-${externalId}`;
}

function emitCellEdges(cell: CellModel, multi: boolean, sharedExternalIds: Set<string>, edges: Edge[]) {
  const resolve = (compId: string) => namespaced(cell.id, compId, multi);
  const gwId = (direction: string) => gatewayNodeId(cell.id, direction, multi);
  const extId = (externalId: string) =>
    sharedExternalIds.has(externalId) ? `external-${externalId}` : externalNodeId(cell.id, externalId, multi);

  cell.edges.forEach((edge) => {
    if (edge.kind === "internal") {
      const handles = internalEdgeHandles();
      const data = connectionData(edge.id, [resolve(edge.source), resolve(edge.target)]);
      edges.push({
        id: edge.id,
        data,
        source: resolve(edge.source),
        sourceHandle: handles.sourceHandle,
        target: resolve(edge.target),
        targetHandle: handles.targetHandle,
        label: edge.label,
        type: "floating",
        animated: false,
        markerEnd: directionArrow(edge.direction),
        className: `edge-${edge.direction}`
      });
      return;
    }

    if (edge.kind === "inbound") {
      const gatewayId = gwId(edge.direction);
      const externalId = extId(edge.source);
      const data = connectionData(edge.id, [externalId, gatewayId, resolve(edge.target)]);
      edges.push(
        {
          id: `${edge.id}-external-gateway`,
          data,
          source: externalId,
          sourceHandle: externalSourceHandle(edge.direction),
          target: gatewayId,
          targetHandle: gatewayTargetHandle(edge.direction),
          label: edge.label,
          type: "smoothstep",
          animated: true,
          className: `edge-${edge.direction}`
        },
        {
          id: `${edge.id}-gateway-component`,
          data,
          source: gatewayId,
          sourceHandle: gatewaySourceHandle(edge.direction),
          target: resolve(edge.target),
          targetHandle: componentHandle(edge.direction, "target"),
          type: "smoothstep",
          animated: true,
          markerEnd: directionArrow(edge.direction),
          className: `edge-${edge.direction}`
        }
      );
      return;
    }

    if (edge.kind === "exposure") {
      const gatewayId = gwId(edge.direction);
      const startsAtGateway = edge.source === edge.direction;

      if (startsAtGateway) {
        const data = connectionData(edge.id, [gatewayId, resolve(edge.target)]);
        edges.push({
          id: `${edge.id}-gateway-component`,
          data,
          source: gatewayId,
          sourceHandle: gatewaySourceHandle(edge.direction),
          target: resolve(edge.target),
          targetHandle: componentHandle(edge.direction, "target"),
          label: edge.label,
          type: "smoothstep",
          animated: true,
          markerEnd: directionArrow(edge.direction),
          className: `edge-${edge.direction}`
        });
        return;
      }

      const data = connectionData(edge.id, [resolve(edge.source), gatewayId]);
      edges.push({
        id: `${edge.id}-component-gateway`,
        data,
        source: resolve(edge.source),
        sourceHandle: componentHandle(edge.direction, "source"),
        target: gatewayId,
        targetHandle: gatewayTargetHandle(edge.direction),
        label: edge.label,
        type: "smoothstep",
        animated: true,
        className: `edge-${edge.direction}`
      });
      return;
    }

    const gatewayId = gwId(edge.direction);
    const externalId = extId(edge.target);
    const data = connectionData(edge.id, [resolve(edge.source), gatewayId, externalId]);
    edges.push(
      {
        id: `${edge.id}-component-gateway`,
        data,
        source: resolve(edge.source),
        sourceHandle: componentHandle(edge.direction, "source"),
        target: gatewayId,
        targetHandle: gatewayTargetHandle(edge.direction),
        type: "smoothstep",
        animated: true,
        className: `edge-${edge.direction}`
      },
      {
        id: `${edge.id}-gateway-external`,
        data,
        source: gatewayId,
        sourceHandle: gatewaySourceHandle(edge.direction),
        target: externalId,
        targetHandle: externalTargetHandle(edge.direction),
        label: edge.label,
        type: "smoothstep",
        animated: true,
        markerEnd: directionArrow(edge.direction),
        className: `edge-${edge.direction}`
      }
    );
  });
}

function emitCrossEdges(project: ProjectModel, multi: boolean, edges: Edge[]) {
  project.crossEdges.forEach((edge) => {
    if (edge.mode !== "connected") {
      return;
    }

    const srcComp = namespaced(edge.sourceCell, edge.sourceComp, multi);
    const tgtComp = namespaced(edge.targetCell, edge.targetComp, multi);
    const srcGate = gatewayNodeId(edge.sourceCell, edge.exit, multi);
    const tgtGate = gatewayNodeId(edge.targetCell, edge.entry, multi);
    const data = connectionData(edge.id, [srcComp, srcGate, tgtGate, tgtComp]);

    edges.push(
      {
        id: `${edge.id}-component-gateway`,
        data,
        source: srcComp,
        sourceHandle: componentHandle(edge.exit, "source"),
        target: srcGate,
        targetHandle: gatewayTargetHandle(edge.exit),
        type: "step",
        animated: true,
        className: "edge-cross"
      },
      {
        id: `${edge.id}-gateway-gateway`,
        data,
        source: srcGate,
        sourceHandle: gatewaySourceHandle(edge.exit),
        target: tgtGate,
        targetHandle: gatewayTargetHandle(edge.entry),
        label: edge.label,
        type: "step",
        animated: true,
        className: "edge-cross"
      },
      {
        id: `${edge.id}-gateway-component`,
        data,
        source: tgtGate,
        sourceHandle: gatewaySourceHandle(edge.entry),
        target: tgtComp,
        targetHandle: componentHandle(edge.entry, "target"),
        type: "step",
        animated: true,
        markerEnd: arrowMarker(CROSS_ARROW_COLOR),
        className: "edge-cross"
      }
    );
  });
}

function emitDecoupledCrossEdges(
  project: ProjectModel,
  multi: boolean,
  cellBoxes: Map<string, { originX: number; originY: number; width: number; height: number }>,
  nodes: Node<FlowNodeData>[],
  edges: Edge[]
) {
  project.crossEdges.forEach((edge) => {
    if (edge.mode !== "decoupled") {
      return;
    }

    const srcBox = cellBoxes.get(edge.sourceCell);
    const tgtBox = cellBoxes.get(edge.targetCell);
    if (!srcBox || !tgtBox) {
      return;
    }

    const srcComp = namespaced(edge.sourceCell, edge.sourceComp, multi);
    const tgtComp = namespaced(edge.targetCell, edge.targetComp, multi);
    const srcGate = gatewayNodeId(edge.sourceCell, edge.exit, multi);
    const tgtGate = gatewayNodeId(edge.targetCell, edge.entry, multi);
    const stubOutId = `xstub-${edge.id}-out`;
    const stubInId = `xstub-${edge.id}-in`;

    const outPosition = externalPosition(edge.exit, 0, 1, srcBox.width, srcBox.height);
    const inPosition = externalPosition(edge.entry, 0, 1, tgtBox.width, tgtBox.height);

    nodes.push({
      id: stubOutId,
      type: "external",
      position: { x: srcBox.originX + outPosition.x, y: srcBox.originY + outPosition.y },
      data: {
        nodeId: stubOutId,
        label: `${edge.targetCell}.${edge.targetComp}`,
        externalType: undefined,
        direction: edge.exit,
        layoutKind: "external",
        cellId: edge.sourceCell
      },
      draggable: true
    });
    nodes.push({
      id: stubInId,
      type: "external",
      position: { x: tgtBox.originX + inPosition.x, y: tgtBox.originY + inPosition.y },
      data: {
        nodeId: stubInId,
        label: `${edge.sourceCell}.${edge.sourceComp}`,
        externalType: undefined,
        direction: edge.entry,
        layoutKind: "external",
        cellId: edge.targetCell
      },
      draggable: true
    });

    const outData = connectionData(`${edge.id}-out`, [srcComp, srcGate, stubOutId]);
    edges.push(
      {
        id: `${edge.id}-out-component-gateway`,
        data: outData,
        source: srcComp,
        sourceHandle: componentHandle(edge.exit, "source"),
        target: srcGate,
        targetHandle: gatewayTargetHandle(edge.exit),
        type: "step",
        animated: true,
        className: "edge-cross"
      },
      {
        id: `${edge.id}-out-gateway-external`,
        data: outData,
        source: srcGate,
        sourceHandle: gatewaySourceHandle(edge.exit),
        target: stubOutId,
        targetHandle: externalTargetHandle(edge.exit),
        label: edge.label,
        type: "step",
        animated: true,
        markerEnd: arrowMarker(CROSS_ARROW_COLOR),
        className: "edge-cross"
      }
    );

    const inData = connectionData(`${edge.id}-in`, [stubInId, tgtGate, tgtComp]);
    edges.push(
      {
        id: `${edge.id}-in-external-gateway`,
        data: inData,
        source: stubInId,
        sourceHandle: externalSourceHandle(edge.entry),
        target: tgtGate,
        targetHandle: gatewayTargetHandle(edge.entry),
        type: "step",
        animated: true,
        className: "edge-cross"
      },
      {
        id: `${edge.id}-in-gateway-component`,
        data: inData,
        source: tgtGate,
        sourceHandle: gatewaySourceHandle(edge.entry),
        target: tgtComp,
        targetHandle: componentHandle(edge.entry, "target"),
        type: "step",
        animated: true,
        markerEnd: arrowMarker(CROSS_ARROW_COLOR),
        className: "edge-cross"
      }
    );
  });
}

export function toReactFlow(project: ProjectModel) {
  const multi = project.cells.length > 1;
  const nodes: Node<FlowNodeData>[] = [];
  const edges: Edge[] = [];

  const cellBoxes = new Map<string, { originX: number; originY: number; width: number; height: number }>();
  const sharedExternalIds = new Set(project.sharedExternals.map((ext) => ext.id));

  const layouts = new Map<string, ReturnType<typeof layoutCell>>();
  project.cells.forEach((cell) => layouts.set(cell.id, layoutCell(cell)));

  const gatewayDirectionsByCell = new Map<string, Set<BoundaryDirection>>();
  project.cells.forEach((cell) => {
    const dirs = new Set<BoundaryDirection>();
    cell.externals.forEach((external) => dirs.add(external.direction as BoundaryDirection));
    cell.edges
      .filter((edge) => edge.kind === "exposure" || edge.kind === "inbound" || edge.kind === "outbound")
      .forEach((edge) => dirs.add(edge.direction as BoundaryDirection));
    gatewayDirectionsByCell.set(cell.id, dirs);
  });
  project.crossEdges.forEach((edge) => {
    gatewayDirectionsByCell.get(edge.sourceCell)?.add(edge.exit);
    gatewayDirectionsByCell.get(edge.targetCell)?.add(edge.entry);
  });

  const cellGraph = new dagre.graphlib.Graph();
  cellGraph.setDefaultEdgeLabel(() => ({}));
  cellGraph.setGraph({ rankdir: "LR", ranksep: 260, nodesep: 200, marginx: 60, marginy: 60 });

  project.cells.forEach((cell) => {
    const layout = layouts.get(cell.id)!;
    cellGraph.setNode(`cell-${cell.id}`, { width: layout.width, height: layout.height });
  });
  project.sharedExternals.forEach((ext) => {
    cellGraph.setNode(`external-${ext.id}`, { width: externalSize, height: externalSize });
  });
  project.crossEdges
    .filter((edge) => edge.mode === "connected")
    .forEach((edge) => cellGraph.setEdge(`cell-${edge.sourceCell}`, `cell-${edge.targetCell}`));

  dagre.layout(cellGraph);

  project.cells.forEach((cell) => {
    const layout = layouts.get(cell.id)!;
    const g = cellGraph.node(`cell-${cell.id}`) ?? { x: layout.width / 2, y: layout.height / 2 };
    const originX = g.x - layout.width / 2;
    const originY = g.y - layout.height / 2;
    cellBoxes.set(cell.id, { originX, originY, width: layout.width, height: layout.height });

    nodes.push({
      id: `cell-${cell.id}`,
      type: "cellBoundary",
      position: { x: originX, y: originY },
      data: {
        title: cell.label ?? (multi ? cell.id : project.title),
        version: cell.version,
        width: layout.width,
        height: layout.height,
        layoutKind: "cell",
        cellId: cell.id
      },
      draggable: false,
      selectable: false
    });

    layout.nodes.forEach(({ component, x, y }) => {
      const id = namespaced(cell.id, component.id, multi);
      const sourceLine = componentSourceLine(cell, component.id, component.line);
      nodes.push({
        id,
        type: "component",
        position: { x: originX + x, y: originY + y },
        data: {
          nodeId: id,
          label: component.label ?? component.id,
          componentType: component.type,
          motionKey: motionKeyForLine(cell.id, "component", sourceLine, component.id),
          layoutKind: "component",
          cellId: cell.id
        },
        draggable: true
      });
    });

    const byDirection = cell.externals.reduce<Record<string, string[]>>((groups, external) => {
      groups[external.direction] = [...(groups[external.direction] ?? []), external.id];
      return groups;
    }, {});

    const gatewayDirections = gatewayDirectionsByCell.get(cell.id) ?? new Set<BoundaryDirection>();
    Array.from(gatewayDirections).forEach((direction) => {
      const position = gatewayPosition(direction, layout.width, layout.height);
      const id = gatewayNodeId(cell.id, direction, multi);
      nodes.push({
        id,
        type: "gateway",
        position: { x: originX + position.x, y: originY + position.y },
        data: { nodeId: id, direction, layoutKind: "gateway", cellId: cell.id },
        draggable: false
      });
    });

    cell.externals.forEach((external) => {
      const peers = byDirection[external.direction] ?? [];
      const position = externalPosition(
        external.direction as BoundaryDirection,
        peers.indexOf(external.id),
        peers.length,
        layout.width,
        layout.height
      );
      const id = externalNodeId(cell.id, external.id, multi);
      nodes.push({
        id,
        type: "external",
        position: { x: originX + position.x, y: originY + position.y },
        data: {
          nodeId: id,
          label: external.label ?? external.id,
          externalType: external.type,
          direction: external.direction,
          motionKey: motionKeyForLine(cell.id, "external", external.line, external.id),
          layoutKind: "external",
          cellId: cell.id
        },
        draggable: true
      });
    });

    emitCellEdges(cell, multi, sharedExternalIds, edges);
  });

  emitCrossEdges(project, multi, edges);
  emitDecoupledCrossEdges(project, multi, cellBoxes, nodes, edges);

  project.sharedExternals.forEach((ext) => {
    const g = cellGraph.node(`external-${ext.id}`) ?? { x: 0, y: 0 };
    nodes.push({
      id: `external-${ext.id}`,
      type: "external",
      position: { x: g.x - externalSize / 2, y: g.y - externalSize / 2 },
      data: {
        nodeId: `external-${ext.id}`,
        label: ext.label ?? ext.id,
        externalType: ext.type,
        direction: ext.direction ?? "east",
        motionKey: motionKeyForLine("shared", "external", ext.line, ext.id),
        layoutKind: "shared-external"
      },
      draggable: true
    });
  });

  const xs = nodes.flatMap((n) => [n.position.x, n.position.x + (n.width ?? 0)]);
  const ys = nodes.flatMap((n) => [n.position.y, n.position.y + (n.height ?? 0)]);
  const minX = xs.length ? Math.min(...xs) : 0;
  const minY = ys.length ? Math.min(...ys) : 0;
  const maxX = xs.length ? Math.max(...xs) : 0;
  const maxY = ys.length ? Math.max(...ys) : 0;
  const width = maxX - minX + 400;
  const height = maxY - minY + 400;

  return { nodes, edges, cellSize: { width, height } };
}
