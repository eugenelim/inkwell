package settings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/graph"
)

type stubFetcher struct {
	settings *graph.MailboxSettings
	err      error
}

func (s *stubFetcher) GetMailboxSettings(_ context.Context) (*graph.MailboxSettings, error) {
	return s.settings, s.err
}

func TestResolvedTimeZoneConfigOverrides(t *testing.T) {
	f := &stubFetcher{settings: &graph.MailboxSettings{TimeZone: "America/New_York"}}
	m := New(f, "Europe/London")
	m.cached = f.settings

	loc := m.ResolvedTimeZone()
	require.Equal(t, "Europe/London", loc.String())
}

func TestResolvedTimeZoneMailboxFallback(t *testing.T) {
	f := &stubFetcher{settings: &graph.MailboxSettings{TimeZone: "America/New_York"}}
	m := New(f, "")
	m.cached = f.settings

	loc := m.ResolvedTimeZone()
	require.Equal(t, "America/New_York", loc.String())
}

func TestResolvedTimeZoneSystemFallback(t *testing.T) {
	m := New(&stubFetcher{}, "")

	loc := m.ResolvedTimeZone()
	require.Equal(t, time.Local, loc)
}

func TestResolvedTimeZoneUnparseable(t *testing.T) {
	f := &stubFetcher{settings: &graph.MailboxSettings{TimeZone: "Not/A/Valid/TZ"}}
	m := New(f, "")
	m.cached = f.settings

	loc := m.ResolvedTimeZone()
	require.Equal(t, time.Local, loc)
}

func TestRefreshUpdatesCache(t *testing.T) {
	want := &graph.MailboxSettings{TimeZone: "Asia/Tokyo", Language: "ja-JP"}
	f := &stubFetcher{settings: want}
	m := New(f, "")

	require.NoError(t, m.Refresh(context.Background()))
	got := m.GetCached()
	require.NotNil(t, got)
	require.Equal(t, "Asia/Tokyo", got.TimeZone)
	require.Equal(t, "ja-JP", got.Language)
}

func TestRefreshErrorLeavesOldCache(t *testing.T) {
	old := &graph.MailboxSettings{TimeZone: "UTC"}
	f := &stubFetcher{settings: old}
	m := New(f, "")
	m.cached = old

	f.settings = nil
	f.err = errors.New("network down")
	err := m.Refresh(context.Background())
	require.Error(t, err)

	got := m.GetCached()
	require.NotNil(t, got, "cached value should survive a fetch error")
	require.Equal(t, "UTC", got.TimeZone)
}
