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
 * The project's own resource a Promote takes over. Two projects may each hold
 * a resource of the same name, so the project is part of the address.
 */
export type PromoteTarget = { project: string; name: string };

/** The `promote` search param: `<project>/<name>`. Project names carry no slash. */
export function encodePromoteTarget(target: PromoteTarget): string {
  return `${target.project}/${target.name}`;
}

export function decodePromoteTarget(raw: unknown): PromoteTarget | undefined {
  if (typeof raw !== "string") return undefined;
  const slash = raw.indexOf("/");
  if (slash <= 0) return undefined;
  const project = raw.slice(0, slash);
  const name = raw.slice(slash + 1);
  return project && name ? { project, name } : undefined;
}
