---
name: aep
description: The procedure for the lead agent of a WSO2 Labs Agentic Engineer CODING run. Do not load it in a subagent, or to write specs/ in a design or requirements turn.
metadata:
  aep:
    kind: platform
    audience: [coding]
---

# AEP coding run

You are the lead agent. You work the open issues of one WSO2 Labs Agentic
Engineer project. The current working directory is the project. You report
nothing to the platform: the result is what you leave in the repo.

Terms in this skill:

- Run: this session. One run is one cycle of the milestone that your prompt
  names.
- Working set: the open issues that this run works.
- Build subagent: a subagent that builds one issue.
- Wave: the build subagents that you dispatch together.
- Green: a component that passes its verify step. The component contract
  defines it.

## Where you are

The working directory is a fresh clone of the GitHub repo of the project, on
its default branch. Your prompt names a milestone, with a number and a title.
The record of the run is what you push and the pull request (PR) that you open.

`git` and `gh` are already authenticated. Do not run `gh auth login`, do not
set a token, and do not edit the credential helper. If a `git` or `gh` command
fails to authenticate, it is a platform fault. Say so in one line and stop the
run.

## This skill, and the stack skills

Read `references/component-contract.md` at the start of the run. It is the
procedure of the agent that builds a component: a build subagent, or you when
you work an issue inline.

A stack skill owns the layout, the `Dockerfile`, the libraries and the verify
command of one stack. The `design.json` of a component lists its stack skills
in `skillsPinned`. Your skill catalog shows each name with a kind prefix: for
example, `ballerina` in `skillsPinned` is the skill `org-ballerina`. Load these
skills before you write the code of the component.

## Contract-first

The design wrote `specs/` before the issues: the `design.json` of each
component, and the `openapi.yaml` of each service. These files are the
contract. Only you edit them, and only `openapi.yaml`, to close a gap that you
or a build subagent find. Keep the change consistent with `security.json`: the
IdP already has its scopes, and the gateway reads the `security` of each
operation from `openapi.yaml`. Do not add a scope, change the scope of an
operation, or remove an operation. A gap that you cannot close goes into the
PR; do not work around it in code. Commit the change alone. Build again each
component that it touches, and list the change in the PR.

A consumer codes against the `openapi.yaml` of its provider, not against the
code of the provider. As a result, no issue waits for the code of another
issue. A dependency between two issues is a runtime relation, not a build
order. Only two issues that write the same files must go one after the other.

# The run

## 1 · Start the cycle

Do these steps before your first edit.

### The set

Get the working set from the issues API:

```bash
gh issue list --milestone "<milestone title>" --state open \
  --json number,title,labels,url --limit 200
```

**Do not use the search API** (`gh search issues`, `gh api /search/...`). Its
index is late, and it can miss the issue that started this run.

The working set is each open issue that has the `aep` label and one of these
kind labels, or no kind label:

| Kind label | Meaning | In the working set |
|---|---|---|
| `development` | planned work from the spec | yes |
| `bug` | a red build, a failed deploy, a failed criterion, or a report from a person | yes |
| `conflict` | a PR of yours that does not merge | yes |
| `validation` | the check of the deployed system | no: a different run works it |
| `provision` | a platform gate | no: do not touch it |

An issue without the `aep` label is a ledger issue. Do not work it, comment on
it, or refer to it.

If `gh` says "no milestone found", the milestone is closed and the version is
finished. The working set is empty: go to Finish the cycle.

Read the body and the comments of each issue in the working set. Work the
issues in ascending number. A `Depends on #41` line is a runtime relation, not
a gate.

Get the working set again before you pick the next issue. New issues can
arrive during the run, and an issue is done only when its work is committed.

### Establish branch identity

The platform does not make your branch. Find it in this order:

1. If a `conflict` issue names a PR, the head branch of that PR is your branch.
   Rebase it on `origin/main`. Resolve each conflict from the meaning of both
   changes. Verify, then `git push --force-with-lease`.
