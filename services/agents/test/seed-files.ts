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
 * TEST-ONLY fixture (never a production code path): the starting spec bundle the
 * agent tests mutate. Mirrors the hello-api example bundle — free-form
 * prose, markdown-with-frontmatter, and indentation-sensitive OpenAPI YAML: the
 * three shapes the file tools must handle. `@aep/agent-stream` keeps its own
 * client-side copy for the fold tests.
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

  "specs/design/design.cell": `title Hello API

component hello-api as "Hello API" service

north -> hello-api
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
  "dependencies": [],
  "skillsPinned": ["go", "api-management"],
  "description": "A simple public Go HTTP service (port 9090, net/http) that returns a hello-world JSON message. No authentication. Endpoints are specified in openapi.yaml."
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
