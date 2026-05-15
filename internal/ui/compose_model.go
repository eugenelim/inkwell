package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eugenelim/inkwell/internal/compose"
	"github.com/eugenelim/inkwell/internal/store"
)

// ComposeKind enumerates compose flavors. v0.13.x ships
// ComposeKindReply only; the rest land with PR 7-iii alongside
// the corresponding action types in `internal/action`.
type ComposeKind int

const (
	ComposeKindReply ComposeKind = iota
	ComposeKindReplyAll
	ComposeKindForward
	ComposeKindNew
)

// ComposeFieldKind identifies which field of the compose form has
// focus. Tab cycles forward (Body → To → Cc → Subject → Body);
// Shift+Tab cycles backward.
type ComposeFieldKind int

const (
	ComposeFieldBody ComposeFieldKind = iota
	ComposeFieldTo
	ComposeFieldCc
	ComposeFieldSubject
)

// ComposeSnapshot is the JSON-serialisable view of a compose
// session's state. Used by the spec 15 v2 §7 crash-recovery path
// (PR 7-ii) to persist into the `compose_sessions` table and
// restore on launch. The shape is deliberately flat for easy TOML/
// JSON round-tripping.
//
// Attachments holds staged local files (spec 15 §5 / F-1). The UI
// attachment picker is a follow-up; this field is wired through the
// save → executor → graph pipeline so it works when the picker lands.
type ComposeSnapshot struct {
	Kind        ComposeKind             `json:"kind"`
	SourceID    string                  `json:"source_id,omitempty"`
	To          string                  `json:"to,omitempty"`
	Cc          string                  `json:"cc,omitempty"`
	Subject     string                  `json:"subject,omitempty"`
	Body        string                  `json:"body,omitempty"`
	Attachments []AttachmentSnapshotRef `json:"attachments,omitempty"`
	// MarkdownMode mirrors the live ComposeModel.MarkdownMode at
	// snapshot time (spec 33 §6.5). Persisted in compose_sessions
	// so a resumed session converts on the same format the user
	// originally selected. Body field is raw user text (Markdown
	// source or plain) per the §6.5 invariant — opaque to any
	// consumer that isn't the textarea.
	MarkdownMode bool `json:"markdown_mode,omitempty"`
}

