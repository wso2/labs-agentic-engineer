screen EmployeeClaims "An employee's own submitted claims and their status"
  navbar "Expense Claims"
  sidebar "My Claims -> EmployeeClaims"
  row
    heading "My Claims"
    right
    button "New Claim" primary -> NewClaim
  table "Date | Category | Amount | Status" -> ClaimDetail
    row "2024-03-01 | Travel | 120.00 | Pending"
    row "2024-02-14 | Meals | 42.50 | Approved"
    row "2024-02-02 | Supplies | 18.00 | Rejected"

screen NewClaim "Submit a new expense claim"
  navbar "Expense Claims"
  sidebar "My Claims -> EmployeeClaims"
  heading "New Claim"
  input "Amount"
  input "Date"
  select "Category"
  textarea "Description"
  row
    right
    button "Cancel" -> EmployeeClaims
    button "Submit" primary -> EmployeeClaims

screen ClaimDetail "A single claim's detail, editable while pending"
  navbar "Expense Claims"
  sidebar "My Claims -> EmployeeClaims"
  heading "Claim Detail"
  badge "Pending" warning
  text "Amount: 120.00"
  text "Date: 2024-03-01"
  text "Category: Travel"
  textarea "Description"
  text "Rejection reason (if rejected)"
  row
    right
    button "Withdraw" danger -> EmployeeClaims
    button "Save Changes" primary -> EmployeeClaims

screen ManagerQueue "Claims pending the manager's approval"
  navbar "Expense Claims"
  sidebar "Approvals -> ManagerQueue"
  heading "Approval Queue"
  table "Employee | Date | Category | Amount" -> ClaimReview
    row "J. Smith | 2024-03-01 | Travel | 120.00"
    row "A. Lee | 2024-03-02 | Meals | 35.00"

screen ClaimReview "Review a single claim and decide"
  navbar "Expense Claims"
  sidebar "Approvals -> ManagerQueue"
  heading "Review Claim"
  text "Employee: J. Smith"
  text "Amount: 120.00"
  text "Date: 2024-03-01"
  text "Category: Travel"
  text "Description"
  textarea "Rejection reason (if rejecting)"
  row
    right
    button "Reject" danger -> ManagerQueue
    button "Approve" primary -> ManagerQueue

screen FinanceApproved "Every approved claim, ready for payroll export"
  navbar "Expense Claims"
  sidebar "Approved Claims -> FinanceApproved | Export History -> ExportHistory"
  row
    heading "Approved Claims"
    right
    button "Export to Payroll" primary -> ExportHistory
  table "Employee | Date | Category | Amount"
    row "J. Smith | 2024-03-01 | Travel | 120.00"
    row "A. Lee | 2024-02-14 | Meals | 42.50"

screen ExportHistory "Past payroll export batches"
  navbar "Expense Claims"
  sidebar "Approved Claims -> FinanceApproved | Export History -> ExportHistory"
  heading "Export History"
  table "Exported At | Exported By | Claim Count"
    row "2024-03-01 09:00 | finance.user | 12"
    row "2024-02-01 09:00 | finance.user | 9"

flow "My Claims"
  role "Employee"
  description "An employee submits a claim and tracks it through to a decision"
  EmployeeClaims
  NewClaim
  ClaimDetail

flow "Approvals"
  role "Manager"
  description "A manager reviews and decides on claims from their team"
  ManagerQueue
  ClaimReview

flow "Payroll Export"
  role "FinanceReviewer"
  description "Finance reviews approved claims and exports them to payroll"
  FinanceApproved
  ExportHistory
