package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	model, err := p.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Math Agent with Multiple Tools ===")

	// Define math tools
	tools := []types.Tool{
		createCalculatorTool(),
		createSquareRootTool(),
		createPowerTool(),
		createFactorialTool(),
	}

	// Test problems
	problems := []string{
		"Calculate the square root of 144 plus 8",
		"What is 5 factorial times 3?",
		"Calculate 2 to the power of 10, then divide by 4",
		"Find the square root of (15 + 25) * 2",
	}

	for i, problem := range problems {
		fmt.Printf("Problem %d: %s\n", i+1, problem)
		solveProblem(ctx, model, problem, tools)
		fmt.Println(strings.Repeat("-", 60))
	}
}

func solveProblem(ctx context.Context, model provider.LanguageModel, problem string, tools []types.Tool) {
	stepNum := 0

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: problem,
		System: `You are a mathematical assistant with access to calculator tools.
Solve the problem step by step, using the tools as needed.
Show your work and explain your reasoning.`,
		Tools:    tools,
		StopWhen: []ai.StopCondition{ai.StepCountIs(10)},
		OnStepFinish: func(ctx context.Context, step types.StepResult, userContext interface{}) {
			stepNum++
			fmt.Printf("  Step %d:\n", stepNum)

			if len(step.ToolCalls) > 0 {
				for _, tc := range step.ToolCalls {
					fmt.Printf("    Tool: %s\n", tc.ToolName)
					fmt.Printf("    Args: %v\n", tc.Arguments)
				}
			}

			if len(step.ToolResults) > 0 {
				for _, tr := range step.ToolResults {
					fmt.Printf("    Result: %v\n", tr.Result)
				}
			}

			if step.Text != "" {
				fmt.Printf("    Reasoning: %s\n", step.Text)
			}
		},
	})

	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Printf("\nFinal Answer: %s\n", result.Text)
	fmt.Printf("Total Steps: %d\n", len(result.Steps))
	fmt.Printf("Token Usage: %d\n\n", result.Usage.TotalTokens)
}

func createCalculatorTool() types.Tool {
	return types.Tool{
		Name:        "calculator",
		Description: "Perform basic arithmetic operations: add, subtract, multiply, divide",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"enum": []string{"add", "subtract", "multiply", "divide"},
				},
				"a": map[string]interface{}{
					"type":        "number",
					"description": "First number",
				},
				"b": map[string]interface{}{
					"type":        "number",
					"description": "Second number",
				},
			},
			"required": []string{"operation", "a", "b"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			operation := params["operation"].(string)
			a := params["a"].(float64)
			b := params["b"].(float64)

			var result float64
			switch operation {
			case "add":
				result = a + b
			case "subtract":
				result = a - b
			case "multiply":
				result = a * b
			case "divide":
				if b == 0 {
					return nil, fmt.Errorf("division by zero")
				}
				result = a / b
			}

			return map[string]interface{}{
				"result":    result,
				"operation": operation,
				"operands":  []float64{a, b},
			}, nil
		},
	}
}

func createSquareRootTool() types.Tool {
	return types.Tool{
		Name:        "sqrt",
		Description: "Calculate the square root of a number",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"number": map[string]interface{}{
					"type":        "number",
					"description": "Number to find square root of",
					"minimum":     0,
				},
			},
			"required": []string{"number"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			number := params["number"].(float64)
			if number < 0 {
				return nil, fmt.Errorf("cannot calculate square root of negative number")
			}

			result := math.Sqrt(number)
			return map[string]interface{}{
				"result": result,
				"input":  number,
			}, nil
		},
	}
}

func createPowerTool() types.Tool {
	return types.Tool{
		Name:        "power",
		Description: "Calculate a number raised to a power (base^exponent)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"base": map[string]interface{}{
					"type":        "number",
					"description": "Base number",
				},
				"exponent": map[string]interface{}{
					"type":        "number",
					"description": "Exponent",
				},
			},
			"required": []string{"base", "exponent"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			base := params["base"].(float64)
			exponent := params["exponent"].(float64)

			result := math.Pow(base, exponent)
			return map[string]interface{}{
				"result":   result,
				"base":     base,
				"exponent": exponent,
			}, nil
		},
	}
}

func createFactorialTool() types.Tool {
	return types.Tool{
		Name:        "factorial",
		Description: "Calculate the factorial of a positive integer (n!)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"n": map[string]interface{}{
					"type":        "integer",
					"description": "Positive integer",
					"minimum":     0,
					"maximum":     20,
				},
			},
			"required": []string{"n"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			// Handle both float64 and string from JSON
			var n int64
			switch v := params["n"].(type) {
			case float64:
				n = int64(v)
			case string:
				parsed, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid integer: %v", v)
				}
				n = parsed
			default:
				return nil, fmt.Errorf("invalid type for n: %T", v)
			}

			if n < 0 {
				return nil, fmt.Errorf("factorial not defined for negative numbers")
			}
			if n > 20 {
				return nil, fmt.Errorf("factorial too large (max 20)")
			}

			result := int64(1)
			for i := int64(2); i <= n; i++ {
				result *= i
			}

			return map[string]interface{}{
				"result": result,
				"input":  n,
			}, nil
		},
	}
}
