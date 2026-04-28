//go:build !race

package store

func isRaceEnabled() bool { return false }
