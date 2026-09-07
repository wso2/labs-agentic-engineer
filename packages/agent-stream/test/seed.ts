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
 * A self-contained spec-bundle fixture for this package's fold tests: free-form
 * prose, markdown-with-frontmatter, indentation-sensitive OpenAPI YAML, and a
 * schema-gated component `design.json` — the shapes `FileBundle` must handle.
 * The agents service keeps its own copy (`SEED_FILES` in `test/seed-files.ts`); this
 * package is fold-only and depends on nothing in the service, so its tests carry
 * their own corpus.
 */
export const SEED_FILES: Record<string, string> = {
  "specs/requirements/prd.md": `# Overview

A simple API that responds with "Hello, World!" when called.

# Personas

- Developer — calls the API to get a hello world response.

# Features

- A developer sends a request to the API.
- The API responds with "Hello, World!" in the response body.
- The response is in JSON format with a message field.
- The API is accessible via a single endpoint.
- Requests work without requiring any parameters or authentication.
`,

  "specs/design/design.cell": `---
sourceSpec: v1
---
title Hello API

component hello-api as "Hello API" service
component postgres as "Postgres" database

north -> hello-api
hello-api -> postgres
`,

  "specs/design/components/hello-api/design.json": `{
  "name": "hello-api",
  "type": "service",
  "version": "0.1.0",
  "language": "Go",
  "buildpack": "docker",
  "appPath": "hello-api",
  "entrypoint": "deployment/service",
  "exposure": "internet",
  "dependencies": [{ "kind": "platform-resource", "name": "postgres", "resourceType": "postgres" }],
  "description": "Implement a simple Go HTTP service on port 9090 using net/http. Expose GET /hello that returns {\\"message\\": \\"Hello, World!\\"} with Content-Type: application/json. Include GET /health returning 200 OK for liveness probes. This is a public API — no authentication required, no X-User-Id checks."
}
`,

  "specs/design/components/hello-api/openapi.yaml": `openapi: 3.0.3
info:
  title: Hello API
  version: 1.0.0
  description: A simple API that responds with "Hello, World!" when called.

servers:
  - url: /
    description: Default server

paths:
  /hello:
    get:
      summary: Get hello world message
      description: Returns a simple "Hello, World!" message in JSON format.
      operationId: getHello
      responses:
        '200':
          description: Successful response with hello message
          content:
            application/json:
              schema:
                type: object
                required:
                  - message
                properties:
                  message:
                    type: string
                    example: "Hello, World!"

  /health:
    get:
      summary: Health check endpoint
      description: Returns the health status of the service.
      operationId: getHealth
      responses:
        '200':
          description: Service is healthy
`,
};
