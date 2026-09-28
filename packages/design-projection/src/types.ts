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
 * design.json — the DERIVED machine view of a design bundle. Nobody authors
 * this: it is projected deterministically from the component design.md
 * frontmatter whenever a consumer asks. Consumers: the cell diagram (its
 * Project model maps 1:1 onto components/services/connections/
 * deploymentMetadata), coding-agent dispatch, and task generation.
 * The authored source of truth stays in the spec bundle's frontmatter.
 */

export interface ProjectDesign {
  /** Cell-diagram model version this projection targets. */
  modelVersion: string;
  /** Project identity (the platform's project id; thread name in the playground). */
  id: string;
  name: string;
  components: ProjectDesignComponent[];
}

export interface ProjectDesignComponent {
  /** Component directory name, e.g. "expense-api". */
  id: string;
  type: string; // authored kind, passed through ("service" | "webapp" | "ai-agent" | future kinds)
  version: string;
  /**
   * Skill names preloaded for this component's coding agent (from the
   * component's design.json `skillsPinned`, née `skillsPinned`). Normalized
   * output — always the winning value; the raw-document dual-key
   * compatibility lives in project-design.ts, not here.
   */
  skillsPinned: string[];
  /** Build facts for the coding agent / OpenChoreo (from frontmatter). */
  build: {
    language?: string;
    buildpack?: string;
    appPath?: string;
    entrypoint?: string;
  };
  /** Exposed services (cell-diagram shape); present for type=service. */
  services?: Record<string, ProjectDesignService>;
  /** Typed dependency edges (cell-diagram shape). */
  connections: ProjectDesignConnection[];
  /** Bundle-relative paths of this component's spec artifacts. */
  artifacts: Record<string, string>;
}

export interface ProjectDesignService {
  id: string;
  label: string;
  /** Protocol, e.g. "http". */
  type: string;
  deploymentMetadata: {
    gateways: {
      internet: { isExposed: boolean };
      intranet: { isExposed: boolean };
    };
  };
}

export interface ProjectDesignConnection {
  /** "<type>://<target>", e.g. "http://expense-api", "datastore://postgres". */
  id: string;
  type: string;
  /** False for systems outside the platform (external SaaS, legacy). */
  onPlatform?: boolean;
}
