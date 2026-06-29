# Final-Review Fixes Report — marketplace P4

## FIX 1 — proceed-gate must return 409, not 500

**File changed:** `asdlc-service/internal/feature/design/design_huma.go` (line ~207)

Added before the catch-all 500 in the `save-design` handler:
```go
if errors.Is(err, ErrUnresolvedDependency) {
    return nil, huma.Error409Conflict(err.Error())
}
```
`errors` was already imported. The `ErrUnresolvedDependency` doc-comment in `design_service.go` already stated "surfaced (as 409 by the controller)" — now true.

**New handler test file:** `asdlc-service/internal/feature/design/design_huma_test.go`

Two tests added:
- `TestSaveAndProceedHandler_UnresolvedDependency_Returns409`: wraps the sentinel with `fmt.Errorf("%w: ...", ErrUnresolvedDependency, ...)`, drives the Huma handler via `humatest`, asserts 409 and the gate message in the response body.
- `TestSaveAndProceedHandler_SpecNotApproved_Returns409`: regression guard for the existing ErrSpecNotApproved → 409 mapping.

**Test result:** PASS (`go test ./internal/feature/design/... → ok`)

---

## FIX 2/3 — access-pending / Reason taxonomy doc alignment

**File changed:** `asdlc-service/models/design.go` (Reason field doc-comment, lines ~60-69)

Updated to enumerate ALL reasons with their origin:
- Platform-computed: `access-required`, `not-found`, `needs-spec`
- Client-derived (NOT emitted by platform): `access-pending` (console derives from in-flight AccessRequest), `needs-input`
- `""` (n/a or resolved)

`resolveOrgServices` behavior unchanged — comment only.

---

## FIX 4 — open drawer shows stale dep after onChanged

**File changed:** `console/src/pages/ProjectArchitecturePage.tsx`

Added a `useEffect` keyed on `effectiveComponents` (after the `const effectiveComponents = ...` line):
```tsx
useEffect(() => {
  if (!activeDep) return;
  for (const comp of effectiveComponents) {
    if (comp.name === activeDep.component) {
      const freshDep = comp.dependencies?.find((d) => d.name === activeDep.dependency.name);
      if (freshDep) {
        setActiveDep({ component: activeDep.component, dependency: freshDep });
      }
      return;
    }
  }
}, [effectiveComponents]);
```
When no match is found (dep removed), `activeDep` is left as-is — no crash. Drawer props/contract unchanged.

**Build result:** `pnpm run build` ✓ — `pnpm vitest run src/pages/architecture/` → 4 files, 19 tests PASS

---

## FIX 5 — stale doc reference

**File changed:** `console/src/services/api/specs.ts` (line ~23)

Changed:
```
See asdlc-service/internal/feature/dependencies/spec_huma.go (A4).
```
to:
```
See asdlc-service/internal/feature/design/design_huma.go (A4).
```

---

## FIX 6 — stale comment in dispatch_cascade_hook.go

**File changed:** `asdlc-service/internal/feature/codingagent/dispatch_cascade_hook.go` (~line 197)

Updated the comment from "consumer resumes on its OWN org-service gate independently" to reflect the block-at-proceed model (A2c): "The consumer cannot proceed past the save-and-proceed gate until the provider is granted (block-at-proceed model, A2c); there is no separate org-service On-Hold gate." Behavior unchanged.

---

## FIX 7 — SSRF guard: reject CGNAT 100.64.0.0/10

**File changed:** `asdlc-service/internal/feature/artifacts/spec_collect.go`

Added package-level `cgnatNet` var (lazy init):
```go
var cgnatNet = func() *net.IPNet {
    _, n, _ := net.ParseCIDR("100.64.0.0/10")
    return n
}()
```

Added to the IP guard in `FetchSpecFromURL`'s `DialContext`:
```go
if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || cgnatNet.Contains(ip) {
```

**Test result:** `go test ./internal/feature/artifacts/...` → ok (existing tests cover the guard; the CGNAT range is not tested by the existing stubs, but the production code change is build-verified and logic is trivially correct).

---

## Verify-command outputs

### 1. Go tests
```
cd asdlc-service && go test ./internal/feature/design/... ./internal/feature/artifacts/... ./internal/feature/codingagent/...
ok  github.com/wso2/asdlc/asdlc-service/internal/feature/design       0.643s
ok  github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts    0.543s
ok  github.com/wso2/asdlc/asdlc-service/internal/feature/codingagent  0.593s
```

### 2. make openapi
```
wrote api/openapi.yaml (147086 bytes)
```
(Not committed — gitignored.)

### 3. Console build + vitest
```
pnpm run build → ✓ built in 17.71s
pnpm vitest run src/pages/architecture/ → 4 files, 19 tests PASS
```
