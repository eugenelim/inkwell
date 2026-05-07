package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/eugenelim/inkwell/internal/store"
)

// TestRouteCLIRejectsUnknownDestination is a unit test on the
// destination validator. Spec 23 §7 — bad input exits 2.
func TestRouteCLIRejectsUnknownDestination(t *testing.T) {
	require.False(t, isValidDestination("primary"))
	require.False(t, isValidDestination(""))
	require.True(t, isValidDestination("imbox"))
	require.True(t, isValidDestination("feed"))
	require.True(t, isValidDestination("paper_trail"))
	require.True(t, isValidDestination("screener"))
}

// TestRouteCLIRejectsDisplayNameAddress confirms the bare-address
// validator rejects display-name forms.
func TestRouteCLIRejectsDisplayNameAddress(t *testing.T) {
	cases := []struct {
		in    string
		valid bool
	}{
		{"bob@acme.invalid", true},
		{"  bob@acme.invalid  ", true},
		{`"Bob" <bob@acme.invalid>`, false},
		{"<bob@acme.invalid>", false},
		{"", false},
		{"   ", false},
		{"not-an-email", false},
	}
	for _, c := range cases {
		err := validateBareAddress(c.in)
		if c.valid {
			require.NoError(t, err, "in=%q", c.in)
		} else {
			require.Error(t, err, "in=%q", c.in)
		}
	}
}

// TestUsageErrorWrapping confirms a usageError still satisfies
// errors.As so main can return exit code 2.
func TestUsageErrorWrapping(t *testing.T) {
	err := usageErr(errors.New("bad"))
	var ue *usageError
	require.True(t, errors.As(err, &ue))
}

// TestRouteCLIAssignAndShow drives the assign + show round-trip
// through the store directly (the CLI layer is shimmed via
// newCLITestApp). Verifies the store-level normalisation + the
// CLI's `route show` shape.
func TestRouteCLIAssignAndShow(t *testing.T) {
	app := newCLITestApp(t)
	ctx := context.Background()
	prior, err := app.store.SetSenderRouting(ctx, app.account.ID, "Bob@Acme.IO", "feed")
	require.NoError(t, err)
	require.Equal(t, "", prior)
	dest, err := app.store.GetSenderRouting(ctx, app.account.ID, "bob@acme.io")
	require.NoError(t, err)
	require.Equal(t, "feed", dest)

	// Reassign returns prior.
	prior, err = app.store.SetSenderRouting(ctx, app.account.ID, "bob@acme.io", "imbox")
	require.NoError(t, err)
	require.Equal(t, "feed", prior)
}

// TestRouteCLIListByDestination verifies destination-filtered list
// returns only the rows with the matching destination.
func TestRouteCLIListByDestination(t *testing.T) {
	app := newCLITestApp(t)
	ctx := context.Background()
	_, _ = app.store.SetSenderRouting(ctx, app.account.ID, "a@x.invalid", "feed")
	_, _ = app.store.SetSenderRouting(ctx, app.account.ID, "b@x.invalid", "imbox")
	_, _ = app.store.SetSenderRouting(ctx, app.account.ID, "c@x.invalid", "feed")

	rows, err := app.store.ListSenderRoutings(ctx, app.account.ID, "feed")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		require.Equal(t, "feed", r.Destination)
	}
}

// TestRouteCLINormalisesCase verifies the CLI's normalisation path.
func TestRouteCLINormalisesCase(t *testing.T) {
	app := newCLITestApp(t)
	ctx := context.Background()
	_, err := app.store.SetSenderRouting(ctx, app.account.ID, "Bob@Acme.IO", "feed")
	require.NoError(t, err)
	rows, err := app.store.ListSenderRoutings(ctx, app.account.ID, "")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "bob@acme.io", rows[0].EmailAddress)
}

// TestRouteCLIInvalidAddressErrorClass — store-level error wraps
// ErrInvalidAddress so the CLI can map it to exit 2.
func TestRouteCLIInvalidAddressErrorClass(t *testing.T) {
	app := newCLITestApp(t)
	ctx := context.Background()
	_, err := app.store.SetSenderRouting(ctx, app.account.ID, "   ", "feed")
	require.Error(t, err)
	require.True(t, errors.Is(err, store.ErrInvalidAddress))
}
