package customaction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/eugenelim/inkwell/internal/pattern"
)

// Deps is the subset of the executor surface the loader needs at
// validation time. PatternCompile is the spec 08 pattern parser
// reference — tests pass a stub, production wires pattern.Compile.
type Deps struct {
	PatternCompile func(string, pattern.CompileOptions) (*pattern.Compiled, error)
	PatternOpts    pattern.CompileOptions
	Now            func() time.Time
	// Logger receives the deprecation warnings (e.g. roadmap-syntax
	// alias rewrite, prompt_confirm legacy alias). Optional — nil
	// suppresses warnings.
	Logger *slog.Logger
	// ReservedKeys is the set of single-key strings already bound by
	// the static KeyMap (post-[bindings] override). The loader rejects
	// any custom action whose key collides with one of these.
	// Empty / nil disables the cross-check (used by the CLI
	// `validate` subcommand which doesn't have a KeyMap).
	ReservedKeys map[string]string // key → KeyMap field name (e.g. "a" → "Archive")
}

// nameRegex bounds custom action names to a stable subset matching
// spec 27 §3.7: [a-z][a-z0-9_]{0,31}.
var nameRegex = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// keyRegex bounds the single-key form spec 27 §5.3 ships: a single
// rune, ctrl+<rune>, or alt+<rune>. Chord-style strings are rejected.
var keyRegex = regexp.MustCompile(`^([a-zA-Z0-9~!@#$%^&*()\-_=+\[\]{};:'",.<>/?\\|` + "`" + `]|ctrl\+[a-z]|alt\+[a-z])$`)

// roadmapAliases rewrites the roadmap's draft single-brace syntax
// to Go text/template form (§4.2 alias table).
var roadmapAliases = map[string]string{
	"{sender}":          "{{.From}}",
	"{sender_name}":     "{{.FromName}}",
	"{sender_domain}":   "{{.SenderDomain}}",
	"{subject}":         "{{.Subject}}",
	"{conversation_id}": "{{.ConversationID}}",
	"{message_id}":      "{{.MessageID}}",
	"{user_input}":      "{{.UserInput}}",
	"{folder}":          "{{.Folder}}",
}

// reservedStackCategories are the spec 25 thread-level category
// constants the per-message add_category / remove_category ops must
// not literally contain (templated values are not re-validated).
var reservedStackCategories = map[string]struct{}{
	"Inkwell/ReplyLater": {},
	"Inkwell/SetAside":   {},
}

// perMessageVars is the set of template variables that bind to the
// focused-message context. RequiresMessageContext is true if any
// step references one of these.
var perMessageVars = map[string]struct{}{
	"From":           {},
	"FromName":       {},
	"SenderDomain":   {},
	"To":             {},
	"Subject":        {},
	"ConversationID": {},
	"MessageID":      {},
	"Date":           {},
	"Folder":         {},
	"IsRead":         {},
	"FlagStatus":     {},
}

// scopeNames maps the TOML scope strings to the typed enum.
var scopeNames = map[string]Scope{
	"list":    ScopeList,
	"viewer":  ScopeViewer,
	"folders": ScopeFolders,
}

// rawAction is the on-disk shape. Decoded via BurntSushi/toml; extra
// keys produce a hard error per spec 27 §3.7 step 1 (inverted from
// the [bindings] gate).
type rawAction struct {
	Name                string    `toml:"name"`
	Key                 string    `toml:"key"`
	Description         string    `toml:"description"`
	When                []string  `toml:"when"`
	Confirm             string    `toml:"confirm"`
	PromptConfirm       *bool     `toml:"prompt_confirm"`
	StopOnError         *bool     `toml:"stop_on_error"`
	AllowFolderTemplate bool      `toml:"allow_folder_template"`
	AllowURLTemplate    bool      `toml:"allow_url_template"`
	Sequence            []rawStep `toml:"sequence"`
}

type rawStep struct {
	Op          string `toml:"op"`
	StopOnError *bool  `toml:"stop_on_error"`

	// Op-specific params. The decoder keeps the raw map plus typed
	// fields for the most common ones; oddballs (e.g. set_thread_muted.value)
	// pull from the map.
	Destination string `toml:"destination"`
	Category    string `toml:"category"`
	Pattern     string `toml:"pattern"`
	Prompt      string `toml:"prompt"`
	URL         string `toml:"url"`
	Value       *bool  `toml:"value"`
}

type rawCatalogue struct {
	CustomAction []rawAction `toml:"custom_action"`
}

