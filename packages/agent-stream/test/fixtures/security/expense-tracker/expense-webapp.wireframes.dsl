screen MyClaims "Claims I have submitted"
  navbar "Expenses"
  sidebar "My claims | Submit -> SubmitClaim | Approvals -> Approvals | Reports -> Reports"
  table "Date | Amount | Status"
    row "2026-01-04 | 42.00 | Submitted"

screen SubmitClaim "Send a new claim for approval"
  navbar "Expenses"
  sidebar "My claims -> MyClaims | Submit | Approvals -> Approvals | Reports -> Reports"
  input "Amount"
  button "Send" primary -> MyClaims

screen Approvals "Claims waiting on me"
  navbar "Expenses"
  sidebar "My claims -> MyClaims | Submit -> SubmitClaim | Approvals | Reports -> Reports"
  table "Claimant | Amount | Submitted"
    row "R. Perera | 42.00 | today"

screen Reports "Monthly totals"
  navbar "Expenses"
  sidebar "My claims -> MyClaims | Submit -> SubmitClaim | Approvals -> Approvals | Reports"
  chart "Spend by month"
