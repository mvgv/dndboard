// Package content defines the shared Compendium and Adventure format that the
// Forge writes and the server reads: pure-data entries (weapons, stat blocks),
// features with declarative effects, schema validation, and a review status
// gate. It depends on no game code.
//
// A document is JSON with "weapons", "stat_blocks" and "features" lists.
// Every entry carries a "status" (draft or approved); [Parse] validates the
// whole document and returns only approved entries.
//
// A [Feature] is either a list of [Effect]s (trigger, conditions, frequency,
// outcome), optionally behind an [Activation] such as Rage's, or flagged
// "requires_implementation" with no effects, in which case the rules engine
// supplies hand-written code under the feature's ID (ADR 0007). The effect
// vocabulary is closed: [Parse] rejects unknown words and conditions or
// outcomes that don't fit their trigger.
package content
