# The rules engine is the sole mechanical authority

The Ruleset must be applied exactly, and language models are unreliable at exact rule-following. So every mechanical outcome (dice, attack and damage resolution, saves, conditions, resources, movement legality) is decided by a deterministic rules engine running on the server. The Narrator describes, plays NPCs and monsters, and *requests* checks or actions, but those requests are validated and resolved by the engine; the Narrator never decides a mechanical result.

## Consequences

- Monster turns in combat are chosen by deterministic tactical code, not the Narrator. Narrator-driven monster tactics stay in the backlog.
- The server is authoritative; clients only display state and submit intents.
