package subscription

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func createJWT(payload string) string {
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"
}

func TestParseJWTPayload(t *testing.T) {
	if p := ParseJWTPayload(createJWT(`{"subject":"user"}`)); p["subject"] != "user" {
		t.Fatal(p)
	}
	for _, tok := range []string{"not-a-jwt", "header.payload", "header.not-json.signature", createJWT(`"not-an-object"`)} {
		if ParseJWTPayload(tok) != nil {
			t.Errorf("%s must be nil", tok)
		}
	}
	if exp, ok := GetJWTExpiresAt(createJWT(`{"exp":1234}`)); !ok || exp != 1_234_000 {
		t.Fatal(exp)
	}
	for _, p := range []string{`{}`, `{"exp":"1234"}`, `{"exp":null}`} {
		if _, ok := GetJWTExpiresAt(createJWT(p)); ok {
			t.Errorf("%s must have no expiry", p)
		}
	}
}

type call struct {
	name string
	args []string
	env  []string
}

func stubExec(t *testing.T, out string, err error) *[]call {
	t.Helper()
	var calls []call
	orig := ExecFile
	ExecFile = func(_ context.Context, name string, args, env []string) (string, error) {
		calls = append(calls, call{name, args, env})
		return out, err
	}
	t.Cleanup(func() { ExecFile = orig })
	return &calls
}

func TestReadMacOSKeychainPassword(t *testing.T) {
	calls := stubExec(t, "stored-password\n", nil)
	if got := ReadMacOSKeychainPassword(context.Background(), "service-name", "account-name"); got != "stored-password" {
		t.Fatal(got)
	}
	want := call{"/usr/bin/security", []string{"find-generic-password", "-s", "service-name", "-a", "account-name", "-w"}, nil}
	if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], want) {
		t.Fatal(*calls)
	}
	stubExec(t, "\n", nil)
	if ReadMacOSKeychainPassword(context.Background(), "s", "u") != "" {
		t.Fatal("empty output")
	}
	stubExec(t, "x", errors.New("not found"))
	if ReadMacOSKeychainPassword(context.Background(), "s", "u") != "" {
		t.Fatal("failure")
	}
}

func TestReadLinuxSecretServicePassword(t *testing.T) {
	calls := stubExec(t, " stored-password ", nil)
	got := ReadLinuxSecretServicePassword(context.Background(), []SecretServiceAttribute{{"service", "service-name"}, {"username", "account-name"}, {"target", "default"}})
	if got != " stored-password " {
		t.Fatal(got)
	}
	if !reflect.DeepEqual((*calls)[0].args, []string{"lookup", "service", "service-name", "username", "account-name", "target", "default"}) || (*calls)[0].name != "secret-tool" {
		t.Fatal(*calls)
	}
	stubExec(t, "x", errors.New("not found"))
	if ReadLinuxSecretServicePassword(context.Background(), nil) != "" {
		t.Fatal("failure")
	}
}

func TestReadWindowsCredentialManagerPassword(t *testing.T) {
	calls := stubExec(t, " stored-password ", nil)
	if got := ReadWindowsCredentialManagerPassword(context.Background(), "account.service"); got != " stored-password " {
		t.Fatal(got)
	}
	c := (*calls)[0]
	if c.name != "powershell.exe" || !reflect.DeepEqual(c.args[:3], []string{"-NoProfile", "-NonInteractive", "-Command"}) ||
		!strings.Contains(c.args[3], "[AISDKCredentialManager]::Read($env:AI_SDK_WINDOWS_CREDENTIAL_MANAGER_TARGET)") {
		t.Fatal(c)
	}
	var hasSource, hasTarget bool
	for _, kv := range c.env {
		hasSource = hasSource || (strings.HasPrefix(kv, "AI_SDK_WINDOWS_CREDENTIAL_MANAGER_SOURCE=") && strings.Contains(kv, "CredReadW"))
		hasTarget = hasTarget || kv == "AI_SDK_WINDOWS_CREDENTIAL_MANAGER_TARGET=account.service"
	}
	if !hasSource || !hasTarget {
		t.Fatal("source/target must be passed through the environment")
	}
	stubExec(t, "", nil)
	if ReadWindowsCredentialManagerPassword(context.Background(), "t") != "" {
		t.Fatal("empty")
	}
}
