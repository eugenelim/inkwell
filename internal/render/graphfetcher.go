package render

import (
	"context"

	"github.com/eugenelim/inkwell/internal/graph"
)

// graphBodyFetcher is the production [BodyFetcher] adapter that calls
// graph.Client.GetMessageBody. The render package is the consumer side
// of the dependency (CLAUDE.md §2: render → graph is allowed; the
// reverse would be a violation).
type graphBodyFetcher struct {
	gc *graph.Client
}

// NewGraphBodyFetcher returns a [BodyFetcher] backed by gc.
func NewGraphBodyFetcher(gc *graph.Client) BodyFetcher {
	return &graphBodyFetcher{gc: gc}
}

// FetchBody implements [BodyFetcher].
func (f *graphBodyFetcher) FetchBody(ctx context.Context, messageID string) (FetchedBody, error) {
	m, err := f.gc.GetMessageBody(ctx, messageID)
	if err != nil {
		return FetchedBody{}, err
	}
	atts := make([]FetchedAttachment, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		atts = append(atts, FetchedAttachment{
			ID:          a.ID,
			Name:        a.Name,
			ContentType: a.ContentType,
			Size:        a.Size,
			IsInline:    a.IsInline,
			ContentID:   a.ContentID,
		})
	}
	if m.Body == nil {
		return FetchedBody{ContentType: "text", Content: "", Attachments: atts}, nil
	}
	return FetchedBody{
		ContentType: m.Body.ContentType,
		Content:     m.Body.Content,
		Attachments: atts,
	}, nil
}
