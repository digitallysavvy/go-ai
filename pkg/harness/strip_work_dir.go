package harness

import (
	"encoding/json"
	"regexp"
)

// StripWorkDir removes the session working-directory prefix from path-bearing
// fields of a stream part, returning a new part for display to consumers.
//
// Harness adapters run the agent in a per-session working directory that is a
// subdirectory of the sandbox root, and the adapter's tools use absolute
// paths so they resolve against the root regardless of where the runtime
// process operates. The absolute paths are correct but noisy in a UI, so this
// strips the prefix for the consumer-facing projection only. Mirrors TS
// `stripWorkDir` (internal/strip-work-dir.ts).
//
// Boundary-aware prefix replacement (rather than rewriting known path fields)
// is used deliberately: ToolResultPart.Result is free-form text — command
// stdout, grep output — where paths can appear anywhere and field-aware
// rewriting is impossible.
func StripWorkDir(part StreamPart, sessionWorkDir string) StreamPart {
	if sessionWorkDir == "" || part == nil {
		return part
	}

	switch p := part.(type) {
	case *ToolInputDeltaPart:
		cp := *p
		cp.Delta = stripString(p.Delta, sessionWorkDir)
		return &cp
	case *ToolCallPart:
		cp := *p
		cp.Input = stripString(p.Input, sessionWorkDir)
		return &cp
	case *ToolResultPart:
		cp := *p
		cp.Result = stripDeep(p.Result, sessionWorkDir)
		return &cp
	case *FileChangePart:
		cp := *p
		cp.Path = stripString(p.Path, sessionWorkDir)
		return &cp
	default:
		return part
	}
}

// stripString replaces occurrences of the working directory in value. A
// reference to the directory followed by a path separator becomes
// workspace-relative (`/work/dir/src/a.ts` -> `src/a.ts`); a bare reference to
// the directory itself becomes `.`.
func stripString(value, workDir string) string {
	out, _ := stripStreamingStringWithPending(value, workDir, true, "", false)
	return out
}

// stripDeep recursively strips workDir from every string nested in an
// arbitrary JSON-like value (as produced by encoding/json.Unmarshal into
// interface{}: map[string]interface{}, []interface{}, string, and other
// scalar leaves left unchanged).
func stripDeep(value interface{}, workDir string) interface{} {
	switch v := value.(type) {
	case string:
		return stripString(v, workDir)
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = stripDeep(item, workDir)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for k, val := range v {
			out[k] = stripDeep(val, workDir)
		}
		return out
	case json.RawMessage:
		var decoded interface{}
		if err := json.Unmarshal(v, &decoded); err != nil {
			return v
		}
		return stripDeep(decoded, workDir)
	default:
		return value
	}
}

// toolInputWorkDirStripper strips the working directory from a streaming
// sequence of tool-input-start/delta/end parts, buffering a delta's trailing
// bytes when they might be a partial match of workDir until either more
// input arrives or the block ends. Mirrors TS
// `createToolInputWorkDirStripper`.
type toolInputWorkDirStripper struct {
	sessionWorkDir string
	state          map[string]*toolInputStripState
}

type toolInputStripState struct {
	pending            string
	precedingCharacter string
	havePrecedingChar  bool
}

// NewToolInputWorkDirStripper returns a stateful stripper for one turn's
// tool-input-start/delta/end parts.
func NewToolInputWorkDirStripper(sessionWorkDir string) func(part StreamPart) []StreamPart {
	s := &toolInputWorkDirStripper{sessionWorkDir: sessionWorkDir, state: map[string]*toolInputStripState{}}
	return s.strip
}

