screen ClaimsList "An employee's own claims and their status"
  navbar "Expense Tracker"
  sidebar "My Claims -> ClaimsList | New Claim -> NewClaim"
  heading "My Claims"
  row
    text "Track what you've filed and what's been decided"
    right
    button "New Claim" primary -> NewClaim
  table "Description | Amount | Date | Status" -> ClaimDetail
    row "Taxi to client site | $42.00 | 2026-09-12 | submitted"
    row "Conference ticket | $350.00 | 2026-09-01 | approved"
    row "Team lunch | $60.00 | 2026-08-20 | rejected"

screen NewClaim "Submit a new expense claim"
  navbar "Expense Tracker"
  sidebar "My Claims -> ClaimsList | New Claim -> NewClaim"
  heading "New Claim"
  input "Description"
  input "Amount"
  input "Date"
  row
    button "Cancel" -> ClaimsList
    right
    button "Submit" primary -> ClaimsList

screen ClaimDetail "The decision and reason on one of the employee's claims"
  navbar "Expense Tracker"
  sidebar "My Claims -> ClaimsList | New Claim -> NewClaim"
  heading "Claim Detail"
  text "Conference ticket — $350.00 — 2026-09-01"
  badge "Approved" success
  text "Decided on 2026-09-05"

screen ApprovalQueue "Every submitted claim waiting for a decision"
  navbar "Expense Tracker"
  sidebar "Queue -> ApprovalQueue | Monthly Report -> MonthlyReport"
  heading "Approval Queue"
  table "Employee | Description | Amount | Date" -> ApprovalDetail
    row "Jane Doe | Taxi to client site | $42.00 | 2026-09-12"
    row "Sam Lee | Hotel stay | $220.00 | 2026-09-10"

screen ApprovalDetail "Decide one submitted claim"
  navbar "Expense Tracker"
  sidebar "Queue -> ApprovalQueue | Monthly Report -> MonthlyReport"
  heading "Claim from Jane Doe"
  text "Taxi to client site — $42.00 — 2026-09-12"
  textarea "Reason (required to reject)"
  row
    button "Reject" danger -> ApprovalQueue
    right
    button "Approve" primary -> ApprovalQueue

screen MonthlyReport "The month's total of approved claims"
  navbar "Expense Tracker"
  sidebar "Queue -> ApprovalQueue | Monthly Report -> MonthlyReport"
  heading "Monthly Report"
  card "September 2026 total | $2,480.00 | across 14 approved claims"

flow "My claims"
  role "Employee"
  description "An employee files a claim and follows its status"
  ClaimsList
  NewClaim
  ClaimDetail

flow "Approval queue"
  role "Approver"
  description "An approver decides queued claims and reads the monthly total"
  ApprovalQueue
  ApprovalDetail
  MonthlyReport
