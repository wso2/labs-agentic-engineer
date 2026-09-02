<!--
Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).

WSO2 LLC. licenses this file to you under the Apache License,
Version 2.0 (the "License"); you may not use this file except
in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied.  See the License for the
specific language governing permissions and limitations
under the License.
-->

# Introduction

Agentic Engineer is a spec-driven software platform. A person describes what they want. Agents write specs in GitHub. Coding agents write the code. The platform builds and deploys to a development environment.

This threat model reviews **intended WSO2 Cloud production**. It is not a model of only what is running today. Where today is weaker than that intended shape, we name the gap. Later chapters mark those gaps as controls not yet in place.

Cloud identity is **Thunder** (the WSO2 Cloud Platform IdP). It is not Asgardeo SaaS.

You can also install Agentic Engineer as an OpenChoreo module on your own cluster. This review does not cover that path.

## What runs where

On Cloud, Agentic Engineer platform services run on the Cloud control-plane cluster (`cloud-cp`):

- Console (browser app)
- Agentic Engineer API
- Design-agent service
- Collab (live spec editing)
- Postgres and Temporal

Customer apps and coding-agent Jobs run on the customer dataplane (`cloud-dp-oc-dp`). Image builds run on the workflow / CI plane (`cloud-dp-oc-ci`).

A person signs in with Thunder. The browser talks to the console over public HTTPS. The console proxies API and collab traffic inside the cluster, so those hops skip the public API host. GitHub sends webhooks to a public API path. That path does not use gateway login-token checks; the API checks an HMAC secret instead. Coding-agent Jobs on the dataplane call the **public** Agentic Engineer API. Dataplane pods cannot use control-plane DNS.

Cloud GitHub is a **GitHub App** and **public repositories only**. Org-level GitHub PAT is not the Cloud connect story.

## Today's gaps (not yet in place)

These are intended production needs that today's Cloud install does not fully have.

- Platform replicas default to zero. Pods do not run unless someone overrides the count.
- Only a `development` environment exists. There is no production environment or promotion path.
- A namespace override can put every organization's OpenChoreo projects into one shared namespace. The intended model is one organization namespace each.
- New GitHub repos are public until the workflow plane can clone private repos.
- The Cloud API overlay (platform impersonation and secret-manager wiring) is not in the Cloud GitOps we can see. Production depends on a private overlay.
- Billing may not have a coding-agent metric. The design expects a block when the organization is over quota.
- Basic RBAC is the intended API control. Today any valid organization login token (JWT) can call organization APIs.

## Architecture Diagram

**D1 — Agentic Engineer on WSO2 Cloud (intended production)**

![D1 Agentic Engineer on WSO2 Cloud](diagrams/d1-architecture.png)
