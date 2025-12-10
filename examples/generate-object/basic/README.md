# Generate Object - Basic Example

Learn how to generate structured JSON objects using AI with type-safe schemas.

## Features Demonstrated

- **JSON Schema definition** for structured output
- **Type-safe object generation** with Go structs
- **Schema validation** with required fields
- **Complex nested structures** (arrays of objects)
- **Multiple generation examples** with different prompts
- **Usage tracking** for API calls

## Prerequisites

- Go 1.21 or higher
- OpenAI API key

## Setup

```bash
export OPENAI_API_KEY=sk-...
```

## Running the Example

```bash
cd examples/generate-object/basic
go run main.go
```

## What You'll See

The example demonstrates three different scenarios:

### 1. Simple Recipe Generation

Generates a lasagna recipe with structured ingredients and steps:

```
Recipe: Classic Lasagna
Prep Time: 30 minutes
Cook Time: 60 minutes

Ingredients:
  - Lasagna noodles: 1 lb
  - Ground beef: 1 lb
  - Ricotta cheese: 2 cups
  - Mozzarella cheese: 2 cups
  ...

Steps:
  1. Preheat oven to 375°F
  2. Cook lasagna noodles according to package directions
  3. Brown ground beef in a large skillet
  ...
```

### 2. Vegan Recipe with Constraints

Generates a vegan pasta recipe with specific requirements (under 30 minutes):

```json
{
  "name": "Quick Vegan Pasta Primavera",
  "ingredients": [
    {"name": "Pasta", "amount": "12 oz"},
    {"name": "Cherry tomatoes", "amount": "2 cups"},
    ...
  ],
  "steps": [...],
  "prepTime": 10,
  "cookTime": 15
}
```

### 3. Multiple Recipes

Generates 3 recipes with different difficulty levels and cuisines.

## Code Highlights

### Defining a JSON Schema

```go
recipeSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name": map[string]interface{}{
            "type":        "string",
            "description": "The name of the recipe",
        },
        "ingredients": map[string]interface{}{
            "type": "array",
            "items": map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "name":   map[string]interface{}{"type": "string"},
                    "amount": map[string]interface{}{"type": "string"},
                },
                "required": []string{"name", "amount"},
            },
        },
        "steps": map[string]interface{}{
            "type":  "array",
            "items": map[string]interface{}{"type": "string"},
        },
    },
    "required": []string{"name", "ingredients", "steps"},
})
```

### Generating the Object

```go
result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
    Model:  model,
    Prompt: "Generate a lasagna recipe.",
    Schema: recipeSchema,
})
if err != nil {
    log.Fatal(err)
}

// The result.Object contains the structured data
// You can marshal it to your Go struct:
var recipe Recipe
jsonBytes, _ := json.Marshal(result.Object)
json.Unmarshal(jsonBytes, &recipe)
```

### Using the Generated Object

```go
fmt.Printf("Recipe: %s\n", recipe.Name)
fmt.Printf("Prep Time: %d minutes\n", recipe.PrepTime)

for _, ing := range recipe.Ingredients {
    fmt.Printf("  - %s: %s\n", ing.Name, ing.Amount)
}
```

## What You'll Learn

1. **Schema Definition**: How to define JSON schemas for structured output
2. **Type Safety**: How to map JSON schemas to Go structs
3. **Complex Types**: Working with nested arrays and objects
4. **Validation**: Using required fields and type constraints
5. **Prompt Engineering**: How to guide the AI with clear prompts
6. **Error Handling**: Proper error handling for generation failures

## JSON Schema Basics

### Simple Types

```go
// String field
"name": map[string]interface{}{
    "type": "string",
    "description": "Field description",
}

// Integer field
"age": map[string]interface{}{
    "type": "integer",
    "minimum": 0,
    "maximum": 150,
}

// Boolean field
"isActive": map[string]interface{}{
    "type": "boolean",
}
```

### Complex Types

```go
// Array of strings
"tags": map[string]interface{}{
    "type": "array",
    "items": map[string]interface{}{"type": "string"},
}

// Array of objects
"items": map[string]interface{}{
    "type": "array",
    "items": map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "name":  map[string]interface{}{"type": "string"},
            "value": map[string]interface{}{"type": "number"},
        },
    },
}

// Enum (restricted values)
"status": map[string]interface{}{
    "type": "string",
    "enum": []string{"pending", "active", "completed"},
}
```

### Required Fields

```go
schema := map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name": map[string]interface{}{"type": "string"},
        "email": map[string]interface{}{"type": "string"},
    },
    "required": []string{"name", "email"},  // These fields must be present
}
```

## Common Use Cases

### 1. Data Extraction

Extract structured data from unstructured text:

```go
// Extract contact information
schema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name":  map[string]interface{}{"type": "string"},
        "email": map[string]interface{}{"type": "string"},
        "phone": map[string]interface{}{"type": "string"},
    },
})

result, _ := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
    Model:  model,
    Prompt: "Extract contact info from: John Smith, john@example.com, 555-1234",
    Schema: schema,
})
```

### 2. Content Generation

Generate structured content like blog posts, product descriptions:

```go
articleSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "title":    map[string]interface{}{"type": "string"},
        "summary":  map[string]interface{}{"type": "string"},
        "sections": map[string]interface{}{
            "type": "array",
            "items": map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "heading": map[string]interface{}{"type": "string"},
                    "content": map[string]interface{}{"type": "string"},
                },
            },
        },
    },
})
```

### 3. Classification

Classify text into structured categories:

```go
classificationSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "category": map[string]interface{}{
            "type": "string",
            "enum": []string{"urgent", "normal", "low"},
        },
        "sentiment": map[string]interface{}{
            "type": "string",
            "enum": []string{"positive", "neutral", "negative"},
        },
        "topics": map[string]interface{}{
            "type":  "array",
            "items": map[string]interface{}{"type": "string"},
        },
    },
})
```

## Next Steps

- **[validation](../validation)** - Add JSON schema validation
- **[complex](../complex)** - Work with more complex nested structures
- **[stream-object](../../stream-object)** - Stream objects as they're generated
- **[providers/openai/structured-outputs](../../providers/openai/structured-outputs)** - Use OpenAI's native structured outputs

## Troubleshooting

### Schema Not Followed

If the AI doesn't follow your schema:
1. Make your prompt more explicit about the requirements
2. Add descriptions to schema fields
3. Use required fields to enforce presence
4. Try a more capable model (e.g., gpt-4 instead of gpt-4o-mini)

### Invalid JSON Output

If you get invalid JSON:
1. Check your schema for errors
2. Ensure all property names are valid
3. Verify required fields are properly specified
4. Use the validation example to add JSON validation

### Performance Issues

If generation is slow:
1. Use gpt-4o-mini for faster responses
2. Reduce schema complexity
3. Keep prompts concise
4. Set appropriate maxTokens limits

## Documentation

- [Go AI SDK Documentation](../../../docs)
- [JSON Schema Specification](https://json-schema.org/)
- [OpenAI Function Calling](https://platform.openai.com/docs/guides/function-calling)
