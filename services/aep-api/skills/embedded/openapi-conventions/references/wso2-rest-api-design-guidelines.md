# WSO2 REST API Design Guidelines — Reference Summary

Source: WSO2 REST APIs Design Guidelines whitepaper.
Read this at the start of the Design Workflow. It covers the full 7-step design process,
resource taxonomy, URI rules, HTTP semantics, special behaviour patterns, and error format.

---

## Overall Approach

Designing a RESTful API follows a sequential seven-step process. Each step builds on the
previous one — the resource model follows from the data model, URIs follow from the resource
model, HTTP methods follow from the resource types, and so on.

1. Create Data Model
2. Derive Resources
3. Decide on Representations
4. Name Resources by URIs
5. Determine HTTP Methods
6. Determine Special Behaviour
7. Consider Errors

The goal of following this process is to produce a Level 2 Richardson Maturity Model API —
one that has a well-defined resource model, correct use of HTTP methods, appropriate headers,
and meaningful status codes.

Not every step needs to be followed in full every time:

- If the data model is already known, step 1 can be omitted
- If the resource model is already decided, step 2 can be skipped
- If the API is straightforward with no concurrent updates or long-running operations,
  step 6 can be simplified

Each step is detailed in the sections that follow.

---

## 1. Data Model

The design process starts with the data model — the entities the system manages, their
key attributes, and their relationships. This step is independent of any URI or method
decisions. Nail down the entity model first; everything else follows from it.

The data model is abstract — it is independent of the JSON schema you will eventually write.
Think of it as an ER diagram in words.

---

## 2. Resource Derivation

Once the data model is clear, classify each entity and business action into one of five
resource types. The type determines the URI pattern and the allowed HTTP methods.

### 2.1 Collection
A homogeneous, addressable set of entity instances of the same type.

- URI: **plural noun**, no trailing slash — `/products`, `/orders`, `/customers`
- Methods: GET (list, paginated), POST (create — server assigns URI)
- POST returns **201 Created** + `Location` header pointing to the new atomic resource
- GET returns a pagination envelope: `{ count, next, previous, data: [...] }`

### 2.2 Atomic
A single, uniquely addressable instance of an entity.

- URI: parent collection + `/{id}` — `/products/{productId}`, `/orders/{orderId}`
- Methods: GET (retrieve), PUT (full replace, idempotent), DELETE (remove, idempotent)
- PUT replaces the entire resource; the request body must include the full representation
- DELETE returns **204 No Content** on success

### 2.3 Composite
An aggregation of multiple different entity types into one response. Not directly mapped
to a single entity — used when clients need a joined view to avoid N+1 requests.

- URI: descriptive noun phrase, plural — `/order-summaries`, `/product-catalogs`
- Methods: typically GET only
- Mutations target the constituent atomic resources, not the composite

### 2.4 Controller
An operation that spans multiple resources or orchestrates a multi-step workflow.
Uses a verb URI (the exception to the noun rule).

- URI: verb or verb phrase — `/cart/checkout`, `/batch-import`, `/reports/generate`
- Methods: POST (always — triggers an action, not idempotent by default)
- If long-running: return **202 Accepted** + `Content-Location` for polling (see §6)

### 2.5 Processing Function
A targeted operation on a single atomic resource — typically a state transition, partial
computation, or side-effecting sub-action. Scoped to one resource instance.

- URI: atomic path + verb sub-path — `/orders/{orderId}/cancel`, `/orders/{orderId}/fulfill`
- Methods: POST
- Returns **200** with the updated resource, or **202** if long-running
- Use instead of PATCH when the transition has business meaning and side effects

**Decision guide:**
- Entity with multiple instances → Collection + Atomic
- Entity owned by a parent → child Collection + Atomic under parent path (`/orders/{orderId}/items`)
- Non-CRUD action on one entity instance → Processing Function
- Non-CRUD action spanning multiple resources → Controller
- Pre-joined view across entity types → Composite

---

## 3. Representations

