package pattern

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLexer_RegexToken(t *testing.T) {
	toks, err := lex(`~b /auth.*token=[a-f0-9]+/`)
	require.NoError(t, err)
	require.Len(t, toks, 3) // op, regex, eof
	require.Equal(t, tkOperator, toks[0].kind)
	require.Equal(t, "b", toks[0].val)
	require.Equal(t, tkRegex, toks[1].kind)
	require.Equal(t, `auth.*token=[a-f0-9]+`, toks[1].val)
}

func TestLexer_RegexEscapedSlash(t *testing.T) {
	toks, err := lex(`~b /https:\/\/foo.bar/`)
	require.NoError(t, err)
	require.Equal(t, tkRegex, toks[1].kind)
	require.Equal(t, `https://foo.bar`, toks[1].val)
}

func TestLexer_UnterminatedRegex(t *testing.T) {
	_, err := lex(`~b /unterminated`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unterminated regex")
}

func TestParser_RejectsRegexOnHeader(t *testing.T) {
	_, err := Parse(`~h /foo/`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "~h does not support regex")
}

func TestParser_RejectsRegexOnFrom(t *testing.T) {
	_, err := Parse(`~f /foo/`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "regex is only supported on ~s, ~b, ~B")
}

func TestParser_AcceptsRegexOnSubject(t *testing.T) {
	root, err := Parse(`~s /^\[release\]/`)
	require.NoError(t, err)
	p, ok := root.(Predicate)
	require.True(t, ok)
	require.Equal(t, FieldSubject, p.Field)
	rv, ok := p.Value.(RegexValue)
	require.True(t, ok)
	require.Equal(t, `^\[release\]`, rv.Raw)
	require.NotNil(t, rv.Compiled)
}

func TestParser_RegexCompileError(t *testing.T) {
	_, err := Parse(`~b /(unclosed/`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "regex compile error")
}

func TestSelect_RegexOnBody_RequiresIndex(t *testing.T) {
	root, err := Parse(`~b /token/`)
	require.NoError(t, err)
	_, err = CompileNode(root, CompileOptions{BodyIndexEnabled: false})
	require.ErrorIs(t, err, ErrRegexRequiresLocalIndex)
}

func TestSelect_RegexOnSubject_AdmittedWithoutIndex(t *testing.T) {
	root, err := Parse(`~s /^urgent/`)
	require.NoError(t, err)
	c, err := CompileNode(root, CompileOptions{BodyIndexEnabled: false})
	require.NoError(t, err)
	require.Equal(t, StrategyLocalRegex, c.Strategy)
}

func TestSelect_RegexOnBody_RoutesToLocalRegex(t *testing.T) {
	root, err := Parse(`~b /token=[a-f0-9]+/`)
	require.NoError(t, err)
	c, err := CompileNode(root, CompileOptions{BodyIndexEnabled: true})
	require.NoError(t, err)
	require.Equal(t, StrategyLocalRegex, c.Strategy)
}

func TestCompileLocalRegex_ExtractsLiterals(t *testing.T) {
	root, err := Parse(`~b /auth.*token=[a-f0-9]+/`)
	require.NoError(t, err)
	plan, err := CompileLocalRegex(root, CompileOptions{BodyIndexEnabled: true})
	require.NoError(t, err)
	// Both "auth" and "token=" are ≥3-char literals from concatenation.
	require.Contains(t, plan.Literals, "auth")
	require.Contains(t, plan.Literals, "token=")
}

func TestCompileLocalRegex_RefusesWithoutLiteral(t *testing.T) {
	cases := []string{
		`~b /^.$/`,
		`~b /[a-z]+/`,
		`~b /.*x.*y.*/`,
	}
	for _, src := range cases {
		t.Run(src, func(t *testing.T) {
			root, err := Parse(src)
			require.NoError(t, err)
			_, err = CompileLocalRegex(root, CompileOptions{BodyIndexEnabled: true})
			require.ErrorIs(t, err, ErrRegexUnboundedScan)
		})
	}
}

func TestCompileLocalRegex_BodyIndexDisabled(t *testing.T) {
	root, err := Parse(`~b /auth/`)
	require.NoError(t, err)
	_, err = CompileLocalRegex(root, CompileOptions{BodyIndexEnabled: false})
	require.ErrorIs(t, err, ErrRegexRequiresLocalIndex)
}

func TestCompileLocalRegex_StructuralPartCarried(t *testing.T) {
	root, err := Parse(`~b /token=[a-f0-9]+/ ~F`)
	require.NoError(t, err)
	plan, err := CompileLocalRegex(root, CompileOptions{BodyIndexEnabled: true})
	require.NoError(t, err)
	require.Contains(t, plan.StructuralWhere, "flag_status = 'flagged'")
}

func TestMandatoryLiterals_Spec35Cases(t *testing.T) {
	cases := []struct {
		src         string
		mustContain []string
	}{
		{`auth.*token=[a-f0-9]+`, []string{"auth", "token="}},
		{`hello world`, []string{"hello world"}},
		{`^password`, []string{"password"}},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			re := regexp.MustCompile(c.src)
			lits := mandatoryLiterals(re)
			for _, want := range c.mustContain {
				require.Contains(t, lits, want, "literal %q missing from %v", want, lits)
			}
		})
	}
}

func TestEmitLocal_BodyRoutingFlipsOnFlag(t *testing.T) {
	// `~b *alpha*` forces a LIKE-shaped predicate; bare `~b alpha`
	// uses MatchExact which emits `=` rather than LIKE.
	root, err := Parse(`~b *alpha*`)
	require.NoError(t, err)

	c, err := CompileLocalWithOpts(root, CompileOptions{BodyIndexEnabled: false})
	require.NoError(t, err)
	require.Contains(t, c.Where, "body_preview LIKE")

	c, err = CompileLocalWithOpts(root, CompileOptions{BodyIndexEnabled: true})
	require.NoError(t, err)
	require.Contains(t, c.Where, "bt.content LIKE")
}

func TestEmitLocal_SubjectOrBodyRoutingFlipsOnFlag(t *testing.T) {
	root, err := Parse(`~B *alpha*`)
	require.NoError(t, err)

	c, err := CompileLocalWithOpts(root, CompileOptions{BodyIndexEnabled: false})
	require.NoError(t, err)
	require.Contains(t, c.Where, "body_preview")

	c, err = CompileLocalWithOpts(root, CompileOptions{BodyIndexEnabled: true})
	require.NoError(t, err)
	require.Contains(t, c.Where, "bt.content")
	require.Contains(t, c.Where, "subject")
}
