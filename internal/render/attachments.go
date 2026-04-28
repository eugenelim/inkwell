package render

import (
	"fmt"
	"strings"

	"github.com/eu-gene-lim/inkwell/internal/store"
)

// Attachments renders the metadata-only attachment list. Bytes are not
// fetched until the user invokes :save or :open.
func (r *renderer) Attachments(atts []store.Attachment, theme Theme) string {
	if len(atts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(theme.HeaderLabel.Render("Attachments:"))
	b.WriteByte('\n')
	for _, a := range atts {
		mark := " "
		if a.IsInline {
			mark = "📎"
		}
		size := humanBytes(a.Size)
		line := fmt.Sprintf("  %s %s · %s · %s", mark, a.Name, a.ContentType, size)
		b.WriteString(theme.Attachment.Render(line))
		b.WriteByte('\n')
	}
	return b.String()
}

// humanBytes formats a byte size into a short human-friendly string.
func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
}
