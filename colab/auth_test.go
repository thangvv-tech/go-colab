package colab_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/oauth2"

	"github.com/thangvv-tech/go-colab/colab"
)

// compile-time interface checks
var _ colab.Authenticator = (*colab.ADCAuth)(nil)
var _ colab.Authenticator = (*colab.OAuth2Auth)(nil)

func TestADCAuthImplementsAuthenticator(t *testing.T) {
	// satisfied by compile-time var declarations above
}

func TestOAuth2AuthDefaultTokenPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", "go-colab", "token.json")
	auth := colab.NewOAuth2Auth("cfg.json", "")
	if auth.TokenPath != expected {
		t.Fatalf("TokenPath = %q, want %q", auth.TokenPath, expected)
	}
}

func TestOAuth2AuthConfigBytesUsesCachedToken(t *testing.T) {
	config := []byte(`{"installed":{"client_id":"client","client_secret":"secret","redirect_uris":["http://localhost"],"auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token"}}`)
	tokenPath := filepath.Join(t.TempDir(), "token.json")
	if err := os.WriteFile(tokenPath, []byte(`{"access_token":"test","token_type":"Bearer","expiry":"2099-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}
	auth := colab.NewOAuth2AuthFromConfig(config, tokenPath)
	client, err := auth.HTTPClient(context.Background())
	if err != nil || client == nil {
		t.Fatalf("client=%v err=%v", client, err)
	}
}

func TestColabScopes(t *testing.T) {
	found := false
	for _, s := range colab.ColabScopes {
		if s == "https://www.googleapis.com/auth/colaboratory" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing colaboratory scope")
	}
}

func TestTokenRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	tokenPath := filepath.Join(tmp, "token.json")

	tok := &oauth2.Token{
		AccessToken: "test-access-token",
		TokenType:   "Bearer",
	}
	b, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, b, 0600); err != nil {
		t.Fatal(err)
	}

	// read back and verify
	var got oauth2.Token
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "test-access-token" {
		t.Fatalf("AccessToken = %q, want %q", got.AccessToken, "test-access-token")
	}
}