// MultiError aggregates per-action validation errors with file:line
// context where available.
type MultiError struct {
	Errs []error
}

func (m *MultiError) Error() string {
	parts := make([]string, len(m.Errs))
	for i, err := range m.Errs {
		parts[i] = err.Error()
	}
	return strings.Join(parts, "; ")
}

// LoadCatalogue reads actions.toml from path, validates every action,
// pre-compiles templates and patterns, and returns the catalogue. A
// missing file returns an empty catalogue and nil error per spec 27
// §3.1. Validation failures aggregate into a MultiError.
func LoadCatalogue(ctx context.Context, path string, deps Deps) (*Catalogue, error) {
	cat := &Catalogue{
		ByName: map[string]*Action{},
		ByKey:  map[string]*Action{},
	}
	if path == "" {
		return cat, nil
	}
	// #nosec G304 — path is the user's actions.toml (config-derived).
	// Same trust model as config.toml: single-user desktop tool, the
	// user owns the path.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cat, nil
		}
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	var raw rawCatalogue
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("%s: unknown key(s): %s", path, strings.Join(keys, ", "))
	}
	if len(raw.CustomAction) == 0 {
		return cat, nil
	}
	if len(raw.CustomAction) > 256 {
		return nil, fmt.Errorf("%s: %d custom actions exceeds the cap of 256", path, len(raw.CustomAction))
	}

	var errs []error
	for i := range raw.CustomAction {
		ra := &raw.CustomAction[i]
		action, verr := validateAction(ra, deps)
		if verr != nil {
			errs = append(errs, fmt.Errorf("custom_action[%d] %q: %w", i, ra.Name, verr))
			continue
		}
		// Cross-action: name uniqueness.
		if _, dup := cat.ByName[action.Name]; dup {
			errs = append(errs, fmt.Errorf("custom_action[%d] %q: duplicate name", i, action.Name))
			continue
		}
		// Cross-action: key uniqueness.
		if action.Key != "" {
			if other, dup := cat.ByKey[action.Key]; dup {
				errs = append(errs, fmt.Errorf("custom_action %q: key %q already bound to custom action %q", action.Name, action.Key, other.Name))
				continue
			}
			if deps.ReservedKeys != nil {
				if name, hit := deps.ReservedKeys[action.Key]; hit {
					errs = append(errs, fmt.Errorf("custom_action %q: key %q already bound to KeyMap.%s — set [bindings].%s to a different key, or rename the custom action's key", action.Name, action.Key, name, strings.ToLower(name)))
					continue
				}
			}
			cat.ByKey[action.Key] = action
		}
		cat.Actions = append(cat.Actions, *action)
		cat.ByName[action.Name] = &cat.Actions[len(cat.Actions)-1]
		// Refresh the ByKey pointer to point into the slice (the
		// `action` local was a separate allocation).
		if action.Key != "" {
			cat.ByKey[action.Key] = &cat.Actions[len(cat.Actions)-1]
		}
	}
	if len(errs) > 0 {
		return nil, &MultiError{Errs: errs}
	}
	return cat, nil
}

