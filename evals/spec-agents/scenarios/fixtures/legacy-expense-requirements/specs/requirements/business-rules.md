# Business rules

## Manager approval threshold
- When: expense amount is at or above the org threshold
- Then: a manager approval is required before finance may pay
- Provenance: `services/expense/rules.go:40-62`

## Receipt required above threshold
- When: amount ≥ receipt threshold
- Then: submission without a receipt is rejected
- Provenance: `services/expense/submit.go:88-101`

## Reference values from the legacy app — not a design constraint on implementation shape

```
category,limit
travel,5000
meals,75
equipment,2000
```
