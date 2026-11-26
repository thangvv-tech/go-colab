package colab

import (
	"strings"
	"testing"
)

func TestAuthenticateCodeDisablesEphemeralAuth(t *testing.T) {
	code := authenticateCode()
	if !strings.Contains(code, "USE_AUTH_EPHEM") || !strings.Contains(code, "auth.authenticate_user()") {
		t.Fatalf("unexpected auth code: %q", code)
	}
}
