Feature: Ticket queue

  @story-5
  Rule: A support agent can filter the queue by status, priority, assignee and SLA breach

    Scenario: Filtering by priority
      Given a queue with one "critical" ticket and one "low" ticket, both created for this run
      When Sam the support agent filters the queue by priority "critical" and this run's tickets
      Then only the "critical" ticket is listed

    Scenario: Filtering by assignee
      Given one ticket assigned to Sam and one assigned to Priya, both created for this run
      When Sam filters the queue by assignee Priya and this run's tickets
      Then only the ticket assigned to Priya is listed

    Scenario: Filtering by SLA breach
      Given one ticket past its SLA due time and one within it, both created for this run
      When Sam filters the queue to SLA breaches among this run's tickets
      Then only the ticket past its SLA due time is listed

  @story-5 @story-10
  Rule: Only support agents and team leads see the whole queue

    @negative
    Scenario: An employee cannot open the queue
      Given Ana the employee is signed in
      When Ana tries to open the whole queue
      Then no tickets raised by other employees are shown to her

  @story-10
  Rule: A team lead sees the whole queue with SLA breaches highlighted

    Scenario: Breach highlighted
      Given one ticket past its SLA due time created for this run
      When Tara the team lead opens the queue
      Then that ticket is shown as breached
