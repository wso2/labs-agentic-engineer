# 01 Overview

## Goal

Today the control plane holds the organization's secrets and does the organization's work. After this change:

- **The control plane decides and writes secrets.** `aep-api` authorizes users, stores rows in Postgres, and writes secret values once through the SM API. It never reads a secret value back.
- **The org dataplane does the work and holds the secrets.** The design agent, live collaboration and all git work run in each organization's own dataplane. Only dataplane containers read secret values, and each container reads only the secrets it needs.

One architecture serves a local install and WSO2 Cloud.

![S1: AE in one picture](diagrams/S1-big-picture.png)

Source: [S1-big-picture.excalidraw](diagrams/S1-big-picture.excalidraw). The detailed picture is in [03-components.md](03-components.md).

## Words you need first

| Word | Meaning here |
|---|---|
| **Room** | One live collaboration session over a single project's spec bundle. A Room is a session. It is not a pod, a container or a Resource. |
| **SM API** | The platform's write-only door to vault. It gives back names and keys, never a value. |
| **SecretReference** | An OpenChoreo object that names a vault path for a secret. It holds names, never values. |
| **Publisher client** | The organization's Thunder OAuth application (`aep-publisher-<org>` on the Platform IdP). The dataplane uses it to call `aep-api`. It is used only from the dataplane to the control plane. |
| **Environment Thunder** | The Thunder identity provider for one organization and one environment. It is a different issuer from the Platform IdP. |
| **CP / DP** | Control plane (where `aep-api` runs) / the organization's dataplane (where the work runs). |
| **gitpat** | The GitHub personal access token an organization gives AE. It is the only GitHub connect path. A GitHub App may come later. |

All other words are in [14-glossary.md](14-glossary.md).

## Scope

In scope:

- Where every organization secret is written, stored and read.
- The dataplane authoring runtime: Resource `ae-studio` and its three containers.
- The coding agent Job and how it is sandboxed.
- How the control plane and the dataplane authenticate to each other, in both directions.
- How the browser reaches live collaboration, and how GitHub reaches the webhook receiver.
- WSO2 Cloud trust boundaries, with today's missing controls tagged as gaps.

Out of scope:

- The console UI.
- Implementing the change.
- A third architecture for self-hosted OpenChoreo. It follows the local install.
- The internals of WSO2 Cloud, Thunder, Kubernetes and OpenChoreo. This spec uses them as a platform.

## Problem → fix

![C3: what moves, and why](diagrams/C3-today-vs-intended.png)

Source: [C3-today-vs-intended.excalidraw](diagrams/C3-today-vs-intended.excalidraw).

| # | Problem today | Fix |
|---|---|---|
| 1 | Secret values are kept in the control-plane database. Vault gets a copy only if the copy works. | Secrets live only in vault. Postgres holds no secret values. |
| 2 | `aep-api` uses the gitpat for all git work. | Only `ae-studio-tools` and `ae-coding-tools`, in the dataplane, use it. |
| 3 | The Anthropic key is taken out of the database and sent with every chat turn. | The key is given to the agent from vault. `aep-api` never reads it. |
| 4 | The coding agent (an AI with a shell) can read all its secrets. | The gitpat and the publisher client move to a separate tools container. The AI keeps only the Anthropic key. |
| 5 | The AI agent gets a copy of the user's full login token. | The agent never gets the user's login token. It gets only a 5-minute token that works on nothing but this org's agent. |
| 6 | One webhook secret for all orgs. | One secret per org, checked in that org's dataplane. |
| 7 | All orgs share the same services. | Each org's work runs in its own dataplane. |

Details of each problem are in [02-today-and-problems.md](02-today-and-problems.md).
