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
 * Where a component's prototype lives: one file per `web-application`
 * component, in the slot beside its `design.json`.
 */

const COMPONENTS_DIR = "specs/design/components/";
const FILE_NAME = "prototype.json";

const ARTIFACT_RE = /^specs\/design\/components\/([^/]+)\/prototype\.json$/;

function isComponentName(name: string): boolean {
  return name !== "" && name !== "." && name !== ".." && !name.includes("/");
}

/** `specs/design/components/<component>/prototype.json`. */
export function prototypeArtifactPath(component: string): string {
  if (!isComponentName(component)) {
    throw new Error(`prototypeArtifactPath: ${JSON.stringify(component)} is not a component directory name`);
  }
  return `${COMPONENTS_DIR}${component}/${FILE_NAME}`;
}

/** The component a prototype artifact path belongs to, or undefined when `path` is not one. */
export function prototypeArtifactComponent(path: string): string | undefined {
  const component = ARTIFACT_RE.exec(path)?.[1];
  return component !== undefined && isComponentName(component) ? component : undefined;
}

/** True only for `specs/design/components/<component>/prototype.json`. */
export function isPrototypeArtifactPath(path: string): boolean {
  return prototypeArtifactComponent(path) !== undefined;
}
