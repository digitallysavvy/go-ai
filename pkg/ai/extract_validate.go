package ai

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Mirrors TypeScript's packages/ai/src/extract/validate-extraction.ts.

var extractionDurationUnits = []string{
	provider.ExtractionDurationSeconds, provider.ExtractionDurationMinutes,
	provider.ExtractionDurationHours, provider.ExtractionDurationDays,
	provider.ExtractionDurationWeeks, provider.ExtractionDurationMonths,
	provider.ExtractionDurationYears,
}

var extractionListItems = []string{"text", "number", "date"}

var (
	extractionTimePattern     = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9](:[0-5][0-9])?$`)
	extractionDatePattern     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	extractionDatetimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9](:[0-5][0-9](\.[0-9]{1,9})?)?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])?$`)
)

// isExtractionDate is TS isDate: a YYYY-MM-DD calendar date.
func isExtractionDate(value string) bool {
	if !extractionDatePattern.MatchString(value) {
		return false
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

// extractionNumber reads a Go number value as a float64 (TS number).
func extractionNumber(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	}
	return 0, false
}

// isExtractionNumber is TS isNumber: finite, an integer when integer is set,
// and representable with at most decimals decimal places.
func isExtractionNumber(value interface{}, integer *bool, decimals *int) bool {
	v, ok := extractionNumber(value)
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	if integer != nil && *integer && v != math.Trunc(v) {
		return false
	}
	if decimals != nil {
		rounded, err := strconv.ParseFloat(strconv.FormatFloat(v, 'f', *decimals, 64), 64)
		if err != nil || rounded != v {
			return false
		}
	}
	return true
}

// extractionList reads a list value (TS dense array). ok is false when value
// is not a list; a nil list is an empty list.
func extractionList(value interface{}) ([]interface{}, bool) {
	switch v := value.(type) {
	case []interface{}:
		return v, true
	case []string:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = item
		}
		return out, true
	case []float64:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = item
		}
		return out, true
	case []int:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = item
		}
		return out, true
	}
	return nil, false
}

// extractionValueKind describes what isValue checks: the field type, plus
// the integer / decimals rules of number-like fields.
type extractionValueKind struct {
	fieldType string
	integer   *bool
	decimals  *int
}

func (k extractionValueKind) isValue(value interface{}) bool {
	switch k.fieldType {
	case provider.ExtractionFieldText:
		_, ok := value.(string)
		return ok
	case provider.ExtractionFieldNumber:
		return isExtractionNumber(value, k.integer, k.decimals)
	case provider.ExtractionFieldDuration:
		v, _ := extractionNumber(value)
		return isExtractionNumber(value, k.integer, k.decimals) && v >= 0
	case provider.ExtractionFieldDate:
		s, ok := value.(string)
		return ok && isExtractionDate(s)
	case provider.ExtractionFieldTime:
		s, ok := value.(string)
		return ok && extractionTimePattern.MatchString(s)
	case provider.ExtractionFieldDateTime:
		s, ok := value.(string)
		return ok && extractionDatetimePattern.MatchString(s) && isExtractionDate(s[:10])
	}
	return false
}

func utf16Length(s string) int { return len(utf16.Encode([]rune(s))) }

// isExtractionSource is TS isSource: the source points into the state.
func isExtractionSource(source *provider.ExtractionSource, state []provider.DecisionStatePart) bool {
	if source == nil || source.Part < 0 || source.Part >= len(state) {
		return false
	}
	part := state[source.Part]
	switch source.Type {
	case provider.ExtractionSourceText:
		return part.Type == "text" && source.Start >= 0 && source.Start < source.End && source.End <= utf16Length(part.Text)
	case provider.ExtractionSourceFile:
		return part.Type == "file"
	}
	return false
}

func isExtractionProbability(value *float64) bool {
	return value == nil || isDecisionProbability(*value)
}

// validateExtractionInput mirrors TS validateExtractionInput. Go keeps the
// structural state checks its types do not guarantee (see
// validateDecisionInput).
func validateExtractionInput(state interface{}, fields map[string]provider.ExtractionField, order []string, referenceDate string) error {
	if !isSerializableDecisionJSON(decisionStateJSON(state)) {
		return invalidDecisionInput("state", state, invalidDecisionStateJSONMessage)
	}
	if err := validateDecisionState(state); err != nil {
		return err
	}
	if referenceDate != "" && !isExtractionDate(referenceDate) {
		return invalidDecisionInput("referenceDate", referenceDate, "must be a YYYY-MM-DD calendar date")
	}
	if len(fields) == 0 {
		return invalidDecisionInput("fields", fields, "must be a nonempty field map")
	}
	for _, id := range provider.OrderedKeys(fields, order) {
		field := fields[id]
		parameter := fmt.Sprintf("fields.%s", id)
		if !isSerializableDecisionJSON(field.Instructions) {
			return invalidDecisionInput(parameter, field, "instructions must not contain non-finite numbers, cycles, or array holes")
		}
		if field.Instructions != nil && !isDecisionInput(field.Instructions) {
			return invalidDecisionInput(parameter, field, "instructions must be a JSON-compatible string, object, or array")
		}
		if field.Decimals != nil && (*field.Decimals < 0 || *field.Decimals > 6) {
			return invalidDecisionInput(parameter, field, "decimals must be an integer from 0 to 6")
		}
		if field.Type == provider.ExtractionFieldDuration && field.Unit != "" && !containsString(extractionDurationUnits, field.Unit) {
			return invalidDecisionInput(parameter, field, "duration unit must be one of "+strings.Join(extractionDurationUnits, ", "))
		}
		if field.Type == provider.ExtractionFieldList && !containsString(extractionListItems, field.Items) {
			return invalidDecisionInput(parameter, field, "list items must be one of "+strings.Join(extractionListItems, ", "))
		}
	}
	return nil
}

