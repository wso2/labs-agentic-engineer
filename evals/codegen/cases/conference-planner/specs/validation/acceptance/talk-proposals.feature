Feature: Talk proposals

  @story-4
  Rule: Speakers submit proposals to an event

    Scenario: Submitting a proposal
      Given Olivia has created an event
      When Sam the speaker submits a proposal titled "Scaling Queues" to that event
      Then Sam's proposals list shows "Scaling Queues"

  @story-5
  Rule: A speaker sees only their own proposals, with status

    Scenario: A new proposal is submitted
      Given Sam has submitted a proposal "Scaling Queues"
      When Sam opens his proposals
      Then "Scaling Queues" has status "submitted"

    @negative
    Scenario: Another speaker's proposals are not visible
      Given Sam has submitted a proposal "Scaling Queues"
      When Sara the speaker opens her proposals
      Then Sara's proposals list does not contain "Scaling Queues"

  @story-6
  Rule: A reviewer scores each proposal once from 1 to 5

    Scenario: Scoring a proposal
      Given Sam has submitted a proposal "Scaling Queues"
      When Rita the reviewer scores it 4
      Then the proposal shows an average score of 4 from 1 review

    @negative
    Scenario: A second score from the same reviewer is refused
      Given Rita has scored "Scaling Queues" 4
      When Rita scores it 2
      Then the proposal still shows an average score of 4 from 1 review

    @negative
    Scenario: A score of 6 is refused
      Given Sam has submitted a proposal "Scaling Queues"
      When Rita tries to score it 6
      Then the proposal has no reviews

  @story-7
  Rule: Only organizers decide on proposals

    Scenario: Accepting a scored proposal
      Given "Scaling Queues" has an average score of 4
      When Olivia accepts it
      Then Sam sees "Scaling Queues" with status "accepted"

    @negative
    Scenario: A reviewer cannot accept a proposal
      Given "Scaling Queues" has an average score of 4
      When Rita looks for a way to accept it
      Then the proposal status is still "submitted"

  @story-8
  Rule: An accepted proposal becomes a scheduled session

    Scenario: Scheduling an accepted proposal
      Given "Scaling Queues" is accepted for an event with room "Hall A"
      When Olivia schedules it in "Hall A" from "11:00" to "12:00" with capacity 50
      Then the event schedule shows "Scaling Queues" in "Hall A" with 50 seats remaining

    @negative
    Scenario: A rejected proposal cannot be scheduled
      Given "Scaling Queues" is rejected
      When Olivia tries to schedule it
      Then the event has no new session
