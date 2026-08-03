package sensitive

import "testing"

func TestIsCredentialName(t *testing.T) {
	for _, value := range []string{
		"Authorization", "X-Auth-Token", "Api-Key", "private_key", "privateKey",
		"clientSecret", "credentials", "user_password", "X-Signature",
	} {
		if !IsCredentialName(value) {
			t.Errorf("IsCredentialName(%q) = false, want true", value)
		}
	}
	for _, value := range []string{
		"User-Agent", "Traceparent", "X-Request-ID", "max_tokens", "prompt_tokens",
		"token_count", "monkey", "idempotency-key", "sec-websocket-key",
	} {
		if IsCredentialName(value) {
			t.Errorf("IsCredentialName(%q) = true, want false", value)
		}
	}
}
