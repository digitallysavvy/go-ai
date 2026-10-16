// Package subscription ports the native-subscription helpers of
// `@ai-sdk/harness/utils` (cdc12a1): JWT payload parsing and OS credential
// store readers (macOS Keychain, Linux Secret Service, Windows Credential
// Manager). These are the only host-side process executions of the harness;
// adapters call them only when harnessutil.ShouldResolveNativeSubscription
// allows it.
package subscription

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// ExecFileFunc runs a program with arguments (no shell) and returns stdout.
// env == nil inherits the host environment.
type ExecFileFunc func(ctx context.Context, name string, args []string, env []string) (string, error)

// MaxBuffer is Node's default `execFile` `maxBuffer`: a child whose stdout
// exceeds it is killed and the call fails.
const MaxBuffer = 1024 * 1024

// CappedBuffer collects up to Limit bytes and then fails the write, calling
// OnOverflow (to kill the child) once. Mirrors Node's `maxBuffer` handling.
type CappedBuffer struct {
	Limit      int
	OnOverflow func()
	buf        bytes.Buffer
}

// ErrMaxBuffer is returned once a child's output exceeds MaxBuffer.
var ErrMaxBuffer = errors.New("stdout maxBuffer length exceeded")

func (b *CappedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.Limit {
		if b.OnOverflow != nil {
			b.OnOverflow()
		}
		return 0, ErrMaxBuffer
	}
	return b.buf.Write(p)
}

// String returns the collected output.
func (b *CappedBuffer) String() string { return b.buf.String() }

// RunCapped runs the command newCmd builds (it must use the ctx it is given
// with exec.CommandContext) and collects at most MaxBuffer bytes of stdout. A
// child that writes more is killed and the call fails, like Node's execFile.
func RunCapped(ctx context.Context, newCmd func(ctx context.Context) *exec.Cmd) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := newCmd(ctx)
	stdout := &CappedBuffer{Limit: MaxBuffer, OnOverflow: cancel}
	cmd.Stdout = stdout
	err := cmd.Run()
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	return stdout.String(), err
}

// ExecFile is the process runner used by the readers; replaceable in tests.
var ExecFile ExecFileFunc = func(ctx context.Context, name string, args []string, env []string) (string, error) {
	return RunCapped(ctx, func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, name, args...)
		if env != nil {
			cmd.Env = env
		}
		return cmd
	})
}

// ParseJWTPayload decodes the payload of a three-segment JWT. It returns nil
// when the token is malformed or the payload is not a JSON object. Mirrors TS
// `parseJwtPayload`.
func ParseJWTPayload(token string) map[string]any {
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		return nil
	}
	raw := decodeNodeBase64(segments[1])
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload
}

// decodeNodeBase64 decodes like Node's Buffer.from(text, 'base64url'): both
// the URL-safe and standard alphabets are accepted, characters outside them
// (whitespace, junk) are skipped, decoding stops at the first '=' and a
// dangling final sextet is dropped. Never fails.
func decodeNodeBase64(text string) []byte {
	out := make([]byte, 0, len(text)*3/4)
	var acc uint32
	bits := 0
	for i := 0; i < len(text); i++ {
		var v byte
		switch c := text[i]; {
		case c >= 'A' && c <= 'Z':
			v = c - 'A'
		case c >= 'a' && c <= 'z':
			v = c - 'a' + 26
		case c >= '0' && c <= '9':
			v = c - '0' + 52
		case c == '-' || c == '+':
			v = 62
		case c == '_' || c == '/':
			v = 63
		case c == '=':
			return out
		default:
			continue
		}
		acc = acc<<6 | uint32(v)
		bits += 6
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(acc>>uint(bits)))
			acc &= 1<<uint(bits) - 1
		}
	}
	return out
}

// GetJWTExpiresAt returns the `exp` claim converted to epoch milliseconds.
// Mirrors TS `getJwtExpiresAt`.
func GetJWTExpiresAt(token string) (int64, bool) {
	payload := ParseJWTPayload(token)
	exp, ok := payload["exp"].(float64)
	if !ok || math.IsInf(exp, 0) || math.IsNaN(exp) {
		return 0, false
	}
	return int64(exp * 1000), true
}

