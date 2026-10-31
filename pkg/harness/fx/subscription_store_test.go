package fx

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil/subscription"
)

// Port of TS fx-subscription.test.ts.

// noKeyring is a keyring with no entries.
type noKeyring struct{}

func (noKeyring) read(context.Context, string, string) (string, error) { return "", nil }
func (noKeyring) write(context.Context, string, string, string) error  { return nil }

type keyringCall struct{ op, service, account string }

type fakeKeyring struct {
	mu       sync.Mutex
	stored   string
	calls    []keyringCall
	failNext error
}

func (k *fakeKeyring) read(_ context.Context, service, account string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, keyringCall{"read", service, account})
	return k.stored, nil
}

func (k *fakeKeyring) write(_ context.Context, service, account, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, keyringCall{"write", service, account})
	if k.failNext != nil {
		err := k.failNext
		k.failNext = nil
		return err
	}
	k.stored = value
	return nil
}

const subExpiresAt = int64(2_000_000_000_000)

func fxCredentialsDoc(accessToken string) map[string]any {
	return map[string]any{"version": 1, "access_token": accessToken, "refresh_token": "refresh", "account_id": "account", "expires_at_ms": subExpiresAt}
}

func codexCredentialsDoc() map[string]any {
	return map[string]any{
		"auth_mode": "chatgpt",
		"tokens":    map[string]any{"access_token": "native-codex", "refresh_token": "refresh", "account_id": "account", "id_token": "preserve-id"},
		"extra":     true,
	}
}

func grokCredentialsDoc() map[string]any {
	return map[string]any{
		grokCredentialScope: map[string]any{"key": "native-grok", "refresh_token": "refresh", "user_id": "account", "expires_at": isoString(subExpiresAt), "extra": true},
		"other":             map[string]any{"key": "preserve"},
	}
}

