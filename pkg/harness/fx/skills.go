package fx

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Port of TS fx-skills.ts with libfx's createSkillsAdapter (libfx
// skills.js, Apache-2.0): skills become instruction sections.

const (
	maxSkills                 = 64
	maxSkillInstructionsBytes = 64 << 10
)

func escapeSkillAttribute(value string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;").Replace(value)
}

// skillsInstructions renders skills the way libfx's createSkillsAdapter does.
func skillsInstructions(skills []harness.Skill) (string, error) {
	if len(skills) > maxSkills {
		return "", fmt.Errorf("skills must be an array with at most %d records", maxSkills)
	}
	names := map[string]bool{}
	sections := make([]string, 0, len(skills))
	for _, skill := range skills {
		if names[skill.Name] {
			return "", fmt.Errorf("duplicate skill name: %s", skill.Name)
		}
		names[skill.Name] = true
		resources := make([]string, 0, len(skill.Files))
		for _, f := range skill.Files {
			resources = append(resources, fmt.Sprintf("<resource uri=\"%s\">\n%s\n</resource>", escapeSkillAttribute(f.Path), f.Content))
		}
		var parts []string
		for _, p := range []string{
			fmt.Sprintf("<skill name=\"%s\">", escapeSkillAttribute(skill.Name)),
			descriptionTag(skill.Description),
			skill.Content,
			strings.Join(resources, "\n"),
			"</skill>",
		} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		sections = append(sections, strings.Join(parts, "\n"))
	}
	instructions := strings.Join(sections, "\n\n")
	if len(instructions) > maxSkillInstructionsBytes {
		return "", fmt.Errorf("skill instructions exceed the %d byte libfx limit", maxSkillInstructionsBytes)
	}
	return instructions, nil
}

func descriptionTag(description string) string {
	if description == "" {
		return ""
	}
	return "<description>" + description + "</description>"
}
