## ADDED Requirements

### Requirement: Recording interaction history never fails a challenge request

The challenge API SHALL record the opening and closing of a challenge interaction as a side effect
of publication and withdrawal, and SHALL treat that recording as best effort: a failure to record
SHALL be logged and SHALL NOT change the response, and SHALL NOT prevent the value from being
published or withdrawn.

#### Scenario: History failure does not fail a publication

- **WHEN** a challenge value is published and its interaction record cannot be written
- **THEN** the request succeeds, the value is answerable, and the failure is logged

#### Scenario: History failure does not fail a withdrawal

- **WHEN** a challenge value is withdrawn and its interaction record cannot be closed
- **THEN** the request succeeds and the value is no longer answerable

#### Scenario: A retried publication does not open a second interaction

- **WHEN** the same value is published again at the same alias while the first is still live
- **THEN** the existing interaction is reused with its original publication time
