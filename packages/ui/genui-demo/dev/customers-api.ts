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

import type { Plugin } from "vite";

interface Customer {
  id: string;
  name: string;
  contactName: string;
  contactEmail: string;
  contactPhone?: string;
}

// A stand-in for /api/customers (the real one listens on localhost:2001), so
// the demo can show every outcome with nothing else running. GET lists the
// customers; POST answers 201 on success, 400 with field errors, or 409 for a
// taken name. Its checks mirror what a server would do; the catalog's own
// schema catches most of them earlier, in the browser.
export function mockCustomersApi(): Plugin {
  const customers: Customer[] = [
    { id: "c-1", name: "Acme", contactName: "Jane Doe", contactEmail: "jane@acme.test" },
  ];
  return {
    name: "mock-customers-api",
    configureServer(server) {
      server.middlewares.use("/api/customers", (req, res, next) => {
        if (req.method === "GET") {
          res.setHeader("Content-Type", "application/json");
          res.end(JSON.stringify(customers));
          return;
        }
        if (req.method !== "POST") return next();
        let raw = "";
        req.on("data", (chunk: Buffer) => (raw += chunk.toString()));
        req.on("end", () => {
          const send = (status: number, body: unknown) => {
            res.statusCode = status;
            res.setHeader("Content-Type", "application/json");
            res.end(JSON.stringify(body));
          };
          let input: Partial<Customer>;
          try {
            input = JSON.parse(raw) as Partial<Customer>;
          } catch {
            return send(400, { message: "The request body is not JSON." });
          }
          const fieldErrors: Record<string, string> = {};
          if (!input.name?.trim()) fieldErrors.name = "Name is required.";
          if (!input.contactName?.trim()) fieldErrors.contactName = "Contact name is required.";
          if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(input.contactEmail ?? "")) {
            fieldErrors.contactEmail = "Contact email is not a valid address.";
          }
          if (Object.keys(fieldErrors).length > 0) {
            return send(400, { message: "Some fields are invalid.", fieldErrors });
          }
          const name = input.name!.trim();
          if (customers.some((c) => c.name.toLowerCase() === name.toLowerCase())) {
            return send(409, {
              message: `A customer named "${name}" already exists.`,
              fieldErrors: { name: "Pick a different name." },
            });
          }
          const customer: Customer = {
            id: `c-${customers.length + 1}`,
            name,
            contactName: input.contactName!.trim(),
            contactEmail: input.contactEmail!,
            ...(input.contactPhone ? { contactPhone: input.contactPhone } : {}),
          };
          customers.push(customer);
          send(201, customer);
        });
      });
    },
  };
}