func (s *toolInputWorkDirStripper) strip(part StreamPart) []StreamPart {
	if s.sessionWorkDir == "" {
		return []StreamPart{part}
	}

	switch p := part.(type) {
	case *ToolInputStartPart:
		s.state[p.ID] = &toolInputStripState{}
		return []StreamPart{part}

	case *ToolInputDeltaPart:
		st := s.state[p.ID]
		if st == nil {
			st = &toolInputStripState{}
			s.state[p.ID] = st
		}
		value := st.pending + p.Delta
		var preceding string
		havePreceding := st.havePrecedingChar
		if havePreceding {
			preceding = st.precedingCharacter
		}
		output, pending := stripStreamingStringWithPending(value, s.sessionWorkDir, false, preceding, havePreceding)
		pendingStart := len(value) - len(pending)
		if len(pending) > 0 {
			if pendingStart > 0 {
				st.precedingCharacter = string(value[pendingStart-1])
				st.havePrecedingChar = true
			}
			// else: keep the previous preceding character (nothing consumed
			// yet from this delta).
		} else if len(value) > 0 {
			st.precedingCharacter = string(value[len(value)-1])
			st.havePrecedingChar = true
		}
		st.pending = pending
		if len(output) == 0 {
			return nil
		}
		cp := *p
		cp.Delta = output
		return []StreamPart{&cp}

	case *ToolInputEndPart:
		st := s.state[p.ID]
		delete(s.state, p.ID)
		if st == nil || st.pending == "" {
			return []StreamPart{part}
		}
		output := stripString(st.pending, s.sessionWorkDir)
		if output == "" {
			return []StreamPart{part}
		}
		return []StreamPart{&ToolInputDeltaPart{ID: p.ID, Delta: output}, part}

	default:
		return []StreamPart{part}
	}
}

var pathBoundaryRe = regexp.MustCompile(`[\s"'` + "`" + `=]`)
var pathTerminatorRe = regexp.MustCompile(`[\s"'` + "`" + `;|&<>()]`)

// stripStreamingStringWithPending incrementally strips workDir occurrences
// from value. When final is false, a trailing fragment that might still
// become a match once more input arrives is held back in the returned
// pending string instead of being emitted.
func stripStreamingStringWithPending(value, workDir string, final bool, precedingCharacter string, havePrecedingCharacter bool) (output string, pending string) {
	remaining := value
	characterBeforeRemaining := precedingCharacter
	haveChar := havePrecedingCharacter

	for len(remaining) > 0 {
		matchIndex := indexOf(remaining, workDir)
		if matchIndex >= 0 {
			followingIndex := matchIndex + len(workDir)
			hasBoundary := isPathBoundary(remaining, matchIndex, characterBeforeRemaining, haveChar)

			if hasBoundary && followingIndex == len(remaining) && !final {
				return output + remaining[:matchIndex], remaining[matchIndex:]
			}

			isPath := hasBoundary && (followingIndex == len(remaining) ||
				remaining[followingIndex] == '/' ||
				isPathTerminatorByte(remaining, followingIndex))

			if !isPath {
				output += remaining[:followingIndex]
				if followingIndex > 0 {
					characterBeforeRemaining = string(remaining[followingIndex-1])
					haveChar = true
				}
				remaining = remaining[followingIndex:]
			} else if followingIndex < len(remaining) && remaining[followingIndex] == '/' {
				output += remaining[:matchIndex]
				characterBeforeRemaining = "/"
				haveChar = true
				remaining = remaining[followingIndex+1:]
			} else {
				output += remaining[:matchIndex] + "."
				remaining = remaining[followingIndex:]
				haveChar = false
			}
			continue
		}

		if final {
			return output + remaining, ""
		}

		pendingLength := len(remaining)
		if pendingLength > len(workDir)-1 {
			pendingLength = len(workDir) - 1
		}
		for pendingLength > 0 {
			suffix := remaining[len(remaining)-pendingLength:]
			if hasPrefix(workDir, suffix) && isPathBoundary(remaining, len(remaining)-pendingLength, characterBeforeRemaining, haveChar) {
				break
			}
			pendingLength--
		}
		outputLength := len(remaining) - pendingLength
		return output + remaining[:outputLength], remaining[outputLength:]
	}

	return output, ""
}

func indexOf(s, substr string) int {
	if substr == "" {
		return -1
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func isPathBoundary(value string, index int, precedingCharacter string, havePrecedingCharacter bool) bool {
	var character string
	if index == 0 {
		if !havePrecedingCharacter {
			return true
		}
		character = precedingCharacter
	} else {
		character = string(value[index-1])
	}
	return pathBoundaryRe.MatchString(character)
}

func isPathTerminatorByte(value string, index int) bool {
	if index >= len(value) {
		return true
	}
	return pathTerminatorRe.MatchString(string(value[index]))
}
