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

import type { components } from "../../../generated/aep-api";

type ConfigKeyDTO = components["schemas"]["ConfigKeyDTO"];
type EnvValueCellDTO = components["schemas"]["EnvValueCellDTO"];

export const REQUIRED_FIELD = "This field is required";
export const REQUIRED_CONFIG_KEY = "Add at least one config key";

export type RegisterFormErrors = {
  name?: string;
  description?: string;
  consumptionInstructions?: string;
  configKeys?: string;
  keys: { key?: string; description?: string }[];
  values: Record<string, string>;
};

export function envValueCellKey(environment: string, key: string): string {
  return `${environment}:${key}`;
}

function cellConfigured(
  cells: EnvValueCellDTO[] | undefined,
  environment: string,
  key: string,
): boolean {
  return (cells ?? []).some(
    (c) => c.environment === environment && c.key === key && c.status === "configured",
  );
}

/** Field errors for a Register/Save click. Empty object keys mean that slot is valid. */
export function validateRegisterForm(input: {
  name: string;
  description: string;
  consumptionInstructions: string;
  keys: ConfigKeyDTO[];
  values: Record<string, string>;
  envNames: string[];
  isEdit: boolean;
  envCells?: EnvValueCellDTO[];
}): RegisterFormErrors | null {
  const errors: RegisterFormErrors = { keys: [], values: {} };
  let invalid = false;

  if (!input.name.trim()) {
    errors.name = REQUIRED_FIELD;
    invalid = true;
  }
  if (!input.description.trim()) {
    errors.description = REQUIRED_FIELD;
    invalid = true;
  }
  if (!input.consumptionInstructions.trim()) {
    errors.consumptionInstructions = REQUIRED_FIELD;
    invalid = true;
  }
  if (input.keys.length === 0) {
    errors.configKeys = REQUIRED_CONFIG_KEY;
    invalid = true;
  }
  errors.keys = input.keys.map((cfg) => {
    const row: { key?: string; description?: string } = {};
    if (!cfg.key.trim()) {
      row.key = REQUIRED_FIELD;
      invalid = true;
    }
    if (!(cfg.description ?? "").trim()) {
      row.description = REQUIRED_FIELD;
      invalid = true;
    }
    return row;
  });
  for (const cfg of input.keys) {
    const key = cfg.key.trim();
    if (!key) continue;
    for (const environment of input.envNames) {
      const filled = (input.values[envValueCellKey(environment, key)] ?? "").trim();
      if (filled) continue;
      if (
        input.isEdit &&
        cfg.secret &&
        cellConfigured(input.envCells, environment, cfg.key)
      ) {
        continue;
      }
      errors.values[envValueCellKey(environment, cfg.key)] = REQUIRED_FIELD;
      invalid = true;
    }
  }
  return invalid ? errors : null;
}
