// Package sync orchestrates Microsoft Graph ↔ local store reconciliation.
// It owns the sync state machine, polling cadence, backfill, and delta
// loops. See spec 03 and ARCH §4.
package sync
