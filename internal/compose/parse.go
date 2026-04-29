package compose

import (
	"errors"
	"os"
	"strings"
)

// ParsedDraft is what the user produced after editing the tempfile:
// the recipient lists, subject, and body.
type ParsedDraft struct {
	To      []string
	Cc      []string
	Bcc     []string
	Subject string
	Body    string
}

// ErrNoRecipients is returned by Parse when the To list is empty
// (the only header that's mandatory for a reply).
var ErrNoRecipients = errors.New("no recipients (To: line is empty)")

// ErrEmpty is returned when the file is empty (user cancelled by
// nuking the body in their editor).
var ErrEmpty = errors.New("draft is empty — discarded")

// Parse reads the tempfile and splits it into headers + body.
// Format: RFC-2822-style headers up to the first blank line, then
// the body. Headers we recognise: To, Cc, Bcc, Subject. Unknown
// headers are silently ignored — a future iter could surface them
// as a parse warning.
func Parse(path string) (*ParsedDraft, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, ErrEmpty
	}
	text := string(raw)
	// Find the first blank line.
	idx := strings.Index(text, "\n\n")
	if idx < 0 {
		// No blank line — treat the whole file as headers (no body).
		return parseHeaders(text)
	}
	headers, err := parseHeaders(text[:idx])
	if err != nil {
		return nil, err
	}
	headers.Body = strings.TrimLeft(text[idx+2:], "\n")
	if len(headers.To) == 0 {
		return headers, ErrNoRecipients
	}
	return headers, nil
}

func parseHeaders(block string) (*ParsedDraft, error) {
	d := &ParsedDraft{}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue // ignore garbage lines
		}
		key := strings.ToLower(strings.TrimSpace(line[:colon]))
		val := strings.TrimSpace(line[colon+1:])
		switch key {
		case "to":
			d.To = splitAddrs(val)
		case "cc":
			d.Cc = splitAddrs(val)
		case "bcc":
			d.Bcc = splitAddrs(val)
		case "subject":
			d.Subject = val
		}
	}
	return d, nil
}

// splitAddrs takes "alice@x, bob@x" and returns ["alice@x", "bob@x"].
// Empty entries are skipped so trailing commas / empty lines
// (`Cc:`) don't produce phantom recipients.
func splitAddrs(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
