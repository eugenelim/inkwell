package rules

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/eugenelim/inkwell/internal/store"
)

// DefaultPath returns the default rules.toml path under
// `$XDG_CONFIG_HOME/inkwell/rules.toml` (or ~/.config/inkwell/rules.toml).
// Returns the empty string and a non-nil error if the user's home
// directory cannot be resolved.
func DefaultPath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "inkwell", "rules.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "inkwell", "rules.toml"), nil
}

// EncodeCatalogue returns the canonical TOML representation of a
// catalogue (the format `inkwell rules pull` writes back to disk).
// Round-tripping a pulled catalogue through Encode → parseCatalogue
// must produce the same Rule slice (this is gated by a test).
func EncodeCatalogue(rules []Rule) ([]byte, error) {
	doc := rulesFile{
		Rules: make([]ruleTOML, 0, len(rules)),
	}
	for _, r := range rules {
		t, err := toTOML(r)
		if err != nil {
			return nil, err
		}
		doc.Rules = append(doc.Rules, t)
	}

	var buf bytes.Buffer
	buf.WriteString("# rules.toml — managed by `inkwell rules pull` and\n")
	buf.WriteString("# `inkwell rules apply`. Hand-edits are welcome; run\n")
	buf.WriteString("# `inkwell rules apply --dry-run` before pushing.\n")
	buf.WriteString("#\n")
	buf.WriteString("# Inside one predicate, list items are OR'd; predicates AND\n")
	buf.WriteString("# together. See spec 32 §6.3 for the v1 catalogue.\n\n")
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("encode rules.toml: %w", err)
	}
	return buf.Bytes(), nil
}

func toTOML(r Rule) (ruleTOML, error) {
	out := ruleTOML{
		ID:       r.ID,
		Name:     r.Name,
		Sequence: r.Sequence,
	}
	if !r.Enabled {
		// Emit `enabled = false` explicitly; absent key means default-true.
		f := false
		out.Enabled = &f
	}
	switch r.Confirm {
	case 1: // customaction.ConfirmAlways
		out.Confirm = "always"
	case 2: // customaction.ConfirmNever
		out.Confirm = "never"
	}
	out.When = predicatesToTOML(r.When)
	out.Except = predicatesToTOML(r.Except)
	out.Then = actionsToTOML(r.Then)
	return out, nil
}

func predicatesToTOML(p store.MessagePredicates) *predicatesTOML {
	if isEmptyPredicates(p) {
		return nil
	}
	out := &predicatesTOML{
		BodyContains:          p.BodyContains,
		BodyOrSubjectContains: p.BodyOrSubjectContains,
		SubjectContains:       p.SubjectContains,
		HeaderContains:        p.HeaderContains,
		SenderContains:        p.SenderContains,
		RecipientContains:     p.RecipientContains,
		SentToMe:              p.SentToMe,
		SentCcMe:              p.SentCcMe,
		SentOnlyToMe:          p.SentOnlyToMe,
		SentToOrCcMe:          p.SentToOrCcMe,
		NotSentToMe:           p.NotSentToMe,
		HasAttachments:        p.HasAttachments,
		Importance:            p.Importance,
		Sensitivity:           p.Sensitivity,
		Categories:            p.Categories,
		IsAutomaticReply:      p.IsAutomaticReply,
		IsAutomaticForward:    p.IsAutomaticForward,
		Flag:                  p.MessageActionFlag,
	}
	if len(p.FromAddresses) > 0 {
		out.From = make([]recipientOrString, 0, len(p.FromAddresses))
		for _, r := range p.FromAddresses {
			out.From = append(out.From, recipientOrString{Address: r.EmailAddress.Address, Name: r.EmailAddress.Name})
		}
	}
	if len(p.SentToAddresses) > 0 {
		out.SentTo = make([]recipientOrString, 0, len(p.SentToAddresses))
		for _, r := range p.SentToAddresses {
			out.SentTo = append(out.SentTo, recipientOrString{Address: r.EmailAddress.Address, Name: r.EmailAddress.Name})
		}
	}
	if p.WithinSizeRange != nil {
		min := p.WithinSizeRange.MinimumSize
		max := p.WithinSizeRange.MaximumSize
		out.SizeMinKB = &min
		out.SizeMaxKB = &max
	}
	return out
}

func actionsToTOML(a store.MessageActions) *actionsTOML {
	if isEmptyStoreActions(a) {
		return nil
	}
	return &actionsTOML{
		MarkRead:       a.MarkAsRead,
		MarkImportance: a.MarkImportance,
		Move:           a.MoveToFolder,
		Copy:           a.CopyToFolder,
		AddCategories:  a.AssignCategories,
		Delete:         a.Delete,
		Stop:           a.StopProcessingRules,
	}
}

func isEmptyPredicates(p store.MessagePredicates) bool {
	return len(p.BodyContains) == 0 &&
		len(p.BodyOrSubjectContains) == 0 &&
		len(p.SubjectContains) == 0 &&
		len(p.HeaderContains) == 0 &&
		len(p.FromAddresses) == 0 &&
		len(p.SenderContains) == 0 &&
		len(p.SentToAddresses) == 0 &&
		len(p.RecipientContains) == 0 &&
		p.SentToMe == nil &&
		p.SentCcMe == nil &&
		p.SentOnlyToMe == nil &&
		p.SentToOrCcMe == nil &&
		p.NotSentToMe == nil &&
		p.HasAttachments == nil &&
		p.Importance == "" &&
		p.Sensitivity == "" &&
		p.WithinSizeRange == nil &&
		len(p.Categories) == 0 &&
		p.IsAutomaticReply == nil &&
		p.IsAutomaticForward == nil &&
		p.MessageActionFlag == ""
}

func isEmptyStoreActions(a store.MessageActions) bool {
	return a.MarkAsRead == nil &&
		a.MarkImportance == "" &&
		a.MoveToFolder == "" &&
		a.CopyToFolder == "" &&
		len(a.AssignCategories) == 0 &&
		a.Delete == nil &&
		a.StopProcessingRules == nil
}

// AtomicWriteFile writes content to path via a sibling .tmp file +
// fsync + rename. On any write / fsync error the .tmp file is
// os.Remove'd before returning so the user's directory does not
// accumulate orphans (spec 32 §6.5 step 7).
func AtomicWriteFile(path string, content []byte, mode os.FileMode) (retErr error) {
	dir := filepath.Dir(path)
	tmp := path + ".tmp"

	// Defer cleanup so we remove the tmp file on any error path
	// (including the rare case where Rename succeeds but the
	// subsequent operations fail). On the happy path the rename
	// removed the tmp, so os.Remove returns os.ErrNotExist which we
	// swallow.
	defer func() {
		if retErr != nil {
			_ = os.Remove(tmp)
		}
	}()

	// Ensure parent dir exists.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode) // #nosec G304 — tmp is path+".tmp" derived from the user's rules.toml destination. Path-traversal rejected by config.Validate; atomic rename replaces the final file.
	if err != nil {
		return fmt.Errorf("open tmp file: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return fmt.Errorf("write tmp file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("fsync tmp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close tmp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename tmp file: %w", err)
	}
	return nil
}
