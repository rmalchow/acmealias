# acme-challenge-api Specification

## Purpose
TBD - created by archiving change add-acme-alias-service. Update Purpose after archive.
## Requirements
### Requirement: Present endpoint accepts challenge values

The service SHALL expose `POST /present` accepting a JSON body with string fields `fqdn` and
`value`. On success it SHALL persist the value as an active challenge record for the alias
mapped to `fqdn` and respond `200 OK`.

The service SHALL accept `fqdn` with or without a trailing dot and SHALL treat DNS names
case-insensitively.

The service SHALL accept the submitted name in either of two forms, and SHALL apply identical
normalization, authorization and responses to both:

- a name within the served zone, which SHALL be resolved to an alias by its label;
- any other name, which SHALL be resolved to an alias by challenge name.

Neither form SHALL be treated as an error, and the service SHALL NOT infer a fault from which
form it receives: ACME clients legitimately differ in whether they resolve the alias themselves
before calling the provider.

#### Scenario: Valid challenge is published

- **WHEN** a request `{"fqdn": "_acme-challenge.shop.example.com.", "value": "gfj9Xq"}` is received with a valid credential authorized for that name
- **THEN** the service responds `200 OK`
- **AND** the value is retrievable as a TXT answer for the mapped alias before the response is sent

#### Scenario: A name within the served zone resolves by label

- **WHEN** a request carries `fqdn` `"shop-example-com.<zone>."` and an alias with the label `shop-example-com` exists
- **THEN** the service resolves that alias and responds `200 OK`

#### Scenario: A challenge name outside the zone resolves by challenge name

- **WHEN** a request carries `fqdn` `"_acme-challenge.shop.example.com."` and an alias exists for that challenge name
- **THEN** the service resolves the same alias and responds `200 OK`

#### Scenario: Both forms are authorized identically

- **WHEN** a credential not scoped to the owning load balancer presents each form for the same alias
- **THEN** both requests are refused with the same status

#### Scenario: The submitted form is observable

- **WHEN** a challenge is published
- **THEN** which of the two forms was submitted is recorded as a log field and counted in a metric

#### Scenario: Trailing dot and case are normalized

- **WHEN** a request carries `fqdn` `"_ACME-Challenge.Shop.Example.com"` and an alias exists for `_acme-challenge.shop.example.com`
- **THEN** the service resolves the same alias and responds `200 OK`

#### Scenario: Malformed body is rejected

- **WHEN** a request body is not valid JSON, or `fqdn` or `value` is missing or empty
- **THEN** the service responds `400 Bad Request` and stores nothing
- **AND** the response names the expected `fqdn` and `value` fields

#### Scenario: RAW-mode body is rejected diagnosably

- **WHEN** a request body carries `domain`, `token` and `keyAuth` instead of `fqdn` and `value`
- **THEN** the service responds `400 Bad Request` naming the expected fields

### Requirement: Presenting a value never displaces an existing one

`POST /present` SHALL add the submitted value to the set of active values for the alias. It
SHALL NOT remove or replace any other active value for that alias.

Presenting a value that is already active for the alias SHALL be idempotent and SHALL extend
the record's expiry.

#### Scenario: Two values coexist at one challenge name

- **WHEN** two different values are presented for the same `fqdn`
- **THEN** both are answerable as TXT records for the mapped alias at the same time

#### Scenario: Repeated present is idempotent

- **WHEN** the same `fqdn` and `value` are presented twice
- **THEN** the service responds `200 OK` both times and exactly one active record exists

### Requirement: Cleanup endpoint withdraws exactly one challenge value

The service SHALL expose `POST /cleanup` accepting the same body shape as `/present`. It SHALL
delete only the active record matching both the resolved alias and the submitted value, and
respond `200 OK`.

Cleanup SHALL be idempotent: removing a record that does not exist is not an error.

#### Scenario: Published value is withdrawn

- **WHEN** `/cleanup` is called with the `fqdn` and `value` of an active record
- **THEN** the service responds `200 OK`
- **AND** subsequent DNS queries for the mapped alias do not return that value

#### Scenario: Sibling values survive cleanup

- **WHEN** two values are active for one alias and `/cleanup` is called for one of them
- **THEN** the other value remains answerable

#### Scenario: Cleanup of an unknown value succeeds

- **WHEN** `/cleanup` is called with a value that is not active
- **THEN** the service responds `200 OK` and no other record is affected

### Requirement: Challenge writes are authorized against the inventory

The service SHALL resolve the submitted `fqdn` to an alias and SHALL reject the request unless
the alias exists and belongs to the load balancer that the presented credential is scoped
to.

Authentication SHALL be evaluated before authorization, and an unauthenticated request SHALL
receive the same response whether or not the submitted name exists in the inventory.

#### Scenario: Unknown name is refused

- **WHEN** an authenticated request presents a challenge for a name with no alias
- **THEN** the service responds `403 Forbidden` and stores nothing

#### Scenario: Name belonging to another load balancer is refused

- **WHEN** an authenticated request presents a challenge for an alias owned by a different load balancer
- **THEN** the service responds `403 Forbidden` and stores nothing

#### Scenario: Unauthenticated request reveals nothing

- **WHEN** a request without valid credentials presents a mapped name and a request without valid credentials presents an unmapped name
- **THEN** both responses are `401 Unauthorized` and indistinguishable

#### Scenario: Refusals are logged

- **WHEN** a request is refused as unauthorized
- **THEN** the credential identifier, the requested name and the reason are recorded

### Requirement: Storage failures are reported as failures

The service SHALL respond `500 Internal Server Error` when a challenge record cannot be
persisted, and SHALL NOT respond `200 OK` for a write it did not complete.

Error responses SHALL NOT disclose datastore identifiers, driver messages or query text.

#### Scenario: Database is unavailable during present

- **WHEN** the datastore rejects the write
- **THEN** the service responds `500` with a generic error code and the value is not answerable

### Requirement: The challenge API is isolated from management routes

The challenge API SHALL be served on a listener that exposes no management, health or metrics
routes.

#### Scenario: Management route is not reachable on the challenge listener

- **WHEN** any administrative, health or metrics path is requested on the challenge listener
- **THEN** the response is `404 Not Found`

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