// validateAction runs the §3.7 per-action validation pipeline.
func validateAction(ra *rawAction, deps Deps) (*Action, error) {
	if ra.Name == "" {
		return nil, errors.New("name is required")
	}
	if !nameRegex.MatchString(ra.Name) {
		return nil, fmt.Errorf("name %q must match [a-z][a-z0-9_]{0,31}", ra.Name)
	}
	if ra.Description == "" {
		return nil, errors.New("description is required")
	}
	if len(ra.Description) > 80 {
		return nil, fmt.Errorf("description is %d chars; max 80", len(ra.Description))
	}
	if len(ra.Sequence) == 0 {
		return nil, errors.New("sequence is required (≥ 1 step)")
	}
	if len(ra.Sequence) > 32 {
		return nil, fmt.Errorf("sequence has %d steps; max 32", len(ra.Sequence))
	}

	action := &Action{
		Name:           ra.Name,
		Description:    ra.Description,
		AllowFolderTpl: ra.AllowFolderTemplate,
		AllowURLTpl:    ra.AllowURLTemplate,
	}

	// key
	if ra.Key != "" {
		if strings.ContainsAny(ra.Key, " \t<>") {
			return nil, fmt.Errorf("key %q: chord bindings deferred to a future spec — use a single-key binding or `:actions run`", ra.Key)
		}
		if !keyRegex.MatchString(ra.Key) {
			return nil, fmt.Errorf("key %q: must be a single rune, ctrl+<rune>, or alt+<rune>", ra.Key)
		}
		action.Key = ra.Key
	}

	// confirm + prompt_confirm legacy alias
	confirm := strings.TrimSpace(strings.ToLower(ra.Confirm))
	if ra.PromptConfirm != nil && *ra.PromptConfirm {
		if confirm == "" {
			confirm = "always"
		}
		if deps.Logger != nil {
			deps.Logger.Warn("custom_action: prompt_confirm is deprecated; use confirm = \"always\"", "action", ra.Name)
		}
	}
	switch confirm {
	case "", "auto":
		action.Confirm = ConfirmAuto
	case "always":
		action.Confirm = ConfirmAlways
	case "never":
		action.Confirm = ConfirmNever
	default:
		return nil, fmt.Errorf("confirm %q must be one of auto|always|never", ra.Confirm)
	}

	// when
	if len(ra.When) == 0 {
		action.When = []Scope{ScopeList, ScopeViewer}
	} else {
		seen := map[Scope]struct{}{}
		for _, s := range ra.When {
			lc := strings.ToLower(strings.TrimSpace(s))
			scope, ok := scopeNames[lc]
			if !ok {
				return nil, fmt.Errorf("when contains unknown scope %q (allowed: list, viewer, folders)", s)
			}
			if _, dup := seen[scope]; dup {
				continue
			}
			seen[scope] = struct{}{}
			action.When = append(action.When, scope)
		}
	}

	// Validate steps. Track destructive bit, requiresMessageContext
	// aggregate, and emit the §3.7 cross-step checks.
	hasDestructive := false
	hasFiltered := false
	for stepIdx := range ra.Sequence {
		rs := &ra.Sequence[stepIdx]
		step, kind, perMsg, requiresUser, destructive, filtered, err := validateStep(stepIdx, rs, action, deps)
		if err != nil {
			return nil, err
		}
		_ = kind
		if perMsg {
			action.RequiresMessageContext = true
		}
		_ = requiresUser
		if destructive {
			hasDestructive = true
		}
		if filtered {
			hasFiltered = true
		}
		action.Steps = append(action.Steps, step)
	}
	_ = hasFiltered

	// permanent_delete + confirm=never is forbidden.
	if hasDestructive && action.Confirm == ConfirmNever {
		return nil, errors.New("destructive op forbidden with confirm = \"never\"")
	}

	// Resolved StopOnError default: §2.4 — true for destructive
	// sequences, false otherwise. Per-step nilable override stays
	// on Step.StopOnError.
	if ra.StopOnError != nil {
		action.StopOnError = *ra.StopOnError
	} else {
		action.StopOnError = hasDestructive
	}

	return action, nil
}

