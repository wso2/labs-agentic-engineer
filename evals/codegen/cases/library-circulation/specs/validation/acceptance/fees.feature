Feature: Late fees

  @story-12
  Rule: A late return computes a fee automatically and an on-time return does not

    Scenario: Returning 4 days late
      Given Mia has a loan that was due 4 days ago
      When Lena the librarian checks the copy in
      Then Mia has an unpaid fee of 1.00 for that loan

    @negative
    Scenario: Returning on the due date
      Given Mia has a loan due today
      When Lena the librarian checks the copy in
      Then Mia has no fee for that loan

  @story-7
  Rule: A member sees only their own fees

    Scenario: Viewing fees
      Given Mia the member has an unpaid fee of 1.00
      When she opens her fees
      Then the fee of 1.00 is listed as unpaid

    @negative
    Scenario: Another member's fees are not shown
      Given Dan the member has an unpaid fee of 2.00
      When Mia the member opens her fees
      Then no fee of 2.00 is listed

  @story-7 @story-12
  Rule: Only a librarian may mark a fee paid

    Scenario: Marking a fee paid
      Given Mia has an unpaid fee
      When Lena the librarian marks it paid
      Then Mia's fee is shown as paid

    @negative
    Scenario: A member cannot mark their own fee paid
      Given Mia the member has an unpaid fee
      When she tries to mark it paid
      Then her fee is still unpaid
