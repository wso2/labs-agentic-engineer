# IT Help Desk — PRD

## Problem Statement
Employees who need IT help have no single place to raise and track requests, so issues get lost in chat and hallway asks. Support agents cannot see what is waiting or how urgent it is, and team leads cannot tell whether response targets are being missed.

## Solution
An internal help desk web app backed by one API service. Employees raise tickets and follow them through a comment thread; support agents triage, assign and resolve them; a team lead watches the queue, filtered by status, priority, assignee and SLA breach. Priority determines an SLA due time computed by the API.

## Actors
- Employee: raises tickets, sees their own tickets, comments on them, and reopens their own resolved tickets.
- Support Agent: sees the whole queue, triages, assigns, works, comments on and resolves tickets.
- Team Lead: sees the whole queue and SLA breaches, and can assign and reassign tickets.

## User Stories
1. As an Employee, I want to raise a ticket with a title, description and priority, so that IT knows what I need.
2. As an Employee, I want to see the list and status of my own tickets, so that I know where my requests stand.
3. As an Employee, I want to add comments to my ticket, so that I can give or receive more information.
4. As an Employee, I want to reopen a resolved ticket, so that an unsolved problem is worked again.
5. As a Support Agent, I want to view the queue filtered by status, priority, assignee and SLA breach, so that I can find what to work on.
6. As a Support Agent, I want to triage a ticket and set its priority, so that it gets the right SLA.
7. As a Support Agent, I want to assign a ticket to myself or another agent, so that ownership is clear.
8. As a Support Agent, I want to move a ticket to in progress and then resolved, so that its status reflects the work.
9. As a Support Agent, I want to comment on a ticket, so that I can communicate with the requester and colleagues.
10. As a Team Lead, I want to view the whole queue with SLA breaches highlighted, so that I can see where we are falling behind.
11. As a Team Lead, I want to assign and reassign tickets, so that workload is balanced.
12. As an Employee, I want to see the SLA due time on my ticket, so that I know when to expect a response.

## Product Decisions
- Ticket lifecycle: open, triaged, in progress, resolved, reopened. Resolved tickets can be reopened; otherwise transitions move forward. *assumed*
- SLA due time is computed by the API from priority at creation and recomputed when priority changes. *assumed*
- Priorities: low, medium, high, critical; example SLA targets 3 days, 1 day, 8 hours, 2 hours (calendar time). *assumed*
- An SLA breach is a ticket not resolved past its due time. *assumed*
- Comments are visible to all participants including the requester; no internal-only notes. *assumed*
- Sign-in: all users sign in via SSO through the platform IDP; roles distinguish Employee, Support Agent and Team Lead.
- Delivery: one web app and one API service.
- Employees see only their own tickets; agents and team leads see all. *assumed*

## Out of Scope
- Email or any other notifications.
- File uploads and object storage.
- Scheduled or background jobs.
- External integrations.
- AI features.

## Open Questions
1. What real SLA targets per priority does the organization require?