// validateStep checks one step and returns its prepared form. The
// boolean returns are: perMessage (template references per-message
// data), requiresUserInput (template references {{.UserInput}}),
// destructive (permanent_delete*), filtered (move_filtered or
// permanent_delete_filtered).
func validateStep(idx int, rs *rawStep, action *Action, deps Deps) (Step, OpKind, bool, bool, bool, bool, error) {
	if rs.Op == "" {
		return Step{}, "", false, false, false, false, fmt.Errorf("step %d: op is required", idx)
	}
	if msg, deferred := deferredOps[rs.Op]; deferred {
		return Step{}, "", false, false, false, false, fmt.Errorf("step %d: op %q %s; see docs/user/how-to.md", idx, rs.Op, msg)
	}
	op := OpKind(rs.Op)
	spec, ok := ops[op]
	if !ok {
		return Step{}, "", false, false, false, false, fmt.Errorf("step %d: unknown op %q", idx, rs.Op)
	}
	step := Step{
		Op:          op,
		Params:      map[string]any{},
		StopOnError: rs.StopOnError,
	}
	// Project the raw fields into the param map. Each op's validate
	// closure decides which keys are required.
	if rs.Destination != "" {
		step.Params["destination"] = rs.Destination
	}
	if rs.Category != "" {
		step.Params["category"] = rs.Category
	}
	if rs.Pattern != "" {
		step.Params["pattern"] = rs.Pattern
	}
	if rs.Prompt != "" {
		step.Params["prompt"] = rs.Prompt
	}
	if rs.URL != "" {
		step.Params["url"] = rs.URL
	}
	if rs.Value != nil {
		step.Params["value"] = *rs.Value
	}
	if err := spec.Validate(step.Params); err != nil {
		return Step{}, "", false, false, false, false, fmt.Errorf("step %d: op %q: %w", idx, op, err)
	}

	// Templating + per-message-context computation.
	step.Templated = map[string]*template.Template{}
	perMsg := false
	requiresUser := false
	for key, raw := range step.Params {
		s, isStr := raw.(string)
		if !isStr {
			continue
		}
		// Roadmap-syntax rewrite + slog warning.
		rewritten, rewroteAny := rewriteRoadmapAliases(s)
		if rewroteAny && deps.Logger != nil {
			deps.Logger.Warn("custom_action: roadmap single-brace alias rewritten — switch to Go text/template syntax",
				"action", action.Name, "step", idx, "param", key)
		}
		if !strings.Contains(rewritten, "{{") {
			// Literal — still write back if the rewrite did anything.
			if rewroteAny {
				step.Params[key] = rewritten
			}
			// For static-enum params (set_sender_routing.destination),
			// the per-op validator above already enforced the no-template
			// rule; literals pass through.
			continue
		}
		t, err := template.New(action.Name + ":" + string(op) + ":" + key).Parse(rewritten)
		if err != nil {
			return Step{}, "", false, false, false, false, fmt.Errorf("step %d: op %q: param %q: %w", idx, op, key, err)
		}
		// Walk the parsed AST for variable references (§4.3 guards).
		refs := templateRefs(rewritten)
		for ref := range refs {
			if _, hit := perMessageVars[ref]; hit {
				perMsg = true
			}
			if ref == "UserInput" {
				requiresUser = true
			}
		}
		// §4.3 guards: folder + URL templates referencing per-message data
		// require the action-level opt-in flag.
		if perMsg && (op == OpMove || op == OpMoveFiltered) && key == "destination" && !action.AllowFolderTpl {
			return Step{}, "", false, false, false, false, fmt.Errorf("step %d: op %q: destination template references message data — set `allow_folder_template = true` to opt in", idx, op)
		}
		if perMsg && op == OpOpenURL && key == "url" && !action.AllowURLTpl {
			return Step{}, "", false, false, false, false, fmt.Errorf("step %d: op %q: URL template references message data — set `allow_url_template = true` to opt in (PII exfil risk T-CA3)", idx, op)
		}
		step.Templated[key] = t
		// Save rewritten back so non-template execution paths see the canonical form.
		step.Params[key] = rewritten
	}
	step.requiresMsg = perMsg
	step.requiresUser = requiresUser

	// Pattern compile — for literal patterns only. Templated patterns
	// compile at runtime after substitution.
	if patStr, ok := step.Params["pattern"].(string); ok && patStr != "" {
		if _, hasTpl := step.Templated["pattern"]; !hasTpl && deps.PatternCompile != nil {
			compiled, err := deps.PatternCompile(patStr, deps.PatternOpts)
			if err != nil {
				return Step{}, "", false, false, false, false, fmt.Errorf("step %d: op %q: pattern %q: %w", idx, op, patStr, err)
			}
			step.PatternC = compiled
		}
	}

	// Reserved-category literal rejection for per-message
	// add_category / remove_category.
	if op == OpAddCategory || op == OpRemoveCategory {
		if cat, ok := step.Params["category"].(string); ok {
			if _, hasTpl := step.Templated["category"]; !hasTpl {
				if _, reserved := reservedStackCategories[cat]; reserved {
					return Step{}, "", false, false, false, false, fmt.Errorf("step %d: op %q: category %q is thread-level (spec 25) — use `thread_add_category` / `thread_remove_category` instead", idx, op, cat)
				}
			}
		}
	}

	destructive := spec.Destructive
	filtered := op == OpMoveFiltered || op == OpPermanentDeleteFiltered

	return step, op, perMsg, requiresUser, destructive, filtered, nil
}

// rewriteRoadmapAliases walks s and rewrites every alias from the
// roadmap §4.2 table to the Go-template form. Returns the rewritten
// string + a bool indicating whether any rewrite happened.
func rewriteRoadmapAliases(s string) (string, bool) {
	rewrote := false
	for alias, replacement := range roadmapAliases {
		if strings.Contains(s, alias) {
			s = strings.ReplaceAll(s, alias, replacement)
			rewrote = true
		}
	}
	return s, rewrote
}

// templateRefs returns the set of dotted variable names referenced
// in tplSrc (e.g. {{.From}} → "From"). A simple regex scan; the
// text/template parser already validated syntax.
var refRegex = regexp.MustCompile(`\{\{\s*\.([A-Za-z][A-Za-z0-9_]*)`)

func templateRefs(tplSrc string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, match := range refRegex.FindAllStringSubmatch(tplSrc, -1) {
		out[match[1]] = struct{}{}
	}
	return out
}
