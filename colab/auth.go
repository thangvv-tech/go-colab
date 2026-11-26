package colab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ColabScopes are the OAuth scopes required by the Colab API.
var ColabScopes = []string{
	"openid",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/colaboratory",
	"https://www.googleapis.com/auth/drive.file",
}

// AuthProvider identifies the auth strategy.
type AuthProvider string

const (
	AuthProviderOAuth2 AuthProvider = "oauth2"
	AuthProviderADC    AuthProvider = "adc"
)

// Authenticator produces an authenticated *http.Client.
// Backend services use ADCAuth; interactive CLI uses OAuth2Auth.
type Authenticator interface {
	HTTPClient(ctx context.Context) (*http.Client, error)
}

// ADCAuth uses Application Default Credentials (GCE, Cloud Run, service accounts).
type ADCAuth struct{}

func NewADCAuth() *ADCAuth { return &ADCAuth{} }

func (a *ADCAuth) HTTPClient(ctx context.Context) (*http.Client, error) {
	creds, err := google.FindDefaultCredentials(ctx, ColabScopes...)
	if err != nil {
		return nil, fmt.Errorf("colab: ADC credentials not found: %w", err)
	}
	return oauth2.NewClient(ctx, creds.TokenSource), nil
}

// OAuth2Auth performs the installed-app OAuth2 flow with token caching.
type OAuth2Auth struct {
	// ConfigPath is the path to client_secret JSON. It is ignored when ConfigJSON is set.
	ConfigPath string
	// ConfigJSON contains installed-app OAuth client credentials for embedded CLI use.
	ConfigJSON []byte
	// TokenPath is where to cache the token (default ~/.config/go-colab/token.json).
	TokenPath string
}

func NewOAuth2Auth(configPath, tokenPath string) *OAuth2Auth {
	return &OAuth2Auth{ConfigPath: configPath, TokenPath: oauthTokenPath(tokenPath)}
}

// NewOAuth2AuthFromConfig creates OAuth authentication from client config bytes.
// This supports embedded public OAuth client configuration in CLI applications.
func NewOAuth2AuthFromConfig(configJSON []byte, tokenPath string) *OAuth2Auth {
	return &OAuth2Auth{ConfigJSON: append([]byte(nil), configJSON...), TokenPath: oauthTokenPath(tokenPath)}
}

func oauthTokenPath(tokenPath string) string {
	if tokenPath != "" {
		return tokenPath
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "go-colab", "token.json")
}

func (a *OAuth2Auth) HTTPClient(ctx context.Context) (*http.Client, error) {
	b := a.ConfigJSON
	if len(b) == 0 {
		var err error
		b, err = os.ReadFile(a.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("colab: read oauth config: %w", err)
		}
	}
	cfg, err := google.ConfigFromJSON(b, ColabScopes...)
	if err != nil {
		return nil, fmt.Errorf("colab: parse oauth config: %w", err)
	}

	tok, err := loadCachedToken(a.TokenPath)
	if err != nil || !tok.Valid() {
		tok, err = runOAuthFlow(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("colab: oauth flow: %w", err)
		}
		if err := saveCachedToken(a.TokenPath, tok); err != nil {
			// non-fatal: log but continue
			_ = err
		}
	}
	return cfg.Client(ctx, tok), nil
}

func loadCachedToken(path string) (*oauth2.Token, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t oauth2.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func saveCachedToken(path string, t *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

// runOAuthFlow opens the browser and starts a localhost redirect server
// to complete the OAuth2 authorization code flow.
// ponytail: headless/CI use ADCAuth instead; this is CLI-only
func runOAuthFlow(ctx context.Context, cfg *oauth2.Config) (*oauth2.Token, error) {
	cfg.RedirectURL = "http://localhost"
	verifier := oauth2.GenerateVerifier()
	authURL := cfg.AuthCodeURL("state", oauth2.S256ChallengeOption(verifier))
	fmt.Fprintf(os.Stderr, "Open this URL to authenticate:\n%s\n", authURL)
	fmt.Fprintf(os.Stderr, "Enter the authorization code: ")
	var code string
	if _, err := fmt.Scan(&code); err != nil {
		return nil, fmt.Errorf("read auth code: %w", err)
	}
	return cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
}
