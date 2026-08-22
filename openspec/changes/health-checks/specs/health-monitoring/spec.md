## ADDED Requirements

### Requirement: Satellites probe and report; the centre decides

Probes SHALL be executed by satellites and their results reported to the centre. A satellite SHALL
NOT change what it serves on the basis of its own probe results.

The centre SHALL evaluate reported results and SHALL distribute any resulting change, so that every
satellite serves the same answer for the same name.

#### Scenario: A satellite does not act on its own observation

- **WHEN** a satellite's probe of a node fails
- **THEN** the satellite continues to serve that node's address until the centre distributes a change

#### Scenario: Every satellite serves the same answer

- **WHEN** a node's state changes and the change is distributed
- **THEN** every satellite serves the same values for the affected name

### Requirement: A node's state changes only on the agreement of several satellites

The service SHALL change a node's state only when at least a configured number of distinct satellites
report agreeing results. Repeated reports from one satellite for one cycle SHALL count once.

A node SHALL be probed by at least as many satellites as are required to agree.

#### Scenario: A minority report changes nothing

- **WHEN** one satellite of three reports a node unreachable and the others report it reachable
- **THEN** the node's state does not change

#### Scenario: Agreement changes state

- **WHEN** the configured number of satellites report a node unreachable
- **THEN** the node's state changes

#### Scenario: A re-delivered report does not count twice

- **WHEN** a satellite delivers the same cycle's result twice
- **THEN** it contributes one opinion

#### Scenario: Too few probing satellites cannot change state

- **WHEN** fewer satellites are assigned to a node than are required to agree
- **THEN** the node's state does not change and the condition is reported

### Requirement: State changes require consecutive agreeing cycles, and recovery requires more than failure

The service SHALL mark a node unavailable only after a configured number of consecutive cycles
agreeing that it is unreachable, and SHALL return it to service only after a configured number of
consecutive cycles agreeing that it is reachable.

The number required to return a node to service SHALL be greater than one.

#### Scenario: A single failing cycle does not remove a node

- **WHEN** one cycle reports a node unreachable and the next reports it reachable
- **THEN** the node's state does not change

#### Scenario: Recovery is not immediate

- **WHEN** an unavailable node passes one cycle
- **THEN** it does not yet return to service

#### Scenario: Sustained failure removes a node

- **WHEN** the configured number of consecutive cycles agree a node is unreachable
- **THEN** it is marked unavailable

### Requirement: Probe results are retained as history, separately from current state

The service SHALL retain probe results as an append-only history recording the target, the satellite,
the time, the outcome and the observed latency, and SHALL hold current node state separately.

History SHALL be retained under a policy that bounds its growth, and the evaluation of state SHALL NOT
depend on scanning that history.

#### Scenario: History answers when a condition began

- **WHEN** a node has been unavailable for some time
- **THEN** the history reports when its results first began to fail

#### Scenario: State evaluation does not depend on history retention

- **WHEN** history older than the retention policy is removed
- **THEN** current node state is unaffected

#### Scenario: History is confined to its account

- **WHEN** a member of one account reads probe history
- **THEN** only results for that account's nodes are returned

### Requirement: Delivered probe results are recorded as billable events

The service SHALL record a delivered probe result as an event available for metering. An undelivered
result SHALL produce no event.

#### Scenario: A delivered result produces an event

- **WHEN** a satellite delivers a probe result
- **THEN** an event is recorded for it

#### Scenario: An undelivered result produces nothing

- **WHEN** a satellite discards a batch it cannot deliver
- **THEN** no event is recorded for its contents
