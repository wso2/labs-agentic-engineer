# Dual-mode OpenChoreo auth: forward the user JWT; impersonate the org on M2M calls

## Status

accepted (2026-05; arc #15 → #17 → #18 → #21)

## Context

platform-api routes **and bills** every OC call by the token's `ouId` claim.
The seeded BFF M2M client carries `ouId = Admin` (`a5000000-…`), so an
M2M-only routing strategy dumped every write into the Admin tenant and 402'd
on the empty billing entitlement. The transport logic lives in
`asdlc-service/clients/openchoreo/transport.go`.

## Decision

User-initiated OC calls **forward the inbound user JWT** (platform-api routes
by the caller's `ouId`; no impersonation header). Async / service calls
(webhooks, watchers, dispatch, build) carry the BFF M2M token **and** set
`X-Impersonate-Org` to the target org UUID resolved from the URL's
`.../namespaces/{namespace}/...` segment, so platform-api routes/bills the
correct org. A distinct ctx marker (`middleware.WithServiceIdentity` /
`IsServiceIdentity`) signals service-identity vs user-JWT — an M2M token is
**never** placed in the user-token ctx key (#21). The org resolver prefers the
Thunder-issued `ouId`, is keyed by org **handle**, and **aborts the call** on
resolver error (transport returns the error rather than silently mis-routing /
mis-billing). #22.

## Why

A brief detour (#17) bypassed platform-api entirely (M2M-only, OC REST direct
against DP-Thunder) but that lost per-org routing; impersonation is the
durable model. Going live in cloud also needs platform-api to grant the BFF
M2M client the impersonation policy (`cpapi:proxy`).

## Consequences

- Every async OC-writing path must run under service-identity + impersonation
  or it mis-bills.
- The cloud `cluster-gateway-proxy` now also validates platform-idp JWTs, so
  the BFF attaches its M2M token there too (#23). See
  [[adr-coding-agent-via-cluster-gateway-proxy]] for the proxy's earlier
  un-authed assumption.
