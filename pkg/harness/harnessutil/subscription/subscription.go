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
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"strings"
)

// ExecFileFunc runs a program with arguments (no shell) and returns stdout.
// env == nil inherits the host environment.
type ExecFileFunc func(ctx context.Context, name string, args []string, env []string) (string, error)

// ExecFile is the process runner used by the readers; replaceable in tests.
var ExecFile ExecFileFunc = func(ctx context.Context, name string, args []string, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	return stdout.String(), err
}

// ParseJWTPayload decodes the payload of a three-segment JWT. It returns nil
// when the token is malformed or the payload is not a JSON object. Mirrors TS
// `parseJwtPayload`.
func ParseJWTPayload(token string) map[string]any {
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		return nil
	}
	// Node's base64url decoder tolerates padding and trailing garbage; accept
	// both padded and unpadded input.
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(segments[1], "="))
	if err != nil {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload
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
	return strings.TrimSpace(out)
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
