# Generate Object - Complex Example

Demonstrates deeply nested structures, optional fields, and complex relationships for advanced structured AI output.

## Features Demonstrated

- **Deep nesting** (5+ levels)
- **Arrays of complex objects**
- **Optional fields** and nullable values
- **Multiple relationship types**
- **Real-world data models** (company, e-commerce, education)
- **Mixed data types** in nested structures

## Prerequisites

- Go 1.21 or higher
- OpenAI API key
- Recommended: gpt-4o or gpt-4 for best results with complex schemas

## Setup

```bash
export OPENAI_API_KEY=sk-...
```

## Running the Example

```bash
cd examples/generate-object/complex
go run main.go
```

## Examples

### 1. Complex Nested Company Structure

Generates a complete organizational hierarchy:

```json
{
  "company": {
    "name": "TechCorp Solutions",
    "founded": 2015,
    "headquarters": {
      "city": "San Francisco",
      "country": "USA",
      "address": {
        "street": "123 Tech Street",
        "zipCode": "94105"
      }
    },
    "departments": [
      {
        "name": "Engineering",
        "head": {
          "name": "Jane Smith",
          "email": "jane@techcorp.com",
          "phone": "+1-555-0100"
        },
        "teams": [
          {
            "name": "Backend",
            "members": [
              {
                "name": "John Doe",
                "role": "Software Engineer",
                "email": "john@techcorp.com",
                "seniority": "senior"
              }
            ],
            "projects": [
              {
                "name": "API Redesign",
                "status": "active",
                "budget": 250000,
                "deadline": "2024-06-30"
              }
            ]
          }
        ]
      }
    ]
  }
}
```

### 2. Deep E-commerce Order

Complete order with customer, items, pricing, and shipping:

```json
{
  "order": {
    "orderId": "ORD-2024-001",
    "orderStatus": "processing",
    "customer": {
      "customerId": "CUST-12345",
      "name": "Alice Johnson",
      "addresses": [
        {
          "type": "shipping",
          "street": "456 Main St",
          "city": "Boston",
          "state": "MA",
          "zipCode": "02101",
          "country": "USA"
        }
      ],
      "paymentMethods": [
        {
          "type": "credit_card",
          "last4": "4242"
        }
      ]
    },
    "items": [
      {
        "productId": "PROD-001",
        "productName": "Wireless Headphones",
        "quantity": 1,
        "unitPrice": 199.99,
        "discount": {
          "amount": 20.00,
          "percentage": 10,
          "code": "SAVE10"
        },
        "subtotal": 179.99
      }
    ],
    "pricing": {
      "subtotal": 179.99,
      "tax": 14.40,
      "shipping": 9.99,
      "total": 204.38,
      "currency": "USD"
    }
  }
}
```

### 3. Course Curriculum with Modules

Full educational course structure:

```json
{
  "course": {
    "title": "Introduction to Machine Learning",
    "code": "CS-401",
    "credits": 4,
    "level": "intermediate",
    "modules": [
      {
        "moduleNumber": 1,
        "title": "Foundations",
        "lessons": [
          {
            "lessonNumber": 1,
            "title": "What is Machine Learning?",
            "type": "video",
            "duration": "45 minutes",
            "resources": [
              {
                "title": "ML Basics",
                "type": "pdf",
                "url": "https://example.com/ml-basics.pdf"
              }
            ]
          }
        ],
        "assessment": {
          "type": "quiz",
          "weight": 10,
          "duration": "30 minutes"
        }
      }
    ]
  }
}
```

## Design Patterns for Complex Schemas

### 1. Deep Nesting Strategy

```go
// Level 1: Root
"company": map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        // Level 2: Department
        "departments": map[string]interface{}{
            "type": "array",
            "items": map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    // Level 3: Team
                    "teams": map[string]interface{}{
                        "type": "array",
                        "items": map[string]interface{}{
                            "type": "object",
                            "properties": map[string]interface{}{
                                // Level 4: Member
                                "members": map[string]interface{}{
                                    // ...
                                },
                            },
                        },
                    },
                },
            },
        },
    },
}
```

### 2. Optional Fields Pattern

```go
// All fields are optional by default unless in "required" array
schema := map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "required_field": map[string]interface{}{"type": "string"},
        "optional_field": map[string]interface{}{"type": "string"},
    },
    "required": []string{"required_field"},  // Only this is required
}
```

### 3. Polymorphic Types

