# tests/e2e — the live walk

`acme-expenses-walk.sh` drives the console (`apps/console`) with
[agent-browser](https://github.com/vercel-labs/agent-browser) through the
running example, Mark at Acme Corp building Acme Expenses, against your
dev-env and its real agents. It is run on demand, and before merging
shell-level changes; it is not part of `make test` and does not run in CI.

It covers the screens wired to aep-api so far:

1. **Sign in**: the app sends the browser to Thunder, signs in, and lands on
   the Dashboard (the dev-env org is set up, so onboarding passes straight through).
2. **New project**: types "An expense tracker for our staff", attaches
   `fixtures/expense-policy.pdf`, presses Continue, and checks the details form
   suggests `expense-tracker-staff` and lists the document. With `E2E_CREATE=1`
   it then creates the project and checks the overview opens with the prompt as
   the chat's first message and the kickoff turn starting.

Later steps (spec, design, build) are added as their screens are wired.

## Prerequisites

- A dev-env cluster (`make dev-env`).
- `http://localhost:8090/callback` added by hand to the `aep-console-client`
  redirect URIs in the cluster's Thunder (see `apps/console/README.md`).
- The console's dev server running in real mode on :8090:

  ```sh
  cd apps/console
  API_PROXY_TARGET=http://console.ae.localhost:8080/aep-api-service \
  VITE_THUNDER_URL=http://thunder.openchoreo.localhost:8080 \
  pnpm dev
  ```

- `agent-browser` and `jq` on the `PATH`.

## Run

```sh
make e2e-walk                 # or: bash tests/e2e/acme-expenses-walk.sh
E2E_CREATE=1 make e2e-walk    # also create a real project (see below)
```

It prints one `ok` line per passed check and stops at the first failure with
`step N failed: <what>`, exiting non-zero. The browser session is closed on
exit, pass or fail.

| Variable | Default | |
|---|---|---|
| `BASE_URL` | `http://localhost:8090` | where the app runs |
| `E2E_USERNAME` / `E2E_PASSWORD` | `admin` / `Admin@123` | the ThunderID admin that `setup-env-for-aectl.sh` creates |
| `E2E_CREATE` | `0` | `1` goes past the details form and creates the project |
| `E2E_SESSION` | `acme-expenses-walk` | the agent-browser session name |
| `E2E_BROWSER_ARGS` | `--no-sandbox` | Chrome launch args |

## The create guard

By default the walk stops before **Create project**, so a run creates nothing.
With `E2E_CREATE=1` it creates `acme-expenses-<unix time>`, which makes a real
GitHub repository in your connected organization and starts a real agent turn
(model calls, charged to your key).

On exit, pass or fail, it deletes that project through aep-api
(`DELETE /projects/{name}`) as the signed-in user. That removes the platform's
side of it: the OpenChoreo project, its run supervisors, webhook, repository
record, executions and runs. **The GitHub repository survives** a project delete
by design, with its issues and milestones; delete it by hand
(`gh repo delete <org>/acme-expenses-<unix time>`).
