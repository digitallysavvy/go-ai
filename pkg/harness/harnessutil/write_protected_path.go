package harnessutil

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// WriteProtectionScanLimit is the upper bound on the entries scanned inside
// write-protected directories per check. Exceeding it makes the check report
// the write as protected, so a large protected directory costs a host approval
// instead of an unbounded filesystem walk. Mirrors TS
// WRITE_PROTECTION_SCAN_LIMIT.
const WriteProtectionScanLimit = 2000

// AssertWriteProtectedPathEntries rejects an entry that is not a relative path
// without empty, "." or ".." segments. Mirrors TS
// assertWriteProtectedPathEntries (71bef690f9).
func AssertWriteProtectedPathEntries(entries []string) error {
	for _, entry := range entries {
		invalid := entry == "" || posixpath.IsAbs(entry)
		if !invalid {
			for _, segment := range strings.Split(entry, "/") {
				if segment == "" || segment == "." || segment == ".." {
					invalid = true
					break
				}
			}
		}
		if invalid {
			return fmt.Errorf("Write-protected path entries must be relative paths without empty, \".\" or \"..\" segments; received %s.", jsStringLiteral(entry)) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}
	return nil
}

// jsStringLiteral is JSON.stringify of a string.
func jsStringLiteral(s string) string {
	encoded, err := jsstr.Marshal(s)
	if err != nil {
		return strconv.Quote(s)
	}
	return string(encoded)
}

// isInsideWriteProtectionPath is TS isInsideWriteProtectionPath for absolute
// paths: candidate is parent itself or lies below it.
func isInsideWriteProtectionPath(parent, candidate string) bool {
	parent, candidate = posixpath.Normalize(parent), posixpath.Normalize(candidate)
	parent, candidate = strings.TrimRight(parent, "/"), strings.TrimRight(candidate, "/")
	return candidate == parent || strings.HasPrefix(candidate, parent+"/")
}

type writeProtectionTarget struct {
	path      string
	multiLink bool
}

// matchesWriteProtectedPath is TS matchesWriteProtectedPath: a target matches
// when it is a protected path, lies inside one, or is a parent of one, since
// moving or deleting a parent replaces the protected contents. Hard links are
// invisible to path resolution, so while any protected path exists, files with
// more than one link match as well.
func matchesWriteProtectedPath(targets []writeProtectionTarget, protectedPaths []string, anyProtectedPathExists bool) bool {
	for _, target := range targets {
		for _, protected := range protectedPaths {
			if isInsideWriteProtectionPath(protected, target.path) || isInsideWriteProtectionPath(target.path, protected) {
				return true
			}
		}
		if anyProtectedPathExists && target.multiLink {
			return true
		}
	}
	return false
}

// writeProtectionShellFunctions holds the shell functions shared by every write
// protection script. They only use bash 3.2 features and options supported by
// GNU, BSD and BusyBox tools, so the scripts run in minimal sandboxes as well
// as on macOS. canonical sets canonical_path to the realpath of an existing
// entry (framed so trailing newlines survive command substitution); resolve
// sets resolved to the canonical path of a possibly missing entry (dangling
// symlinks and missing "." or ".." segments fail); scan lists every entry below
// a directory without following symlinks, reports the canonical target of each
// symlink, and stops with X once the entry budget is exceeded. Every path is
// emitted base64 encoded, one record per line. Verbatim from TS
// WRITE_PROTECTION_SHELL_FUNCTIONS.
const writeProtectionShellFunctions = `
frame=__AI_SDK_WRITE_PROTECTION_FRAME__
b64() { printf '%s' "$1" | base64 | tr -d '\r\n'; }
emit() {
  printf '%s' "$1"
  shift
  for emit_field in "$@"; do printf ' %s' "$emit_field"; done
  printf '\n'
}
canonical() {
  canonical_framed=$(realpath "$1" 2>/dev/null; canonical_status=$?; printf '%s' "$frame"; exit "$canonical_status") || return 1
  canonical_path=${canonical_framed%"$frame"}
  canonical_path=${canonical_path%$'\n'}
  case "$canonical_path" in /*) ;; *) return 1 ;; esac
}
resolve() {
  resolve_target=$1
  resolve_missing=
  while [ ! -e "$resolve_target" ] && [ ! -L "$resolve_target" ]; do
    resolve_parent=${resolve_target%/*}
    [ -n "$resolve_parent" ] || resolve_parent=/
    resolve_segment=${resolve_target##*/}
    if [ "$resolve_parent" = "$resolve_target" ] || [ -z "$resolve_segment" ] || [ "$resolve_segment" = . ] || [ "$resolve_segment" = .. ]; then
      return 1
    fi
    resolve_missing=/$resolve_segment$resolve_missing
    resolve_target=$resolve_parent
  done
  if [ -L "$resolve_target" ] && [ ! -e "$resolve_target" ]; then return 1; fi
  canonical "$resolve_target" || return 1
  resolved=${canonical_path%/}$resolve_missing
  [ -n "$resolved" ] || resolved=/
}
scan() {
  (
    set -o pipefail
    find "$1" -mindepth 1 -print0 | {
      scan_count=0
      while IFS= read -r -d '' scan_entry; do
        scan_count=$((scan_count + 1))
        if [ "$scan_count" -gt "$scan_budget" ]; then
          emit X
          exit 0
        fi
        if [ -L "$scan_entry" ]; then
          if [ ! -e "$scan_entry" ] || ! canonical "$scan_entry"; then
            exit 1
          fi
          if [ -d "$canonical_path" ]; then scan_is_dir=1; else scan_is_dir=0; fi
          emit S "$scan_is_dir" "$(b64 "$canonical_path")"
        fi
      done
      emit C "$scan_count"
    }
  )
}
`

// IsSandboxWriteProtectedPathOptions are the options of
// IsSandboxWriteProtectedPath.
type IsSandboxWriteProtectedPathOptions struct {
	// Sandbox is inspected through Run.
	Sandbox providerutils.SandboxSession
	// WorkDir is the session working directory; WriteProtectedPaths entries
	// are relative to it.
	WorkDir string
	// Paths are absolute sandbox paths, already mapped the way the adapter's
	// file tools map them.
	Paths []string
	// WriteProtectedPaths entries are relative to WorkDir and must not contain
	// empty, "." or ".." segments.
	WriteProtectedPaths []string
}

// IsSandboxWriteProtectedPath reports whether writing to any of opts.Paths
// touches a write-protected path and therefore requires the host's approval.
// It runs on the host and inspects the sandbox filesystem through
// Sandbox.Run, with one command for the common case and one more per round of
// symlinked directories to scan. Mirrors TS isSandboxWriteProtectedPath
// (71bef690f9).
//
// A path matches when its canonical location is a protected path, lies inside
// one, or is a parent of one. Symlinks inside protected directories protect
// their targets, and while any protected path exists, writes to files with
// more than one link match too. Any failure reports the write as protected;
// only an invalid WriteProtectedPaths entry returns an error.
func IsSandboxWriteProtectedPath(ctx context.Context, opts IsSandboxWriteProtectedPathOptions) (bool, error) {
	if err := AssertWriteProtectedPathEntries(opts.WriteProtectedPaths); err != nil {
		return false, err
	}
	if len(opts.Paths) == 0 {
		return true, nil
	}
	if len(opts.WriteProtectedPaths) == 0 {
		return false, nil
	}
	return isSandboxWriteProtected(ctx, opts), nil
}

func isSandboxWriteProtected(ctx context.Context, opts IsSandboxWriteProtectedPathOptions) bool {
	var targetPaths []string
	for _, filePath := range opts.Paths {
		if providerutils.JSTrim(filePath) == "" || !posixpath.IsAbs(filePath) {
			return true
		}
		candidates := []string{filePath}
		if normalized := posixpath.Normalize(filePath); normalized != filePath {
			candidates = append(candidates, normalized)
		}
		for _, candidate := range candidates {
			targetPaths = append(targetPaths, withoutTrailingSlashes(candidate))
		}
	}

	quoted := func(values []string) string {
		parts := make([]string, len(values))
		for i, v := range values {
			parts[i] = ShellQuote(v)
		}
		return strings.Join(parts, " ")
	}
	initial := runWriteProtectionScript(ctx, opts.Sandbox, strings.Join([]string{
		writeProtectionShellFunctions,
		"workdir=" + ShellQuote(opts.WorkDir),
		`canonical "$workdir" || exit 3`,
		`canonical_workdir=$canonical_path`,
		`emit W "$(b64 "$canonical_workdir")"`,
		`scan_roots=()`,
		"for entry in " + quoted(opts.WriteProtectedPaths) + "; do",
		`  resolve "$workdir/$entry" || exit 3`,
		`  root_exists=0`,
		`  root_is_dir=0`,
		`  if [ -e "$resolved" ] || [ -L "$resolved" ]; then root_exists=1; fi`,
		`  if [ "$root_exists" = 1 ] && [ -d "$resolved" ] && [ ! -L "$resolved" ]; then`,
		`    root_is_dir=1`,
		`    root_seen=0`,
		`    for scan_root in "${scan_roots[@]}"; do [ "$scan_root" = "$resolved" ] && root_seen=1; done`,
		`    [ "$root_seen" = 1 ] || scan_roots+=("$resolved")`,
		`  fi`,
		`  emit P "$root_exists" "$root_is_dir" "$(b64 "$resolved")"`,
		`done`,
		"for target in " + quoted(targetPaths) + "; do",
		`  resolve "$target" || exit 3`,
		`  target_multi_link=0`,
		`  if [ -e "$resolved" ] && [ -n "$(find "$resolved" -prune -type f -links +1 -print)" ]; then target_multi_link=1; fi`,
		`  emit T "$target_multi_link" "$(b64 "$resolved")"`,
		`done`,
		"scan_budget=" + strconv.Itoa(WriteProtectionScanLimit),
		`for scan_root in "${scan_roots[@]}"; do scan "$scan_root" || exit 3; done`,
		`emit E`,
	}, "\n"))
	if initial == nil {
		return true
	}

	var canonicalWorkDir string
	haveWorkDir := false
	var roots, targets []writeProtectionRecord
	for _, record := range initial {
		switch record.typ {
		case "W":
			if !haveWorkDir {
				canonicalWorkDir, haveWorkDir = record.path, true
			}
		case "P":
			roots = append(roots, record)
		case "T":
			targets = append(targets, record)
		}
	}
	if !haveWorkDir || len(roots) != len(opts.WriteProtectedPaths) || len(targets) != len(targetPaths) {
		return true
	}

	var protectedPaths []string
	for _, entry := range opts.WriteProtectedPaths {
		protectedPaths = append(protectedPaths, posixpath.Join(canonicalWorkDir, entry))
	}
	anyExists := false
	scanned := map[string]bool{}
	for _, root := range roots {
		protectedPaths = append(protectedPaths, root.path)
		if root.flags[1] == "1" {
			scanned[root.path] = true
		}
		if root.flags[0] == "1" {
			anyExists = true
		}
	}

	records := initial
	entryCount := 0
	for {
		for _, record := range records {
			if record.typ == "X" {
				return true
			}
		}
		for _, record := range records {
			if record.typ == "C" {
				n, _ := strconv.Atoi(record.flags[0])
				entryCount += n
			}
		}
		if entryCount > WriteProtectionScanLimit {
			return true
		}

		var queue []string
		for _, record := range records {
			if record.typ != "S" {
				continue
			}
			protectedPaths = append(protectedPaths, record.path)
			if record.flags[0] == "1" && !scanned[record.path] && isInsideWriteProtectionPath(canonicalWorkDir, record.path) {
				scanned[record.path] = true
				queue = append(queue, record.path)
			}
		}
		if len(queue) == 0 {
			break
		}

		next := runWriteProtectionScript(ctx, opts.Sandbox, strings.Join([]string{
			writeProtectionShellFunctions,
			"scan_budget=" + strconv.Itoa(WriteProtectionScanLimit-entryCount),
			"for scan_root in " + quoted(queue) + `; do scan "$scan_root" || exit 3; done`,
			`emit E`,
		}, "\n"))
		if next == nil {
			return true
		}
		records = next
	}

	matchTargets := make([]writeProtectionTarget, len(targets))
	for i, record := range targets {
		matchTargets[i] = writeProtectionTarget{path: record.path, multiLink: record.flags[0] == "1"}
	}
	return matchesWriteProtectedPath(matchTargets, protectedPaths, anyExists)
}

type writeProtectionRecord struct {
	typ   string
	flags []string
	path  string
}

// writeProtectionRecordFlagCount is TS RECORD_PATH_FIELD_COUNT: the number of
// 0/1 flags before the base64 path of each path-carrying record.
var writeProtectionRecordFlagCount = map[string]int{"W": 0, "P": 2, "T": 1, "S": 1}

var (
	writeProtectionDigits = regexp.MustCompile(`^[0-9]+$`)
	writeProtectionBase64 = regexp.MustCompile(`^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$`)
)

// runWriteProtectionScript runs script in the sandbox and parses its records.
// It returns nil for a failed run or output that is not exactly the expected
// framing, which every caller reads as "protected".
func runWriteProtectionScript(ctx context.Context, sandbox providerutils.SandboxSession, script string) []writeProtectionRecord {
	result, err := sandbox.Run(ctx, providerutils.SandboxProcessOptions{Command: "bash -c " + ShellQuote(script)})
	if err != nil || result.ExitCode != 0 {
		return nil
	}
	lines := strings.Split(result.Stdout, "\n")
	if lines[len(lines)-1] != "" {
		return nil
	}
	lines = lines[:len(lines)-1]
	if len(lines) == 0 || lines[len(lines)-1] != "E" {
		return nil
	}
	records := []writeProtectionRecord{}
	for _, line := range lines[:len(lines)-1] {
		fields := strings.Split(line, " ")
		typ, fields := fields[0], fields[1:]
		flagCount, ok := writeProtectionRecordFlagCount[typ]
		if !ok {
			switch {
			case typ == "C" && len(fields) == 1 && writeProtectionDigits.MatchString(fields[0]):
				records = append(records, writeProtectionRecord{typ: typ, flags: fields})
				continue
			case typ == "X" && len(fields) == 0:
				records = append(records, writeProtectionRecord{typ: typ, flags: []string{}})
				continue
			}
			return nil
		}
		if len(fields) != flagCount+1 {
			return nil
		}
		flags := fields[:flagCount]
		for _, flag := range flags {
			if flag != "0" && flag != "1" {
				return nil
			}
		}
		decoded, ok := decodeWriteProtectionPath(fields[flagCount])
		if !ok || !posixpath.IsAbs(decoded) {
			return nil
		}
		records = append(records, writeProtectionRecord{typ: typ, flags: flags, path: decoded})
	}
	return records
}

// decodeWriteProtectionPath is TS decodeRecordPath: strict base64 of valid
// UTF-8 (TextDecoder fatal, which also drops a leading byte order mark).
func decodeWriteProtectionPath(encoded string) (string, bool) {
	if encoded == "" || !writeProtectionBase64.MatchString(encoded) {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !utf8.Valid(raw) {
		return "", false
	}
	return strings.TrimPrefix(string(raw), "\ufeff"), true
}

func withoutTrailingSlashes(filePath string) string {
	trimmed := strings.TrimRight(filePath, "/")
	if trimmed == "" {
		return "/"
	}
	return trimmed
}
