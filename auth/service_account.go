package auth

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const SATokenSourceKey ContextKey = "sa_token_source"

// NewServiceAccountTokenSource creates an oauth2.TokenSource scoped to GTM API access.
// If keyJSON is non-empty, it is parsed as a service account JSON key.
// If impersonateSubject is also set, Domain-Wide Delegation is used to impersonate
// that Google Workspace user (required for GTM, which rejects service account emails
// in its user management — only real Google account emails can be GTM users).
// Otherwise Application Default Credentials are used (Workload Identity on GCP).
func NewServiceAccountTokenSource(ctx context.Context, keyJSON string, impersonateSubject string) (oauth2.TokenSource, error) {
	if keyJSON != "" {
		if impersonateSubject != "" {
			// DWD: service account impersonates a real Workspace user who has GTM access.
			// Requires DWD enabled on the SA in GCP and authorized in Workspace Admin.
			jwtConf, err := google.JWTConfigFromJSON([]byte(keyJSON), GoogleScopes...)
			if err != nil {
				return nil, fmt.Errorf("invalid GOOGLE_SERVICE_ACCOUNT_KEY_JSON for DWD: %w", err)
			}
			jwtConf.Subject = impersonateSubject
			return jwtConf.TokenSource(ctx), nil
		}
		creds, err := google.CredentialsFromJSON(ctx, []byte(keyJSON), GoogleScopes...)
		if err != nil {
			return nil, fmt.Errorf("invalid GOOGLE_SERVICE_ACCOUNT_KEY_JSON: %w", err)
		}
		return creds.TokenSource, nil
	}

	creds, err := google.FindDefaultCredentials(ctx, GoogleScopes...)
	if err != nil {
		return nil, fmt.Errorf("no service account credentials found (set GOOGLE_SERVICE_ACCOUNT_KEY_JSON or use Workload Identity): %w", err)
	}
	return creds.TokenSource, nil
}

// GetSATokenSource retrieves the service account token source from context.
// Returns nil when not in S2S mode.
func GetSATokenSource(ctx context.Context) oauth2.TokenSource {
	if ts, ok := ctx.Value(SATokenSourceKey).(oauth2.TokenSource); ok {
		return ts
	}
	return nil
}