// ReadMacOSKeychainPassword reads a generic password with
// `/usr/bin/security find-generic-password -s <service> -a <account> -w`.
// Output is trimmed; "" means not found. Mirrors TS
// `readMacOSKeychainPassword`.
func ReadMacOSKeychainPassword(ctx context.Context, service, account string) string {
	out, err := ExecFile(ctx, "/usr/bin/security", []string{"find-generic-password", "-s", service, "-a", account, "-w"}, nil)
	if err != nil {
		return ""
	}
	return providerutils.JSTrim(out)
}

// SecretServiceAttribute is one `secret-tool lookup` attribute/value pair.
// A slice keeps the argument order deterministic (TS iterates object entries
// in insertion order).
type SecretServiceAttribute struct {
	Name  string
	Value string
}

// ReadLinuxSecretServicePassword runs `secret-tool lookup <attr> <value>...`.
// Output is returned untrimmed; "" means not found. Mirrors TS
// `readLinuxSecretServicePassword`.
func ReadLinuxSecretServicePassword(ctx context.Context, attributes []SecretServiceAttribute) string {
	args := []string{"lookup"}
	for _, a := range attributes {
		args = append(args, a.Name, a.Value)
	}
	out, err := ExecFile(ctx, "secret-tool", args, nil)
	if err != nil {
		return ""
	}
	return out
}

// windowsCredentialManagerSource is the C# helper TS passes to Add-Type.
const windowsCredentialManagerSource = `
using System;
using System.Runtime.InteropServices;
using System.Text;
public static class AISDKCredentialManager {
  [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
  public struct Credential {
    public UInt32 Flags; public UInt32 Type; public string TargetName;
    public string Comment; public System.Runtime.InteropServices.ComTypes.FILETIME LastWritten;
    public UInt32 CredentialBlobSize; public IntPtr CredentialBlob;
    public UInt32 Persist; public UInt32 AttributeCount; public IntPtr Attributes;
    public string TargetAlias; public string UserName;
  }
  [DllImport("advapi32.dll", EntryPoint = "CredReadW", CharSet = CharSet.Unicode, SetLastError = true)]
  static extern bool CredRead(string target, UInt32 type, UInt32 flags, out IntPtr credential);
  [DllImport("advapi32.dll", SetLastError = true)] static extern void CredFree(IntPtr credential);
  public static string Read(string target) {
    IntPtr pointer;
    if (!CredRead(target, 1, 0, out pointer)) return null;
    try {
      Credential value = Marshal.PtrToStructure<Credential>(pointer);
      byte[] bytes = new byte[value.CredentialBlobSize];
      Marshal.Copy(value.CredentialBlob, bytes, 0, bytes.Length);
      return Encoding.Unicode.GetString(bytes);
    } finally { CredFree(pointer); }
  }
}`

const windowsCredentialManagerCommand = `Add-Type -TypeDefinition $env:AI_SDK_WINDOWS_CREDENTIAL_MANAGER_SOURCE; $value = [AISDKCredentialManager]::Read($env:AI_SDK_WINDOWS_CREDENTIAL_MANAGER_TARGET); if ($null -ne $value) { [Console]::Out.Write($value) }`

// ReadWindowsCredentialManagerPassword reads a generic credential via
// PowerShell + CredReadW. The source and target are passed through the
// environment (never interpolated into the command). "" means not found.
// Mirrors TS `readWindowsCredentialManagerPassword`.
func ReadWindowsCredentialManagerPassword(ctx context.Context, targetName string) string {
	env := append(os.Environ(),
		"AI_SDK_WINDOWS_CREDENTIAL_MANAGER_SOURCE="+windowsCredentialManagerSource,
		"AI_SDK_WINDOWS_CREDENTIAL_MANAGER_TARGET="+targetName,
	)
	out, err := ExecFile(ctx, "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", windowsCredentialManagerCommand}, env)
	if err != nil {
		return ""
	}
	return out
}