func invalidExtractionAnswer(answers interface{}, message string) error {
	return invalidDecisionAnswer(answers, message)
}

// normalizeExtractionAnswers is TS extract.ts' empty-value normalization
// (d99098a979): an empty string or empty list value becomes null (and a list
// answer loses its items). The provider's answers are never modified.
func normalizeExtractionAnswers(answers map[string]provider.ExtractionAnswer) map[string]provider.ExtractionAnswer {
	var out map[string]provider.ExtractionAnswer
	for id, answer := range answers {
		if answer.Type == "refusal" {
			continue
		}
		empty := false
		switch v := answer.Value.(type) {
		case string:
			empty = v == ""
		default:
			if list, ok := extractionList(v); ok {
				empty = len(list) == 0
			}
		}
		if !empty {
			continue
		}
		if out == nil {
			out = make(map[string]provider.ExtractionAnswer, len(answers))
			for k, a := range answers {
				out[k] = a
			}
		}
		answer.Value = nil
		if answer.Type == provider.ExtractionFieldList {
			answer.Items = nil
		}
		out[id] = answer
	}
	if out == nil {
		return answers
	}
	return out
}

// validateExtractionAnswers mirrors TS validateExtractionAnswers.
func validateExtractionAnswers(fields map[string]provider.ExtractionField, order []string, state []provider.DecisionStatePart, answers map[string]provider.ExtractionAnswer) error {
	complete := len(answers) == len(fields)
	if complete {
		for id := range fields {
			if _, ok := answers[id]; !ok {
				complete = false
				break
			}
		}
	}
	if !complete {
		return invalidExtractionAnswer(answers, "Extraction must return exactly one answer for every field.")
	}

	for _, id := range provider.OrderedKeys(fields, order) {
		field := fields[id]
		answer := answers[id]
		if answer.Type == "refusal" {
			continue
		}
		if answer.Type != field.Type {
			return invalidExtractionAnswer(answers, fmt.Sprintf("Field \"%s\" returned an answer with the wrong type.", id))
		}
		if !isExtractionProbability(answer.PNull) || !isExtractionProbability(answer.Confidence) {
			return invalidExtractionAnswer(answers, fmt.Sprintf("Field \"%s\" must return pNull and confidence as finite probabilities in [0, 1].", id))
		}

		if field.Type == provider.ExtractionFieldList {
			item := extractionValueKind{fieldType: field.Items}
			if field.Items == "number" {
				item.integer, item.decimals = field.Integer, field.Decimals
			}
			var values []interface{}
			if answer.Value != nil {
				list, ok := extractionList(answer.Value)
				if !ok {
					return invalidExtractionAnswer(answers, fmt.Sprintf("Field \"%s\" must return null or a list of %s values.", id, field.Items))
				}
				for _, entry := range list {
					if !item.isValue(entry) {
						return invalidExtractionAnswer(answers, fmt.Sprintf("Field \"%s\" must return null or a list of %s values.", id, field.Items))
					}
				}
				values = list
			}
			if answer.Items != nil {
				valid := answer.Value != nil && len(answer.Items) == len(values)
				if valid {
					for _, entry := range answer.Items {
						if !isExtractionProbability(entry.Confidence) || (entry.Source != nil && !isExtractionSource(entry.Source, state)) {
							valid = false
							break
						}
					}
				}
				if !valid {
					return invalidExtractionAnswer(answers, fmt.Sprintf("Field \"%s\" must return one item entry per list value, with confidences in [0, 1] and sources inside the state.", id))
				}
			}
			continue
		}

		kind := extractionValueKind{fieldType: field.Type, integer: field.Integer, decimals: field.Decimals}
		if answer.Value != nil && !kind.isValue(answer.Value) {
			return invalidExtractionAnswer(answers, fmt.Sprintf("Field \"%s\" must return null or a valid %s value.", id, field.Type))
		}
		if answer.Guess != nil && !kind.isValue(answer.Guess) {
			return invalidExtractionAnswer(answers, fmt.Sprintf("Field \"%s\" must return a valid %s guess.", id, field.Type))
		}
		if answer.Source != nil && !isExtractionSource(answer.Source, state) {
			return invalidExtractionAnswer(answers, fmt.Sprintf("Field \"%s\" returned a source outside the state.", id))
		}
	}
	return nil
}
