package fx

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil/subscription"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

// Port of TS fx-subscription.ts (resolveFxAuthentication): how the libfx
// runtime finds a codex or grok subscription, and the stores it reads and
// writes rotated credentials through. RuntimeSandboxACP keeps the v0.5.x
// readers in subscription.go.
//
// Discovery order: fx Codex (~/.fx/chatgpt-auth.json), fx Grok
// (~/.fx/grok-auth.json), native Codex ($CODEX_HOME/auth.json or the OS
// keyring, as Codex's cli_auth_credentials_store selects), native Grok
// ($GROK_HOME/auth.json). Credentials are never logged; they reach the host
// fetch edge only.

const (
	grokCredentialScope = "https://auth.x.ai::b1a00492-073a-47ea-816f-4c329264a828"
	codexKeyringService = "Codex Auth"
)

// fxSubscription is one selected credential store (TS FxSubscription).
type fxSubscription struct {
	provider string
	read     func(ctx context.Context) (libfx.Credentials, error)
	write    func(ctx context.Context, c libfx.Credentials) error
}

// credentialStore reads and writes one credential document.
type credentialStore struct {
	read  func(ctx context.Context) (*jsonorder.Object, error)
	write func(ctx context.Context, value *jsonorder.Object) error
}

// codexKeyring is the OS credential store Codex uses. Implementations only
// ever touch the entry the caller names: service "Codex Auth" and the
// account Codex derives from its home.
type codexKeyring interface {
	read(ctx context.Context, service, account string) (string, error)
	write(ctx context.Context, service, account, value string) error
}

type credentialFormat string

const (
	formatFx    credentialFormat = "fx"
	formatCodex credentialFormat = "codex"
	formatGrok  credentialFormat = "grok"
)

type resolveAuthOptions struct {
	Auth             AuthenticationMode
	Env              map[string]string
	HomeDirectory    string
	GOOS             string
	RequiredProvider string
	Keyring          codexKeyring
	Now              func() time.Time
}

var codexStoreMode = regexp.MustCompile(`^\s*cli_auth_credentials_store\s*=\s*["'](file|keyring|auto|ephemeral)["']\s*(?:#.*)?$`)