func putDoc(t *testing.T, dir, file string, value any) {
	t.Helper()
	path := filepath.Join(dir, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(value)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

type subTest struct {
	t       *testing.T
	dir     string
	keyring codexKeyring
}

func newSubTest(t *testing.T) *subTest {
	return &subTest{t: t, dir: t.TempDir(), keyring: noKeyring{}}
}

func (s *subTest) resolve(auth AuthenticationMode, env map[string]string, required string) (fxAuthentication, error) {
	return resolveFxAuthentication(context.Background(), resolveAuthOptions{Auth: auth, Env: env, HomeDirectory: s.dir, RequiredProvider: required, Keyring: s.keyring})
}

func (s *subTest) mustSubscription(auth fxAuthentication, err error) *fxSubscription {
	s.t.Helper()
	if err != nil {
		s.t.Fatal(err)
	}
	if auth.Subscription == nil {
		s.t.Fatalf("expected a subscription, got %+v", auth)
	}
	return auth.Subscription
}

func (s *subTest) readToken(sub *fxSubscription) string {
	s.t.Helper()
	c, err := sub.read(context.Background())
	if err != nil {
		s.t.Fatal(err)
	}
	return c.AccessToken
}

func TestFxSubscriptionDiscoveryOrder(t *testing.T) {
	s := newSubTest(t)
	sources := []struct {
		file     string
		value    any
		provider string
		token    string
	}{
		{".fx/chatgpt-auth.json", fxCredentialsDoc("fx-codex"), "codex", "fx-codex"},
		{".fx/grok-auth.json", fxCredentialsDoc("fx-grok"), "grok", "fx-grok"},
		{".codex/auth.json", codexCredentialsDoc(), "codex", "native-codex"},
		{".grok/auth.json", grokCredentialsDoc(), "grok", "native-grok"},
	}
	for _, src := range sources {
		putDoc(t, s.dir, src.file, src.value)
	}
	for _, src := range sources {
		auth, err := s.resolve(harness.Authentication{}, map[string]string{}, "")
		sub := s.mustSubscription(auth, err)
		if auth.Provider != src.provider || s.readToken(sub) != src.token {
			t.Fatalf("got %s/%s, want %s/%s", auth.Provider, s.readToken(sub), src.provider, src.token)
		}
		if err := os.Remove(filepath.Join(s.dir, src.file)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.resolve(harness.Authentication{}, map[string]string{}, ""); err == nil || !strings.Contains(err.Error(), "No subscription credentials were available") {
		t.Fatalf("err = %v", err)
	}
}

func TestFxSubscriptionPrefersGateway(t *testing.T) {
	for _, mode := range []string{"", "auto", "ai-gateway"} {
		s := newSubTest(t)
		putDoc(t, s.dir, ".fx/chatgpt-auth.json", fxCredentialsDoc("subscription"))
		got, err := s.resolve(harness.AuthMode(mode), map[string]string{"AI_GATEWAY_API_KEY": "gateway"}, "")
		if err != nil || got != (fxAuthentication{Provider: "gateway", APIKey: "gateway", BaseURL: "https://ai-gateway.vercel.sh"}) {
			t.Errorf("%q: %+v, %v", mode, got, err)
		}
	}
}

func TestFxSubscriptionDirectModeKeepsGatewayFallback(t *testing.T) {
	s := newSubTest(t)
	putDoc(t, s.dir, ".fx/chatgpt-auth.json", fxCredentialsDoc("subscription"))
	if got, _ := s.resolve(harness.AuthMode("direct"), map[string]string{"AI_GATEWAY_API_KEY": "gateway"}, ""); got.Provider != "codex" {
		t.Fatalf("provider %q", got.Provider)
	}
	_ = os.Remove(filepath.Join(s.dir, ".fx/chatgpt-auth.json"))
	if got, err := s.resolve(harness.AuthMode("direct"), map[string]string{"VERCEL_OIDC_TOKEN": "gateway"}, ""); err != nil || got.Provider != "gateway" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestFxSubscriptionIsolatesExplicitEnvironments(t *testing.T) {
	s := newSubTest(t)
	putDoc(t, s.dir, ".fx/chatgpt-auth.json", fxCredentialsDoc("subscription"))
	if _, err := s.resolve(harness.AuthEnvironment(map[string]string{}), map[string]string{"AI_GATEWAY_API_KEY": "host"}, ""); err == nil || !strings.Contains(err.Error(), "fx requires") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := s.resolve(harness.AuthEnvironment(map[string]string{"AI_GATEWAY_API_KEY": "explicit"}), nil, ""); got.Provider != "gateway" {
		t.Fatalf("provider %q", got.Provider)
	}
	if _, err := s.resolve(harness.AuthMode("ai-gateway"), nil, ""); err == nil || !strings.Contains(err.Error(), "fx requires") {
		t.Fatalf("err = %v", err)
	}
}

func TestFxSubscriptionSkipsMalformedStores(t *testing.T) {
	s := newSubTest(t)
	putDoc(t, s.dir, ".fx/chatgpt-auth.json", map[string]any{"version": 2})
	putDoc(t, s.dir, ".fx/grok-auth.json", map[string]any{})
	putDoc(t, s.dir, ".codex/auth.json", map[string]any{"auth_mode": "apikey", "OPENAI_API_KEY": "ignore"})
	putDoc(t, s.dir, ".grok/auth.json", grokCredentialsDoc())
	if got, _ := s.resolve(harness.Authentication{}, nil, ""); got.Provider != "grok" {
		t.Fatalf("provider %q", got.Provider)
	}
	// Not JSON at all, and a __proto__ key, are skipped too.
	if err := os.WriteFile(filepath.Join(s.dir, ".grok/auth.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	putDoc(t, s.dir, ".codex/auth.json", map[string]any{"auth_mode": "chatgpt", "__proto__": map[string]any{}, "tokens": map[string]any{"access_token": "x"}})
	if _, err := s.resolve(harness.Authentication{}, nil, ""); err == nil {
		t.Fatal("malformed stores were used")
	}
}

func TestFxSubscriptionHonorsNativeHomeOverrides(t *testing.T) {
	s := newSubTest(t)
	putDoc(t, s.dir, "custom-codex/auth.json", codexCredentialsDoc())
	putDoc(t, s.dir, "custom-grok/auth.json", grokCredentialsDoc())
	env := map[string]string{"CODEX_HOME": filepath.Join(s.dir, "custom-codex"), "GROK_HOME": filepath.Join(s.dir, "custom-grok")}
	if got, _ := s.resolve(harness.Authentication{}, env, ""); got.Provider != "codex" {
		t.Fatalf("provider %q", got.Provider)
	}
	_ = os.Remove(filepath.Join(s.dir, "custom-codex/auth.json"))
	if got, _ := s.resolve(harness.Authentication{}, env, ""); got.Provider != "grok" {
		t.Fatalf("provider %q", got.Provider)
	}
}

func TestFxSubscriptionPinsProviderOnResume(t *testing.T) {
	s := newSubTest(t)
	putDoc(t, s.dir, ".fx/chatgpt-auth.json", fxCredentialsDoc("codex"))
	putDoc(t, s.dir, ".fx/grok-auth.json", fxCredentialsDoc("grok"))
	gw := map[string]string{"AI_GATEWAY_API_KEY": "gateway"}
	if got, _ := s.resolve(harness.Authentication{}, gw, "grok"); got.Provider != "grok" {
		t.Fatalf("provider %q", got.Provider)
	}
	if got, _ := s.resolve(harness.AuthMode("direct"), gw, "gateway"); got.Provider != "gateway" {
		t.Fatalf("provider %q", got.Provider)
	}
	if _, err := s.resolve(harness.AuthMode("ai-gateway"), nil, "codex"); err == nil || !strings.Contains(err.Error(), "does not allow") {
		t.Fatalf("err = %v", err)
	}
	_ = os.Remove(filepath.Join(s.dir, ".fx/chatgpt-auth.json"))
	if _, err := s.resolve(harness.Authentication{}, gw, "codex"); err == nil || !strings.Contains(err.Error(), "no codex subscription") {
		t.Fatalf("err = %v", err)
	}
}

func TestFxSubscriptionRereadsAndUpdatesFxStore(t *testing.T) {
	for _, name := range []string{"chatgpt", "grok"} {
		t.Run(name, func(t *testing.T) {
			s := newSubTest(t)
			file := ".fx/" + name + "-auth.json"
			doc := fxCredentialsDoc("initial")
			doc["extra"] = true
			putDoc(t, s.dir, file, doc)
			sub := s.mustSubscription(s.resolve(harness.Authentication{}, nil, ""))
			// A document in fx's own key order, which the write keeps.
			if err := os.WriteFile(filepath.Join(s.dir, file), []byte(`{"version":1,"access_token":"external-rotation","refresh_token":"refresh","account_id":"account","expires_at_ms":2000000000000,"extra":true}`), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := s.readToken(sub); got != "external-rotation" {
				t.Fatalf("read %q", got)
			}
			if err := sub.write(context.Background(), libfx.Credentials{AccessToken: "rotated", RefreshToken: "rotated-refresh", AccountID: "account", ExpiresAt: subExpiresAt + 1000}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.dir, file)
			got := readDoc(t, path)
			want := map[string]any{"access_token": "rotated", "account_id": "account", "expires_at_ms": float64(2000000001000), "extra": true, "refresh_token": "rotated-refresh", "version": float64(1)}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("saved %v", got)
			}
			raw, _ := os.ReadFile(path)
			if !strings.HasPrefix(string(raw), "{\n  \"version\": 1,\n  \"access_token\": \"rotated\",") || !strings.HasSuffix(string(raw), "}\n") {
				t.Fatalf("saved text keeps neither the key order nor JSON.stringify's layout:\n%s", raw)
			}
			if runtime.GOOS != "windows" {
				info, _ := os.Stat(path)
				if info.Mode().Perm() != 0o600 {
					t.Fatalf("mode %v", info.Mode().Perm())
				}
			}
			entries, _ := os.ReadDir(filepath.Join(s.dir, ".fx"))
			if len(entries) != 1 || entries[0].Name() != name+"-auth.json" {
				t.Fatalf("directory holds %v", entries)
			}
			_ = os.Remove(path)
			if _, err := sub.read(context.Background()); err == nil || !strings.Contains(err.Error(), "no longer available") {
				t.Fatalf("err = %v", err)
			}
			if err := sub.write(context.Background(), libfx.Credentials{AccessToken: "lost"}); err == nil || !strings.Contains(err.Error(), "Cannot update") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFxSubscriptionPreservesNativeCodexMetadata(t *testing.T) {
	s := newSubTest(t)
	claims, _ := json.Marshal(map[string]any{"exp": 2_000_000_000})
	token := "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	doc := codexCredentialsDoc()
	doc["tokens"].(map[string]any)["access_token"] = token
	putDoc(t, s.dir, ".codex/auth.json", doc)
	sub := s.mustSubscription(s.resolve(harness.Authentication{}, nil, ""))
	c, err := sub.read(context.Background())
	if err != nil || c.ExpiresAt != subExpiresAt {
		t.Fatalf("%v %v", c, err)
	}
	if err := sub.write(context.Background(), libfx.Credentials{AccessToken: "rotated"}); err != nil {
		t.Fatal(err)
	}
	saved := readDoc(t, filepath.Join(s.dir, ".codex/auth.json"))
	if want := map[string]any{"access_token": "rotated", "account_id": "account", "id_token": "preserve-id", "refresh_token": "refresh"}; !reflect.DeepEqual(saved["tokens"], want) {
		t.Fatalf("tokens %v", saved["tokens"])
	}
	if saved["extra"] != true {
		t.Fatal("extra lost")
	}
	if _, err := time.Parse(time.RFC3339, saved["last_refresh"].(string)); err != nil {
		t.Fatalf("last_refresh %v", saved["last_refresh"])
	}
}

func TestFxSubscriptionGrokExpiryFormats(t *testing.T) {
	for _, expiry := range []any{isoString(subExpiresAt), subExpiresAt, subExpiresAt / 1000} {
		s := newSubTest(t)
		doc := grokCredentialsDoc()
		doc[grokCredentialScope].(map[string]any)["expires_at"] = expiry
		putDoc(t, s.dir, ".grok/auth.json", doc)
		sub := s.mustSubscription(s.resolve(harness.Authentication{}, nil, ""))
		c, err := sub.read(context.Background())
		if err != nil || c != (libfx.Credentials{AccessToken: "native-grok", RefreshToken: "refresh", AccountID: "account", ExpiresAt: subExpiresAt}) {
			t.Fatalf("%v: %v %v", expiry, c, err)
		}
		if err := sub.write(context.Background(), libfx.Credentials{AccessToken: "rotated", ExpiresAt: subExpiresAt + 1000}); err != nil {
			t.Fatal(err)
		}
		saved := readDoc(t, filepath.Join(s.dir, ".grok/auth.json"))
		want := readDocValue(t, grokCredentialsDoc())
		scope := want[grokCredentialScope].(map[string]any)
		scope["key"] = "rotated"
		scope["expires_at"] = isoString(subExpiresAt + 1000)
		if !reflect.DeepEqual(saved, want) {
			t.Fatalf("%v: saved %v", expiry, saved)
		}
	}
}

func readDocValue(t *testing.T, v any) map[string]any {
	b, _ := json.Marshal(v)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFxSubscriptionDoesNotFallBackAfterSelectedStoreDisappears(t *testing.T) {
	s := newSubTest(t)
	putDoc(t, s.dir, ".fx/chatgpt-auth.json", fxCredentialsDoc("selected"))
	putDoc(t, s.dir, ".fx/grok-auth.json", fxCredentialsDoc("fallback"))
	sub := s.mustSubscription(s.resolve(harness.Authentication{}, nil, ""))
	_ = os.Remove(filepath.Join(s.dir, ".fx/chatgpt-auth.json"))
	if _, err := sub.read(context.Background()); err == nil || !strings.Contains(err.Error(), "no longer available") {
		t.Fatalf("err = %v", err)
	}
}

func codexKeyringAccount(t *testing.T, codexHome string) string {
	real, err := filepath.EvalSymlinks(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(real))
	return "cli|" + hex.EncodeToString(sum[:])[:16]
}

func TestFxSubscriptionNativeCodexStoreModes(t *testing.T) {
	for _, mode := range []string{"file", "keyring", "auto", "ephemeral"} {
		t.Run(mode, func(t *testing.T) {
			s := newSubTest(t)
			putDoc(t, s.dir, ".codex/auth.json", codexCredentialsDoc())
			if err := os.WriteFile(filepath.Join(s.dir, ".codex/config.toml"), []byte(`cli_auth_credentials_store = "`+mode+`"`+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			doc := codexCredentialsDoc()
			doc["tokens"].(map[string]any)["access_token"] = "keyring"
			stored, _ := json.Marshal(doc)
			kr := &fakeKeyring{stored: string(stored)}
			s.keyring = kr
			if mode == "ephemeral" {
				if _, err := s.resolve(harness.Authentication{}, nil, ""); err == nil || !strings.Contains(err.Error(), "No subscription credentials") {
					t.Fatalf("err = %v", err)
				}
				if len(kr.calls) != 0 {
					t.Fatal("ephemeral mode read the keyring")
				}
				return
			}
			sub := s.mustSubscription(s.resolve(harness.Authentication{}, nil, ""))
			want := "keyring"
			if mode == "file" {
				want = "native-codex"
			}
			if got := s.readToken(sub); got != want {
				t.Fatalf("read %q, want %q", got, want)
			}
			if err := sub.write(context.Background(), libfx.Credentials{AccessToken: "rotated"}); err != nil {
				t.Fatal(err)
			}
			if mode == "file" {
				if len(kr.calls) != 0 {
					t.Fatal("file mode touched the keyring")
				}
				return
			}
			account := codexKeyringAccount(t, filepath.Join(s.dir, ".codex"))
			for _, c := range kr.calls {
				if c.service != "Codex Auth" || c.account != account {
					t.Fatalf("keyring call %+v, want only Codex Auth / %s", c, account)
				}
			}
			var saved map[string]any
			_ = json.Unmarshal([]byte(kr.stored), &saved)
			if saved["tokens"].(map[string]any)["access_token"] != "rotated" {
				t.Fatalf("keyring holds %s", kr.stored)
			}
			if readDoc(t, filepath.Join(s.dir, ".codex/auth.json"))["tokens"].(map[string]any)["access_token"] != "native-codex" {
				t.Fatal("the file changed in keyring mode")
			}
			kr.failNext = errors.New("write failed")
			if err := sub.write(context.Background(), libfx.Credentials{AccessToken: "failure"}); err == nil || err.Error() != "write failed" {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFxSubscriptionAutoModeFallsBackToFile(t *testing.T) {
	s := newSubTest(t)
	putDoc(t, s.dir, ".codex/auth.json", codexCredentialsDoc())
	if err := os.WriteFile(filepath.Join(s.dir, ".codex/config.toml"), []byte(`cli_auth_credentials_store = "auto"`), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := s.mustSubscription(s.resolve(harness.Authentication{}, nil, ""))
	if got := s.readToken(sub); got != "native-codex" {
		t.Fatalf("read %q", got)
	}
}

func TestFxSubscriptionCodexConfigStopsAtFirstTable(t *testing.T) {
	s := newSubTest(t)
	putDoc(t, s.dir, ".codex/auth.json", codexCredentialsDoc())
	config := "model = \"x\"\n\ufeff[profile]\ncli_auth_credentials_store = \"ephemeral\"\n"
	if err := os.WriteFile(filepath.Join(s.dir, ".codex/config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.resolve(harness.Authentication{}, nil, ""); err != nil || got.Provider != "codex" {
		t.Fatalf("%+v %v", got, err)
	}
}

// TestOSKeyringTouchesOnlyCodexEntry checks the exact commands the macOS,
// Linux and Windows keyrings run: they read and write only Codex's entry,
// the value never appears in a command argument, and a failed write does
// not surface the program's output.
func TestOSKeyringTouchesOnlyCodexEntry(t *testing.T) {
	origExec, origInput := subscription.ExecFile, subscription.ExecFileInput
	t.Cleanup(func() { subscription.ExecFile, subscription.ExecFileInput = origExec, origInput })
	type call struct {
		name  string
		args  []string
		input string
		env   []string
	}
	var calls []call
	subscription.ExecFile = func(_ context.Context, name string, args []string, env []string) (string, error) {
		calls = append(calls, call{name: name, args: args})
		return `{"auth_mode":"chatgpt","tokens":{"access_token":"k"}}` + "\n", nil
	}
	fail := false
	subscription.ExecFileInput = func(_ context.Context, name string, args []string, input string, env []string) error {
		calls = append(calls, call{name: name, args: args, input: input, env: env})
		if fail {
			return errors.New("stderr echoes secret-value")
		}
		return nil
	}
	account := "cli|0123456789abcdef"
	for _, goos := range []string{"darwin", "linux", "windows"} {
		calls = nil
		k := osCodexKeyring(goos)
		got, err := k.read(context.Background(), codexKeyringService, account)
		if err != nil || !strings.Contains(got, `"access_token":"k"`) {
			t.Fatalf("%s read %q %v", goos, got, err)
		}
		if err := k.write(context.Background(), codexKeyringService, account, "secret-value"); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 2 {
			t.Fatalf("%s calls %+v", goos, calls)
		}
		read, write := calls[0], calls[1]
		switch goos {
		case "darwin":
			if read.name != "/usr/bin/security" || strings.Join(read.args, " ") != "find-generic-password -s Codex Auth -a "+account+" -w" {
				t.Errorf("read %+v", read)
			}
			if write.name != "/usr/bin/security" || strings.Join(write.args, " ") != "-i" ||
				write.input != `add-generic-password -U -s "Codex Auth" -a "`+account+`" -X `+hex.EncodeToString([]byte("secret-value"))+"\n" {
				t.Errorf("write %+v", write)
			}
		case "linux":
			if read.name != "secret-tool" || strings.Join(read.args, ",") != "lookup,service,Codex Auth,username,"+account+",target,default" {
				t.Errorf("read %+v", read)
			}
			if write.name != "secret-tool" || write.input != "secret-value" || strings.Join(write.args, ",") != "store,--label,"+account+"@Codex Auth:default,service,Codex Auth,username,"+account+",target,default,application,rust-keyring" {
				t.Errorf("write %+v", write)
			}
		case "windows":
			if read.name != "powershell.exe" || write.name != "powershell.exe" || write.input != "secret-value" {
				t.Errorf("calls %+v", calls)
			}
			if !containsEnv(write.env, "AI_SDK_CODEX_CREDENTIAL_TARGET="+account+".Codex Auth") || !containsEnv(write.env, "AI_SDK_CODEX_CREDENTIAL_USER="+account) {
				t.Errorf("write env %v", write.env)
			}
		}
		for _, c := range calls {
			if strings.Contains(strings.Join(c.args, " "), "secret-value") {
				t.Errorf("%s: the value appears in the arguments: %v", goos, c.args)
			}
		}
		fail = true
		if err := k.write(context.Background(), codexKeyringService, account, "secret-value"); err == nil || err.Error() != "Unable to update the Codex credential store." {
			t.Errorf("%s failed write = %v", goos, err)
		}
		fail = false
	}
	// A program that cannot be started is an access failure, as in TS.
	subscription.ExecFileInput = origInput
	// Never run a real credential program here: macOS's is an absolute
	// path, so that case only runs off macOS, and PATH holds nothing.
	t.Setenv("PATH", t.TempDir())
	for _, goos := range []string{"darwin", "linux", "windows"} {
		if goos == "darwin" && runtime.GOOS == "darwin" {
			continue
		}
		if _, statErr := os.Stat("/usr/bin/security"); goos == "darwin" && statErr == nil {
			continue
		}
		err := osKeyring{goos}.write(context.Background(), codexKeyringService, account, "secret-value")
		if err == nil || err.Error() != "Unable to access the Codex credential store." {
			t.Errorf("%s write without the program = %v", goos, err)
		}
	}
	if osCodexKeyring("plan9") != nil {
		t.Error("an unsupported OS has a keyring")
	}
}

func containsEnv(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

// TS credentialsFromDocument reads a Grok `expires_at` string with
// Date.parse and a number as seconds below 1e10, else milliseconds.
func TestGrokCredentialExpiresAtFormats(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{`"2026-10-27T10:00:00.000Z"`, 1793095200000},
		{`"2026-10-27T10:00:00+0200"`, 1793088000000},
		{`"2026-10-27"`, 1793059200000},
		{`"Tue, 27 Oct 2026 10:00:00 GMT"`, 1793095200000},
		{`"Oct 27 2026 10:00 GMT+05:30"`, 1793075400000},
		{`"not a date"`, 0},
		{`1793095200`, 1793095200000},
		{`1793095200123`, 1793095200123},
	}
	for _, c := range cases {
		doc := parseCredentialDocument([]byte(`{"` + grokCredentialScope + `":{"key":"k","refresh_token":"r","user_id":"u","expires_at":` + c.raw + `}}`))
		cred := credentialsFromDocument(doc, formatGrok)
		if cred == nil || cred.ExpiresAt != c.want {
			t.Errorf("expires_at %s: got %+v, want %d", c.raw, cred, c.want)
		}
	}
}
