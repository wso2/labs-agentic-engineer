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
 * THE PLAN AS A COMPOSE FILE. Pure: a `WirePlan` and the session's gateway
 * identity in, YAML out.
 *
 * Compose rather than host processes, and each service BUILT FROM ITS OWN
 * DOCKERFILE rather than run with a per-stack command. That costs a real image
 * build on every service change, and buys the three things a host process
 * cannot: the Dockerfile the platform will build is proved here first, there is
 * no per-stack "how do I run this" table to keep current, and the ports problem
 * disappears — every service listens on the one port the component contract
 * names, siblings reach each other by service name, and only the host side is
 * mapped. Ten services never collide, and no stack skill learns a variable.
 *
 * The file is written into the state dir, never into the project: the tree a
 * coding run left is the tree the next one reads.
 */

import { stringify } from "yaml";
import { SERVICE_PORT, type WirePlan } from "./plan.js";

/** The identity the session's services will trust, minted per project by `assertion.ts`. */
export interface GatewayIdentity {
  /** PEM X.509 certificate carrying the public half. */
  certificate: string;
  issuer: string;
  header: string;
}

/**
 * Health, without curl.
 *
 * The JRE images the stack skills build on carry bash and no curl, so an HTTP
 * probe would fail on every image for a reason that has nothing to do with the
 * service. A TCP connect through bash's own /dev/tcp answers the only question
 * `--wait` needs: is it listening yet. `start_period` covers a JVM's start and
 * the retries cover a slow first connection to the database.
 */
const SERVICE_HEALTHCHECK = {
  test: ["CMD", "bash", "-c", `exec 3<>/dev/tcp/127.0.0.1/${String(SERVICE_PORT)}`],
  interval: "5s",
  timeout: "3s",
  retries: 40,
  start_period: "15s",
};

interface ComposeService {
  image?: string;
  build?: { context: string };
  environment?: Record<string, string>;
  ports?: string[];
  volumes?: string[];
  depends_on?: Record<string, { condition: string }>;
  healthcheck?: Record<string, unknown>;
}

/**
 * The compose document.
 *
 * `projectDir` is absolute and build contexts are resolved against it, so the
 * file can be run from the state dir (where it lives) without a relative path
 * pointing at nothing. `initSql` is the developer's optional schema file, passed
 * only when it exists.
 */
export function composeDocument(
  plan: WirePlan,
  gateway: GatewayIdentity,
  projectDir: string,
  initSql?: string,
): string {
  const services: Record<string, ComposeService> = {};
  const volumes: Record<string, Record<string, never>> = {};

  for (const database of plan.databases) {
    services[database.name] = {
      image: "postgres:16",
      environment: {
        POSTGRES_DB: database.database,
        POSTGRES_USER: database.user,
        POSTGRES_PASSWORD: database.password,
      },
      // A schema the app does not create itself: Postgres runs everything in
      // that directory once, when it initializes an empty data volume, so this
      // takes effect on the first start and after `--fresh` and never again.
      volumes: [
        `${database.volume}:/var/lib/postgresql/data`,
        ...(initSql ? [`${initSql}:/docker-entrypoint-initdb.d/init.sql:ro`] : []),
      ],
      healthcheck: {
        test: ["CMD-SHELL", `pg_isready -U ${database.user}`],
        interval: "3s",
        timeout: "3s",
        retries: 20,
      },
    };
    volumes[database.volume] = {};
  }

  for (const service of plan.services) {
    services[service.name] = {
      build: { context: `${projectDir}/${service.appPath}` },
      environment: {
        ...service.env,
        // The whole trust anchor. The service verifies every assertion against
        // this certificate and starts only if all three are set (see the
        // `ballerina` skill's gateway_assertion.bal), so whoever holds the
        // matching key is the gateway as far as it can tell — here, the dev
        // server in front of the app.
        GATEWAY_ASSERTION_CERTIFICATE: gateway.certificate,
        GATEWAY_ASSERTION_ISSUER: gateway.issuer,
        GATEWAY_ASSERTION_HEADER: gateway.header,
      },
      ports: [`${String(service.hostPort)}:${String(SERVICE_PORT)}`],
      ...(service.dependsOn.length > 0
        ? {
            depends_on: Object.fromEntries(
              [...service.dependsOn].sort().map((name) => [name, { condition: "service_healthy" }]),
            ),
          }
        : {}),
      healthcheck: SERVICE_HEALTHCHECK,
    };
  }

  const document: Record<string, unknown> = { name: plan.composeProject, services };
  if (Object.keys(volumes).length > 0) document.volumes = volumes;
  return stringify(document, { lineWidth: 0 });
}

/** What may be written down and shown: the plan with every secret replaced. */
export function maskedPlan(plan: WirePlan): WirePlan {
  return {
    ...plan,
    databases: plan.databases.map((database) => ({ ...database, password: "***" })),
    services: plan.services.map((service) => ({
      ...service,
      env: Object.fromEntries(
        Object.entries(service.env).map(([key, value]) => [
          key,
          maskValue(key, value, service.secretEnv),
        ]),
      ),
    })),
  };
}

/**
 * A value is masked by PROVENANCE first and by its name second.
 *
 * `secretEnv` is the authority: the planner knows a value is a password because
 * of the dependency output it read it from, and that is true whatever the design
 * chose to call the variable. The name pattern stays as a second net for
 * everything the planner did not mint — a variable a design set by hand, a
 * future dependency kind nobody has classified yet — but it is a net, not the
 * rule. It is deliberately not the rule because it cannot be one: it is
 * case-sensitive and matches `PASSWORD` literally, so `DB_PASS` and
 * `db_password` both walk straight through it, and what walks through is written
 * to `plan.json` and handed to the triage agent.
 */
function maskValue(key: string, value: string, secretEnv: readonly string[]): string {
  if (secretEnv.includes(key)) return "***";
  return /PASSWORD|SECRET|CERTIFICATE|TOKEN|KEY$/.test(key) ? "***" : value;
}
