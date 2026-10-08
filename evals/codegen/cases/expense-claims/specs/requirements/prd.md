# Expense Claims — PRD

## Problem Statement

Employees who incur business expenses today have no consistent way to submit
claims for reimbursement, and managers have no consistent way to review and
approve them. Finance then has to manually gather approved claims to prepare
payroll reimbursements, which is slow and error-prone. The result is delayed
reimbursements for employees, inconsistent approval records for managers, and
manual reconciliation work for finance.

## Solution

A web application where employees submit expense claims, managers approve or
reject those claims, and finance exports the approved claims into a format
payroll can consume. The system keeps a clear record of every claim's status
from submission through approval to export, without relying on email
notifications or storing receipt files.

## Actors

- **Employee** — submits expense claims, tracks their own claims' status, and
  can edit or withdraw a claim while it is still pending.
- **Manager** — reviews expense claims submitted by their employees, and
  approves or rejects each one, optionally with a reason.
- **Finance** — views all approved claims and exports them into a payroll-ready
  format.

## User Stories

1. As an Employee, I want to submit an expense claim with amount, date,
   category, and description, so that I can request reimbursement.
2. As an Employee, I want to view the status of my submitted claims
   (pending, approved, rejected), so that I know where each stands.
3. As an Employee, I want to edit or withdraw a claim while it is still
   pending, so that I can correct mistakes before it is reviewed. *assumed*
4. As a Manager, I want to see a list of claims pending my approval, so that I
   can act on them.
5. As a Manager, I want to approve a claim, so that it becomes eligible for
   payroll export.
6. As a Manager, I want to reject a claim with a reason, so that the employee
   understands why it was not approved.
7. As Finance, I want to view all approved claims, so that I can prepare them
   for payroll.
8. As Finance, I want to export approved claims to a file payroll can import,
   so that reimbursement can be processed without manual re-entry. *assumed*
9. As Finance, I want exported claims to be marked as exported, so that the
   same claim is never exported twice.

## Product Decisions

- **Sign-in**: every user signs in via SSO through Thunder, the platform
  identity provider. (Organization default.)
- **Notifications**: the product sends no email or other notifications;
  actors must check the application to see status changes. (Stated by the
  user.)
- **Attachments**: claims carry no file attachments (e.g. receipt images or
  PDFs), since object storage is out of scope for this project. (Stated by
  the user.)
- **Export format**: approved claims are exported as a downloadable CSV file.
  *assumed*
- **Currency**: all claims are submitted and exported in a single, organization-
  wide currency. *assumed*
- **Roles**: a user is either an Employee, a Manager, or Finance; a Manager
  only sees claims from employees assigned to them. *assumed*

## Out of Scope

- Email or push notifications of any kind.
- Receipt/file attachments and any object storage.
- Direct integration with a payroll provider's API (export is a file only).
- Multi-currency claims or currency conversion.
- Expense policy enforcement or automated fraud detection.
- Multi-level approval chains (only a single manager approval step).

## Open Questions

1. Which payroll system will import the exported file, and does it require a
   specific column layout or file format beyond CSV?
2. Should Finance be able to reject/reverse a claim after export, or is export
   final?

## Further Notes

None.
