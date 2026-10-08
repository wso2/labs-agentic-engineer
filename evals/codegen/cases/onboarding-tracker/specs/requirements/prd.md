# Onboarding Tracker — PRD

## Problem Statement

When a company hires someone new, the work to get them ready to start touches
three separate teams — IT (equipment, accounts, access), HR (paperwork,
benefits, orientation) and Facilities (desk, badge, parking) — and today
nobody has one place to see whether all of it is actually done. Tasks slip
between teams, nobody notices an overdue item until the new hire's first day
arrives without a laptop or a badge, and there is no single view of where any
one new hire's onboarding actually stands.

## Solution

A shared onboarding tracker where HR creates an onboarding record for each new
hire, tasks are assigned across IT, HR and Facilities, each team updates the
status of its own tasks, and overdue items are flagged directly in the app so
staff can see at a glance what needs attention — with no email or push
notifications involved.

## Actors

- **HR Coordinator** — creates a new hire's onboarding record, assigns tasks
  across all three departments, monitors overall progress, and marks
  onboarding complete.
- **IT Staff** — views and updates the status of IT tasks assigned across all
  new hires.
- **Facilities Staff** — views and updates the status of Facilities tasks
  assigned across all new hires.
- **Admin** — manages the catalog of standard onboarding task templates used
  when a new onboarding record is created. *assumed*

## User Stories

1. As an HR Coordinator, I want to create a new hire's onboarding record, so
   that IT, HR and Facilities all know a new onboarding has started.
2. As an HR Coordinator, I want the onboarding record to be pre-populated with
   a standard set of tasks for each department, so that I don't have to build
   the task list from scratch every time. *assumed*
3. As an HR Coordinator, I want to add, edit or remove individual tasks on a
   specific new hire's onboarding record, so that I can handle exceptions to
   the standard checklist.
4. As an HR Coordinator, I want to set a due date on each task, so that
   overdue items can be identified.
5. As an IT Staff member, I want to see a list of all IT tasks across all new
   hires, so that I know what my team owes.
6. As an IT Staff member, I want to mark an IT task as in-progress or
   complete, so that others can see my team's status.
7. As a Facilities Staff member, I want to see a list of all Facilities tasks
   across all new hires, so that I know what my team owes.
8. As a Facilities Staff member, I want to mark a Facilities task as
   in-progress or complete, so that others can see my team's status.
9. As an HR Coordinator, I want to see a dashboard listing every active new
   hire and the completion status of their onboarding across all three
   departments, so that I can spot who is falling behind.
10. As any staff member (HR, IT or Facilities), I want tasks that are past
    their due date and not yet complete to be visibly flagged as overdue in
    the app, so that I notice them without needing an email or notification.
11. As an HR Coordinator, I want to mark a new hire's onboarding as complete
    once every task across all departments is done, so that the record can be
    closed out.
12. As an Admin, I want to manage the standard task templates (name,
    department, default due offset) used when a new onboarding record is
    created, so that the checklist stays current as processes change.
    *assumed*

## Product Decisions

- **Sign-in**: all users sign in via SSO through Thunder, the platform IDP.
- **Notifications**: no email or push notifications of any kind. Overdue
  tasks are surfaced only through in-app visual indicators (e.g., on the
  dashboard and task lists) — this was a hard requirement from the brief, not
  a fallback.
- **No object storage**: the product does not support uploading or storing
  files or documents against a task or onboarding record. Tasks are tracked as
  structured data only (name, department, status, due date, notes as text).
- **Roles**: access is split by department — HR Coordinator, IT Staff,
  Facilities Staff, and Admin — each seeing and acting only on the tasks
  relevant to their role, with HR Coordinator having the broadest view across
  all departments. *assumed*
- **Task templates**: standard per-department task templates exist so new
  onboarding records start pre-populated rather than being built from
  scratch each time. *assumed*

## Out of Scope

- Email, SMS or push notifications of any kind.
- File or document upload/storage (offer letters, ID scans, signed forms,
  etc.).
- Self-service access for the new hire themselves (this is an internal staff
  tool for IT/HR/Facilities, not a new-hire-facing portal).
- Payroll, benefits enrollment, or background-check system integrations.
- Offboarding / termination checklists.

## Open Questions

1. Should IT Staff and Facilities Staff be able to see each other's tasks for
   a given new hire (read-only), or only their own department's tasks? —
   deferred; design will default to each department seeing only its own
   tasks plus HR Coordinator having full visibility, until the user says
   otherwise.
2. Is there a fixed, known list of standard onboarding tasks per department
   the organization already uses, or should the initial template catalog be
   invented for the first version? — open; no answer available from this
   run's input.

## Further Notes

This PRD was generated without an interactive interview (headless run); all
judgment calls are marked `*assumed*` above and are open to being overturned
in a later round.