- Default format: **JSON** (`application/json`)
- If multiple formats are needed, use content negotiation via `Accept` header
- Schema conventions:
  - All schemas typed as `object` with explicit properties
  - Each property has a `type` and a `description`
  - Use `format` qualifiers where appropriate (`uuid`, `date-time`, `email`, `uri`)
  - Enum values listed explicitly in the schema
- Request and response bodies use `$ref` to named schemas in `components/schemas`
- Include at least one concrete inline example per request body and success response

---

## 4. URI Naming

### Format
```
/{feature-code}/{version}/{resource-path}
```
- `feature-code`: short kebab-case domain name — `order-management`, `product-catalog`, `store`
- `version`: `v{major}.{minor}` — `v1.0`, `v2.0` (never `v1`, never a date stamp)
- `resource-path`: derived from the resource taxonomy above

### Casing and characters
- All path segments: **kebab-case** — `/order-items`, `/product-categories`, `/shopping-carts`
- No camelCase, PascalCase, snake_case, or underscores in path segments
- No special characters: `$ & + , ; = ? @ %`
- No trailing slashes

### Nouns vs. verbs
- Collection, Atomic, Composite: **nouns only** (plural for collections, singular with `{id}` for atomics)
- Controller, Processing Function: **verbs** — this is the sanctioned exception
- Never embed HTTP method names in a URI: no `/getUsers`, `/deleteOrder`, `/createProduct`

### Child resources
Owned entities nest under their parent — keep nesting to two levels maximum:
```
/orders/{orderId}/items
/orders/{orderId}/items/{itemId}
```
If nesting would go deeper, flatten and use query parameters for filtering instead.

### Query strings
- Filtering: `?status=active`, `?category=electronics`
- Searching: `?query=<term>`
- Sorting: `?sort=createdAt&order=desc`
- Pagination: `?limit=20&offset=0`
- Query parameter names: camelCase is acceptable; kebab-case is also fine — be consistent

### Versioning
- Include version in the base path: `/{feature-code}/v1.0/...`
- Support the current version + one previous major version simultaneously
- Breaking changes require a new major version (`v2.0`); additive changes stay in the minor (`v1.1`)

---

## 5. HTTP Methods

| Method | Safety | Idempotent | Primary use |
|--------|--------|------------|-------------|
| GET    | Yes    | Yes        | Retrieve — no side effects |
| PUT    | No     | Yes        | Full replacement of a resource |
| POST   | No     | No         | Create (factory) or trigger an action |
| DELETE | No     | Yes        | Remove a resource |

**POST as factory**: server generates and assigns the resource URI. Response:
- **201 Created** + `Location` header with the new resource URI
- Do not accept a client-supplied URI in the POST body

**PUT semantics**: the request body replaces the entire resource. Any field absent is cleared.
Clients must send the full representation. Use POST-based Processing Functions for partial
updates that have business meaning.

**DELETE**: returns **204 No Content** on success; **404 Not Found** if already gone.

**GET**: safe and cacheable. Use `ETag` / `Last-Modified` headers on responses to enable
client-side caching and conditional requests.

---

## 6. Special Behaviour

### Pagination
Apply to all Collection GET operations.

Request parameters:
- `limit` (integer, default 20, max 100) — items per page
- `offset` (integer, default 0) — zero-based start index

Response envelope:
```json
{
  "count": 150,
  "next": "/products?limit=20&offset=40",
  "previous": "/products?limit=20&offset=0",
  "data": [ ... ]
}
```
- `count`: total matching items (not just this page)
- `next` / `previous`: full relative URIs for adjacent pages; `null` when no adjacent page exists

### Filtering and searching
Add query parameters to collection GETs — do not create separate endpoints:
- `GET /products?category=electronics&status=active`
- `GET /orders?customerId=abc-123&status=shipped`

### Caching
Include `ETag` and `Last-Modified` on GET responses for resources that change infrequently.
Clients use `If-None-Match` / `If-Modified-Since` → server returns **304 Not Modified** if unchanged.

### Concurrency control
For resources subject to concurrent updates:
- Server includes `ETag` in GET/PUT responses
- Client sends `If-Match: <etag>` on the next PUT
- If the resource changed in the meantime: **412 Precondition Failed**
- Also supported: `If-Unmodified-Since` / `Last-Modified` pair

