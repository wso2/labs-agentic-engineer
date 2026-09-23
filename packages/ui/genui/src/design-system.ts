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

import type { ComponentType, ReactNode } from "react";
import type { GenUiComponentName, GenUiPropsOf } from "./catalog/index.js";

/**
 * What every implementation receives. This is AEP's own render contract, not a
 * renderer library's: implementations never import json-render (or A2UI), so
 * swapping the library means rewriting the adapter, not the design systems.
 */
export interface GenUiRenderProps<P> {
  /** Props already resolved (bindings evaluated) and shaped by the catalog. */
  props: P;
  /** Rendered child elements, for components with hasChildren. */
  children?: ReactNode;
  /** Fire one of the component's declared events. */
  emit: (event: string) => void;
  /**
   * Write a new value for a prop the spec bound with `$bindState` (e.g. what
   * the user typed into a TextField). A no-op for a prop that is not bound.
   */
  setProp: <K extends keyof P & string>(prop: K, value: P[K]) => void;
}

/** One implementation per catalog component, checked at compile time. */
export type GenUiImplementations = {
  [K in GenUiComponentName]: ComponentType<GenUiRenderProps<GenUiPropsOf<K>>>;
};

/**
 * Everything a design system supplies to render generated UIs. Each design
 * system lives in its own package (today @aep/ui-genui-oxygen) so a host
 * installs only the one it uses, and every one is held to the same catalog
 * and the same conformance suite (@aep/ui-genui/testing).
 */
export interface GenUiDesignSystem {
  /** Shown in demos and test names, e.g. "Oxygen UI". */
  readonly name: string;
  readonly components: GenUiImplementations;
  /**
   * Stands in for an element that cannot be shown: props that fail the
   * component's schema, or a type outside the catalog. The message is written
   * by the adapter, so every design system says the same thing.
   */
  readonly InvalidElement: ComponentType<{ message: string }>;
  /** Wraps the whole generated UI, e.g. to scope the design system's styles. */
  readonly Root?: ComponentType<{ children: ReactNode }>;
}