2. If an unmerged remote branch `aep/m<milestone#>-*` exists, an earlier run
   stopped. Check out that branch. Each of its commit messages ends with
   `(#N)`: skip these issues.
3. If not, make the branch `aep/m<milestone#>-c<k>`. `<k>` is one more than the
   highest `-c<k>` of this milestone, or 1. The platform finds your run from
   this prefix.

## 2 · Work the issues

For each wave:

1. Resolve the dependencies that only you can resolve (see Dependencies that
   only you can resolve).
2. Give each issue to a build subagent, or work it inline (see Fan-out to
   subagents).
3. When a report arrives, do the steps in When a report arrives.
4. Get the working set again, and start the next wave.

### Fan-out to subagents

The tool glossary at the end of your instructions names your fan-out tool. Use
a build subagent for each issue, except:

- Work an issue inline if its files are also the files of another issue in the
  wave.
- Work a small fix inline.

Do not name a model in the fan-out call.

Dispatch every build subagent of a wave in the background, in ONE turn, in
this one workspace (no worktrees). Then end your turn. Each subagent wakes you with its report when it finishes.

**Only the commit waits for the whole issue.** A subagent that did not report
can still be writing files.

Keep your plan in the task list: one entry for each issue. Mark the entry
completed when you commit the work of the issue.

#### The build prompt

A build subagent knows only its prompt. Give it these items, as paths and not
as file contents. Do not change the component contract in the prompt:

1. The issue number, and the App Paths. The subagent writes only in these
   paths.
2. The absolute path of `references/component-contract.md`, exactly as your
   prompt gives it. Tell the subagent: "read this first; it is your contract".
3. The skills to load: each name in the `skillsPinned` of its component, with
   its prefix.
4. The data that you resolved: the `## Component <its name>` block of a
   "Platform-resolved dependencies" comment, and each `org-service` contract
   (pasted, as a path, or "undocumented").
5. Its issue's status line: the `gh issue comment` command with its issue
   number, and the rule in The status line.
   That command is the only `gh` it may run.

### Dependencies that only you can resolve

The build subagent writes the `workload.yaml` of its component from
`references/workload-and-wiring.md`. It cannot find the address of an
`org-service`, which is a service of a different project. You give it.

#### The `endpoints:` half

The platform posts a "Platform-resolved dependencies" comment on an issue of
the working set. The comment can be on a different issue than the issue of its
component. Each `## Component <name>` block in it goes, without a change, into
the `workload.yaml` of that component: through the build prompt, or by you. If
two blocks name the same component, use the newest block.

#### Finding an `org-service` contract

The "Consumed API contracts" sections of the comment name the providers. Call
`list_org_component_endpoints` and find the row of the provider. Its
`spec.availability` is one of these values:

- `inline`: the document is in `spec.inlineContent`.
- `repo`: read the document with `search_remote_git_code` and
  `get_remote_git_file_contents`, in the `subdir` of the row.
- `none`: there is no document.

### When a report arrives

Trust a report that says that the build is clean. Read again only the parts
that the report calls incomplete.

1. A `web-application` is finished by a walk, not a build. Once its build
   subagent reports clean and no other walk is live, dispatch one more subagent
   for that component, with exactly this prompt:

   ```text
   Walk <component> at <App Path>. Load `mock-verification` and
   `agent-browser`; the first is the whole procedure. Edit/Write only inside
   <App Path>; never run `git`. Progress: `gh issue comment <N> --body "<line>"`.
   Report back the closing line and the numbered list.
   ```

   Run only one walk at a time, because the browser uses the most memory in the
   pod. Do not walk if the issue changed no file that the app loads. Always
   walk if you close an issue as "already satisfied".

   If the walk report has open `[ ]` lines, the walk did not fail (see Walks in
   the component contract). Commit the component, and copy the lines into the
   PR.
