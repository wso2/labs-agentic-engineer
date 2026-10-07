Feature: Ticket lifecycle

  @story-6
  Rule: A support agent can triage a ticket and set its priority

    Scenario: Triage moves an open ticket to triaged
      Given Ana the employee has raised an open "low" ticket titled "Replace keyboard"
      When Sam the support agent triages it with priority "high"
      Then the ticket has status "triaged" and priority "high"

    Scenario: Raising priority brings the SLA due time forward
      Given Ana the employee has raised an open "low" ticket titled "Replace keyboard"
      When Sam the support agent triages it with priority "critical"
      Then the ticket's SLA due time is earlier than before

    @negative
    Scenario: An employee cannot triage
      Given Ana the employee has raised an open ticket titled "Replace keyboard"
      When Ana tries to triage it
      Then the ticket status is still "open"

  @story-8
  Rule: A support agent moves a ticket through in progress to resolved

    Scenario: Working a ticket to resolution
      Given a triaged ticket titled "VPN not connecting"
      When Sam the support agent starts the ticket and then resolves it
      Then the ticket has status "resolved"

    @negative
    Scenario: An open ticket cannot be resolved directly
      Given an open ticket titled "VPN not connecting" that is not triaged
      When Sam the support agent tries to resolve it
      Then the ticket status is still "open"

  @story-4
  Rule: Only the employee who raised a ticket may reopen it, and only once resolved

    Scenario: Reopening a resolved ticket
      Given Ana the employee's ticket "Cannot print" is resolved
      When Ana reopens it
      Then the ticket has status "reopened"

    @negative
    Scenario: Another employee cannot reopen it
      Given Ana the employee's ticket "Cannot print" is resolved
      When Lee the employee tries to reopen it
      Then the ticket status is still "resolved"

    @negative
    Scenario: An open ticket cannot be reopened
      Given Ana the employee's ticket "Cannot print" is open
      When Ana tries to reopen it
      Then the ticket status is still "open"