// ExecFileInputFunc runs a program with arguments (no shell), feeding input
// on stdin. env == nil inherits the host environment.
type ExecFileInputFunc func(ctx context.Context, name string, args []string, input string, env []string) error

// ExecFileInput is the stdin-feeding process runner used by the credential
// store writers; replaceable in tests. Mirrors TS `runCommandWithInput`: a
// non-zero exit returns an error carrying stderr.
var ExecFileInput ExecFileInputFunc = func(ctx context.Context, name string, args []string, input string, env []string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := stderr.String(); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}

func securityQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

// WriteMacOSKeychainPassword stores (or updates) a generic password through
// `security -i`, feeding the hex-encoded value on stdin so the secret never
// appears in the process arguments. Mirrors the TS Codex keyring writer.
func WriteMacOSKeychainPassword(ctx context.Context, service, account, value string) error {
	command := "add-generic-password -U -s " + securityQuote(service) + " -a " + securityQuote(account) +
		" -X " + hex.EncodeToString([]byte(value)) + "\n"
	return ExecFileInput(ctx, "/usr/bin/security", []string{"-i"}, command, nil)
}

// WriteLinuxSecretServicePassword stores a secret with `secret-tool store`,
// matching the attributes the Rust `keyring` crate (used by Codex) writes.
func WriteLinuxSecretServicePassword(ctx context.Context, service, account, value string) error {
	return ExecFileInput(ctx, "secret-tool", []string{
		"store", "--label", account + "@" + service + ":default",
		"service", service, "username", account, "target", "default", "application", "rust-keyring",
	}, value, nil)
}

const windowsCredentialWriteSource = `
using System;
using System.Runtime.InteropServices;
using System.Text;
public static class CodexCredentialManager {
  [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)]
  public struct Credential {
    public UInt32 Flags; public UInt32 Type; public string TargetName;
    public string Comment; public System.Runtime.InteropServices.ComTypes.FILETIME LastWritten;
    public UInt32 CredentialBlobSize; public IntPtr CredentialBlob;
    public UInt32 Persist; public UInt32 AttributeCount; public IntPtr Attributes;
    public string TargetAlias; public string UserName;
  }
  [DllImport("advapi32.dll", EntryPoint = "CredWriteW", CharSet = CharSet.Unicode, SetLastError = true)]
  static extern bool CredWrite(ref Credential credential, UInt32 flags);
  public static void Write(string target, string userName, string value) {
    byte[] bytes = Encoding.Unicode.GetBytes(value);
    IntPtr blob = Marshal.AllocHGlobal(bytes.Length);
    try {
      Marshal.Copy(bytes, 0, blob, bytes.Length);
      Credential credential = new Credential {
        Type = 1, TargetName = target, CredentialBlobSize = (UInt32)bytes.Length,
        CredentialBlob = blob, Persist = 3, UserName = userName
      };
      if (!CredWrite(ref credential, 0)) throw new System.ComponentModel.Win32Exception();
    } finally { Marshal.FreeHGlobal(blob); }
  }
}`

// WriteWindowsCredentialManagerPassword stores a generic credential via
// PowerShell + CredWriteW. The value is fed on stdin; source, target and user
// name travel in the environment, never interpolated into the command.
func WriteWindowsCredentialManagerPassword(ctx context.Context, targetName, userName, value string) error {
	script := `Add-Type -TypeDefinition $env:AI_SDK_CODEX_CREDENTIAL_SOURCE; $value = [Console]::In.ReadToEnd(); [CodexCredentialManager]::Write($env:AI_SDK_CODEX_CREDENTIAL_TARGET, $env:AI_SDK_CODEX_CREDENTIAL_USER, $value)`
	env := append(os.Environ(),
		"AI_SDK_CODEX_CREDENTIAL_SOURCE="+windowsCredentialWriteSource,
		"AI_SDK_CODEX_CREDENTIAL_TARGET="+targetName,
		"AI_SDK_CODEX_CREDENTIAL_USER="+userName,
	)
	return ExecFileInput(ctx, "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", script}, value, env)
}