// resolveFxAuthentication mirrors TS resolveFxAuthentication.
func resolveFxAuthentication(ctx context.Context, o resolveAuthOptions) (fxAuthentication, error) {
	env := o.Env
	if env == nil {
		env = map[string]string{}
	}
	environment := harnessutil.AuthenticationEnvironment(o.Auth, env)
	gateway := harnessutil.GetAIGatewayAuthFromEnv(environment)
	isolated := harnessutil.IsAuthenticationEnvironment(o.Auth)
	required := o.RequiredProvider
	nativeRequired := required != "" && required != "gateway"
	if nativeRequired && (isolated || o.Auth.Mode == harness.AuthModeAIGateway) {
		return fxAuthentication{}, errors.New("Cannot resume fx: authentication does not allow the saved subscription provider.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	mode := o.Auth.Mode
	if nativeRequired {
		mode = harness.AuthModeDirect
	}
	gatewayEnv := map[string]string{}
	if gateway.APIKey != "" {
		gatewayEnv["AI_GATEWAY_API_KEY"] = gateway.APIKey
	}
	if !isolated && required != "gateway" && harnessutil.ShouldResolveNativeSubscription(mode, gatewayEnv, false) {
		home := o.HomeDirectory
		if home == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return fxAuthentication{}, err
			}
			home = h
		}
		now := o.Now
		if now == nil {
			now = time.Now
		}
		bind := func(provider string, format credentialFormat, store credentialStore) (fxAuthentication, bool, error) {
			sub, err := bindSubscription(ctx, provider, format, store, now)
			if err != nil || sub == nil {
				return fxAuthentication{}, false, err
			}
			return fxAuthentication{Provider: provider, Subscription: sub}, true, nil
		}
		for _, provider := range []string{"codex", "grok"} {
			if required != "" && provider != required {
				continue
			}
			name := "grok-auth.json"
			if provider == "codex" {
				name = "chatgpt-auth.json"
			}
			if a, ok, err := bind(provider, formatFx, fileStore(filepath.Join(home, ".fx", name))); err != nil || ok {
				return a, err
			}
		}
		if required == "" || required == "codex" {
			codexHome, err := resolveHome(env, "CODEX_HOME", filepath.Join(home, ".codex"))
			if err != nil {
				return fxAuthentication{}, err
			}
			storeMode := "file"
			config, _ := os.ReadFile(filepath.Join(codexHome, "config.toml"))
			for _, line := range regexp.MustCompile(`\r?\n`).Split(string(config), -1) {
				if strings.HasPrefix(strings.TrimLeftFunc(line, jsSpace), "[") {
					break
				}
				if m := codexStoreMode.FindStringSubmatch(line); m != nil {
					storeMode = m[1]
					break
				}
			}
			if storeMode == "keyring" || storeMode == "auto" {
				keyring := o.Keyring
				if keyring == nil {
					goos := o.GOOS
					if goos == "" {
						goos = runtime.GOOS
					}
					keyring = osCodexKeyring(goos)
				}
				if keyring != nil {
					canonical := codexHome
					if real, err := filepath.EvalSymlinks(codexHome); err == nil {
						canonical = real
					}
					sum := sha256.Sum256([]byte(canonical))
					if a, ok, err := bind("codex", formatCodex, keyringStore(keyring, "cli|"+hex.EncodeToString(sum[:])[:16])); err != nil || ok {
						return a, err
					}
				}
			}
			if storeMode == "file" || storeMode == "auto" {
				if a, ok, err := bind("codex", formatCodex, fileStore(filepath.Join(codexHome, "auth.json"))); err != nil || ok {
					return a, err
				}
			}
		}
		if required == "" || required == "grok" {
			grokHome, err := resolveHome(env, "GROK_HOME", filepath.Join(home, ".grok"))
			if err != nil {
				return fxAuthentication{}, err
			}
			if a, ok, err := bind("grok", formatGrok, fileStore(filepath.Join(grokHome, "auth.json"))); err != nil || ok {
				return a, err
			}
		}
	}
	if nativeRequired {
		return fxAuthentication{}, fmt.Errorf("Cannot resume fx: no %s subscription credentials are available.", required) //nolint:staticcheck // matches TS SDK's exact error text
	}
	if gateway.APIKey == "" {
		return fxAuthentication{}, errors.New(errNoGatewayCredential) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return fxAuthentication{Provider: "gateway", APIKey: gateway.APIKey, BaseURL: gateway.BaseURL}, nil
}

// jsSpace is the set String.prototype.trimStart removes.
func jsSpace(r rune) bool { return unicode.IsSpace(r) || r == 0xFEFF }

// resolveHome is path.resolve(env[name] ?? fallback).
func resolveHome(env map[string]string, name, fallback string) (string, error) {
	p := fallback
	if v, ok := env[name]; ok {
		p = v
	}
	return filepath.Abs(p)
}

func bindSubscription(ctx context.Context, provider string, format credentialFormat, store credentialStore, now func() time.Time) (*fxSubscription, error) {
	value, err := store.read(ctx)
	if err != nil {
		return nil, err
	}
	if value == nil || credentialsFromDocument(value, format) == nil {
		return nil, nil
	}
	return &fxSubscription{
		provider: provider,
		read: func(ctx context.Context) (libfx.Credentials, error) {
			value, err := store.read(ctx)
			if err != nil {
				return libfx.Credentials{}, err
			}
			var c *libfx.Credentials
			if value != nil {
				c = credentialsFromDocument(value, format)
			}
			if c == nil {
				return libfx.Credentials{}, fmt.Errorf("The selected fx %s subscription credentials are no longer available.", provider) //nolint:staticcheck // matches TS SDK's exact error text
			}
			return *c, nil
		},
		write: func(ctx context.Context, c libfx.Credentials) error {
			value, err := store.read(ctx)
			if err != nil {
				return err
			}
			if value == nil || credentialsFromDocument(value, format) == nil {
				return errors.New("Cannot update the selected fx subscription credential store.") //nolint:staticcheck // matches TS SDK's exact error text
			}
			next := copyObject(credentialRecord(value, format))
			accessKey, accountKey := "access_token", "account_id"
			if format == formatGrok {
				accessKey, accountKey = "key", "user_id"
			}
			next.Set(accessKey, c.AccessToken)
			if c.RefreshToken != "" {
				next.Set("refresh_token", c.RefreshToken)
			}
			if c.AccountID != "" {
				next.Set(accountKey, c.AccountID)
			}
			if format == formatFx && c.ExpiresAt != 0 {
				next.Set("expires_at_ms", json.Number(fmt.Sprint(c.ExpiresAt)))
			}
			if format == formatGrok && c.ExpiresAt != 0 {
				next.Set("expires_at", isoString(c.ExpiresAt))
			}
			var doc *jsonorder.Object
			switch format {
			case formatFx:
				doc = next
			case formatCodex:
				doc = copyObject(value)
				doc.Set("tokens", next)
				doc.Set("last_refresh", isoString(now().UnixMilli()))
			default:
				doc = copyObject(value)
				doc.Set(grokCredentialScope, next)
			}
			return store.write(ctx, doc)
		},
	}, nil
}

// isoString is Date.prototype.toISOString.
func isoString(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

func copyObject(o *jsonorder.Object) *jsonorder.Object {
	out := jsonorder.New()
	if o == nil {
		return out
	}
	for _, k := range o.Keys() {
		v, _ := o.Get(k)
		out.Set(k, v)
	}
	return out
}

func objectField(o *jsonorder.Object, key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	return o.Get(key)
}

func credentialRecord(value *jsonorder.Object, format credentialFormat) *jsonorder.Object {
	if format == formatFx {
		if v, _ := objectField(value, "version"); numberValue(v) == 1 {
			return value
		}
		return nil
	}
	var record any
	if format == formatCodex {
		if mode, _ := objectField(value, "auth_mode"); mode == "chatgpt" {
			record, _ = objectField(value, "tokens")
		}
	} else {
		record, _ = objectField(value, grokCredentialScope)
	}
	obj, _ := record.(*jsonorder.Object)
	return obj
}

// numberValue returns a JSON number's value, or NaN.
func numberValue(v any) float64 {
	n, ok := v.(json.Number)
	if !ok {
		return math.NaN()
	}
	f, err := n.Float64()
	if err != nil {
		return math.NaN()
	}
	return f
}

func nonEmptyString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok && s != ""
}

func credentialsFromDocument(value *jsonorder.Object, format credentialFormat) *libfx.Credentials {
	record := credentialRecord(value, format)
	if record == nil {
		return nil
	}
	accessKey, accountKey := "access_token", "account_id"
	if format == formatGrok {
		accessKey, accountKey = "key", "user_id"
	}
	rawAccess, _ := record.Get(accessKey)
	access, ok := nonEmptyString(rawAccess)
	if !ok {
		return nil
	}
	rawRefresh, _ := record.Get("refresh_token")
	refresh, hasRefresh := nonEmptyString(rawRefresh)
	rawAccount, _ := record.Get(accountKey)
	account, hasAccount := nonEmptyString(rawAccount)
	var expiresAt int64
	switch format {
	case formatFx:
		raw, _ := record.Get("expires_at_ms")
		exp := numberValue(raw)
		if !hasRefresh || !hasAccount || math.IsNaN(exp) || math.IsInf(exp, 0) {
			return nil
		}
		expiresAt = int64(exp)
	case formatCodex:
		if exp, ok := subscription.GetJWTExpiresAt(access); ok {
			expiresAt = exp
		}
	default:
		raw, _ := record.Get("expires_at")
		switch v := raw.(type) {
		case string:
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				expiresAt = t.UnixMilli()
			}
		case json.Number:
			if f := numberValue(v); !math.IsNaN(f) && !math.IsInf(f, 0) {
				if f < 10_000_000_000 {
					f *= 1000
				}
				expiresAt = int64(f)
			}
		}
	}
	c := &libfx.Credentials{AccessToken: access, ExpiresAt: expiresAt}
	if hasRefresh {
		c.RefreshToken = refresh
	}
	if hasAccount {
		c.AccountID = account
	}
	return c
}

