package tc

// KeyNewBest, KeyNewV2, and KeyNewV3 are privateKey sentinel values for AddOnion that instruct
// the daemon to generate a new key using the best-available, RSA-1024, or ED25519-V3 algorithm.
// The returned Onion.PrivateKey holds the generated key when one of these sentinels is used.
const (
	KeyNewBest = "NEW:BEST"
	KeyNewV2   = "NEW:RSA1024"
	KeyNewV3   = "NEW:ED25519-V3"

	statusCodeOK         = 250
	scopeAuth            = "AUTH"
	scopeVersion         = "VERSION"
	keyAuthMethods       = "METHODS"
	keyAuthCookieFile    = "COOKIEFILE"
	authMethodSafeCookie = "SAFECOOKIE"
)
