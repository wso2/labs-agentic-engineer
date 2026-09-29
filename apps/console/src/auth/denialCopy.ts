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
 * The sentences the console uses to say a surface is withheld, for the ones
 * more than one place says.
 *
 * Keyed by what is withheld, NOT by the permission that withholds it: the two
 * are not the same shape. `ae:build-view` alone backs four different and
 * equally correct sentences in the project nav — builds, deployments,
 * validations, issues — so a key per permission could only be reached by
 * making three of them vaguer than the surface they belong to.
 *
 * What lives here is the copy that had drifted into several files at once,
 * and in particular every pair where a nav tooltip and the page it leads to
 * must agree: AppLayout's rail says why a destination is locked, and the
 * destination says it again on arrival. Bespoke one-offs stay at their call
 * site — a single sentence used once is not shared copy, and hoisting it here
 * would only put it further from the button it describes.
 */
export const DENIED = {
  // The nav rail (AppLayout), paired with the surface each leg leads to. The
  // rail is covered as a unit rather than by the ≥2-uses rule above: four of
  // its eight legs are gated on ae:build-view alone, and leaving the
  // single-use ones inline would scatter one rail's voice across two places.
  viewTheSpec: "You don't have permission to view the spec.",
  viewBuilds: "You don't have permission to view builds.",
  viewDeployments: "You don't have permission to view deployments.",
  viewValidations: "You don't have permission to view validations.",
  viewIssues: "You don't have permission to view issues.",
  viewResources: "You don't have permission to view resources.",
  viewEndpoints: "You don't have permission to view endpoints.",
  viewAlerts: "You don't have permission to view alerts.",

  // Settings: each section's tab tooltip paired with the section's own body.
  viewCredentials: "You don't have permission to view credentials.",
  viewSkills: "You don't have permission to view skills.",
  viewUsage: "You don't have permission to view usage.",

  // Writes whose sentence is the same wherever the action is offered.
  configureSkills: "You don't have permission to configure skills.",
  configureResources: "You don't have permission to configure resources.",
  registerOrEditResources:
    "You don't have permission to register or edit resources.",
  createProject: "You don't have permission to create a new project.",
  promoteProject: "You don't have permission to promote this project.",
  generateDesign: "You don't have permission to generate the design.",
  cancelRun: "You don't have permission to cancel this run.",
} as const;
