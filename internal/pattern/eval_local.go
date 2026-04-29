package pattern

import (
	"fmt"
	"strings"
)

// SQLClause is the result of evaluating an AST against the local
// store. The caller composes "SELECT ... FROM messages WHERE " + Where
// and passes Args to the parameterised query.
type SQLClause struct {
	Where string
	Args  []any
}

// CompileLocal produces a [SQLClause] that selects the messages
// matching the AST against the local SQLite store. Returns an error
// when the AST contains a node the local backend can't evaluate
// (currently only ~h header lookups, which are server-only — bulk
// ops will route those to Graph $filter via spec 09).
func CompileLocal(root Node) (*SQLClause, error) {
	if root == nil {
		return nil, fmt.Errorf("CompileLocal: nil AST")
	}
	w, args, err := emitLocal(root)
	if err != nil {
		return nil, err
	}
	return &SQLClause{Where: w, Args: args}, nil
}

func emitLocal(n Node) (string, []any, error) {
	switch v := n.(type) {
	case And:
		l, la, err := emitLocal(v.L)
		if err != nil {
			return "", nil, err
		}
		r, ra, err := emitLocal(v.R)
		if err != nil {
			return "", nil, err
		}
		return "(" + l + " AND " + r + ")", append(la, ra...), nil
	case Or:
		l, la, err := emitLocal(v.L)
		if err != nil {
			return "", nil, err
		}
		r, ra, err := emitLocal(v.R)
		if err != nil {
			return "", nil, err
		}
		return "(" + l + " OR " + r + ")", append(la, ra...), nil
	case Not:
		s, args, err := emitLocal(v.X)
		if err != nil {
			return "", nil, err
		}
		return "(NOT " + s + ")", args, nil
	case Predicate:
		return emitPredicate(v)
	}
	return "", nil, fmt.Errorf("emitLocal: unknown node %T", n)
}

func emitPredicate(p Predicate) (string, []any, error) {
	switch p.Field {
	case FieldHasAttachments:
		return "has_attachments = 1", nil, nil
	case FieldUnread:
		return "is_read = 0", nil, nil
	case FieldRead:
		return "is_read = 1", nil, nil
	case FieldFlagged:
		return "flag_status = 'flagged'", nil, nil
	}

	switch v := p.Value.(type) {
	case StringValue:
		return emitStringPredicate(p.Field, v)
	case DateValue:
		return emitDatePredicate(p.Field, v)
	}
	return "", nil, fmt.Errorf("emitPredicate: unsupported value type %T for field %v", p.Value, p.Field)
}

func emitStringPredicate(f Field, v StringValue) (string, []any, error) {
	switch f {
	case FieldFrom:
		return likeAny([]string{"from_address", "from_name"}, v)
	case FieldTo:
		// to_addresses is JSON; LOWER + LIKE on the serialised payload
		// is the cheap-correct path. Spec 02 stores addresses lowercase.
		return likeOne("to_addresses", v)
	case FieldCc:
		return likeOne("cc_addresses", v)
	case FieldRecipient:
		return likeAny([]string{"to_addresses", "cc_addresses"}, v)
	case FieldSubject:
		return likeOne("subject", v)
	case FieldBody:
		return likeOne("body_preview", v)
	case FieldSubjectOrBody:
		return likeAny([]string{"subject", "body_preview"}, v)
	case FieldFolder:
		return likeOne("folder_id", v) // exact match in practice; folder names not in messages row
	case FieldCategory:
		return likeOne("categories", v) // JSON array; LIKE is the cheap path
	case FieldImportance:
		return "importance = ?", []any{strings.ToLower(v.Raw)}, nil
	case FieldInferenceCls:
		return "inference_class = ?", []any{strings.ToLower(v.Raw)}, nil
	case FieldConversation:
		return "conversation_id = ?", []any{v.Raw}, nil
	case FieldHeader:
		return "", nil, fmt.Errorf("~h header lookup is server-only")
	}
	return "", nil, fmt.Errorf("emitStringPredicate: unsupported field %v", f)
}

// likeOne renders a single-column LIKE predicate (or = for exact matches).
// LIKE clauses include `ESCAPE '\'` so the literal-`%`/`_` escapes
// produced by likeArgs are honoured. Without it SQLite treats `\` as
// plain text and `:filter 50%` would silently match nothing.
func likeOne(col string, v StringValue) (string, []any, error) {
	op, arg := likeArgs(v)
	if op == "=" {
		return col + " = ?", []any{arg}, nil
	}
	return col + ` LIKE ? ESCAPE '\'`, []any{arg}, nil
}

// likeAny renders an OR over multiple columns.
func likeAny(cols []string, v StringValue) (string, []any, error) {
	op, arg := likeArgs(v)
	parts := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols))
	for _, c := range cols {
		if op == "=" {
			parts = append(parts, c+" = ?")
		} else {
			parts = append(parts, c+` LIKE ? ESCAPE '\'`)
		}
		args = append(args, arg)
	}
	return "(" + strings.Join(parts, " OR ") + ")", args, nil
}

// likeArgs translates the [MatchKind] into a SQL operator + bound arg.
// Returns ("=", raw) for exact matches, ("LIKE", "%..." / "...%" /
// "%...%") for wildcards. Escapes literal `%` and `_` in the raw value.
func likeArgs(v StringValue) (string, string) {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(v.Raw)
	switch v.Match {
	case MatchPrefix:
		return "LIKE", esc + "%"
	case MatchSuffix:
		return "LIKE", "%" + esc
	case MatchContains:
		return "LIKE", "%" + esc + "%"
	}
	return "=", v.Raw
}

func emitDatePredicate(f Field, v DateValue) (string, []any, error) {
	col := "received_at"
	if f == FieldDateSent {
		col = "sent_at"
	}
	switch v.Op {
	case DateBefore:
		return col + " < ?", []any{v.At.Unix()}, nil
	case DateBeforeEq:
		return col + " <= ?", []any{v.At.Unix()}, nil
	case DateAfter:
		return col + " > ?", []any{v.At.Unix()}, nil
	case DateAfterEq:
		return col + " >= ?", []any{v.At.Unix()}, nil
	case DateOn:
		// Inclusive of At, exclusive of End (next day's 00:00).
		return "(" + col + " >= ? AND " + col + " < ?)", []any{v.At.Unix(), v.End.Unix()}, nil
	case DateRange:
		return "(" + col + " >= ? AND " + col + " < ?)", []any{v.At.Unix(), v.End.Unix()}, nil
	case DateWithinLast:
		// "<30d" — received within the last 30 days.
		return col + " >= ?", []any{v.At.Unix()}, nil
	}
	return "", nil, fmt.Errorf("emitDatePredicate: unknown op %v", v.Op)
}
