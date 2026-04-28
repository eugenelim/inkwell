package auth

// PublicClientID is the Microsoft-published "Microsoft Graph Command
// Line Tools" first-party public client. Inkwell deliberately uses this
// well-known app instead of requiring an Entra ID app registration in
// the user's tenant (PRD §4). Changing this value is a code change, not
// user config.
const PublicClientID = "14d82eec-204b-4c2f-b7e8-296a70dab67e"

// CommonAuthority is the multi-tenant Entra ID authority. The user's
// actual home tenant is inferred from the MSAL AuthResult after
// sign-in.
const CommonAuthority = "https://login.microsoftonline.com/common"

// ConsumerTenantID is the well-known Entra tenant ID that hosts personal
// (non-work / non-school) Microsoft accounts. Inkwell refuses to sign in
// against it — see spec 01 §11.
const ConsumerTenantID = "9188040d-6c67-4c5b-b112-36a304b66dad"

// resourceScopes is the locked Microsoft Graph resource scope list
// (spec 01 §6). Adding or removing scopes here changes the surface
// area we ask the user to consent to and must be done via a code
// change, not user config.
//
// PRD §3.2 forbids requesting Mail.Send / Calendars.ReadWrite /
// Mail.*.Shared / etc. even though the public client we use may
// technically support them. CI lint guards reject any code that adds
// such a scope.
var resourceScopes = []string{
	"https://graph.microsoft.com/Mail.Read",
	"https://graph.microsoft.com/Mail.ReadBasic",
	"https://graph.microsoft.com/Mail.ReadWrite",
	"https://graph.microsoft.com/MailboxSettings.Read",
	"https://graph.microsoft.com/MailboxSettings.ReadWrite",
	"https://graph.microsoft.com/Calendars.Read",
	"https://graph.microsoft.com/User.Read",
	"https://graph.microsoft.com/Presence.Read.All",
}

// DefaultScopes returns the default scope list (spec 01 §6).
//
// offline_access is **opt-in** per §6.1: the default omits it so
// signin opens the browser exactly once on every tenant. Tenants that
// grant offline_access can opt in via [account].request_offline_access
// in config; pass requestOfflineAccess=true here to mirror that.
//
// Trade-off without offline_access: no silent refresh — the user
// re-auths whenever the access token expires (~60 minutes).
func DefaultScopes() []string {
	return ScopesWithOfflineAccess(false)
}

// ScopesWithOfflineAccess returns the resource scopes, optionally
// extended with `offline_access`.
func ScopesWithOfflineAccess(requestOfflineAccess bool) []string {
	out := make([]string, 0, len(resourceScopes)+1)
	out = append(out, resourceScopes...)
	if requestOfflineAccess {
		out = append(out, "offline_access")
	}
	return out
}
