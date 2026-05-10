package ui

// ArchiveLabel is the typed config value for [ui].archive_label.
// Two literal values are accepted; everything else fails validation
// at config load (spec 30 §4.1).
type ArchiveLabel string

const (
	// ArchiveLabelArchive renders the archive verb as "archive" /
	// "Archive" — the default; preserves continuity with prior
	// inkwell versions.
	ArchiveLabelArchive ArchiveLabel = "archive"
	// ArchiveLabelDone renders the verb as "done" / "Done" — the
	// HEY/Inbox vocabulary. Same underlying action.
	ArchiveLabelDone ArchiveLabel = "done"
)

// archiveVerbLower returns the imperative-lowercase verb for the
// archive action ("archive" or "done") for use in hint strings,
// status-bar toasts, and filter status bar text. Unknown values
// fall back to "archive" — config validation rejects unknown
// values at load, so this branch is only hit by tests with bare
// zero-value labels.
func archiveVerbLower(label ArchiveLabel) string {
	if label == ArchiveLabelDone {
		return "done"
	}
	return "archive"
}

// archiveVerbTitle returns the title-cased form ("Archive" or
// "Done") for palette titles, confirm modals, and toast headers.
func archiveVerbTitle(label ArchiveLabel) string {
	if label == ArchiveLabelDone {
		return "Done"
	}
	return "Archive"
}

// archiveVerbForName branches on the action name. When name is
// "archive" the result is the configured verb; otherwise the
// name is returned unchanged. The central toast formatter calls
// this once per emission so the only surface that learns about
// the label is the format step.
func archiveVerbForName(name string, label ArchiveLabel) string {
	if name == "archive" {
		return archiveVerbLower(label)
	}
	return name
}

// archiveVerbTitleForName is the title-cased sibling — used by
// bulk and thread toasts which open with a capitalised verb.
func archiveVerbTitleForName(name string, label ArchiveLabel) string {
	if name == "archive" {
		return archiveVerbTitle(label)
	}
	return name
}

// archivePaletteRowTitle is the spec 22 single-message palette row
// title for the archive verb, branded per spec 30 §5.5.
func archivePaletteRowTitle(label ArchiveLabel) string {
	if label == ArchiveLabelDone {
		return "Mark done"
	}
	return "Archive message"
}

// archivePaletteThreadRowTitle is the thread variant.
func archivePaletteThreadRowTitle(label ArchiveLabel) string {
	if label == ArchiveLabelDone {
		return "Mark thread done"
	}
	return "Archive thread"
}

// archiveAvailability rewrites the .Why text on an Availability so
// the configured verb shows in the "no message focused" hint
// rather than a stray `archive:`. Returns the input unchanged when
// the row is OK or the Why text doesn't reference "archive".
func archiveAvailability(in Availability, label ArchiveLabel) Availability {
	if in.OK {
		return in
	}
	verb := archiveVerbLower(label)
	if verb == "archive" {
		return in
	}
	// Replace any leading "archive:" token; leave the rest alone.
	if len(in.Why) >= len("archive:") && in.Why[:len("archive:")] == "archive:" {
		in.Why = verb + in.Why[len("archive:"):]
	}
	return in
}
