# Business rules

## Manager approval threshold
- When: expense amount is at or above the org threshold
- Then: a manager approval is required before finance may pay
- Provenance: `services/expense/rules.go:40-62`

## Receipt required above threshold
- When: amount ≥ receipt threshold
- Then: submission without a receipt is rejected
- Provenance: `services/expense/submit.go:88-101`

## Approval escalation
- When: an expense is routed for approval
- Then: it lands on an approver who is never the submitter
- Provenance: `services/expense/approval.go:200-248`
- Logic:
  ```
  1. start with the submitter's direct manager as the approver
  2. if amount > director_threshold: escalate to the manager's own manager
  3. if the resolved approver equals the submitter (self-approval):
     escalate one level further up the chain
  4. if no approver is found after 3 escalations: route to a finance admin
     as the fallback approver
  ```

## Reference values/logic from the legacy app — not a design constraint on implementation shape

```
category,limit
travel,5000
meals,75
equipment,2000
```
