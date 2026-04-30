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
type ComposeSnapshot struct {
	Kind     ComposeKind `json:"kind"`
	SourceID string      `json:"source_id,omitempty"`
	To       string      `json:"to,omitempty"`
	Cc       string      `json:"cc,omitempty"`
	Subject  string      `json:"subject,omitempty"`
	Body     string      `json:"body,omitempty"`
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

	to      textinput.Model
	cc      textinput.Model
	subject textinput.Model
	body    textarea.Model

	focused ComposeFieldKind
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
	subj := src.Subject
	if !hasReplyPrefix(subj) {
		subj = "Re: " + subj
	} else {
		// Normalise to canonical "Re: " casing.
		subj = "Re: " + strings.TrimSpace(subj[3:])
	}
	m.SetSubject(subj)

	// Reuse the existing template to format the body block. It
	// emits a multi-line string that begins with header lines (To/
	// Cc/Subject + blank separator) followed by the quote chain.
	// We drop the header lines because the in-modal form renders
	// them separately.
	full := compose.ReplySkeleton(src, renderedBody)
	m.SetBody(stripSkeletonHeaders(full))
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
		Kind:     m.Kind,
		SourceID: m.SourceID,
		To:       m.To(),
		Cc:       m.Cc(),
		Subject:  m.Subject(),
		Body:     m.Body(),
	}
}

// Restore populates the form from a Snapshot. Used by the
// resume-on-startup flow that PR 7-ii will wire.
func (m *ComposeModel) Restore(s ComposeSnapshot) {
	m.Kind = s.Kind
	m.SourceID = s.SourceID
	m.SetTo(s.To)
	m.SetCc(s.Cc)
	m.SetSubject(s.Subject)
	m.SetBody(s.Body)
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

	footer := t.Dim.Render("Ctrl+S / Esc save  ·  Ctrl+D discard  ·  Tab cycle field")

	composed := strings.Join([]string{header, "", bodySection, "", footer}, "\n")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Top,
		t.Modal.Render(composed))
}
