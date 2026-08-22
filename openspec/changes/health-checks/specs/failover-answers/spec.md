## ADDED Requirements

### Requirement: A recordset may be health-checked, and its served values follow node state

The service SHALL allow a recordset to be marked health-checked. The values served for such a
recordset SHALL be its stored values, excluding those whose node is currently unavailable.

The stored recordset SHALL NOT be altered by a state change. A value excluded by node state SHALL be
served again when that node returns to service, without being re-added.

#### Scenario: An unavailable node's value is not served

- **WHEN** a node of a health-checked recordset is marked unavailable
- **THEN** its value is not served and the remaining values are

#### Scenario: The stored recordset is unchanged

- **WHEN** a value is excluded by node state
- **THEN** the stored recordset still contains it

#### Scenario: A returning node is served again

- **WHEN** an unavailable node returns to service
- **THEN** its value is served again without any change to the stored recordset

#### Scenario: An unchecked recordset is unaffected

- **WHEN** a recordset is not marked health-checked
- **THEN** all its stored values are served regardless of node state

### Requirement: A health-checked recordset is never served empty

When every value of a health-checked recordset would be excluded by node state, the service SHALL
serve all of its stored values.

The condition SHALL be reported as an operational alarm.

#### Scenario: All nodes unavailable serves everything

- **WHEN** every node of a health-checked recordset is marked unavailable
- **THEN** every stored value is served

#### Scenario: The condition is reported

- **WHEN** a health-checked recordset has no available node
- **THEN** the condition is reported as an alarm

#### Scenario: Recovery of one node restores filtering

- **WHEN** one node of an all-unavailable recordset returns to service
- **THEN** only available values are served again

### Requirement: Health-checked recordsets carry a time to live suited to failover

The service SHALL apply to health-checked recordsets a default time to live lower than that applied
to recordsets that are not health-checked, and SHALL make it configurable.

The service SHALL document that the time to live, and not the interval between probes, bounds how
quickly a change is observed by resolvers.

#### Scenario: A health-checked recordset has a lower default

- **WHEN** a recordset is marked health-checked and no time to live is given
- **THEN** the applied default is lower than that for a recordset that is not health-checked

#### Scenario: The time to live is configurable

- **WHEN** a time to live is given for a health-checked recordset
- **THEN** it is applied

### Requirement: A failover decision is distributed like any other change

A change to the values served for a health-checked recordset SHALL be distributed to satellites
through the same mechanism that distributes any other change to what is served.

#### Scenario: A decision reaches every satellite

- **WHEN** a node is marked unavailable
- **THEN** the resulting served values are distributed to every satellite

#### Scenario: A disconnected satellite serves its last known values

- **WHEN** a satellite cannot reach the centre and a failover decision is made
- **THEN** it continues to serve what it last received until it reconnects
