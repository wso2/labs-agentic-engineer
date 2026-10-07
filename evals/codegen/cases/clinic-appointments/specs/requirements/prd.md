# Clinic Appointments — PRD

## Problem Statement
Clinics juggle doctor availability, patient bookings and front-desk check-in by phone and paper. Double bookings, last-minute cancellations and scattered visit notes cost doctors' time and frustrate patients.

## Solution
A web app backed by one API service where doctors publish availability as time slots, patients book, reschedule and cancel appointments, receptionists book on behalf of patients and check them in, and doctors see their day and write visit notes that only doctors can read.

## Actors
- Patient: browses available slots and books, reschedules and cancels their own appointments.
- Doctor: publishes availability slots, sees their own daily schedule, and writes and reads visit notes.
- Receptionist: books, reschedules and cancels appointments for any patient and checks patients in on arrival; cannot read visit notes.

## User Stories
1. As a doctor, I want to publish my availability as time slots, so that patients can book them.
2. As a doctor, I want to remove an unbooked slot, so that my availability stays accurate.
3. As a patient, I want to browse available slots by doctor and date, so that I can choose a time that suits me.
4. As a patient, I want to book an available slot, so that I have a confirmed appointment.
5. As a patient, I want to reschedule my appointment to another available slot, so that I can adapt to changes.
6. As a patient, I want to cancel my appointment, so that the slot is freed for others.
7. As a patient, I want to see my upcoming and past appointments, so that I know my schedule.
8. As a receptionist, I want to book, reschedule or cancel an appointment for a patient, so that patients who call or walk in are served.
9. As a receptionist, I want to check in a patient on arrival, so that the doctor knows they are present.
10. As a doctor, I want to see my appointments for the day, so that I can prepare for my visits.
11. As a doctor, I want to write visit notes on an appointment, so that the patient's care is recorded.
12. As a doctor, I want visit notes to be readable only by doctors, so that patient information stays confidential.
13. As a user, I want to sign in, so that I only see what my role allows.

## Product Decisions
- A slot can be booked by at most one appointment; double booking is prevented.
- An appointment cannot be cancelled (or rescheduled away from) within 2 hours of its start time; this applies to patients and receptionists alike.
- Sign-in: SSO through Thunder, the platform IDP (org default); roles are patient, doctor, receptionist.
- Patients see only their own appointments; doctors see only their own schedule *assumed*
- Visit notes are readable and writable only by doctors; any doctor may read notes of their own appointments only *assumed*
- Slot length is chosen by the doctor when publishing *assumed*
- Check-in is allowed only on the day of the appointment *assumed*
- Delivered as one web app and one API service.

## Out of Scope
- Email or any other notifications.
- File uploads or object storage.
- Scheduled or background jobs.
- External integrations.
- AI features.

## Open Questions
1. How are patient and doctor accounts created and assigned roles (self-sign-up vs. administrator-provided)?
