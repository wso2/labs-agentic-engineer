Feature: Front desk

  @story-8
  Rule: A receptionist may book, reschedule and cancel for any patient, within the same rules

    Scenario: Booking for a patient
      Given Dr. Rivera has an available slot at "10:30" on a future date
      When Rita the receptionist books that slot for Pat the patient
      Then Pat has a booked appointment at "10:30"

    @negative
    Scenario: A receptionist cannot book a taken slot
      Given Pat the patient has booked Dr. Rivera's slot at "10:30" on a future date
      When Rita the receptionist tries to book that slot for Sam the patient
      Then the slot has exactly one appointment

    @negative
    Scenario: A receptionist cannot cancel within 2 hours of the start
      Given Pat the patient has an appointment starting in 1 hour
      When Rita the receptionist tries to cancel it
      Then the appointment is still booked

  @story-9
  Rule: Only a receptionist checks patients in, and only on the day of the appointment

    Scenario: Checking in on the day
      Given Pat the patient has an appointment later today
      When Rita the receptionist checks Pat in
      Then Pat's appointment is checked in

    @negative
    Scenario: Checking in for a future day is refused
      Given Pat the patient has an appointment next week
      When Rita the receptionist tries to check Pat in
      Then Pat's appointment is still booked

    @negative
    Scenario: A patient cannot check in
      Given Pat the patient has an appointment later today
      When Pat tries to check herself in
      Then Pat's appointment is still booked
