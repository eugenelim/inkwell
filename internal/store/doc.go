// Package store is the sole owner of the local SQLite cache (mail.db).
// All other packages use this typed API; no other package opens the DB.
// See spec 02 and ARCH §3, §7.
package store
