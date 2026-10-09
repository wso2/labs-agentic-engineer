# ADR-0003 — A thread takes the chat panel; the breadcrumb moves between threads

**Status:** Accepted · 2026-10-08
**Related:** [ADR-0002](ADR-0002-questions-are-answered-on-a-questions-card.md)
(each view's Questions card), `pages-and-cards.md` (the Issues Page and Issue
card), `lexicon.md` (Chat).

## Context

A project has a main chat, and two kinds of branch: the Issues Page's chat
and each open issue's own chat. They were drawn as a sheet stacked over the
main chat inside the panel, with a **↑ Main chat** strip that minimised the
sheet to a link at the end of the main thread, a **<Project> └ Issues › #N**
path under the strip, a **Work on Issues here · Start** row until the Issues
chat was started, and per-project started/minimised state in the shell. Two
layers in a narrow column, three ways back to the main chat and a state the
user had to manage, for what the page in view already says: which thread the
user is in.

## Decision

1. **The panel holds one thread, the page's** (`chatViewFor`): the main chat
   on every project page; the Issues chat on the Issues Page; an open issue's
   chat on its card (and on the Questions card answering it). A closed issue,
   or one not read yet, has none: its card holds the main chat. There is no
   sheet, no strip, no Start row and no started/minimised state; the shell
   keeps only the compose request (`useChatControls`), which waits for the
   thread it names to be in the panel.
2. **The breadcrumb is the thread's path, and moves between threads:**
   `<org> › <Project> › Issues › #N`, cut after the thread in view (`… ›
   Issues` for the Issues chat, `<org> › <Project>` for the main chat).
   Every crumb before the last is a link: the org to its Dashboard, the
   project to its overview (the main chat), Issues to the Issues Page (the
   Issues chat). It names the thread, not the page or card: what a message is
   about stays on the composer's line ("Talking about …").
3. **Going to a thread is going to its page.** The threads menu, Reopen,
   Continued in Issues · Open, Continue on #N · Open, Create Issue and a
   hand-off's New Issue all navigate; the panel follows.

## Consequences

- One composer at a time, keyed by thread: an unsent draft does not follow
  the user to another thread, and is not kept when they come back.
- Each thread's messages and running turn live in its store as before, so a
  turn keeps going while the user is elsewhere and is there when they return.
- The From Issues note still sums up a visit to the Issues Page; an issue's
  card over that page is not leaving it.
- Bringing a thread up no longer focuses its composer; a compose request
  (Create Issue, a busy hand-off) still does.
