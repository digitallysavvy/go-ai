package vercel

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func makeTestOIDCToken(t *testing.T, ownerID, projectID string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(map[string]string{"owner_id": ownerID, "project_id": projectID})
	if err != nil {
		t.Fatal(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestResolveCredentialsPrefersExplicit(t *testing.T) {
	t.Setenv("VERCEL_OIDC_TOKEN", makeTestOIDCToken(t, "env-owner", "env-project"))
	creds, err := ResolveCredentials(Credentials{Token: "tok", TeamID: "team", ProjectID: "proj"})
	if err != nil {
		t.Fatal(err)
	}
	if creds.Token != "tok" || creds.TeamID != "team" || creds.ProjectID != "proj" {
		t.Fatalf("expected explicit credentials to win, got %#v", creds)
	}
}

func TestResolveCredentialsFallsBackToOIDCToken(t *testing.T) {
	t.Setenv("VERCEL_OIDC_TOKEN", makeTestOIDCToken(t, "owner-1", "project-1"))
	creds, err := ResolveCredentials(Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if creds.TeamID != "owner-1" || creds.ProjectID != "project-1" {
		t.Fatalf("expected claims decoded from OIDC token, got %#v", creds)
	}
}

func TestResolveCredentialsErrorsWithNoCredentials(t *testing.T) {
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	_, err := ResolveCredentials(Credentials{})
	if err != ErrNoCredentials {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}
}

func TestResolveCredentialsRejectsPartialExplicit(t *testing.T) {
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	_, err := ResolveCredentials(Credentials{Token: "tok"})
	if err == nil {
		t.Fatal("expected an error for partially-specified credentials")
	}
}
