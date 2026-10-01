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

import { intEnv } from "./env.js";

/** Discrete Postgres fields from release binding (platform shape). */
export interface DatabaseDiscreteConfig {
  host: string;
  port: number; // default 5432 when assembling
  user: string;
  password: string;
  name: string;
  sslMode?: string; // only when DB_SSLMODE set
}

const DEFAULT_PORT = 5432;

function nonEmpty(value: string | undefined): string | undefined {
  return value === undefined || value === "" ? undefined : value;
}

/**
 * Resolve the Postgres connection string for ConversationStore.
 * Precedence: DATABASE_URL (verbatim) > assembled discrete DB_*.
 * Returns undefined when neither path yields a URL → in-memory store.
 */
export function resolveDatabaseUrl(env: typeof process.env = process.env): string | undefined {
  const databaseUrl = nonEmpty(env.DATABASE_URL);
  if (databaseUrl) {
    return databaseUrl;
  }
  const discrete = readDiscreteDatabaseConfig(env);
  return discrete ? assembleDatabaseUrl(discrete) : undefined;
}

/**
 * Build postgres:// URL from discrete fields. Omits sslmode query param when
 * sslMode is undefined/empty (matches aep-api databaseURL).
 */
export function assembleDatabaseUrl(discrete: DatabaseDiscreteConfig): string {
  const userinfo = `${encodeURIComponent(discrete.user)}:${encodeURIComponent(discrete.password)}`;
  const base = `postgres://${userinfo}@${discrete.host}:${discrete.port}/${discrete.name}`;
  if (!discrete.sslMode) {
    return base;
  }
  return `${base}?sslmode=${encodeURIComponent(discrete.sslMode)}`;
}

/**
 * Read discrete fields from env. Returns null when DATABASE_URL is set or when
 * the discrete set is incomplete (no partial assembly).
 */
export function readDiscreteDatabaseConfig(
  env: typeof process.env = process.env,
): DatabaseDiscreteConfig | null {
  if (nonEmpty(env.DATABASE_URL)) {
    return null;
  }

  const host = nonEmpty(env.DB_HOST);
  const user = nonEmpty(env.DB_USER);
  const password = nonEmpty(env.DB_PASSWORD);
  const name = nonEmpty(env.DB_NAME);

  if (!host || !user || !password || !name) {
    return null;
  }

  const port = intEnv(env.DB_PORT, DEFAULT_PORT);
  const sslMode = nonEmpty(env.DB_SSLMODE);

  return {
    host,
    port,
    user,
    password,
    name,
    ...(sslMode ? { sslMode } : {}),
  };
}
