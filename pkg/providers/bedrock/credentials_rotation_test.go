package bedrock

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TS createAmazonBedrock reads accessKeyId/secretAccessKey/sessionToken from the
// environment on every request, so credentials rotated in the environment
// (STS refreshers, aws-vault) must be picked up without recreating the provider.
func TestResolveCredentialsPicksUpRotatedEnvCredentials(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "ak1")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "sk1")
	t.Setenv("AWS_SESSION_TOKEN", "st1")
	p := New(Config{Region: "us-east-1"})

	creds, err := p.resolveCredentials(context.Background())
	if err != nil || creds.AccessKeyID != "ak1" || creds.SessionToken != "st1" {
		t.Fatalf("first creds = %+v, err = %v", creds, err)
	}

	t.Setenv("AWS_ACCESS_KEY_ID", "ak2")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "sk2")
	t.Setenv("AWS_SESSION_TOKEN", "st2")
	creds, err = p.resolveCredentials(context.Background())
	if err != nil || creds.AccessKeyID != "ak2" || creds.SecretAccessKey != "sk2" || creds.SessionToken != "st2" {
		t.Fatalf("rotated creds = %+v, err = %v; want ak2/sk2/st2", creds, err)
	}
}

func TestResolveCredentialsPicksUpRotatedSharedFile(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	path := filepath.Join(t.TempDir(), "credentials")
	write := func(ak string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("[default]\naws_access_key_id = "+ak+"\naws_secret_access_key = sk\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("fileak1")
	p := New(Config{Region: "us-east-1", SharedCredentialsFile: path})
	creds, err := p.resolveCredentials(context.Background())
	if err != nil || creds.AccessKeyID != "fileak1" {
		t.Fatalf("first creds = %+v, err = %v", creds, err)
	}
	write("fileak2")
	creds, err = p.resolveCredentials(context.Background())
	if err != nil || creds.AccessKeyID != "fileak2" {
		t.Fatalf("rotated creds = %+v, err = %v; want fileak2", creds, err)
	}
}

// The shared file is parsed once while unchanged; a rewrite, even one that
// keeps the size and the modification time, is seen within the cache TTL.
func TestSharedFileCredentialsCacheKeepsRotationVisible(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	path := filepath.Join(t.TempDir(), "credentials")
	stamp := time.Unix(1_700_000_000, 0)
	write := func(ak string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("[default]\naws_access_key_id = "+ak+"\naws_secret_access_key = sk\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	write("fileak1")
	p := New(Config{Region: "us-east-1", SharedCredentialsFile: path})
	if c, err := p.resolveCredentials(context.Background()); err != nil || c.AccessKeyID != "fileak1" {
		t.Fatalf("first = %+v, %v", c, err)
	}
	cachedAt := p.fileCredsAt
	if _, err := p.resolveCredentials(context.Background()); err != nil || !p.fileCredsAt.Equal(cachedAt) {
		t.Fatalf("an unchanged file must be served from the cache (err %v)", err)
	}
	write("fileak2") // same size, same mtime
	p.fileCredsAt = time.Now().Add(-sharedFileCacheTTL)
	if c, err := p.resolveCredentials(context.Background()); err != nil || c.AccessKeyID != "fileak2" {
		t.Fatalf("after the TTL = %+v, %v; want fileak2", c, err)
	}
}
