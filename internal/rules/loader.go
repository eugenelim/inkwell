package rules

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/eugenelim/inkwell/internal/customaction"
	"github.com/eugenelim/inkwell/internal/store"
)

// LoadCatalogue parses rules.toml at the supplied path, validates
// every rule against the spec 32 §6.3 catalogue, and returns the
// catalogue. If the file does not exist, returns an empty catalogue
// with nil error (missing-file is not an error). Any validation
// failure returns a multi-error with file:line context per offending
// entry; the binary refuses to apply (`docs/CONVENTIONS.md` §9: invalid config =
// hard fail).
func LoadCatalogue(path string) (*Catalogue, error) {
	// Missing file is benign.
	b, err := os.ReadFile(path) // #nosec G304 — path is the user's rules.toml (DefaultPath or --file flag). Path-traversal rejected by config.Validate; single-user desktop tool.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Catalogue{Path: path}, nil
		}
		return nil, fmt.Errorf("read rules.toml: %w", err)
	}
	return parseCatalogue(path, b)
}

// parseCatalogue is the file-content-only entry point exposed for
// unit testing without touching the filesystem.
func parseCatalogue(path string, b []byte) (*Catalogue, error) {
	// First decode into a generic intermediate so we can keep TOML
	// keys we don't recognise (and reject them with a pointed error
	// referencing the offending key + the v1 catalogue).
	var doc rulesFile
	md, err := toml.Decode(string(b), &doc)
	if err != nil {
		return nil, fmt.Errorf("parse rules.toml: %w", err)
	}

	var errs []string
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		// TOML keys we don't know about. Sort for deterministic error
		// output.
		keys := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		for _, k := range keys {
			errs = append(errs, fmt.Sprintf("%s: unknown key %q — see spec 32 §6.3 for the v1 catalogue", path, k))
		}
	}

	cat := &Catalogue{Path: path, Rules: make([]Rule, 0, len(doc.Rules))}
	nameSet := make(map[string]int, len(doc.Rules)) // name → first index
	for i, src := range doc.Rules {
		ruleErrs := validateRuleTOML(path, i, src)
		if len(ruleErrs) > 0 {
			errs = append(errs, ruleErrs...)
			continue
		}
		r, err := convertRuleTOML(src)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: rule %d (%q): %v", path, i, src.Name, err))
			continue
		}
		r.SourcePos = i // 0-indexed; renderers can +1 if presenting as line.

		// Duplicate name detection among ID-less rules.
		if r.ID == "" {
			if prior, dup := nameSet[r.Name]; dup {
				errs = append(errs, fmt.Sprintf("%s: rule %d (%q): duplicate name; rule %d has the same name and no ID set", path, i, src.Name, prior))
			}
			nameSet[r.Name] = i
		}

		cat.Rules = append(cat.Rules, r)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("rules.toml validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return cat, nil
}

// rulesFile is the top-level TOML structure.
type rulesFile struct {
	Rules []ruleTOML `toml:"rule"`
}

// ruleTOML is one [[rule]] block in rules.toml. Field names mirror
// the spec 32 §6.3 catalogue. Decoding into a typed struct (rather
// than map[string]any) lets BurntSushi/toml track which keys we
// didn't consume — that's how unknown-key errors are surfaced via
// `md.Undecoded()` in parseCatalogue.
type ruleTOML struct {
	ID       string `toml:"id"`
	Name     string `toml:"name"`
	Sequence int    `toml:"sequence"`
	// Enabled defaults to true when the TOML key is absent. We model
	// it as *bool to distinguish "absent" from "false"; convertRuleTOML
	// fills the default.
	Enabled *bool  `toml:"enabled"`
	Confirm string `toml:"confirm"`

	When   *predicatesTOML `toml:"when"`
	Then   *actionsTOML    `toml:"then"`
	Except *predicatesTOML `toml:"except"`
}

type predicatesTOML struct {
	BodyContains          []string            `toml:"body_contains"`
	BodyOrSubjectContains []string            `toml:"body_or_subject_contains"`
	SubjectContains       []string            `toml:"subject_contains"`
	HeaderContains        []string            `toml:"header_contains"`
	From                  []recipientOrString `toml:"from"`
	SenderContains        []string            `toml:"sender_contains"`
	SentTo                []recipientOrString `toml:"sent_to"`
	RecipientContains     []string            `toml:"recipient_contains"`
	SentToMe              *bool               `toml:"sent_to_me"`
	SentCcMe              *bool               `toml:"sent_cc_me"`
	SentOnlyToMe          *bool               `toml:"sent_only_to_me"`
	SentToOrCcMe          *bool               `toml:"sent_to_or_cc_me"`
	NotSentToMe           *bool               `toml:"not_sent_to_me"`
	HasAttachments        *bool               `toml:"has_attachments"`
	Importance            string              `toml:"importance"`
	Sensitivity           string              `toml:"sensitivity"`
	SizeMinKB             *int                `toml:"size_min_kb"`
	SizeMaxKB             *int                `toml:"size_max_kb"`
	Categories            []string            `toml:"categories"`
	IsAutomaticReply      *bool               `toml:"is_automatic_reply"`
	IsAutomaticForward    *bool               `toml:"is_automatic_forward"`
	Flag                  string              `toml:"flag"`
}