```go
// Using oneOf for different possible structures
"payment": map[string]interface{}{
    "oneOf": []map[string]interface{}{
        {
            "type": "object",
            "properties": map[string]interface{}{
                "type":       map[string]interface{}{"const": "card"},
                "cardNumber": map[string]interface{}{"type": "string"},
            },
        },
        {
            "type": "object",
            "properties": map[string]interface{}{
                "type":  map[string]interface{}{"const": "paypal"},
                "email": map[string]interface{}{"type": "string"},
            },
        },
    },
}
```

### 4. Self-Referencing Schemas

```go
// For tree structures (e.g., file systems, org charts)
"node": map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name": map[string]interface{}{"type": "string"},
        "children": map[string]interface{}{
            "type": "array",
            "items": map[string]interface{}{
                "$ref": "#/definitions/node",  // References itself
            },
        },
    },
}
```

## Common Use Cases

### 1. API Response Modeling

```go
apiResponseSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "status": map[string]interface{}{"type": "string"},
        "data": map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "users": map[string]interface{}{
                    "type": "array",
                    "items": map[string]interface{}{
                        "type": "object",
                        "properties": map[string]interface{}{
                            "id":      map[string]interface{}{"type": "string"},
                            "profile": map[string]interface{}{
                                "type": "object",
                                // Nested profile structure
                            },
                        },
                    },
                },
            },
        },
        "meta": map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "page":       map[string]interface{}{"type": "integer"},
                "totalPages": map[string]interface{}{"type": "integer"},
            },
        },
    },
})
```

### 2. Configuration Management

```go
configSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "application": map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "server": map[string]interface{}{
                    "type": "object",
                    "properties": map[string]interface{}{
                        "host": map[string]interface{}{"type": "string"},
                        "port": map[string]interface{}{"type": "integer"},
                    },
                },
                "database": map[string]interface{}{
                    "type": "object",
                    "properties": map[string]interface{}{
                        "connections": map[string]interface{}{
                            "type": "array",
                            "items": map[string]interface{}{
                                "type": "object",
                                // Connection details
                            },
                        },
                    },
                },
            },
        },
    },
})
```

### 3. Data Migration

```go
// Converting between different data formats
migrationSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "sourceData": map[string]interface{}{
            "type": "object",
            // Old format
        },
        "targetData": map[string]interface{}{
            "type": "object",
            // New format with transformations
        },
        "mappings": map[string]interface{}{
            "type": "array",
            "items": map[string]interface{}{
                "type": "object",
                "properties": map[string]interface{}{
                    "sourceField": map[string]interface{}{"type": "string"},
                    "targetField": map[string]interface{}{"type": "string"},
                    "transform":   map[string]interface{}{"type": "string"},
                },
            },
        },
    },
})
```

## Performance Considerations

### 1. Token Usage

Complex schemas consume more tokens:
- Simple schema: ~100-200 tokens
- Medium schema: ~500-1000 tokens
- Complex schema: ~2000-5000 tokens

**Tip:** Use gpt-4o-mini for cost-effective complex generation

### 2. Generation Time

Deeply nested structures take longer:
- Simple: 2-5 seconds
- Complex: 10-30 seconds

**Tip:** Set appropriate context timeouts (60s+)

### 3. Model Selection

| Model | Best For |
|-------|----------|
| gpt-4o | Most complex schemas, highest accuracy |
| gpt-4 | Complex schemas, good balance |
| gpt-4o-mini | Simple to medium complexity, cost-effective |
| gpt-3.5-turbo | Simple schemas only |

## Troubleshooting

### Incomplete Nested Structures

If deep nesting is incomplete:
1. Use gpt-4o or gpt-4
2. Increase maxTokens (3000-4000)
3. Simplify the schema depth
4. Break into multiple generation calls

### Schema Complexity Errors

If generation fails with complex schemas:
1. Validate your JSON schema syntax
2. Reduce nesting depth (max 5-6 levels)
3. Split into smaller schemas and combine results
4. Add clear descriptions to guide the AI

### Inconsistent Data Relationships

If related fields don't match:
1. Be explicit in the prompt about relationships
2. Use descriptions to clarify dependencies
3. Consider generating in stages

## Best Practices

1. **Keep schemas focused**: Each schema should represent one clear entity
2. **Use descriptions**: Help the AI understand field purposes
3. **Test incrementally**: Build complex schemas from simple ones
4. **Set appropriate limits**: Use minItems/maxItems to control size
5. **Validate output**: Always validate generated JSON
6. **Cache schemas**: Reuse schema definitions across calls

## Next Steps

- **[basic](../basic)** - Start with simple structures
- **[validation](../validation)** - Add constraints and validation
- **[stream-object](../../stream-object)** - Stream complex objects
- **[http-server](../../http-server)** - Serve structured data via API

## Documentation

- [JSON Schema Specification](https://json-schema.org/)
- [Go AI SDK Docs](../../../docs)