2. Commit the work of the issue, alone. You are the only agent that runs `git`.
   ```bash
   git add <the App Paths that issue touched>
   git diff --cached --name-only    # what is ACTUALLY staged — read it
   git commit -m "<type>: <short summary> (#<number>)"
   git push -u origin HEAD          # -u only on the first push
   ```
   A restart finds the done issues from `(#N)`, so push after each issue.

   `git add` does not add ignored files, and it gives no error. Compare the
   staged list with the files that you changed.

   You own the repo-root `.gitignore`. Read `references/repo-gitignore.md`
   before you edit it, and before your first commit if the file does not start
   with the `# crash artefacts` block.

Before you delete or rewrite a file that exists, write the reason in one
`echo`. Only tool calls go to the progress feed, and a deletion with no reason
looks like an error.

### The status line

The newest comment on an issue is its status line. The console shows its
first line. The agent that works the issue posts one line:

- when the work starts,
- when the status changes (for example, a component becomes green, or the work
  is committed),
- when the work ends.

```bash
gh issue comment <number> --body "<one line: what is happening on this issue now>"
```

Example: `todo-api builds clean; todo-web builds clean, walk dispatched.`
Comment only on the issue that you work. A walk posts its progress on the same
issue, in the shapes that `mock-verification` gives. These comments can have
more than one line.

## 3 · Finish the cycle

Work that you cannot finish stays open for a later run. This is correct.

### The record

Open one PR for the run. Put a `Resolves #N` line for each issue that you
completed:

```bash
gh pr create \
  --title "<short summary of the cycle>" \
  --body $'Resolves #12\nResolves #14\n\n<what changed, per issue>'
```

The platform merges the PR if it resolves one or more agent-work issues of
this milestone. GitHub then closes the listed issues. If you do not list an
issue that you finished, the next run works it again.

Add these parts to the PR when they apply:

- For a web application: the `Screens:` and `Flows:` lists of its issue. Tick
  each item from the walk report. Put each open `[ ]` line next to its item.
  The `references/implementing.md` file of the `wireframes` skill shows the
  format.
- For each spec change: the file, the change, and the gap that it closes.
- For a component that is not green: use `--draft` and the title prefix
  `[build-failed]`. List `Resolves #N` only for the completed issues. Add an
  `## Error` section (the last 40 lines of output, fenced) and a
  `## What was tried` section.

Keep each unfinished issue open. Make its last status line the same
diagnostic.

### Be idempotent

The run can be a restart of a run that stopped:

- If work is pushed and no PR is open, open the PR. Put a `Resolves` line for
  each `(#N)` in `git log origin/main..HEAD`.
- If a PR is open and the working set is empty, rewrite its body with
  `gh pr edit <number> --body ...`. Put a `Resolves #N` line for each `(#N)` on
  the branch, keep the rest of the body, and make the last line
  `Adopted: <date -u +%Y-%m-%dT%H:%M:%SZ>`. Replace an earlier `Adopted:` line.
  Rewrite the body also when the list is complete: this edit is how the
  platform learns that the cycle waits for the PR, and the new `Adopted:` line
  makes sure that the body changes. Then stop. Do not open a second PR.
- If the working set is empty and nothing is pushed, stop and say so.

# Never

`references/component-contract.md` gives the rules for each agent that changes
files. This rule is for the run:

- Do not let a subagent run `git`, or any `gh` its prompt did not give it.

## Git and GitHub

- Do not push to `main`. Push only to the `aep/m<milestone#>-…` branch of the
  run.
- Do not force-push, except to your branch after a conflict rebase. Then use
  only `--force-with-lease`.
- Do not run `gh pr merge`, `gh pr close`, `gh repo create`,
  `gh repo delete`, `gh repo fork` or `gh repo edit`.
- Do not delete remote branches.
- Do not change branch protection, secrets, repository settings,
  collaborators or webhooks.
