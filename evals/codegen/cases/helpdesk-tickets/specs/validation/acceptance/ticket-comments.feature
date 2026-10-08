Feature: Ticket comments

  @story-3
  Rule: An employee can comment on their own ticket

    Scenario: Employee adds a comment
      Given Ana the employee has raised a ticket titled "VPN not connecting" with no comments
      When Ana comments "Still failing after restart"
      Then the ticket's thread has exactly one comment, "Still failing after restart"

    @negative
    Scenario: An employee cannot comment on another employee's ticket
      Given Ana the employee has raised a ticket titled "VPN not connecting" with no comments
      When Lee the employee tries to comment "Me too" on it
      Then the ticket's thread has no comments

  @story-9
  Rule: A support agent can comment on any ticket and the requester sees it

    Scenario: Agent replies
      Given Ana the employee has raised a ticket titled "VPN not connecting" with no comments
      When Sam the support agent comments "Please restart the client"
      Then Ana sees exactly one comment on her ticket, "Please restart the client"