// parseCredentialDocument parses a JSON object, or returns nil.
func parseCredentialDocument(text []byte) *jsonorder.Object {
	if securejson.Check(text) != nil {
		return nil
	}
	v, ok := jsonorder.Parse(text)
	if !ok {
		return nil
	}
	obj, _ := v.(*jsonorder.Object)
	return obj
}

// credentialDocumentText is JSON.stringify(value, null, 2) + "\n".
func credentialDocumentText(value *jsonorder.Object) []byte {
	compact := jsonorder.Stringify(value)
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(compact), "", "  "); err != nil {
		return []byte(compact + "\n")
	}
	out.WriteByte('\n')
	return out.Bytes()
}

// fileStore reads a JSON credential file and replaces it atomically, 0600.
func fileStore(path string) credentialStore {
	return credentialStore{
		read: func(context.Context) (*jsonorder.Object, error) {
			text, err := os.ReadFile(path)
			if err != nil {
				return nil, nil
			}
			return parseCredentialDocument(text), nil
		},
		write: func(_ context.Context, value *jsonorder.Object) error {
			var b [16]byte
			_, _ = rand.Read(b[:])
			tmp := path + "." + hex.EncodeToString(b[:]) + ".tmp"
			defer os.Remove(tmp) //nolint:errcheck
			f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			if _, err := f.Write(credentialDocumentText(value)); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if err := os.Chmod(tmp, 0o600); err != nil {
				return err
			}
			return os.Rename(tmp, path)
		},
	}
}

