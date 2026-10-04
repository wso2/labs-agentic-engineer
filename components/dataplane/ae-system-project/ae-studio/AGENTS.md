# AGENTS.md — ae-studio/

The org's AE Studio: one pod per organization with one container per folder.

| Folder | Package | Container |
|---|---|---|
| `ae-design-agent/` | `@aep/ae-design-agent` | design turns |
| `ae-collab/` | `@aep/ae-collab` | the Room (Hocuspocus) |
| `ae-studio-tools/` | Go module `github.com/wso2/aep/ae-studio-tools` | tools (GitHub, webhooks) |

A fourth container, `webhook-relay` (the public gosmee image, pinned by digest in the chart's
`aeStudio.webhookRelay.image`), runs only when the Resource's `webhookRelayUrl` is set (a local install
with `ae_studio.webhook_relay.enabled`): it subscribes to the org's smee.io channel, which aep-api derives
as `https://smee.io/` + base64url(HMAC-SHA256(seed, org))[:22] from the install seed
`aep/webhook-relay-seed` (`AE_STUDIO_WEBHOOK_RELAY_SEED`), and posts each delivery to
`http://127.0.0.1:8082/webhooks/github`. It mounts nothing and reads no Secret.

The three folders' containers (and the optional relay, a public image that is not built
here) run only in the per-org ae-studio pod (aep-api provisions it; the three images are
built by `skaffold/ae-studio.yaml`); the platform chart has no Deployment for any of them. `ae-studio-tools` has no chart Deployment.
