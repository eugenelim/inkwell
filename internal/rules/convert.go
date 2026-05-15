package rules

import (
	"github.com/eugenelim/inkwell/internal/graph"
	"github.com/eugenelim/inkwell/internal/store"
)

// graphPredicatesFromStore converts the store-layer typed predicates
// (no graph import) into the graph-package types for serialisation.
// Predicates and actions are duplicated between packages because per
// `docs/CONVENTIONS.md` §2 layering store and graph are sibling lower-tier
// packages and cannot import each other; this package, as a mid-tier
// consumer, owns the conversion.
func graphPredicatesFromStore(p store.MessagePredicates) *graph.MessageRulePredicates {
	out := &graph.MessageRulePredicates{
		BodyContains:          p.BodyContains,
		BodyOrSubjectContains: p.BodyOrSubjectContains,
		SubjectContains:       p.SubjectContains,
		HeaderContains:        p.HeaderContains,
		SenderContains:        p.SenderContains,
		RecipientContains:     p.RecipientContains,
		SentToMe:              p.SentToMe,
		SentCcMe:              p.SentCcMe,
		SentOnlyToMe:          p.SentOnlyToMe,
		SentToOrCcMe:          p.SentToOrCcMe,
		NotSentToMe:           p.NotSentToMe,
		HasAttachments:        p.HasAttachments,
		Importance:            p.Importance,
		Sensitivity:           p.Sensitivity,
		Categories:            p.Categories,
		IsAutomaticReply:      p.IsAutomaticReply,
		IsAutomaticForward:    p.IsAutomaticForward,
		MessageActionFlag:     p.MessageActionFlag,
	}
	if len(p.FromAddresses) > 0 {
		out.FromAddresses = make([]graph.Recipient, 0, len(p.FromAddresses))
		for _, r := range p.FromAddresses {
			out.FromAddresses = append(out.FromAddresses, graph.Recipient{
				EmailAddress: graph.EmailAddress{Address: r.EmailAddress.Address, Name: r.EmailAddress.Name},
			})
		}
	}
	if len(p.SentToAddresses) > 0 {
		out.SentToAddresses = make([]graph.Recipient, 0, len(p.SentToAddresses))
		for _, r := range p.SentToAddresses {
			out.SentToAddresses = append(out.SentToAddresses, graph.Recipient{
				EmailAddress: graph.EmailAddress{Address: r.EmailAddress.Address, Name: r.EmailAddress.Name},
			})
		}
	}
	if p.WithinSizeRange != nil {
		out.WithinSizeRange = &graph.MessageRuleSizeKB{
			MinimumSize: p.WithinSizeRange.MinimumSize,
			MaximumSize: p.WithinSizeRange.MaximumSize,
		}
	}
	return out
}

func graphActionsFromStore(a store.MessageActions) *graph.MessageRuleActions {
	return &graph.MessageRuleActions{
		MarkAsRead:          a.MarkAsRead,
		MarkImportance:      a.MarkImportance,
		MoveToFolder:        a.MoveToFolder,
		CopyToFolder:        a.CopyToFolder,
		AssignCategories:    a.AssignCategories,
		Delete:              a.Delete,
		StopProcessingRules: a.StopProcessingRules,
	}
}

// storePredicatesFromGraph converts graph-layer predicates back into
// the store representation. Used by Pull when populating the local
// mirror.
func storePredicatesFromGraph(p *graph.MessageRulePredicates) store.MessagePredicates {
	if p == nil {
		return store.MessagePredicates{}
	}
	out := store.MessagePredicates{
		BodyContains:          p.BodyContains,
		BodyOrSubjectContains: p.BodyOrSubjectContains,
		SubjectContains:       p.SubjectContains,
		HeaderContains:        p.HeaderContains,
		SenderContains:        p.SenderContains,
		RecipientContains:     p.RecipientContains,
		SentToMe:              p.SentToMe,
		SentCcMe:              p.SentCcMe,
		SentOnlyToMe:          p.SentOnlyToMe,
		SentToOrCcMe:          p.SentToOrCcMe,
		NotSentToMe:           p.NotSentToMe,
		HasAttachments:        p.HasAttachments,
		Importance:            p.Importance,
		Sensitivity:           p.Sensitivity,
		Categories:            p.Categories,
		IsAutomaticReply:      p.IsAutomaticReply,
		IsAutomaticForward:    p.IsAutomaticForward,
		MessageActionFlag:     p.MessageActionFlag,
	}
	if len(p.FromAddresses) > 0 {
		out.FromAddresses = make([]store.RuleRecipient, 0, len(p.FromAddresses))
		for _, r := range p.FromAddresses {
			out.FromAddresses = append(out.FromAddresses, store.RuleRecipient{
				EmailAddress: store.RuleEmailAddress{Address: r.EmailAddress.Address, Name: r.EmailAddress.Name},
			})
		}
	}
	if len(p.SentToAddresses) > 0 {
		out.SentToAddresses = make([]store.RuleRecipient, 0, len(p.SentToAddresses))
		for _, r := range p.SentToAddresses {
			out.SentToAddresses = append(out.SentToAddresses, store.RuleRecipient{
				EmailAddress: store.RuleEmailAddress{Address: r.EmailAddress.Address, Name: r.EmailAddress.Name},
			})
		}
	}
	if p.WithinSizeRange != nil {
		out.WithinSizeRange = &store.RuleSizeKB{
			MinimumSize: p.WithinSizeRange.MinimumSize,
			MaximumSize: p.WithinSizeRange.MaximumSize,
		}
	}
	return out
}

func storeActionsFromGraph(a *graph.MessageRuleActions) store.MessageActions {
	if a == nil {
		return store.MessageActions{}
	}
	return store.MessageActions{
		MarkAsRead:          a.MarkAsRead,
		MarkImportance:      a.MarkImportance,
		MoveToFolder:        a.MoveToFolder,
		CopyToFolder:        a.CopyToFolder,
		AssignCategories:    a.AssignCategories,
		Delete:              a.Delete,
		StopProcessingRules: a.StopProcessingRules,
	}
}
