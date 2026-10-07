Feature: F1 Claims

  @story-F1.1
  Rule: An employee can submit an expense claim with a description, an amount and a date

    Scenario: Submitting a complete claim
      Given Olivia is signed in as an Employee
      When Olivia submits a claim for "Taxi to client site" of "42.00" dated "2026-09-12"
      Then her claims include one for "Taxi to client site" with status "submitted"

    @negative
    Scenario: Submitting a claim with no amount is refused
      Given Olivia has no claims yet
      When Olivia tries to submit a claim for "Taxi to client site" dated "2026-09-12" with no amount
      Then she still has no claims

  @story-F1.2
  Rule: An employee sees only their own claims and each one's current status

    Scenario: Olivia sees her own claims list
      Given Olivia has submitted a claim for "Taxi to client site" of "42.00" dated "2026-09-12"
      And Dan has submitted a claim for "Hotel stay" of "220.00" dated "2026-09-10"
      When Olivia views her claims
      Then she sees her claim for "Taxi to client site" with status "submitted"
      And she does not see Dan's claim for "Hotel stay"

  @story-F1.3
  Rule: An employee can see the decision and the reason on a decided claim of theirs

    Scenario: Olivia reads why her claim was rejected
      Given Olivia's claim for "Team lunch" has been rejected with reason "Missing itemised receipt"
      When Olivia opens that claim
      Then she sees the status "rejected" and the reason "Missing itemised receipt"
