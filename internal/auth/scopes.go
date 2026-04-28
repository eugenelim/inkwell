package auth

// DefaultScopes returns the locked scope list (spec 01 §6). Adding or
// removing scopes changes the contract with the tenant admin and must be
// done via a code change, not user config.
//
// offline_access is mandatory: without it MSAL will not issue a refresh
// token and the user device-codes on every launch.
func DefaultScopes() []string {
	return []string{
		"https://graph.microsoft.com/Mail.Read",
		"https://graph.microsoft.com/Mail.ReadBasic",
		"https://graph.microsoft.com/Mail.ReadWrite",
		"https://graph.microsoft.com/MailboxSettings.Read",
		"https://graph.microsoft.com/MailboxSettings.ReadWrite",
		"https://graph.microsoft.com/Calendars.Read",
		"https://graph.microsoft.com/User.Read",
		"https://graph.microsoft.com/Presence.Read.All",
		"offline_access",
	}
}
