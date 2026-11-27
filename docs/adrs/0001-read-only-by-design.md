---
status: accepted
date: 2026-11-27
decision-makers: Ivan Lee
---

# releash only reads flag state and never writes to Unleash

## Context and Problem Statement

releash turns Unleash feature flags into go/no-go decisions for CD pipeline steps. A pipeline could also change flags, for example turning a flag on after a deploy succeeds or rolling a percentage forward. Should releash be able to change flags, or only read them?

## Decision Drivers

* Unleash should be the one place where people see and change release gates, with its audit log and permissions.
* Pipelines run with broad credentials and are easy to misconfigure. Their blast radius should be small.
* The CLI contract should stay small enough to use from plain shell.

## Considered Options

* Read-only: releash evaluates flags and never modifies them.
* Read-write: releash can also toggle flags and edit strategies through the Admin API.

## Decision Outcome

Chosen option: "Read-only", because it keeps every change to a gate in the Unleash UI, where it is audited and permissioned, and lets releash run with a backend token that cannot modify anything.

### Consequences

* Good, because a leaked or misused releash token cannot turn a gate on.
* Good, because releash only needs the Client API and a backend token, which CI systems usually already have.
* Bad, because progressive rollouts driven by the pipeline itself, such as "bump the rollout to 50% after the canary passes", need a different tool or a manual step in the Unleash UI.

### Confirmation

releash uses only Client API endpoints (`/api/client/...`), and the code does not depend on the Admin API. Reviews reject any new command that writes to Unleash.

## Pros and Cons of the Options

### Read-only

* Good, because the only thing releash can do with a token is read flag state and report usage metrics.
* Good, because the command surface is a single boolean check.
* Bad, because pipelines cannot automate flag changes with releash.

### Read-write

* Good, because a pipeline could promote a release end to end without a person.
* Bad, because it needs an admin or personal access token in CI, which can change any flag the token can see.
* Bad, because changes made by pipelines are harder to attribute and review than changes made in the Unleash UI.

## More Information

See [spec 0001](../specs/0001-rollout-decisions.md). Writing to Unleash may be reconsidered as a separate, explicitly opt-in tool, but not as part of `releash check`.
