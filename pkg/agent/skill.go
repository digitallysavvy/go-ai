package agent

import (
	"context"
	"fmt"
	"sync"
)

// Skill represents a reusable agent capability or behavior
// Skills allow agents to encapsulate common patterns and behaviors
// that can be shared across multiple agents
type Skill struct {
	// Name of the skill (must be unique)
	Name string

	// Description of what the skill does
	Description string

	// Instructions provide additional context or prompts for using the skill
	// These can be included in the agent's system prompt when the skill is active
	Instructions string

	// Handler executes the skill logic
	// It receives context and input, returns output or error
	Handler SkillHandler

	// Metadata contains additional skill information
	Metadata map[string]interface{}
}

// SkillHandler is a function that executes a skill
// It receives the input string and returns output or an error
type SkillHandler func(ctx context.Context, input string) (string, error)

// SkillRegistry manages a collection of skills
// It allows registering, retrieving, and listing skills
//
// SkillRegistry is safe for concurrent use: all access to the underlying map
// is guarded by mu, mirroring SubagentRegistry (same hot-loaded
// plugin/skill-registration-during-concurrent-use hazard).
type SkillRegistry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
}

// NewSkillRegistry creates a new skill registry
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills: make(map[string]*Skill),
	}
}

// Register adds a skill to the registry
// Returns an error if a skill with the same name already exists
func (r *SkillRegistry) Register(skill *Skill) error {
	if skill == nil {
		return fmt.Errorf("skill cannot be nil")
	}

	if skill.Name == "" {
		return fmt.Errorf("skill name cannot be empty")
	}

	if skill.Handler == nil {
		return fmt.Errorf("skill handler cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.skills[skill.Name]; exists {
		return fmt.Errorf("skill '%s' already registered", skill.Name)
	}

	r.skills[skill.Name] = skill
	return nil
}

// Unregister removes a skill from the registry
func (r *SkillRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.skills, name)
}

// Get retrieves a skill by name
// Returns the skill and true if found, nil and false otherwise
func (r *SkillRegistry) Get(name string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	skill, exists := r.skills[name]
	return skill, exists
}

// Has checks if a skill exists in the registry
func (r *SkillRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.skills[name]
	return exists
}

// List returns all registered skills, as a copy of the registry's current
// contents (safe to mutate without affecting the registry).
func (r *SkillRegistry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	skills := make([]*Skill, 0, len(r.skills))
	for _, skill := range r.skills {
		skills = append(skills, skill)
	}
	return skills
}

// Names returns the names of all registered skills
func (r *SkillRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	return names
}

// Execute runs a skill by name with the given input
// Returns an error if the skill is not found or execution fails
func (r *SkillRegistry) Execute(ctx context.Context, name string, input string) (string, error) {
	r.mu.RLock()
	skill, exists := r.skills[name]
	r.mu.RUnlock()
	if !exists {
		return "", fmt.Errorf("skill '%s' not found", name)
	}

	return skill.Handler(ctx, input)
}

// Clear removes all skills from the registry
func (r *SkillRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills = make(map[string]*Skill)
}

// Count returns the number of registered skills
func (r *SkillRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.skills)
}
