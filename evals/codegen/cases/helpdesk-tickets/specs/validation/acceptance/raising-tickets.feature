Feature: Raising and following tickets

  @story-1
  Rule: An employee can raise a ticket with a title, description and priority

    Scenario: Raising a ticket
      Given Ana the employee is signed in
      When Ana raises a ticket titled "VPN not connecting" with description "Cannot connect from home" and priority "high"
      Then Ana's ticket list includes "VPN not connecting" with status "open"

    @negative
    Scenario: A ticket without a title is refused
      Given Ana the employee is signed in and has no tickets
      When Ana tries to raise a ticket with an empty title
      Then Ana's ticket list is empty

  @story-2
  Rule: An employee sees only their own tickets

    @negative
    Scenario: Another employee's ticket is not visible
      Given Ana the employee has raised a ticket titled "VPN not connecting"
      And Lee the employee is signed in
      When Lee opens his ticket list
      Then the list does not include "VPN not connecting"

    Scenario: Seeing status
      Given Ana the employee has raised a ticket titled "Replace keyboard"
      When Ana opens her ticket list
      Then "Replace keyboard" is shown with status "open"

  @story-12
  Rule: A ticket shows an SLA due time that depends on its priority

    Scenario: A critical ticket is due before a low one
      Given Ana the employee has raised a "critical" ticket and a "low" ticket
      When Ana opens her ticket list
      Then the "critical" ticket's SLA due time is earlier than the "low" ticket's
