package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// SkillMatcher maps file extensions/patterns to skill names
var SkillMatcher = map[string][]string{
	".tsx":        {"vercel-react-best-practices"},
	".ts":         {"vercel-react-best-practices"},
	".jsx":        {"vercel-react-best-practices"},
	".js":         {"vercel-react-best-practices"},
	".tf":         {"terraform-skill"},
	".tfvars":     {"terraform-skill"},
	"next.config": {"vercel-react-best-practices"},
}

// SkillDirs are directories to search for skills
var SkillDirs = []string{
	".agents/skills",
	".claude/skills",
}

// DiscoverSkills finds skills relevant to the given files in workDir
func DiscoverSkills(workDir string, changedFiles []string) ([]string, error) {
	// Determine which skills are relevant based on changed files
	neededSkills := make(map[string]bool)
	for _, file := range changedFiles {
		ext := filepath.Ext(file)
		base := filepath.Base(file)

		// Check extension matches
		if skills, ok := SkillMatcher[ext]; ok {
			for _, s := range skills {
				neededSkills[s] = true
			}
		}
		// Check base name matches
		for pattern, skills := range SkillMatcher {
			if strings.Contains(base, pattern) {
				for _, s := range skills {
					neededSkills[s] = true
				}
			}
		}
	}

	if len(neededSkills) == 0 {
		return nil, nil
	}

	// Find skill files
	var foundSkills []string
	for _, dir := range SkillDirs {
		skillBase := filepath.Join(workDir, dir)
		for skill := range neededSkills {
			skillPath := filepath.Join(skillBase, skill, "SKILL.md")
			if _, err := os.Stat(skillPath); err == nil {
				foundSkills = append(foundSkills, skillPath)
			}
		}
	}

	return foundSkills, nil
}

// LoadSkillContent reads skill files and returns combined content
func LoadSkillContent(skillPaths []string) (string, error) {
	if len(skillPaths) == 0 {
		return "", nil
	}

	var builder strings.Builder
	builder.WriteString("\n## SKILLS CONTEXT (apply these patterns when reviewing)\n\n")

	for _, path := range skillPaths {
		content, err := os.ReadFile(path)
		if err != nil {
			continue // Skip unreadable skills
		}

		// Extract skill name from path
		dir := filepath.Dir(path)
		skillName := filepath.Base(dir)

		builder.WriteString("### Skill: ")
		builder.WriteString(skillName)
		builder.WriteString("\n\n")
		builder.Write(content)
		builder.WriteString("\n\n")
	}

	return builder.String(), nil
}

// GetFilesFromDiff extracts file paths from a git diff
func GetFilesFromDiff(diff string) []string {
	var files []string
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			// Extract file path from "diff --git a/path b/path"
			parts := strings.Split(line, " ")
			if len(parts) >= 4 {
				file := strings.TrimPrefix(parts[2], "a/")
				files = append(files, file)
			}
		}
	}
	return files
}
