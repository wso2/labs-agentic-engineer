Feature: Doctor availability

  @story-1
  Rule: A doctor publishes availability as bookable time slots

    Scenario: Publishing a morning window
      Given Dr. Rivera has no slots on a future date
      When Dr. Rivera publishes availability from "09:00" to "10:00" in 30-minute slots on that date
      Then Dr. Rivera has exactly 2 slots on that date
      And both slots can be found by a patient browsing that date

  @story-2
  Rule: A doctor may remove only unbooked slots

    Scenario: Removing an unbooked slot
      Given Dr. Rivera has an unbooked slot at "10:00" on a future date
      When Dr. Rivera removes that slot
      Then Dr. Rivera has no slot at "10:00" on that date

    @negative
    Scenario: A booked slot cannot be removed
      Given Dr. Rivera has a slot at "09:30" on a future date booked by Pat the patient
      When Dr. Rivera tries to remove that slot
      Then Pat's appointment is still booked
      And the slot is still in Dr. Rivera's slots
