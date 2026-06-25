package jsonparser

type fixJSONState string

const (
	stateRoot                    fixJSONState = "ROOT"
	stateFinish                  fixJSONState = "FINISH"
	stateInsideString            fixJSONState = "INSIDE_STRING"
	stateInsideStringEscape      fixJSONState = "INSIDE_STRING_ESCAPE"
	stateInsideStringUnicode     fixJSONState = "INSIDE_STRING_UNICODE_ESCAPE"
	stateInsideLiteral           fixJSONState = "INSIDE_LITERAL"
	stateInsideNumber            fixJSONState = "INSIDE_NUMBER"
	stateInsideObjectStart       fixJSONState = "INSIDE_OBJECT_START"
	stateInsideObjectKey         fixJSONState = "INSIDE_OBJECT_KEY"
	stateInsideObjectAfterKey    fixJSONState = "INSIDE_OBJECT_AFTER_KEY"
	stateInsideObjectBeforeValue fixJSONState = "INSIDE_OBJECT_BEFORE_VALUE"
	stateInsideObjectAfterValue  fixJSONState = "INSIDE_OBJECT_AFTER_VALUE"
	stateInsideObjectAfterComma  fixJSONState = "INSIDE_OBJECT_AFTER_COMMA"
	stateInsideArrayStart        fixJSONState = "INSIDE_ARRAY_START"
	stateInsideArrayAfterValue   fixJSONState = "INSIDE_ARRAY_AFTER_VALUE"
	stateInsideArrayAfterComma   fixJSONState = "INSIDE_ARRAY_AFTER_COMMA"
)