### Long-running operations
When a POST triggers work that cannot complete within the request timeout:

1. Return **202 Accepted** + `Content-Location` header pointing to a status resource (e.g., `/jobs/{jobId}`)
2. Client polls `Content-Location` until the job is done
3. When done, the status resource returns **303 See Other** + `Location` pointing to the result

```
POST /reports/generate
→ 202 Accepted
   Content-Location: /jobs/job-8821

GET /jobs/job-8821          (client polls)
→ 200 { "status": "running", "progress": 45 }

GET /jobs/job-8821          (later)
→ 303 See Other
   Location: /reports/rep-7712
```

---

## 7. Errors

### Error schema
All error responses use the same schema — place it in `components/schemas/Error`:

```json
{
  "code": 404,
  "message": "Resource not found",
  "description": "No order with id 'ord-999' exists.",
  "moreInfo": "https://developer.example.com/errors/404"
}
```

Fields:
- `code` (integer, required) — HTTP status code or application error code
- `message` (string, required) — short human-readable label
- `description` (string, optional) — detailed explanation
- `moreInfo` (string, optional) — URI to documentation or further context

### Multiple errors (validation failures)
Return an array when multiple problems exist in one request:
```json
[
  { "code": 400, "message": "Missing required field", "description": "'email' is required." },
  { "code": 400, "message": "Invalid format", "description": "'phone' must be E.164 format." }
]
```

### Status code reference

| Code | Meaning | When to use |
|------|---------|-------------|
| 200  | OK | Successful GET, PUT, Processing Function POST |
| 201  | Created | Successful POST factory — include `Location` header |
| 202  | Accepted | Long-running operation started — include `Content-Location` |
| 204  | No Content | Successful DELETE |
| 303  | See Other | Long-running operation complete — include `Location` to result |
| 304  | Not Modified | Conditional GET; cached version still valid |
| 400  | Bad Request | Validation failure; return error array |
| 401  | Unauthorized | Missing or invalid authentication credential |
| 403  | Forbidden | Authenticated but insufficient permissions |
| 404  | Not Found | Resource does not exist |
| 409  | Conflict | Business rule violation (duplicate, state conflict) |
| 412  | Precondition Failed | `If-Match` / `If-Unmodified-Since` check failed |
| 429  | Too Many Requests | Rate limit exceeded; include `Retry-After` header |
| 500  | Internal Server Error | Unexpected server-side failure |

---

## 8. Worked Example — E-commerce Shopping System

**Entities:** Customer, Product, Category, Cart, CartItem

**Relationships:** Cart belongs to Customer · CartItem belongs to Cart · CartItem references Product · Product belongs to Category

**Business actions:** checkout Cart

**Resource derivation:**

| Entity / Action | Resource Type      | URI                                          | Methods              |
|-----------------|--------------------|----------------------------------------------|----------------------|
| Product         | Collection         | /products                                    | GET, POST            |
| Product         | Atomic             | /products/{productId}                        | GET, PUT, DELETE     |
| Category        | Collection         | /categories                                  | GET, POST            |
| Category        | Atomic             | /categories/{categoryId}                     | GET, PUT, DELETE     |
| Customer        | Collection         | /customers                                   | GET, POST            |
| Customer        | Atomic             | /customers/{customerId}                      | GET, PUT, DELETE     |
| Cart            | Atomic (child)     | /customers/{customerId}/cart                 | GET, PUT             |
| CartItem        | Collection (child) | /customers/{customerId}/cart/items           | GET, POST            |
| CartItem        | Atomic (child)     | /customers/{customerId}/cart/items/{itemId}  | GET, DELETE          |
| checkout Cart   | Controller         | /customers/{customerId}/cart/checkout        | POST                 |

**Base path:** `/store/v1.0`

**Pagination applies to:** GET /products, GET /categories, GET /customers,
GET /customers/{customerId}/cart/items

**Long-running:** POST /customers/{customerId}/cart/checkout → 202 + Content-Location if
payment processing is asynchronous; otherwise 200 with order summary.
