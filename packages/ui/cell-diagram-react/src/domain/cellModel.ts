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

export type BoundaryDirection = "north" | "east" | "south" | "west";
export type EdgeDirection = BoundaryDirection | "internal";
export type EdgeKind = "internal" | "inbound" | "outbound" | "exposure";

export interface Diagnostic {
  severity: "error";
  code?: string;
  message: string;
  line: number;
  column: number;
}

export interface ParsedComponent {
  id: string;
  label?: string;
  type?: string;
  line?: number;
}

export interface ParsedExternal {
  id: string;
  direction: BoundaryDirection;
  label?: string;
  type?: string;
  line?: number;
}

export interface ParsedEdge {
  id: string;
  source: string;
  target: string;
  direction: EdgeDirection;
  kind: EdgeKind;
  label?: string;
  line: number;
}

export interface ParsedCellDocument {
  title?: string;
  version?: string;
  components: ParsedComponent[];
  externals: ParsedExternal[];
  edges: ParsedEdge[];
}

export type ExternalNode = ParsedExternal;

export interface CellDiagramModel {
  title?: string;
  version?: string;
  components: ParsedComponent[];
  externals: ExternalNode[];
  edges: ParsedEdge[];
}

export interface ParseResult {
  document: ParsedCellDocument;
  diagnostics: Diagnostic[];
}

export interface CompileResult {
  model: CellDiagramModel | null;
  diagnostics: Diagnostic[];
}

export type CrossExit = "east" | "south";
export type CrossEntry = "west" | "north";
export type CrossMode = "connected" | "decoupled";

export interface CrossEdge {
  id: string;
  sourceCell: string;
  sourceComp: string;
  targetCell: string;
  targetComp: string;
  exit: CrossExit;
  entry: CrossEntry;
  mode: CrossMode;
  label?: string;
  line: number;
}

export interface CellModel {
  id: string;
  label?: string;
  version?: string;
  components: ParsedComponent[];
  externals: ExternalNode[];
  edges: ParsedEdge[];
}

export interface ProjectModel {
  title?: string;
  cells: CellModel[];
  crossEdges: CrossEdge[];
  sharedExternals: ExternalNode[];
}

export interface ProjectCompileResult {
  model: ProjectModel | null;
  diagnostics: Diagnostic[];
}
