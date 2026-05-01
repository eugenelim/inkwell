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

// ReplyAllSkeleton is ReplySkeleton with the full audience: To
// includes the source's From + remaining To addresses; Cc keeps
// the source's Cc. The user's own address is filtered out so they
// don't see themselves in their own draft. This skeleton mirrors
// what Outlook's Reply All button generates client-side; Graph's
// `/createReplyAll` endpoint (used by the action executor) does
// the same dedup server-side, so this string is the user-visible
// preview rather than the source of truth — but a discrepancy
// between the textarea body header block and the Graph-generated
// To/Cc would surprise the user, so we keep them aligned.
//
// userUPN is the user's own address; pass empty for "no dedup
// available" (e.g., not signed in). The slice argument lists are
// store.EmailAddress so we get both name and address.
func ReplyAllSkeleton(src store.Message, renderedBody, userUPN string) string {
	from := src.FromName
	if from == "" {
		from = src.FromAddress
	}
	subject := src.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	to := dedupAddresses(append([]store.EmailAddress{
		{Name: src.FromName, Address: src.FromAddress},
	}, src.ToAddresses...), userUPN)
	cc := dedupAddresses(src.CcAddresses, userUPN)

	var b strings.Builder
	fmt.Fprintf(&b, "To: %s\n", joinAddrs(to))
	fmt.Fprintf(&b, "Cc: %s\n", joinAddrs(cc))
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

// ForwardSkeleton produces a forward draft for src. Subject is
// prefixed with "Fwd:" (deduped: source already-prefixed stays at
// one Fwd:); To/Cc are empty for the user to fill. Body opens
// with a "Forwarded message" header block carrying the source's
// From / Date / Subject / To, followed by the source body
// line-prefixed (NOT quote-prefixed — forwards traditionally show
// the original verbatim, not as a quoted reply).
func ForwardSkeleton(src store.Message, renderedBody string) string {
	from := src.FromName
	if from == "" {
		from = src.FromAddress
	}
	// Normalise to canonical "Fwd:" — Outlook ships "Fw:", Apple
	// Mail / Gmail / Thunderbird ship "Fwd:". Without normalising,
	// a thread bouncing between MUAs would mix the two prefixes
	// and break thread grouping; canonical form keeps it clean.
	subject := src.Subject
	low := strings.ToLower(strings.TrimSpace(subject))
	switch {
	case strings.HasPrefix(low, "fwd:"):
		subject = "Fwd: " + strings.TrimSpace(subject[4:])
	case strings.HasPrefix(low, "fw:"):
		subject = "Fwd: " + strings.TrimSpace(subject[3:])
	default:
		subject = "Fwd: " + subject
	}

	var b strings.Builder
	b.WriteString("To:\n")
	b.WriteString("Cc:\n")
	fmt.Fprintf(&b, "Subject: %s\n", subject)
	b.WriteString("\n\n")
	b.WriteString("---------- Forwarded message ----------\n")
	fmt.Fprintf(&b, "From:    %s <%s>\n", from, src.FromAddress)
	fmt.Fprintf(&b, "Date:    %s\n", src.SentAt.Format("Mon 2006-01-02 15:04"))
	fmt.Fprintf(&b, "Subject: %s\n", src.Subject)
	if len(src.ToAddresses) > 0 {
		fmt.Fprintf(&b, "To:      %s\n", joinAddrs(src.ToAddresses))
	}
	b.WriteString("\n")
	if renderedBody != "" {
		b.WriteString(strings.TrimRight(renderedBody, "\n"))
		b.WriteByte('\n')
	}
	return b.String()
}

// NewSkeleton produces a blank-canvas draft (no source). The
// in-modal compose pane drops the user into the To field for a
// new message rather than the body since recipients are usually
// the first thing they type.
func NewSkeleton() string {
	var b strings.Builder
	b.WriteString("To:\n")
	b.WriteString("Cc:\n")
	b.WriteString("Subject:\n")
	b.WriteString("\n\n")
	return b.String()
}

// dedupAddresses removes any entries whose address matches userUPN
// (case-insensitive) and any duplicates within the slice. Keeps
// the first occurrence of each. userUPN == "" disables the self-
// filter.
func dedupAddresses(in []store.EmailAddress, userUPN string) []store.EmailAddress {
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

// joinAddrs renders a recipient list as comma-separated bare
// addresses. We drop the display-name decoration (`Bob <b@x>` →
// `b@x`) because the in-modal form's textinput components are
// single-line and Outlook re-resolves names from the address book
// on send.
func joinAddrs(rs []store.EmailAddress) string {
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
