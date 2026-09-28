# AGENTS.md — apps/tryit

`@aep/tryit`: **Try it**, the platform's test app. A static SPA the console opens in a
new tab from a deployment's Try it out card ("Try agent" on an agent).
It signs a person in on the **project's** identity provider as one of the
project's test users and talks to the component's public gateway URL directly.

## What it is not

- It never calls aep-api and holds no aep-api credential. Every coordinate it
  needs arrives in the launch URL (`/#/agent?project&component&issuer&client_id
  &resource&scopes&endpoint`), all of it public.
- It never sees a password: sign-in happens on the identity provider's page.
- It sends a token only to a host under `TRY_IT_GATEWAY_HOSTS` (runtime config,
  default `openchoreoapis.localhost`). The launch URL is public input anyone
  can write; the allowlist is what keeps a real sign-in from being paired with
  an attacker's endpoint. A refused link never reaches sign-in.

## How it fits

- The console's `TryItOut.tsx` builds the launch URL (`tryItAppUrl`) from the
  roles view's `signIn{issuer, clientId}` + `resourceServer`, the published
  test users' scopes, and the deployment's `endpointUrl`; `VITE_TRY_IT_URL`
  names this app's origin.
- aep-api registers `TRY_IT_CALLBACK_URL` (`{origin}/callback`) as a redirect
  URI on every project's sign-in client at deploy time, so the two settings
  move together (compose: `tryit` on `8095:3000`, console's `VITE_TRY_IT_URL`).
- `src/session.ts` carries the RFC 8707 `resource` on all three OAuth legs;
  see `skills/thunder-authentication/assets/app/src/authz/session.ts` for why.

## Layout

`src/launch.ts` (parse + per-tab persistence), `src/session.ts`
(oidc-client-ts), `src/chat.ts` (`POST {endpoint}/chat`), `src/App.tsx`
(launch gate → sign-in → callback → agent), `src/AgentScreen.tsx` (transcript
and composer). Extending to APIs/services is a new hash route beside `#/agent`.

Commands are the uniform root verbs (`make test`, `make lint`, …).
