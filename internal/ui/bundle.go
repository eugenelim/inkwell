package ui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eugenelim/inkwell/internal/store"
)

// bundleToastMsg is the result of bundleToggleCmd. nowBundled=true on
// designate, false on un-designate. seq is the per-address monotonic
// counter — the Update handler discards messages whose seq is older
// than the address's current bundleInflight value (rapid-press race
// guard, spec 26 §6).
type bundleToastMsg struct {
	address    string
	nowBundled bool
	seq        uint64
	err        error
}

// bundledSendersLoadedMsg is the result of loadBundledSendersCmd
// (spec 26 §6.1). The Update handler replaces m.bundledSenders
// wholesale, sweeps stale m.bundleExpanded entries, and invalidates
// the list pane bundle cache.
type bundledSendersLoadedMsg struct {
	addresses []string
	err       error
}

// bundleToggleCmd writes the designate/un-designate decision to the
// store. The desired post-state was already computed synchronously in
// Update from the in-memory bundledSenders set (avoids a TOCTOU race
// window between IsSenderBundled and Add/Remove).
func bundleToggleCmd(ctx context.Context, st store.Store, accountID int64,
	address string, target bool, seq uint64) tea.Cmd {
	return func() tea.Msg {
		addr := strings.ToLower(strings.TrimSpace(address))
		if addr == "" {
			return bundleToastMsg{seq: seq, err: fmt.Errorf("bundle: empty sender address")}
		}
		var err error
		if target {
			err = st.AddBundledSender(ctx, accountID, addr)
		} else {
			err = st.RemoveBundledSender(ctx, accountID, addr)
		}
		if err != nil {
			return bundleToastMsg{address: addr, nowBundled: target, seq: seq, err: err}
		}
		return bundleToastMsg{address: addr, nowBundled: target, seq: seq}
	}
}

// loadBundledSendersCmd refreshes the in-memory designated-sender set
// from the store. Fans out from sign-in init and Ctrl+R Refresh.
func loadBundledSendersCmd(ctx context.Context, st store.Store, accountID int64) tea.Cmd {
	return func() tea.Msg {
		rows, err := st.ListBundledSenders(ctx, accountID)
		if err != nil {
			return bundledSendersLoadedMsg{err: err}
		}
		addrs := make([]string, 0, len(rows))
		for _, r := range rows {
			addrs = append(addrs, r.Address)
		}
		return bundledSendersLoadedMsg{addresses: addrs}
	}
}

// countBundleCollapse returns the number of messages that will be
// hidden under bundle headers in the current list slice for the given
// address, given the active bundle_min_count. This walks the slice
// synchronously inside Update so the toast text is exact (spec 26
// §5.4 / §6).
func countBundleCollapse(messages []store.Message, address string, minCount int) int {
	if minCount <= 0 || address == "" {
		return 0
	}
	addr := strings.ToLower(strings.TrimSpace(address))
	if addr == "" {
		return 0
	}
	total := 0
	for i := 0; i < len(messages); i++ {
		ja := strings.ToLower(strings.TrimSpace(messages[i].FromAddress))
		if ja != addr {
			continue
		}
		j := i
		for j < len(messages) && strings.ToLower(strings.TrimSpace(messages[j].FromAddress)) == addr {
			j++
		}
		runLen := j - i
		if runLen >= minCount {
			total += runLen
		}
		i = j - 1
	}
	return total
}

// bundleAddressForSelection returns the address the B keypress should
// toggle. Empty string means "no actionable target" (focused row is
// not a flat row and has no address; or message has empty
// from_address). The boolean is false in that case.
func bundleAddressForSelection(row renderedRow) (string, bool) {
	if row.IsBundleHeader {
		if row.BundleAddress != "" {
			return row.BundleAddress, true
		}
		return "", false
	}
	addr := strings.ToLower(strings.TrimSpace(row.Message.FromAddress))
	if addr == "" {
		return "", false
	}
	return addr, true
}
