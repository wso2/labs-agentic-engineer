Feature: F3 Monthly report

  @story-F3.1
  Rule: The monthly total counts only approved claims for that calendar month

    Scenario: Reading the monthly total
      Given exactly two claims were approved in September 2026, for "150.00" and "220.00"
      And one claim was approved in August 2026, for "60.00"
      When Priya the approver reads the September 2026 report
      Then the total is "370.00"

    @negative
    Scenario: A submitted claim is not counted in the monthly total
      Given a claim for "42.00" was submitted in September 2026 and has not been decided
      When Priya the approver reads the September 2026 report
      Then that claim's amount is not included in the total
