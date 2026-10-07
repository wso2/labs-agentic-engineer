Feature: Ticket assignment

  @story-7
  Rule: A support agent can assign a ticket to themselves or another agent

    Scenario: Assigning to oneself
      Given an open ticket titled "Cannot print" with no assignee
      When Sam the support agent assigns it to himself
      Then the ticket's assignee is Sam

    Scenario: Assigning to another agent
      Given an open ticket titled "Cannot print" with no assignee
      When Sam the support agent assigns it to Priya the support agent
      Then the ticket's assignee is Priya

  @story-11
  Rule: A team lead can assign and reassign tickets

    Scenario: Reassigning a ticket
      Given a ticket titled "Cannot print" assigned to Priya the support agent
      When Tara the team lead reassigns it to Sam the support agent
      Then the ticket's assignee is Sam

  @story-7 @story-11
  Rule: Employees cannot assign tickets

    @negative
    Scenario: An employee tries to assign
      Given Ana the employee has raised a ticket titled "Cannot print" with no assignee
      When Ana tries to assign it to Sam the support agent
      Then the ticket still has no assignee
