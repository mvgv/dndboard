// Package content defines the shared Compendium and Adventure format that the
// Forge writes and the server reads: pure-data entries (stat blocks, weapons),
// schema validation, and a review status gate. It depends on no game code.
//
// v0 covers weapons and stat blocks. A document is JSON with a "weapons" list
// and a "stat_blocks" list. Every entry carries a "status" (draft or
// approved); [Parse] validates the whole document and returns only approved
// entries.
package content
