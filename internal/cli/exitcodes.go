package cli

// Exit codes per spec 14 §5.3.
const (
	ExitOK          = 0
	ExitError       = 1
	ExitUserError   = 2
	ExitAuthError   = 3
	ExitNetError    = 4
	ExitNotFound    = 5
	ExitNeedConfirm = 6
	ExitThrottled   = 7
	ExitForbidden   = 8
)