// AttachmentSnapshotRef is the UI-layer mirror of action.AttachmentRef.
// Defined here so compose_model.go doesn't import internal/action
// (`docs/CONVENTIONS.md` §2 layering).
type AttachmentSnapshotRef struct {
	LocalPath string `json:"local_path"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
}

// ComposeModel is the in-modal compose pane (spec 15 v2 §6). The
// design pivot from the original spec: instead of writing a
// tempfile and dispatching $EDITOR, the compose UI lives in a
// Bubble Tea overlay so save / discard live in a persistent
// footer — solving the user-reported "select Exit command first"
// friction. $EDITOR drop-out for the body is a follow-up PR.
//
// Each header is its own bubbles/textinput; body is a
// bubbles/textarea. Focus tracking is at the ComposeModel level;
// header fields are blurred when not focused so only the focused
// component's Update receives the keystroke.
type ComposeModel struct {
	Kind     ComposeKind
	SourceID string
	// SessionID identifies this compose session in the
	// `compose_sessions` table (spec 15 §7 / PR 7-ii crash
	// recovery). Set by startCompose on entry; persists across
	// the lifetime of the modal; consumed by saveComposeCmd /
	// discardComposeCmd to mark confirmed_at so the resume scan
	// ignores the row on next launch. Empty before crash-recovery
	// shipped (spec-15 v1) — code paths must guard.
	SessionID string

	// MarkdownMode mirrors [compose] body_format = "markdown" (spec
	// 33 §6.7). Set once at NewCompose() entry from config; never
	// re-read from config during the session. Drives the footer
	// indicator and the .md tempfile extension for Ctrl+E. The
	// saveComposeCmd path reads snap.MarkdownMode (copied from this
	// field at Snapshot() time) to decide whether to convert via
	// goldmark.
	MarkdownMode bool

	to      textinput.Model
	cc      textinput.Model
	subject textinput.Model
	body    textarea.Model

	focused     ComposeFieldKind
	attachments []AttachmentSnapshotRef
}

// NewCompose builds an empty compose model with focus on the body
// field (the user's primary editing target — headers are
// pre-filled by ApplyReplySkeleton). The reply-with-quote flow
// lands the user typing immediately at the cursor.
func NewCompose() ComposeModel {
	to := textinput.New()
	to.Prompt = ""
	cc := textinput.New()
	cc.Prompt = ""
	subj := textinput.New()
	subj.Prompt = ""
	body := textarea.New()
	body.Prompt = ""
	body.ShowLineNumbers = false
	body.Focus()
	return ComposeModel{
		Kind:    ComposeKindReply,
		to:      to,
		cc:      cc,
		subject: subj,
		body:    body,
		focused: ComposeFieldBody,
	}
}

// newComposeWithFormat builds a ComposeModel and sets MarkdownMode
// based on the [compose] body_format config value. "markdown"
// enables MarkdownMode; anything else (including the safe empty
// default) leaves it false. The two NewCompose() entry points that
// feed a rendered view — New() at app construction and
// startComposeOfKind on r/R/f/m — call this helper instead of
// NewCompose() directly. The discard / reset call sites at
// updateCompose's Ctrl+S and Ctrl+D paths use NewCompose() because
// the model is immediately overwritten and never rendered.
func newComposeWithFormat(bodyFormat string) ComposeModel {
	m := NewCompose()
	m.MarkdownMode = bodyFormat == "markdown"
	return m
}

// To / Cc / Subject / Body are read-only accessors for the field
// values. Used by the save Cmd to extract the form state for
// CreateDraftReply dispatch and by tests for assertions.
func (m ComposeModel) To() string      { return m.to.Value() }
func (m ComposeModel) Cc() string      { return m.cc.Value() }
func (m ComposeModel) Subject() string { return m.subject.Value() }
func (m ComposeModel) Body() string    { return m.body.Value() }
func (m ComposeModel) Focused() ComposeFieldKind {
	return m.focused
}

// Attachments returns the staged local-file attachments for this session.
func (m ComposeModel) Attachments() []AttachmentSnapshotRef {
	return append([]AttachmentSnapshotRef(nil), m.attachments...)
}

// AddAttachment appends ref to the staged attachment list.
func (m *ComposeModel) AddAttachment(ref AttachmentSnapshotRef) {
	m.attachments = append(m.attachments, ref)
}

// SetTo / SetCc / SetSubject / SetBody set field values. Used by
// the skeleton population path and by tests.
func (m *ComposeModel) SetTo(v string)      { m.to.SetValue(v) }
func (m *ComposeModel) SetCc(v string)      { m.cc.SetValue(v) }
func (m *ComposeModel) SetSubject(v string) { m.subject.SetValue(v) }
func (m *ComposeModel) SetBody(v string)    { m.body.SetValue(v) }

// ApplyReplySkeleton populates the form for a reply to src. Uses
// the existing `internal/compose/template.go::ReplySkeleton` to
// generate the body's quote chain, then strips the leading
// header block (To/Cc/Subject lines + blank separator) since the
// in-modal flow renders headers as separate fields.
//
// The "Re:" prefix dedup matches Outlook semantics: a source
// already prefixed with "Re:" / "RE:" / "re:" stays as a single
// "Re:". Without dedup, threaded conversations would accumulate
// "Re: Re: Re: ...".
func (m *ComposeModel) ApplyReplySkeleton(src store.Message, renderedBody string) {
	m.Kind = ComposeKindReply
	m.SourceID = src.ID
	m.SetTo(src.FromAddress)
	m.SetCc("")
	m.SetSubject(replyPrefix(src.Subject))
	full := compose.ReplySkeleton(src, renderedBody)
	m.SetBody(stripSkeletonHeaders(full))
}

// ApplyReplyAllSkeleton populates the form for a reply-all. Like
// ApplyReplySkeleton but pre-fills To with src.From + remaining
// To recipients (excluding userUPN to avoid the user emailing
// themselves) and Cc with src.Cc (also userUPN-filtered). Spec 15
// §6.2 / PR 7-iii.
func (m *ComposeModel) ApplyReplyAllSkeleton(src store.Message, renderedBody, userUPN string) {
	m.Kind = ComposeKindReplyAll
	m.SourceID = src.ID

	to := dedupReplyAddresses(append([]store.EmailAddress{
		{Address: src.FromAddress},
	}, src.ToAddresses...), userUPN)
	cc := dedupReplyAddresses(src.CcAddresses, userUPN)
	m.SetTo(joinReplyAddrs(to))
	m.SetCc(joinReplyAddrs(cc))
	m.SetSubject(replyPrefix(src.Subject))

	full := compose.ReplyAllSkeleton(src, renderedBody, userUPN)
	m.SetBody(stripSkeletonHeaders(full))
}

// ApplyForwardSkeleton populates the form for a forward. To/Cc
// start empty (the user fills the recipients); Subject is prefixed
// "Fwd:"; body opens with the canonical "Forwarded message" header
// block. Spec 15 §6.2 / PR 7-iii.
func (m *ComposeModel) ApplyForwardSkeleton(src store.Message, renderedBody string) {
	m.Kind = ComposeKindForward
	m.SourceID = src.ID
	m.SetTo("")
	m.SetCc("")
	m.SetSubject(forwardPrefix(src.Subject))

	full := compose.ForwardSkeleton(src, renderedBody)
	m.SetBody(stripSkeletonHeaders(full))
}

// ApplyNewSkeleton blanks the form for a brand-new message. Focus
// shifts to the To field rather than Body because the user's first
// task is recipient entry (no source-sender to pre-fill from).
// Spec 15 §6 / PR 7-iii.
func (m *ComposeModel) ApplyNewSkeleton() {
	m.Kind = ComposeKindNew
	m.SourceID = ""
	m.SetTo("")
	m.SetCc("")
	m.SetSubject("")
	m.SetBody("")
	m.setFocus(ComposeFieldTo)
}

// humanComposeBytes formats a byte count for the staged-attachment list.
func humanComposeBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
}

// replyPrefix returns the subject decorated with "Re: ". A subject
// already prefixed (any casing) is normalised to canonical "Re: "
// without stacking.
func replyPrefix(subj string) string {
	if !hasReplyPrefix(subj) {
		return "Re: " + subj
	}
	return "Re: " + strings.TrimSpace(subj[3:])
}

// forwardPrefix returns the subject decorated with "Fwd: ".
// Recognises both "Fwd:" and "Fw:" prefixes (Outlook uses Fw:,
// Gmail/Apple use Fwd:); normalises to "Fwd:".
func forwardPrefix(subj string) string {
	low := strings.ToLower(strings.TrimSpace(subj))
	switch {
	case strings.HasPrefix(low, "fwd:"):
		return "Fwd: " + strings.TrimSpace(subj[4:])
	case strings.HasPrefix(low, "fw:"):
		return "Fwd: " + strings.TrimSpace(subj[3:])
	}
	return "Fwd: " + subj
}

// dedupReplyAddresses returns the slice with the user's own UPN
// removed (case-insensitive) and duplicates collapsed. Empty UPN
// disables self-filter. Defined here (vs delegating to the
// internal/compose helper) so the UI doesn't need to import
// internal/compose unexported helpers.
func dedupReplyAddresses(in []store.EmailAddress, userUPN string) []store.EmailAddress {
	if len(in) == 0 {
		return nil
	}
	self := strings.ToLower(strings.TrimSpace(userUPN))
	seen := make(map[string]bool, len(in))
	out := make([]store.EmailAddress, 0, len(in))
	for _, a := range in {
		key := strings.ToLower(strings.TrimSpace(a.Address))
		if key == "" {
			continue
		}
		if self != "" && key == self {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, a)
	}
	return out
}

// joinReplyAddrs renders the dedup'd address list as a single
// comma-separated string for a textinput field. Display names
// are dropped; Outlook re-resolves on send.
func joinReplyAddrs(rs []store.EmailAddress) string {
	if len(rs) == 0 {
		return ""
	}
	out := make([]string, 0, len(rs))
	for _, a := range rs {
		if a.Address != "" {
			out = append(out, a.Address)
		}
	}
	return strings.Join(out, ", ")
}

// hasReplyPrefix recognises the canonical reply prefixes case-
// insensitively. We don't try to catch every locale's variant
// (Sv:, Aw:, etc.) — Outlook's UI normalises those to "Re:"
// server-side anyway.
func hasReplyPrefix(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "re:")
}

// stripSkeletonHeaders drops the leading "To:..\nCc:..\nSubject:..\n
// \n" block from the legacy ReplySkeleton output, leaving just the
// body section. The legacy template was shaped for the tempfile
// flow where headers + body shared one editable file; the in-
// modal flow handles headers separately. Keeping the legacy
// template intact (instead of forking it) means $EDITOR drop-out
// in a follow-up PR can reuse the same skeleton without
// duplicating the quote-chain formatter.
func stripSkeletonHeaders(skeleton string) string {
	idx := strings.Index(skeleton, "\n\n")
	if idx < 0 {
		return skeleton
	}
	return strings.TrimLeft(skeleton[idx+2:], "\n")
}

// NextField rotates focus forward (Body → To → Cc → Subject →
// Body). Each transition blurs the previous field and focuses the
// new one so only the focused bubbles component receives Update.
func (m *ComposeModel) NextField() {
	m.setFocus((m.focused + 1) % 4)
}

// PrevField rotates focus backward.
func (m *ComposeModel) PrevField() {
	m.setFocus((m.focused + 3) % 4)
}

// setFocus blurs the current field and focuses the new one. The
// focused field's Update path runs on each keystroke; blurred
// fields ignore input.
func (m *ComposeModel) setFocus(f ComposeFieldKind) {
	// Blur all.
	m.to.Blur()
	m.cc.Blur()
	m.subject.Blur()
	m.body.Blur()
	m.focused = f
	switch f {
	case ComposeFieldTo:
		m.to.Focus()
	case ComposeFieldCc:
		m.cc.Focus()
	case ComposeFieldSubject:
		m.subject.Focus()
	case ComposeFieldBody:
		m.body.Focus()
	}
}

// UpdateField forwards a tea.Msg to the currently-focused field
// component. Returns the updated ComposeModel + any Cmd the
// component emitted (textarea uses cursor-blink Cmds; we don't
// drop them).
func (m ComposeModel) UpdateField(msg tea.Msg) (ComposeModel, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focused {
	case ComposeFieldTo:
		m.to, cmd = m.to.Update(msg)
	case ComposeFieldCc:
		m.cc, cmd = m.cc.Update(msg)
	case ComposeFieldSubject:
		m.subject, cmd = m.subject.Update(msg)
	case ComposeFieldBody:
		m.body, cmd = m.body.Update(msg)
	}
	return m, cmd
}

// Snapshot captures the current form state in a JSON-friendly
// shape. PR 7-ii's compose_sessions persistence consumes this.
func (m ComposeModel) Snapshot() ComposeSnapshot {
	return ComposeSnapshot{
		Kind:         m.Kind,
		SourceID:     m.SourceID,
		To:           m.To(),
		Cc:           m.Cc(),
		Subject:      m.Subject(),
		Body:         m.Body(),
		Attachments:  m.Attachments(),
		MarkdownMode: m.MarkdownMode,
	}
}

// Restore populates the form from a Snapshot. Used by the
// resume-on-startup flow that PR 7-ii wired.
func (m *ComposeModel) Restore(s ComposeSnapshot) {
	m.Kind = s.Kind
	m.SourceID = s.SourceID
	m.SetTo(s.To)
	m.SetCc(s.Cc)
	m.SetSubject(s.Subject)
	m.SetBody(s.Body)
	m.attachments = s.Attachments
	m.MarkdownMode = s.MarkdownMode
}

// View renders the compose pane: headers stacked at the top, body
// in the middle, persistent footer at the bottom advertising
// Ctrl+S / Ctrl+D / Tab. The footer is the structural fix for
// the user-reported "save / discard at the bottom" friction.
func (m ComposeModel) View(t Theme, width, height int) string {
	const headerLines = 4 // To, Cc, Subject, blank
	const footerLines = 2

	// Set widths for the bubbles components so they fit the modal.
	contentWidth := width - 4
	if contentWidth < 20 {
		contentWidth = 20
	}
	bodyHeight := height - headerLines - footerLines - 2
	if bodyHeight < 3 {
		bodyHeight = 3
	}
	to := m.to
	cc := m.cc
	subj := m.subject
	body := m.body
	to.Width = contentWidth - 12
	cc.Width = contentWidth - 12
	subj.Width = contentWidth - 12
	body.SetWidth(contentWidth)
	body.SetHeight(bodyHeight)

	headerRow := func(label string, kind ComposeFieldKind, view string) string {
		marker := "  "
		labelStyle := t.Help
		if m.focused == kind {
			marker = "▶ "
			labelStyle = t.HelpKey
		}
		return marker + labelStyle.Render(fmt.Sprintf("%-9s", label+":")) + " " + view
	}
	header := strings.Join([]string{
		headerRow("To", ComposeFieldTo, to.View()),
		headerRow("Cc", ComposeFieldCc, cc.View()),
		headerRow("Subject", ComposeFieldSubject, subj.View()),
	}, "\n")

	// Body label + view. Marker on the body label when focused so
	// the user always sees which field they're editing.
	bodyLabel := "  "
	bodyLabelStyle := t.Help
	if m.focused == ComposeFieldBody {
		bodyLabel = "▶ "
		bodyLabelStyle = t.HelpKey
	}
	bodySection := bodyLabel + bodyLabelStyle.Render("Body:") + "\n" + body.View()

	var attSection string
	if len(m.attachments) > 0 {
		lines := make([]string, 0, len(m.attachments))
		for _, a := range m.attachments {
			lines = append(lines, fmt.Sprintf("  [a] %s · %s", a.Name, humanComposeBytes(a.SizeBytes)))
		}
		attSection = strings.Join(lines, "\n")
	}

	footerText := "Ctrl+S / Esc save  ·  Ctrl+D discard  ·  Tab cycle field  ·  Ctrl+E editor  ·  Ctrl+A attach"
	if m.MarkdownMode {
		footerText += "  ·  [md]"
	}
	footer := t.Dim.Render(footerText)

	parts := []string{header, "", bodySection}
	if attSection != "" {
		parts = append(parts, "", attSection)
	}
	parts = append(parts, "", footer)
	composed := strings.Join(parts, "\n")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top,
		t.Modal.Render(composed))
}
