package scaledown

// Known ScaleDown model IDs (TS scaledown-model-catalog.ts, 4b02148c83). Any
// other string is forwarded as is.
const (
	DecisionModelClassify  = "classify"
	DecisionModelClassify1 = "classify-1"
)

// DecisionModelIDs lists the known decision model IDs.
func DecisionModelIDs() []string {
	return []string{DecisionModelClassify, DecisionModelClassify1}
}

// Known ScaleDown extraction model IDs (TS scaledown-model-catalog.ts,
// 4b02148c83). Any other string is forwarded as is.
const (
	ExtractionModelExtract  = "extract"
	ExtractionModelExtract1 = "extract-1"
)

// ExtractionModelIDs lists the known extraction model IDs.
func ExtractionModelIDs() []string {
	return []string{ExtractionModelExtract, ExtractionModelExtract1}
}
