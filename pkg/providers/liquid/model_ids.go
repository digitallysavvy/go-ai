package liquid

// Known Liquid decision model IDs (TS liquidModelCatalog). Any other string is
// forwarded as is.
const (
	DecisionModelD12      = "d1.2"
	DecisionModelD12Ultra = "d1.2-ultra"
)

// DecisionModelIDs lists the known decision model IDs.
func DecisionModelIDs() []string { return []string{DecisionModelD12, DecisionModelD12Ultra} }

// Adapter limits (TS liquidDecisionModelLimits). They apply to custom model IDs
// as well as to catalog models.
const (
	maxQuestions     = 128
	minChoiceOptions = 2
	maxChoiceOptions = 255
	minScoreLevels   = 2
	maxScoreLevels   = 10
	maxImages        = 8
	maxAudioClips    = 8
)
