package sensitive

import (
	"strings"
	"unicode"
)

// IsCredentialName conservatively identifies header and JSON field names that
// commonly carry credentials. It deliberately works on name tokens rather than
// substrings so ordinary fields such as max_tokens, token_count, monkey, and
// idempotency-key are not redacted merely because they contain a short word.
func IsCredentialName(value string) bool {
	parts := nameParts(value)
	if len(parts) == 0 {
		return false
	}
	normalized := strings.Join(parts, "_")
	switch normalized {
	case "authorization", "proxy_authorization", "cookie", "set_cookie", "auth", "authentication",
		"api_key", "apikey", "private_key", "access_key", "secret_key", "signing_key", "encryption_key",
		"token", "password", "passwd", "secret", "credential", "credentials", "bearer", "signature":
		return true
	}
	if isCompactCredentialName(strings.Join(parts, "")) {
		return true
	}

	for _, part := range parts {
		switch part {
		case "authorization", "cookie", "password", "passwd", "secret", "credential", "credentials", "bearer", "signature":
			return true
		}
	}

	last := parts[len(parts)-1]
	if last == "token" {
		return true
	}
	if last != "key" {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "api", "auth", "access", "client", "credential", "encryption", "private", "secret", "session", "signing":
			return true
		}
	}
	return false
}

// isCompactCredentialName catches credential-bearing names whose word
// boundaries have been erased. This matters for HTTP headers because Go's
// canonicalization turns X-AuthToken into X-Authtoken, which no longer has a
// camel-case boundary for nameParts to recover. The suffixes intentionally do
// not include a bare "key" so safe names such as idempotencykey and
// secwebsocketkey remain observable.
func isCompactCredentialName(value string) bool {
	for _, suffix := range []string{
		"authorization", "authentication", "credentials", "credential",
		"password", "passwd", "signature", "cookie", "bearer", "secret", "token", "auth",
		"apikey", "authkey", "accesskey", "clientkey", "credentialkey",
		"encryptionkey", "privatekey", "secretkey", "sessionkey", "signingkey",
	} {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func nameParts(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	var normalized strings.Builder
	previousLowerOrDigit := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if unicode.IsUpper(r) && previousLowerOrDigit && normalized.Len() > 0 {
				normalized.WriteByte('_')
			}
			normalized.WriteRune(unicode.ToLower(r))
			previousLowerOrDigit = unicode.IsLower(r) || unicode.IsDigit(r)
		default:
			if normalized.Len() > 0 {
				normalized.WriteByte('_')
			}
			previousLowerOrDigit = false
		}
	}
	return strings.FieldsFunc(normalized.String(), func(r rune) bool { return r == '_' })
}
