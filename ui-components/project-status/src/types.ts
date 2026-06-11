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

export type StageState = 'done' | 'active' | 'pending' | 'blocked';

export interface StageMetric {
  key: string;
  value: string;
}

export interface Stage {
  id: string;
  name: string;
  iteration: number;
  state: StageState;
  /**
   * Compact identifier shown under the puck (e.g. "Generating", "Spec ready").
   */
  headline: string;
  /**
   * Action-oriented guidance shown in the focus header when this stage is the
   * one being highlighted ("Currently" or "Up next"). Falls back to `headline`
   * when omitted, so passing it is optional but recommended for the live /
   * up-next stage.
   */
  help?: string;
  summary?: string;
  metrics?: StageMetric[];
  artifacts?: string[];
  changes?: string[];
  timestamp?: string;
  duration?: string;
}

/**
 * Whole-project lifecycle state. Drives the pill on the right side of the
 * header strip. Auto-derived from `stages` when not supplied:
 *   any 'active'  → 'active'   ("In progress")
 *   any 'blocked' → 'blocked'  ("Blocked")
 *   all 'done'    → 'done'     ("Shipped")
 *   otherwise     → 'paused'   ("Paused")
 */
export type ProjectState = 'active' | 'paused' | 'blocked' | 'done';

/**
 * Focus line shown above the rail. The component derives this from `stages`
 * by default; supply `focus` to override (e.g. when the host knows something
 * the stages don't, like an agent message).
 */
export interface FocusOverride {
  eyebrowKey?: 'currently' | 'next' | 'shipped' | 'blocked' | 'idle';
  eyebrow: string;
  stage?: string;
  detail?: string;
}

export interface ProjectStatusPolylineProps {
  stages: Stage[];
  onStageClick?: (stage: Stage) => void;
  /**
   * Whether stages are clickable. When false, the internal drawer is suppressed,
   * `onStageClick` is ignored, and nodes render as static (no cursor / focus / aria-button).
   * Default: true.
   */
  interactive?: boolean;
  mode?: 'light' | 'dark';
  /** Show the header strip (focus line + state pill + progress bar). Default: true. */
  showHeader?: boolean;
  /** Override the auto-derived project state pill. */
  projectState?: ProjectState;
  /** Override the auto-derived focus line. */
  focus?: FocusOverride;
  /** No-op. Retained for API compatibility with the previous polyline component. */
  maxIteration?: number;
}

export interface StageDrawerProps {
  stage: Stage | null;
  open: boolean;
  onClose: () => void;
  mode?: 'light' | 'dark';
}
