package harnessutil

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const instructionsMetadataVersion = 1

// WriteInstructionsOptions mirrors TS `WriteInstructionsOptions`.
type WriteInstructionsOptions struct {
	Sandbox          providerutils.SandboxSession
	HomePath         string
	InstructionsFile string
	// Instructions; blank clears previously applied instructions.
	Instructions string
}

// WriteInstructionsResult mirrors TS `WriteInstructionsResult`.
type WriteInstructionsResult struct {
	Changed  bool   `json:"changed"`
	FilePath string `json:"filePath"`
}

type instructionsMetadata struct {
	Version         int     `json:"version"`
	OriginalContent *string `json:"originalContent"`
	Instructions    string  `json:"instructions"`
	AppliedContent  string  `json:"appliedContent"`
}

// WriteInstructions appends harness instructions to a file under HOME
// (e.g. AGENTS.md, CLAUDE.md) while preserving user content, tracking what it
// applied in a companion metadata file so it can replace or remove its own
// block later (255cccf). Mirrors TS `writeInstructions`.
func WriteInstructions(ctx context.Context, opts WriteInstructionsOptions) (*WriteInstructionsResult, error) {
	targetPath, metadataPath, err := resolveInstructionsFilePath(opts.HomePath, opts.InstructionsFile)
	if err != nil {
		return nil, err
	}
	sandbox := opts.Sandbox
	trimmed := strings.TrimSpace(opts.Instructions)
	hasInstructions := trimmed != ""

	currentDisk, err := sandbox.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: targetPath})
	if err != nil {
		return nil, err
	}
	existing, err := readInstructionsMetadata(ctx, sandbox, metadataPath)
	if err != nil {
		return nil, err
	}

	if hasInstructions {
		original := deriveOriginalContent(currentDisk, existing)
		var target string
		if original != nil && strings.TrimSpace(*original) != "" {
			target = trimTrailingNewlines(*original) + "\n\n" + trimmed + "\n"
		} else {
			target = trimmed + "\n"
		}
		if currentDisk != nil && *currentDisk == target && existing != nil &&
			existing.Instructions == trimmed && equalStringPtr(existing.OriginalContent, original) {
			return &WriteInstructionsResult{Changed: false, FilePath: targetPath}, nil
		}
		if err := sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: targetPath, Content: target}); err != nil {
			return nil, err
		}
		content, err := stringifyJSON(instructionsMetadata{
			Version:         instructionsMetadataVersion,
			OriginalContent: original,
			Instructions:    trimmed,
			AppliedContent:  target,
		})
		if err != nil {
			return nil, err
		}
		temporary := metadataPath + ".tmp"
		if err := sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: temporary, Content: content}); err != nil {
			return nil, err
		}
		if err := runSandboxCommand(ctx, sandbox, "mv -f "+ShellQuote(temporary)+" "+ShellQuote(metadataPath), "Failed to update instructions metadata: "+metadataPath); err != nil {
			return nil, err
		}
		return &WriteInstructionsResult{Changed: true, FilePath: targetPath}, nil
	}

	if existing == nil {
		return &WriteInstructionsResult{Changed: false, FilePath: targetPath}, nil
	}
	if restored := deriveRestoredContent(currentDisk, existing); restored != nil {
		if err := sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{Path: targetPath, Content: trimTrailingNewlines(*restored) + "\n"}); err != nil {
			return nil, err
		}
	} else if err := runSandboxCommand(ctx, sandbox, "rm -f -- "+ShellQuote(targetPath), "Failed to remove instructions file: "+targetPath); err != nil {
		return nil, err
	}
	if err := runSandboxCommand(ctx, sandbox, "rm -f -- "+ShellQuote(metadataPath), "Failed to remove instructions metadata: "+metadataPath); err != nil {
		return nil, err
	}
	return &WriteInstructionsResult{Changed: true, FilePath: targetPath}, nil
}

func trimTrailingNewlines(s string) string { return strings.TrimRight(s, "\n") }

func equalStringPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func strPtr(s string) *string { return &s }

// userBaseFromDisk strips the applied instructions suffix. The second return
// reports whether the disk content matched a known shape.
func userBaseFromDisk(currentDisk string, meta *instructionsMetadata) (*string, bool) {
	trimmedDisk := trimTrailingNewlines(currentDisk)
	suffix := "\n\n" + meta.Instructions
	if strings.HasSuffix(trimmedDisk, suffix) {
		base := trimmedDisk[:len(trimmedDisk)-len(suffix)]
		if base == "" {
			return nil, true
		}
		return strPtr(base), true
	}
	if trimmedDisk == meta.Instructions {
		return nil, true
	}
	return nil, false
}

func deriveOriginalContent(currentDisk *string, meta *instructionsMetadata) *string {
	if meta == nil {
		return currentDisk
	}
	if currentDisk == nil || *currentDisk == meta.AppliedContent {
		return meta.OriginalContent
	}
	if base, ok := userBaseFromDisk(*currentDisk, meta); ok {
		return base
	}
	if meta.OriginalContent != nil {
		return meta.OriginalContent
	}
	return currentDisk
}

func deriveRestoredContent(currentDisk *string, meta *instructionsMetadata) *string {
	if currentDisk == nil {
		return nil
	}
	if *currentDisk == meta.AppliedContent {
		if meta.OriginalContent != nil && strings.TrimSpace(*meta.OriginalContent) != "" {
			return meta.OriginalContent
		}
		return nil
	}
	if base, ok := userBaseFromDisk(*currentDisk, meta); ok {
		return base
	}
	return currentDisk
}

func resolveInstructionsFilePath(homePath, instructionsFile string) (string, string, error) {
	extraInvalid := strings.HasSuffix(instructionsFile, "/") || strings.HasSuffix(instructionsFile, `\`) ||
		strings.HasSuffix(instructionsFile, "/.") || strings.HasSuffix(instructionsFile, "/..")
	normalized, err := validateHomeRelativePath(homePath, instructionsFile, "instructionsFile", extraInvalid)
	if err != nil {
		return "", "", err
	}
	target := posixpath.Join(homePath, normalized)
	metadata := posixpath.Join(posixpath.Dirname(target), "."+posixpath.Basename(target)+".ai-sdk-harness-instructions.json")
	return target, metadata, nil
}

func readInstructionsMetadata(ctx context.Context, sandbox providerutils.SandboxSession, metadataPath string) (*instructionsMetadata, error) {
	content, err := sandbox.ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{Path: metadataPath})
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, nil
	}
	invalid := fmt.Errorf("Invalid AI SDK harness instructions metadata: %s", metadataPath)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(*content), &raw); err != nil || raw == nil {
		return nil, invalid
	}
	isString := func(key string) bool {
		var s string
		return json.Unmarshal(raw[key], &s) == nil && raw[key] != nil && string(raw[key]) != "null"
	}
	var m instructionsMetadata
	if err := json.Unmarshal([]byte(*content), &m); err != nil || m.Version != instructionsMetadataVersion ||
		raw["originalContent"] == nil || !(string(raw["originalContent"]) == "null" || isString("originalContent")) ||
		!isString("instructions") || !isString("appliedContent") {
		return nil, invalid
	}
	return &m, nil
}
