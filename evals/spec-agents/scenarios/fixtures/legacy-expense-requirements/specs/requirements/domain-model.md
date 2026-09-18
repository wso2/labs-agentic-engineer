# Domain model

## Entities

### Expense
- fields: id, submitter, amount, currency, category, status, submittedAt, receiptRef
- invariants: amount > 0; status in {draft, submitted, approved, rejected, paid}

### Approval
- fields: expenseId, manager, decision, decidedAt, note
- invariants: one active decision per expense while status is submitted

## Relations
- Employee *—* Expense: one employee submits many expenses
- Expense *—* Approval: an expense may have zero or one approval decision
- Finance admin *—* Expense: pays approved expenses
