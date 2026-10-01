package acp

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/posixpath"
)

// SkillNamePattern is the required ACP skill name shape (kebab-case slug).
// Mirrors TS `ACP_SKILL_NAME_PATTERN`.
var SkillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// DefaultSkillsDirectory mirrors TS `DEFAULT_ACP_SKILLS_DIRECTORY`.
const DefaultSkillsDirectory = ".agents/skills"

// resolvePrivateSessionDirectory mirrors TS
// `resolveACPPrivateSessionDirectory` (a thin re-export of the shared
// harness helper).
func resolvePrivateSessionDirectory(stateDirectory, sessionID string) string {
	return harness.SessionDataDirectoryPath(stateDirectory, sessionID)
}

// resolveSkillsDirectory mirrors TS `resolveACPSkillsDirectory`.
func resolveSkillsDirectory(implementationHomeDir, skillsDirectory string) (string, error) {
	if skillsDirectory == "" {
		skillsDirectory = DefaultSkillsDirectory
	}
	containsTraversal := false
	for _, seg := range regexp.MustCompile(`[\\/]`).Split(skillsDirectory, -1) {
		if seg == ".." {
			containsTraversal = true
		}
	}
	normalized := posixpath.Normalize(skillsDirectory)
	invalid := skillsDirectory == "" || strings.Contains(skillsDirectory, `\`) ||
		posixpath.IsAbs(skillsDirectory) || posixpath.IsWin32Abs(skillsDirectory) || containsTraversal ||
		normalized == "." || strings.HasPrefix(normalized, "../") || strings.Contains(normalized, "/../") || strings.HasSuffix(normalized, "/..")
	if invalid {
		return "", fmt.Errorf("ACP skillsDirectory %q must be a relative POSIX path without traversal.", skillsDirectory) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return posixpath.Join(implementationHomeDir, normalized), nil
}

// validateSkills mirrors TS `validateACPSkills`.
func validateSkills(skills []harness.Skill) error {
	names := map[string]bool{}
	for _, skill := range skills {
		if !SkillNamePattern.MatchString(skill.Name) || skill.Name == "." || skill.Name == ".." {
			return fmt.Errorf("Invalid ACP skill name %q: expected a kebab-case slug.", skill.Name) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if names[skill.Name] {
			return fmt.Errorf("Duplicate ACP skill name %q.", skill.Name) //nolint:staticcheck // matches TS SDK's exact error text
		}
		names[skill.Name] = true

		filePaths := map[string]bool{}
		for _, file := range skill.Files {
			normalized, err := validateAttachedFilePath(skill.Name, file.Path)
			if err != nil {
				return err
			}
			if normalized == "SKILL.md" {
				return fmt.Errorf("Invalid ACP skill file path %q for skill %q: SKILL.md is reserved for the skill definition.", file.Path, skill.Name) //nolint:staticcheck // matches TS SDK's exact error text
			}
			if filePaths[normalized] {
				return fmt.Errorf("Duplicate ACP skill file path %q for skill %q.", file.Path, skill.Name) //nolint:staticcheck // matches TS SDK's exact error text
			}
			filePaths[normalized] = true
		}
	}
	return nil
}

func validateAttachedFilePath(skillName, filePath string) (string, error) {
	containsTraversal := false
	for _, seg := range regexp.MustCompile(`[\\/]`).Split(filePath, -1) {
		if seg == ".." {
			containsTraversal = true
		}
	}
	normalized := posixpath.Normalize(filePath)
	invalid := filePath == "" || strings.HasSuffix(filePath, "/") || strings.Contains(filePath, `\`) ||
		posixpath.IsAbs(filePath) || posixpath.IsWin32Abs(filePath) || containsTraversal ||
		normalized == "." || strings.HasPrefix(normalized, "../") || strings.Contains(normalized, "/../") || strings.HasSuffix(normalized, "/..")
	if invalid {
		return "", fmt.Errorf("Invalid ACP skill file path %q for skill %q: expected a relative POSIX path without traversal.", filePath, skillName) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return normalized, nil
}
