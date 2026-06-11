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

import { createElement, type ReactNode } from 'react';
import type { CustomView } from '@asdlc/explorer';
import { CellDiagramView } from './CellDiagramView.js';
import type { CellDiagramComponent } from './buildProjectModel.js';

/** Stable id used as the Explorer's `activePath` sentinel for the cell diagram. */
export const CELL_DIAGRAM_VIEW_ID = 'cell-diagram';
export const CELL_DIAGRAM_VIEW_LABEL = 'Cell Diagram';

export interface CreateCellDiagramViewOptions {
  components: CellDiagramComponent[];
  label?: string;
  icon?: ReactNode;
  emptyState?: ReactNode;
}

/**
 * Build a {@link CustomView} for the Explorer that renders the cell diagram.
 * Pass the returned object inside `customViews` on `<Explorer>`.
 */
export function createCellDiagramView({
  components,
  label = CELL_DIAGRAM_VIEW_LABEL,
  icon,
  emptyState,
}: CreateCellDiagramViewOptions): CustomView {
  return {
    id: CELL_DIAGRAM_VIEW_ID,
    label,
    icon,
    content: createElement(CellDiagramView, { components, emptyState }),
  };
}
