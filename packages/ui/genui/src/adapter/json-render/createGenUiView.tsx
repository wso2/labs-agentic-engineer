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

import { useEffect, useMemo, useRef, type ComponentType } from "react";
import { createStateStore } from "@json-render/core";
import {
  createRenderer,
  useStateStore,
  type ComponentRenderProps,
  type ComponentRenderer,
} from "@json-render/react";
import {
  actionStateFor,
  dispatchGenUiAction,
  genUiActions,
  genUiActionStatePath,
  genUiComponents,
  type GenUiActionHandlers,
  type GenUiActionName,
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
  function Adapted({ element, children, emit, bindings, loading }: ComponentRenderProps) {
    const { set } = useStateStore();
    const parsed = schema.safeParse(element.props);
    if (!parsed.success) {
      return loading ? null : <InvalidElement message={invalidMessage(name)} />;
    }
    // A failed action rejects (so a spec's onSuccess is skipped and its
    // onError runs) but its outcome is already in state and reported to the
    // host, so an event nobody awaits must not surface as an unhandled
    // rejection.
    const fire = (event: string) => {
      void Promise.resolve(emit(event) as unknown).catch(() => undefined);
    };
    const setProp = (prop: string, value: unknown) => {
      const path = bindings?.[prop];
      if (path) set(path, value);
    };
    // Sound: parsed.data is the output of genUiComponents[K].props, the schema
    // GenUiPropsOf<K> is derived from.
    return (
      <Impl props={parsed.data as GenUiPropsOf<K>} emit={fire} setProp={setProp}>
        {children}
      </Impl>
    );
  }
  Adapted.displayName = `GenUi(${name})`;
  return Adapted;
}

const componentNames = Object.keys(genUiComponents) as GenUiComponentName[];

function isCatalogAction(name: string): name is GenUiActionName {
  return Object.hasOwn(genUiActions, name);
}

// Identifies each store a view creates; see the renderer's key below.
let storeGeneration = 0;

// JSON Pointer escaping for a single path segment (RFC 6901).
function escapePointer(segment: string): string {
  return segment.replaceAll("~", "~0").replaceAll("/", "~1");
}

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
    // One store per spec, so what the user typed survives re-renders; host
    // state is layered in on creation and again whenever the host changes it.
    const hostState = useRef(state);
    hostState.current = state;
    const { store, generation } = useMemo(
      () => ({
        store: createStateStore({ ...(spec?.state ?? {}), ...(hostState.current ?? {}) }),
        generation: ++storeGeneration,
      }),
      [spec],
    );
    useEffect(() => {
      for (const [key, value] of Object.entries(state ?? {})) {
        store.set(`/${escapePointer(key)}`, value);
      }
    }, [store, state]);

    // A fresh closure each render is fine: json-render rebuilds its action
    // proxy on every render too, so the latest handlers are always the ones used.
    const onAction = async (action: string, params?: Record<string, unknown>) => {
      const statePath = isCatalogAction(action) ? genUiActionStatePath(action) : undefined;
      if (statePath) store.set(statePath, { status: "pending", fieldErrors: {} });
      const outcome = await dispatchGenUiAction(handlers, action, params);
      const settled = actionStateFor(outcome);
      if (statePath) store.set(statePath, settled);
      onActionOutcome?.(outcome);
      // Rejecting is what makes json-render skip the spec's onSuccess and run
      // its onError (where "$error.message" is this message).
      if (settled.status === "error") throw new Error(settled.message);
    };

    const view = (
      <CatalogRenderer
        // json-render's StateProvider adopts the store it mounts with and
        // ignores a new one, so a new spec (and so a new store) remounts it.
        // Otherwise action results would go to a store nothing reads.
        key={generation}
        spec={spec}
        store={store}
        // json-render types onAction as returning void but awaits whatever it
        // returns; the promise is what drives onSuccess / onError.
        onAction={onAction as (action: string, params?: Record<string, unknown>) => void}
        loading={loading}
        fallback={UnknownElement}
      />
    );
    return Root ? <Root>{view}</Root> : view;
  }
  GenUiView.displayName = `GenUiView(${designSystem.name})`;
  return GenUiView;
}
