# Deliver build git credentials via OpenChoreo, not a direct in-cluster write

## Status

accepted (2026-06; #33, #34)

## Context

The BFF runs on the control plane, but `dockerfile-builder` builds run on a
separate workflow plane (the CP/WP split in cloud). An in-cluster k8s client
can't reach the WP cluster, so a directly-written build Secret never landed
and private-repo clone failed. The provisioning logic lives in
`asdlc-service/services/build_credentials_service.go`.

## Decision

Land the build GitHub-App token via OC `CreateGitSecret` (OpenBao + per-org
`SecretReference`). The build is triggered with
`repository.secretRef = app-factory-component-build-git-secret`
(`BuildGitSecretName`), and `dockerfile-builder` synthesises the per-run
`<workflowRunName>-git-secret` ExternalSecret from that SecretReference. The
per-org SecretReference is **refreshed (delete + create — OC has no update
verb)** on every build, because the App installation token is short-lived.
Same path local + cloud.

## Why

OpenChoreo owns the cross-plane write, and it works identically on local k3d.
A per-env two-path flag (`BUILD_GIT_SECRET_VIA_OC`) was rejected for
maintainability — one path, not two.

## Consequences

- **Interim:** wso2cloud platform-api does **not** yet route
  `/api/v1alpha1/gitsecrets` (tracked by wso2-enterprise/wso2cloud#319), so
  cloud `CreateGitSecret` 404s. Until then cloud defaults to **public** repos
  (`GITHUB_REPO_VISIBILITY` flipped to public, #34) and build-secret
  provisioning **degrades gracefully** — empty `secretRef` + a loud warning;
  ownership (`ErrRepoNotInOrg`) and disconnect (`ErrOrgDisconnected`) refusals
  stay fatal.
- Local k3d keeps **private** repos, with concurrent-build OC-409 tolerance
  (delete-not-found and create-conflict both mean another same-org build
  raced us).
- PR #33, #34.

## Rejected alternatives

- **Direct in-cluster SSA Secret write** — can't cross CP → WP; the original
  bug.
- **Per-env two-path flag** — maintenance burden of two divergent code paths.
