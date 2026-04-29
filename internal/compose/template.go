// Package compose implements the editor-driven draft authoring flow
// for spec 15 (reply / reply-all / forward / new). v0.11.0 ships the
// reply path; reply-all + forward + new lift later.
//
// inkwell never sends mail. The output of this package is always a
// draft on the user's Drafts folder; the user finalises send in
// Outlook (PRD §3.2 — Mail.Send is denied).
package compose

import (
	"fmt"
	"strings"

	"github.com/eugenelim/inkwell/internal/store"
)

// Kind enumerates the supported compose flavours. v0.11.0 only
// implements KindReply; the others land in follow-up iterations of
// spec 15.
type Kind int

const (
	KindReply Kind = iota
	KindReplyAll
	KindForward
	KindNew
)

// ReplySkeleton returns the pre-populated tempfile body for a reply
// to src. Headers (To / Cc / Subject) come first, separated from the
// body by a blank line. The body block contains a cursor placeholder
// and the quoted source body line-prefixed with "> ".
//
// Example output:
//
//	To: bob@vendor.invalid
//	Cc:
//	Subject: Re: Q4 forecast
//
//	<cursor here>
//
//	On Mon 2026-04-29 14:32, Bob <bob@vendor.invalid> wrote:
//	> Hey team, …
func ReplySkeleton(src store.Message, renderedBody string) string {
	from := src.FromName
	if from == "" {
		from = src.FromAddress
	}
	subject := src.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	var b strings.Builder
	fmt.Fprintf(&b, "To: %s\n", src.FromAddress)
	fmt.Fprintf(&b, "Cc:\n")
	fmt.Fprintf(&b, "Subject: %s\n", subject)
	b.WriteString("\n\n")
	if renderedBody != "" {
		fmt.Fprintf(&b, "On %s, %s <%s> wrote:\n",
			src.SentAt.Format("Mon 2006-01-02 15:04"), from, src.FromAddress)
		b.WriteString(quote(renderedBody))
		b.WriteString("\n")
	}
	return b.String()
}

// quote line-prefixes each body line with "> ". Blank lines stay
// blank-prefixed so quote indentation visually persists.
func quote(body string) string {
	if body == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	var b strings.Builder
	for _, ln := range lines {
		b.WriteString("> ")
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return b.String()
}
