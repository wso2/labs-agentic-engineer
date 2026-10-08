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

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import { tanstackRouter } from "@tanstack/router-plugin/vite";

// The API through the port-forwarded aep-api, or any aep-api the variable names.
// Only the API is proxied: chat turns and the spec Room go from the browser
// straight to the org's AE Studio, at the URLs GET /api/v1/ae-studio names
// (its CORS allow-list has http://localhost:8090).
const apiTarget = process.env.API_PROXY_TARGET || "http://localhost:9090";

export default defineConfig({
  plugins: [
    // Must come before the react plugin (TanStack requirement).
    tanstackRouter({
      target: "react",
      routesDirectory: "./src/routes",
      generatedRouteTree: "./src/generated/routeTree.gen.ts",
    }),
    react(),
  ],
  resolve: {
    dedupe: ["react", "react-dom"],
  },
  optimizeDeps: {
    include: ["@wso2/oxygen-ui-icons-react > lucide-react"],
  },
  // As the old console's: the design viewers (Excalidraw's canvas) read these
  // Node globals.
  define: {
    global: "globalThis",
    "process.env": {},
  },
  server: {
    port: 8090,
    // A fixed port: sign-in redirects back to it, so http://localhost:8090/callback
    // must be a redirect URI of aep-console-client.
    strictPort: true,
    proxy: {
      "/aep-api-service": {
        target: apiTarget,
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/aep-api-service/, ""),
      },
    },
  },
});
