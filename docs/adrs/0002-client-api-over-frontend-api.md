---
status: accepted
date: 2026-11-27
decision-makers: Ivan Lee
---

# Evaluate flags through the Client API with the official Go SDK

## Context and Problem Statement

Unleash offers two ways for a client to get flag state. The Client API returns flag configurations to a backend SDK, which evaluates them locally. The Frontend API, served by Unleash or Unleash Edge, evaluates flags on the server and returns the results. Which one should releash use?

## Decision Drivers

* Results must match what Unleash's own SDKs return for the same flag and context.
* `--loud` should tell a user when the flag they asked for does not exist, the most common mistake.
* releash should work with what CI systems usually have, without extra Unleash infrastructure.

## Considered Options

* Client API with a backend token, evaluated by the official Go SDK.
* Frontend API with a frontend token.
* Client API with releash's own evaluation code instead of the SDK.

## Decision Outcome

Chosen option: "Client API with a backend token, evaluated by the official Go SDK", because it gives the same results as every other Unleash SDK, can tell an unknown flag from a disabled one, and needs only a backend token.

### Consequences

* Good, because every strategy, constraint and segment is evaluated by Unleash's own code.
* Good, because releash appears in Unleash's applications view and usage metrics like any other client.
* Bad, because each check downloads every flag the token can see, not just the one being checked.
* Bad, because the SDK is built for long-running services. releash has to override its persistent backup file, add an HTTP timeout and flush metrics itself (see spec 0001, "Unleash integration").

### Confirmation

The SDK module is pinned in `go.mod`. Integration tests against a real Unleash server check that constraint-based results match the server's configuration, and that an unknown flag is reported as not found under `--loud`.

## Pros and Cons of the Options

### Client API with the official Go SDK

* Good, because evaluation stays correct as Unleash adds strategies and operators, by upgrading the SDK.
* Good, because the full flag list lets releash detect unknown flags.
* Neutral, because the download is larger than a single-flag lookup, which is fine for typical instances.
* Bad, because the SDK's service-oriented defaults need workarounds in a one-shot CLI.

### Frontend API

* Good, because the server evaluates the flag and returns only the result.
* Bad, because it returns only enabled flags, so releash cannot tell an unknown flag from a disabled one.
* Bad, because it requires the Frontend API or Unleash Edge to be enabled and a frontend token, which CI rarely has.

### Client API with releash's own evaluation

* Good, because releash would control startup, storage and shutdown exactly.
* Bad, because reimplementing Unleash's strategies and constraints risks results that differ from other SDKs, which defeats the point of using Unleash as the source of truth.

## More Information

See [spec 0001](../specs/0001-rollout-decisions.md). The SDK workarounds come from a spike on 2026-11-27 against Unleash 8.3.1 and `github.com/Unleash/unleash-go-sdk/v5` v5.1.0, recorded in [the initial specification plan](../agents/plans/2026-11-26-initial-specification.md).