// FixJSON repairs incomplete JSON using the same state-machine strategy as the
// TypeScript AI SDK's fixJson utility.
func FixJSON(input string) string {
	stack := []fixJSONState{stateRoot}
	lastValidIndex := -1
	literalStart := -1
	unicodeEscapeDigits := 0

	push := func(state fixJSONState) {
		stack = append(stack, state)
	}
	pop := func() {
		if len(stack) > 0 {
			stack = stack[:len(stack)-1]
		}
	}
	top := func() fixJSONState {
		if len(stack) == 0 {
			return stateFinish
		}
		return stack[len(stack)-1]
	}

	processAfterObjectValue := func(ch byte, i int) {
		switch ch {
		case ',':
			pop()
			push(stateInsideObjectAfterComma)
		case '}':
			lastValidIndex = i
			pop()
		}
	}
	processAfterArrayValue := func(ch byte, i int) {
		switch ch {
		case ',':
			pop()
			push(stateInsideArrayAfterComma)
		case ']':
			lastValidIndex = i
			pop()
		}
	}
	processValueStart := func(ch byte, i int, swapState fixJSONState) {
		switch ch {
		case '"':
			lastValidIndex = i
			pop()
			push(swapState)
			push(stateInsideString)
		case 'f', 't', 'n':
			lastValidIndex = i
			literalStart = i
			pop()
			push(swapState)
			push(stateInsideLiteral)
		case '-':
			pop()
			push(swapState)
			push(stateInsideNumber)
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			lastValidIndex = i
			pop()
			push(swapState)
			push(stateInsideNumber)
		case '{':
			lastValidIndex = i
			pop()
			push(swapState)
			push(stateInsideObjectStart)
		case '[':
			lastValidIndex = i
			pop()
			push(swapState)
			push(stateInsideArrayStart)
		}
	}

	for i := 0; i < len(input); i++ {
		ch := input[i]

		switch top() {
		case stateRoot:
			processValueStart(ch, i, stateFinish)

		case stateInsideObjectStart:
			switch ch {
			case '"':
				pop()
				push(stateInsideObjectKey)
			case '}':
				lastValidIndex = i
				pop()
			}

		case stateInsideObjectAfterComma:
			if ch == '"' {
				pop()
				push(stateInsideObjectKey)
			}

		case stateInsideObjectKey:
			if ch == '"' {
				pop()
				push(stateInsideObjectAfterKey)
			}

		case stateInsideObjectAfterKey:
			if ch == ':' {
				pop()
				push(stateInsideObjectBeforeValue)
			}

		case stateInsideObjectBeforeValue:
			processValueStart(ch, i, stateInsideObjectAfterValue)

		case stateInsideObjectAfterValue:
			processAfterObjectValue(ch, i)

		case stateInsideString:
			switch ch {
			case '"':
				pop()
				lastValidIndex = i
			case '\\':
				push(stateInsideStringEscape)
			default:
				lastValidIndex = i
			}

		case stateInsideArrayStart:
			if ch == ']' {
				lastValidIndex = i
				pop()
			} else {
				lastValidIndex = i
				processValueStart(ch, i, stateInsideArrayAfterValue)
			}

		case stateInsideArrayAfterValue:
			switch ch {
			case ',':
				pop()
				push(stateInsideArrayAfterComma)
			case ']':
				lastValidIndex = i
				pop()
			default:
				lastValidIndex = i
			}

		case stateInsideArrayAfterComma:
			processValueStart(ch, i, stateInsideArrayAfterValue)

		case stateInsideStringEscape:
			pop()
			if ch == 'u' {
				unicodeEscapeDigits = 0
				push(stateInsideStringUnicode)
			} else {
				lastValidIndex = i
			}

		case stateInsideStringUnicode:
			if isHexDigit(ch) {
				unicodeEscapeDigits++
				if unicodeEscapeDigits == 4 {
					pop()
					lastValidIndex = i
				}
			}

		case stateInsideNumber:
			switch ch {
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
				lastValidIndex = i
			case 'e', 'E', '-', '.':
			case ',':
				pop()
				if top() == stateInsideArrayAfterValue {
					processAfterArrayValue(ch, i)
				}
				if top() == stateInsideObjectAfterValue {
					processAfterObjectValue(ch, i)
				}
			case '}':
				pop()
				if top() == stateInsideObjectAfterValue {
					processAfterObjectValue(ch, i)
				}
			case ']':
				pop()
				if top() == stateInsideArrayAfterValue {
					processAfterArrayValue(ch, i)
				}
			default:
				pop()
			}

		case stateInsideLiteral:
			partialLiteral := input[literalStart : i+1]
			if !hasLiteralPrefix(partialLiteral) {
				pop()
				if top() == stateInsideObjectAfterValue {
					processAfterObjectValue(ch, i)
				} else if top() == stateInsideArrayAfterValue {
					processAfterArrayValue(ch, i)
				}
			} else {
				lastValidIndex = i
			}
		}
	}

	result := ""
	if lastValidIndex >= 0 {
		result = input[:lastValidIndex+1]
	}

	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i] {
		case stateInsideString:
			result += `"`
		case stateInsideObjectKey,
			stateInsideObjectAfterKey,
			stateInsideObjectAfterComma,
			stateInsideObjectStart,
			stateInsideObjectBeforeValue,
			stateInsideObjectAfterValue:
			result += `}`
		case stateInsideArrayStart,
			stateInsideArrayAfterComma,
			stateInsideArrayAfterValue:
			result += `]`
		case stateInsideLiteral:
			if literalStart >= 0 {
				partialLiteral := input[literalStart:]
				result += literalCompletion(partialLiteral)
			}
		}
	}

	return result
}

func isHexDigit(ch byte) bool {
	return (ch >= '0' && ch <= '9') ||
		(ch >= 'A' && ch <= 'F') ||
		(ch >= 'a' && ch <= 'f')
}

func hasLiteralPrefix(s string) bool {
	return hasPrefix("true", s) || hasPrefix("false", s) || hasPrefix("null", s)
}

func literalCompletion(partial string) string {
	switch {
	case hasPrefix("true", partial):
		return "true"[len(partial):]
	case hasPrefix("false", partial):
		return "false"[len(partial):]
	case hasPrefix("null", partial):
		return "null"[len(partial):]
	default:
		return ""
	}
}

func hasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return s[:len(prefix)] == prefix
}
