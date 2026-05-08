package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/eugenelim/inkwell/internal/store"
)

// maxRecipientLines is the truncation threshold for To/Cc.
const maxRecipientLines = 3

// Headers renders the default header set per spec §4. Pass
// [BodyOpts.ShowFullHeaders] to expand into the full header view.
func (r *renderer) Headers(m *store.Message, opts BodyOpts) string {
	if m == nil {
		return ""
	}
	t := opts.Theme
	if t.HeaderLabel.GetForeground() == nil && !t.HeaderLabel.GetBold() {
		t = DefaultTheme()
	}
	fromValue := formatAddress(m.FromAddress, m.FromName)
	to := formatRecipientList(m.ToAddresses)
	cc := formatRecipientList(m.CcAddresses)
	dateStr := formatDate(m.ReceivedAt)
	subjectStr := m.Subject
	if subjectStr == "" {
		subjectStr = "(no subject)"
	}

	var b strings.Builder
	writeHeader(&b, t, "From", fromValue)
	writeHeader(&b, t, "To", to)
	if cc != "" {
		writeHeader(&b, t, "Cc", cc)
	}
	writeHeader(&b, t, "Date", dateStr)
	if stacks := stacksLine(m.Categories); stacks != "" {
		writeHeader(&b, t, "Stacks", stacks)
	}
	writeSubject(&b, t, subjectStr)

	if opts.ShowFullHeaders {
		if m.Importance != "" {
			writeHeader(&b, t, "Importance", m.Importance)
		}
		if len(m.Categories) > 0 {
			writeHeader(&b, t, "Categories", strings.Join(m.Categories, ", "))
		}
		if m.FlagStatus != "" && m.FlagStatus != "notFlagged" {
			writeHeader(&b, t, "Flag", m.FlagStatus)
		}
		if m.HasAttachments {
			writeHeader(&b, t, "Has Attachments", "yes")
		}
		if m.InternetMessageID != "" {
			writeHeader(&b, t, "Message-ID", m.InternetMessageID)
		}
	}
	return b.String()
}

func writeHeader(b *strings.Builder, t Theme, label, value string) {
	b.WriteString(t.HeaderLabel.Render(label + ":"))
	b.WriteByte(' ')
	b.WriteString(t.HeaderValue.Render(value))
	b.WriteByte('\n')
}

// stacksLine builds the spec 25 §5.5 "Stacks:" header value: one
// glyph + label per inkwell stack the message is in. Empty result
// emits no line (the caller skips the writeHeader).
func stacksLine(cats []string) string {
	var parts []string
	if store.IsInCategory(cats, store.CategoryReplyLater) {
		parts = append(parts, "↩ Reply Later")
	}
	if store.IsInCategory(cats, store.CategorySetAside) {
		parts = append(parts, "📌 Set Aside")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

func writeSubject(b *strings.Builder, t Theme, subject string) {
	b.WriteString(t.HeaderLabel.Render("Subject:"))
	b.WriteByte(' ')
	b.WriteString(t.Subject.Render(subject))
	b.WriteByte('\n')
}

// formatAddress renders "Name <addr>" or just "addr" when name is empty.
func formatAddress(address, name string) string {
	if address == "" {
		return name
	}
	if name == "" || name == address {
		return address
	}
	return fmt.Sprintf("%s <%s>", name, address)
}

// formatRecipientList renders the first N recipients with a "(M more)"
// suffix when truncated.
func formatRecipientList(addrs []store.EmailAddress) string {
	if len(addrs) == 0 {
		return ""
	}
	parts := make([]string, 0, maxRecipientLines)
	for i, a := range addrs {
		if i >= maxRecipientLines {
			break
		}
		parts = append(parts, formatAddress(a.Address, a.Name))
	}
	out := strings.Join(parts, ", ")
	if extra := len(addrs) - maxRecipientLines; extra > 0 {
		out += fmt.Sprintf(" (%d more)", extra)
	}
	return out
}

// formatDate is RFC1123 + relative hint when within 24h.
func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	abs := t.Local().Format("Mon 2006-01-02 15:04 MST")
	if d := time.Since(t); d > 0 && d < 24*time.Hour {
		switch {
		case d < time.Minute:
			return fmt.Sprintf("%s (just now)", abs)
		case d < time.Hour:
			return fmt.Sprintf("%s (%dm ago)", abs, int(d.Minutes()))
		default:
			return fmt.Sprintf("%s (%dh ago)", abs, int(d.Hours()))
		}
	}
	return abs
}