func keyringStore(keyring codexKeyring, account string) credentialStore {
	return credentialStore{
		read: func(ctx context.Context) (*jsonorder.Object, error) {
			text, err := keyring.read(ctx, codexKeyringService, account)
			if err != nil || text == "" {
				return nil, nil
			}
			return parseCredentialDocument([]byte(text)), nil
		},
		write: func(ctx context.Context, value *jsonorder.Object) error {
			return keyring.write(ctx, codexKeyringService, account, jsonorder.Stringify(value))
		},
	}
}

// osKeyring reads and writes Codex's entry in the OS credential store
// through the same programs TS uses: /usr/bin/security on macOS,
// secret-tool on Linux and the Windows Credential Manager.
type osKeyring struct{ goos string }

func osCodexKeyring(goos string) codexKeyring {
	if harnessutil.IsMacOS(goos) || harnessutil.IsLinux(goos) || harnessutil.IsWindows(goos) {
		return osKeyring{goos}
	}
	return nil
}

func (k osKeyring) read(ctx context.Context, service, account string) (string, error) {
	switch {
	case harnessutil.IsMacOS(k.goos):
		return subscription.ReadMacOSKeychainPassword(ctx, service, account), nil
	case harnessutil.IsLinux(k.goos):
		return subscription.ReadLinuxSecretServicePassword(ctx, []subscription.SecretServiceAttribute{
			{Name: "service", Value: service}, {Name: "username", Value: account}, {Name: "target", Value: "default"},
		}), nil
	default:
		return subscription.ReadWindowsCredentialManagerPassword(ctx, account+"."+service), nil
	}
}

// write never surfaces the program's output, which could quote the value.
func (k osKeyring) write(ctx context.Context, service, account, value string) error {
	var err error
	switch {
	case harnessutil.IsMacOS(k.goos):
		err = subscription.WriteMacOSKeychainPassword(ctx, service, account, value)
	case harnessutil.IsLinux(k.goos):
		err = subscription.WriteLinuxSecretServicePassword(ctx, service, account, value)
	default:
		err = subscription.WriteWindowsCredentialManagerPassword(ctx, account+"."+service, account, value)
	}
	if err != nil {
		return errors.New("Unable to update the Codex credential store.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	return nil
}