type actionsTOML struct {
	MarkRead       *bool    `toml:"mark_read"`
	MarkImportance string   `toml:"mark_importance"`
	Move           string   `toml:"move"`
	Copy           string   `toml:"copy"`
	AddCategories  []string `toml:"add_categories"`
	Delete         *bool    `toml:"delete"`
	Stop           *bool    `toml:"stop"`
}

// recipientOrString supports both the shorthand `from = ["a@x"]` and
// the full `from = [{ address = "a@x", name = "A" }]` forms. BurntSushi
// surfaces "either a string or a table" as a UnmarshalTOML callback
// (we use the TextUnmarshaler interface via Custom unmarshaling).
type recipientOrString struct {
	Address string
	Name    string
}

// UnmarshalTOML implements the toml.Unmarshaler interface so we can
// accept either a bare string or a {address,name} table.
func (r *recipientOrString) UnmarshalTOML(v any) error {
	switch x := v.(type) {
	case string:
		r.Address = x
		return nil
	case map[string]any:
		addr, _ := x["address"].(string)
		name, _ := x["name"].(string)
		if addr == "" {
			return fmt.Errorf("recipient: `address` is required")
		}
		r.Address = addr
		r.Name = name
		return nil
	default:
		return fmt.Errorf("recipient: expected string or {address, name} table, got %T", v)
	}
}

// validateRuleTOML returns a list of `path:N: …` error strings for the
// supplied rule, or nil when the rule is valid. Errors include
// rule-index context so the caller can correlate without re-walking.
func validateRuleTOML(path string, idx int, r ruleTOML) []string {
	var errs []string
	prefix := fmt.Sprintf("%s: rule %d (%q):", path, idx, r.Name)

	if strings.TrimSpace(r.Name) == "" {
		errs = append(errs, prefix+" name is required")
	}
	if r.Sequence < 0 {
		errs = append(errs, fmt.Sprintf("%s sequence %d must be >= 0", prefix, r.Sequence))
	}
	if r.Then == nil || isEmptyActions(*r.Then) {
		errs = append(errs, prefix+" [rule.then] is required and must contain at least one action")
	}

	// Confirm value.
	switch strings.ToLower(strings.TrimSpace(r.Confirm)) {
	case "", "auto", "always", "never":
		// OK
	default:
		errs = append(errs, fmt.Sprintf("%s confirm %q must be one of \"auto\", \"always\", \"never\"", prefix, r.Confirm))
	}

	// Importance / sensitivity / flag enums.
	if r.When != nil {
		errs = append(errs, validatePredicatesEnums(prefix+" when:", *r.When)...)
	}
	if r.Except != nil {
		errs = append(errs, validatePredicatesEnums(prefix+" except:", *r.Except)...)
	}
	if r.Then != nil {
		if r.Then.MarkImportance != "" {
			switch r.Then.MarkImportance {
			case "low", "normal", "high":
			default:
				errs = append(errs, fmt.Sprintf("%s then.mark_importance %q must be one of \"low\", \"normal\", \"high\"", prefix, r.Then.MarkImportance))
			}
		}
	}

	// Destructive-action gate: delete=true requires confirm=always.
	if r.Then != nil && r.Then.Delete != nil && *r.Then.Delete {
		switch strings.ToLower(strings.TrimSpace(r.Confirm)) {
		case "always":
			// OK
		case "never":
			errs = append(errs, fmt.Sprintf("%s confirm=\"never\" is forbidden for any rule containing `delete = true` (spec 27 §3.4 parity)", prefix))
		default:
			errs = append(errs, fmt.Sprintf("%s rule contains `delete = true` and requires `confirm = \"always\"` at the [[rule]] level (spec 32 §6.4)", prefix))
		}
	}

	// Size range sanity.
	if r.When != nil {
		errs = append(errs, validateSizeRange(prefix+" when:", r.When.SizeMinKB, r.When.SizeMaxKB)...)
	}
	if r.Except != nil {
		errs = append(errs, validateSizeRange(prefix+" except:", r.Except.SizeMinKB, r.Except.SizeMaxKB)...)
	}

	return errs
}

