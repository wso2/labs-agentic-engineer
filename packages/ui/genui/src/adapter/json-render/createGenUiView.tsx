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

import { useMemo, type ComponentType } from "react";
import {
  createRenderer,
  type ComponentRenderProps,
  type ComponentRenderer,
} from "@json-render/react";
import {
  dispatchGenUiAction,
  genUiComponents,
  type GenUiActionHandlers,
  type GenUiComponentName,
  type GenUiDispatchOutcome,
  type GenUiPropsOf,
} from "../../catalog/index.js";
import type { GenUiDesignSystem, GenUiRenderProps } from "../../design-system.js";
import { jsonRenderCatalog } from "./catalog.js";
import type { GenUiSpec } from "./spec.js";

/** The one wording every design system shows for an element it cannot render. */
function invalidMessage(type: string): string {
  return `Could not display a ${type}: its content did not match the expected shape.`;
}

/**
 * Bridges json-render's render props to AEP's GenUiRenderProps. Props are
 * re-parsed with the catalog schema at render time, so an implementation only
 * ever sees props that match its contract even if a spec skipped
 * validateGenUiSpec(). While a spec is still streaming, an element whose props
 * are not complete yet renders nothing instead of a warning.
 */
function adapt<K extends GenUiComponentName>(
  name: K,
  designSystem: GenUiDesignSystem,
): ComponentRenderer {
  // Sound: the mapped type assigns this exact prop type to key K.
  const Impl = designSystem.components[name] as ComponentType<
    GenUiRenderProps<GenUiPropsOf<K>>
  >;
  const { InvalidElement } = designSystem;
  const schema = genUiComponents[name].props;
  function Adapted({ element, children, emit, loading }: ComponentRenderProps) {
    const parsed = schema.safeParse(element.props);
    if (!parsed.success) {
      return loading ? null : <InvalidElement message={invalidMessage(name)} />;
    }
    // Sound: parsed.data is the output of genUiComponents[K].props, the schema
    // GenUiPropsOf<K> is derived from.
    return (
      <Impl props={parsed.data as GenUiPropsOf<K>} emit={emit}>
        {children}
      </Impl>
    );
  }
  Adapted.displayName = `GenUi(${name})`;
  return Adapted;
}

const componentNames = Object.keys(genUiComponents) as GenUiComponentName[];

export interface GenUiViewProps {
  /** The spec to render; null renders nothing. */
  spec: GenUiSpec | null;
  /** Values for the spec's state bindings, layered over the spec's own state. */
  state?: Record<string, unknown>;
  /** What each catalog action does in this host. */
  handlers?: GenUiActionHandlers;
  /** Reports every action a spec fires, including rejected ones. */
  onActionOutcome?: (outcome: GenUiDispatchOutcome) => void;
  /** True while the spec is still streaming in. */
  loading?: boolean;
}

/**
 * Builds the view component for one design system. Call it once per design
 * system at module level (each design-system package exports the result as
 * GenUiView); the renderer is built here, not on every render. Every action
 * passes through dispatchGenUiAction, so only catalog actions with valid
 * params reach the host's handlers. Any provider the design system needs
 * (e.g. OxygenUIThemeProvider) is the host's to supply.
 */
export function createGenUiView(
  designSystem: GenUiDesignSystem,
): ComponentType<GenUiViewProps> {
  const { InvalidElement, Root } = designSystem;
  function UnknownElement({ element }: ComponentRenderProps) {
    return <InvalidElement message={invalidMessage(`"${element.type}"`)} />;
  }
  const CatalogRenderer = createRenderer(
    jsonRenderCatalog,
    Object.fromEntries(componentNames.map((name) => [name, adapt(name, designSystem)])),
  );

  function GenUiView({
    spec,
    state,
    handlers = {},
    onActionOutcome,
    loading = false,
  }: GenUiViewProps) {
    // A fresh closure each render is fine: json-render rebuilds its action
    // proxy on every render too, so the latest handlers are always the ones used.
    const onAction = (action: string, params?: Record<string, unknown>) => {
      void dispatchGenUiAction(handlers, action, params).then((outcome) =>
        onActionOutcome?.(outcome),
      );
    };

    const initialState = useMemo(
      () => ({ ...(spec?.state ?? {}), ...(state ?? {}) }),
      [spec?.state, state],
    );

    const view = (
      <CatalogRenderer
        spec={spec}
        state={initialState}
        onAction={onAction}
        loading={loading}
        fallback={UnknownElement}
      />
    );
    return Root ? <Root>{view}</Root> : view;
  }
  GenUiView.displayName = `GenUiView(${designSystem.name})`;
  return GenUiView;
}
