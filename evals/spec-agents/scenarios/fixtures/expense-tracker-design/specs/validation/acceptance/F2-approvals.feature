Feature: F2 Approvals

  @story-F2.1
  Rule: An approver sees every submitted claim in one queue, from any employee

    Scenario: Approver sees all pending claims
      Given Olivia has submitted a claim for "Taxi to client site" of "42.00" dated "2026-09-12"
      And Dan has submitted a claim for "Hotel stay" of "220.00" dated "2026-09-10"
      When Priya the approver opens the approval queue
      Then she sees both Olivia's claim and Dan's claim waiting for a decision

  @story-F2.2
  Rule: An approver can approve a submitted claim

    Scenario: Approving a submitted claim
      Given Olivia has submitted a claim for "Taxi to client site" of "42.00" dated "2026-09-12"
      When Priya the approver approves that claim
      Then the claim's status is "approved"
      And the claim no longer appears in the approval queue

  @story-F2.3
  Rule: An approver can reject a submitted claim with a reason

    Scenario: Rejecting a submitted claim
      Given Olivia has submitted a claim for "Team lunch" of "60.00" dated "2026-08-20"
      When Priya the approver rejects that claim with reason "Missing itemised receipt"
      Then the claim's status is "rejected"
      And the claim's reason is "Missing itemised receipt"

  @story-F2.2 @story-F2.3 @negative
  Rule: A decision is final — an approved or rejected claim cannot be decided again

    Scenario: Approving an already-approved claim is refused
      Given Olivia's claim for "Taxi to client site" has been approved
      When Priya the approver tries to approve that claim again
      Then the claim's status is still "approved"

    Scenario: Rejecting an already-rejected claim is refused
      Given Olivia's claim for "Team lunch" has been rejected with reason "Missing itemised receipt"
      When Priya the approver tries to reject that claim again with reason "Different reason"
      Then the claim's reason is still "Missing itemised receipt"