func validatePredicatesEnums(prefix string, p predicatesTOML) []string {
	var errs []string
	if p.Importance != "" {
		switch p.Importance {
		case "low", "normal", "high":
		default:
			errs = append(errs, fmt.Sprintf("%s importance %q must be one of \"low\", \"normal\", \"high\"", prefix, p.Importance))
		}
	}
	if p.Sensitivity != "" {
		switch p.Sensitivity {
		case "normal", "personal", "private", "confidential":
		default:
			errs = append(errs, fmt.Sprintf("%s sensitivity %q must be one of \"normal\", \"personal\", \"private\", \"confidential\"", prefix, p.Sensitivity))
		}
	}
	if p.Flag != "" {
		switch p.Flag {
		case "any", "call", "doNotForward", "followUp", "fyi", "forward",
			"noResponseNecessary", "read", "reply", "replyToAll", "review":
		default:
			errs = append(errs, fmt.Sprintf("%s flag %q must be one of any, call, doNotForward, followUp, fyi, forward, noResponseNecessary, read, reply, replyToAll, review", prefix, p.Flag))
		}
	}
	return errs
}

func validateSizeRange(prefix string, minKB, maxKB *int) []string {
	var errs []string
	const maxAllowed = 2_097_151 // Graph's documented int32 KB ceiling (~2 GiB)
	if minKB != nil {
		if *minKB < 0 || *minKB > maxAllowed {
			errs = append(errs, fmt.Sprintf("%s size_min_kb %d must be in [0, %d]", prefix, *minKB, maxAllowed))
		}
	}
	if maxKB != nil {
		if *maxKB < 0 || *maxKB > maxAllowed {
			errs = append(errs, fmt.Sprintf("%s size_max_kb %d must be in [0, %d]", prefix, *maxKB, maxAllowed))
		}
	}
	if minKB != nil && maxKB != nil && *minKB > *maxKB {
		errs = append(errs, fmt.Sprintf("%s size_min_kb (%d) must not exceed size_max_kb (%d)", prefix, *minKB, *maxKB))
	}
	return errs
}

func isEmptyActions(a actionsTOML) bool {
	return a.MarkRead == nil &&
		a.MarkImportance == "" &&
		a.Move == "" &&
		a.Copy == "" &&
		len(a.AddCategories) == 0 &&
		a.Delete == nil &&
		a.Stop == nil
}

// convertRuleTOML converts the validated TOML shape into the
// inkwell-side typed Rule shape, ready for diffing.
func convertRuleTOML(r ruleTOML) (Rule, error) {
	out := Rule{
		ID:       r.ID,
		Name:     strings.TrimSpace(r.Name),
		Sequence: r.Sequence,
		Enabled:  true, // default
		Confirm:  parseConfirm(r.Confirm),
	}
	if r.Enabled != nil {
		out.Enabled = *r.Enabled
	}
	if r.When != nil {
		out.When = predicatesFromTOML(*r.When)
	}
	if r.Except != nil {
		out.Except = predicatesFromTOML(*r.Except)
	}
	if r.Then != nil {
		out.Then = actionsFromTOML(*r.Then)
	}
	return out, nil
}

func parseConfirm(s string) customaction.ConfirmPolicy {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "always":
		return customaction.ConfirmAlways
	case "never":
		return customaction.ConfirmNever
	default:
		return customaction.ConfirmAuto
	}
}

func predicatesFromTOML(p predicatesTOML) store.MessagePredicates {
	out := store.MessagePredicates{
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
		MessageActionFlag:     p.Flag,
	}
	if len(p.From) > 0 {
		out.FromAddresses = make([]store.RuleRecipient, 0, len(p.From))
		for _, r := range p.From {
			out.FromAddresses = append(out.FromAddresses, store.RuleRecipient{
				EmailAddress: store.RuleEmailAddress{Address: r.Address, Name: r.Name},
			})
		}
	}
	if len(p.SentTo) > 0 {
		out.SentToAddresses = make([]store.RuleRecipient, 0, len(p.SentTo))
		for _, r := range p.SentTo {
			out.SentToAddresses = append(out.SentToAddresses, store.RuleRecipient{
				EmailAddress: store.RuleEmailAddress{Address: r.Address, Name: r.Name},
			})
		}
	}
	if p.SizeMinKB != nil || p.SizeMaxKB != nil {
		const maxSentinel = 2_097_151
		min := 0
		max := maxSentinel
		if p.SizeMinKB != nil {
			min = *p.SizeMinKB
		}
		if p.SizeMaxKB != nil {
			max = *p.SizeMaxKB
		}
		out.WithinSizeRange = &store.RuleSizeKB{MinimumSize: min, MaximumSize: max}
	}
	return out
}

func actionsFromTOML(a actionsTOML) store.MessageActions {
	return store.MessageActions{
		MarkAsRead:          a.MarkRead,
		MarkImportance:      a.MarkImportance,
		MoveToFolder:        a.Move,
		CopyToFolder:        a.Copy,
		AssignCategories:    a.AddCategories,
		Delete:              a.Delete,
		StopProcessingRules: a.Stop,
	}
}
