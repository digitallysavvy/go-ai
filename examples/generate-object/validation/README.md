# Generate Object - Validation Example

Demonstrates JSON schema validation with constraints, patterns, and enums for structured AI output.

## Features Demonstrated

- **Type constraints** (minLength, maxLength, minimum, maximum)
- **Pattern validation** (regex patterns for IDs, emails)
- **Enum validation** (restricted value sets)
- **Required fields** enforcement
- **Array constraints** (minItems, maxItems)
- **Format validation** (email format)
- **Boolean validation**
- **Additional properties** control

## Prerequisites

- Go 1.21 or higher
- OpenAI API key

## Setup

```bash
export OPENAI_API_KEY=sk-...
```

## Running the Example

```bash
cd examples/generate-object/validation
go run main.go
```

## What You'll See

### 1. User Profile with Strict Validation

```
User Profile:
  ID: USR-123456
  Name: John Smith
  Email: john.smith@example.com
  Age: 28
  Role: moderator
  Active: true
```

The schema enforces:
- ID pattern: `^[A-Z]{3}-[0-9]{6}$`
- Name length: 2-50 characters
- Email format validation
- Age range: 18-120
- Role enum: admin, user, moderator, guest

### 2. Product Review with Constraints

```
Product Review:
Product: Wireless Keyboard Model X
Rating: 4/5 ⭐
Title: Great keyboard with minor issues
Comment: This wireless keyboard works great for daily use...

Pros:
  + Comfortable typing experience
  + Long battery life
  + Compact design

Cons:
  - Connection occasionally drops
  - No backlight
```

Enforced constraints:
- Rating: 1-5
- Title: 5-100 chars
- Comment: 20-500 chars
- Pros/Cons: 1-5 items each

### 3. Email Classification with Enums

```json
{
  "category": "urgent",
  "sentiment": "negative",
  "priority": 5,
  "requiresResponse": true,
  "topics": ["server downtime", "production issue", "customer impact"]
}
```

## Schema Validation Examples

### String Constraints

```go
"name": map[string]interface{}{
    "type": "string",
    "minLength": 2,      // Minimum 2 characters
    "maxLength": 50,     // Maximum 50 characters
}

"email": map[string]interface{}{
    "type": "string",
    "format": "email",   // Must be valid email
}

"id": map[string]interface{}{
    "type": "string",
    "pattern": "^[A-Z]{3}-[0-9]{6}$",  // Regex pattern
}
```

### Number Constraints

```go
"age": map[string]interface{}{
    "type": "integer",
    "minimum": 18,       // At least 18
    "maximum": 120,      // At most 120
}

"price": map[string]interface{}{
    "type": "number",
    "minimum": 0,
    "exclusiveMinimum": true,  // Greater than 0
}

"discount": map[string]interface{}{
    "type": "number",
    "minimum": 0,
    "maximum": 100,
    "multipleOf": 5,     // Must be multiple of 5
}
```

### Enum (Restricted Values)

```go
"status": map[string]interface{}{
    "type": "string",
    "enum": []string{"pending", "active", "completed", "cancelled"},
}

"priority": map[string]interface{}{
    "type": "integer",
    "enum": []int{1, 2, 3, 4, 5},
}
```

### Array Constraints

```go
"tags": map[string]interface{}{
    "type": "array",
    "items": map[string]interface{}{"type": "string"},
    "minItems": 1,       // At least 1 item
    "maxItems": 5,       // At most 5 items
    "uniqueItems": true, // No duplicates
}

"scores": map[string]interface{}{
    "type": "array",
    "items": map[string]interface{}{
        "type": "integer",
        "minimum": 0,
        "maximum": 100,
    },
}
```

### Required Fields

```go
schema := map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name": map[string]interface{}{"type": "string"},
        "email": map[string]interface{}{"type": "string"},
        "age": map[string]interface{}{"type": "integer"},
    },
    "required": []string{"name", "email"},  // name and email are required
}
```

### Additional Properties

```go
schema := map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name": map[string]interface{}{"type": "string"},
    },
    "additionalProperties": false,  // No extra fields allowed
}

// Or allow additional properties with type constraint:
schema := map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name": map[string]interface{}{"type": "string"},
    },
    "additionalProperties": map[string]interface{}{"type": "string"},
}
```

## Common Validation Patterns

### Email Validation

```go
"email": map[string]interface{}{
    "type": "string",
    "format": "email",
    "pattern": "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$",
}
```

### URL Validation

```go
"website": map[string]interface{}{
    "type": "string",
    "format": "uri",
    "pattern": "^https?://",
}
```

### Date Validation

```go
"birthDate": map[string]interface{}{
    "type": "string",
    "format": "date",  // YYYY-MM-DD
    "pattern": "^\\d{4}-\\d{2}-\\d{2}$",
}

"timestamp": map[string]interface{}{
    "type": "string",
    "format": "date-time",  // ISO 8601
}
```

### Phone Number Validation

```go
"phone": map[string]interface{}{
    "type": "string",
    "pattern": "^\\+?[1-9]\\d{1,14}$",  // E.164 format
}
```

### Credit Card Validation

```go
"cardNumber": map[string]interface{}{
    "type": "string",
    "pattern": "^[0-9]{13,19}$",
    "minLength": 13,
    "maxLength": 19,
}
```

## Use Cases

### 1. Form Validation

Validate user input before processing:

```go
formSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "username": map[string]interface{}{
            "type": "string",
            "minLength": 3,
            "maxLength": 20,
            "pattern": "^[a-zA-Z0-9_]+$",
        },
        "password": map[string]interface{}{
            "type": "string",
            "minLength": 8,
        },
        "terms": map[string]interface{}{
            "type": "boolean",
            "const": true,  // Must be true
        },
    },
    "required": []string{"username", "password", "terms"},
})
```

### 2. API Response Validation

Ensure API responses match expected format:

```go
apiSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "status": map[string]interface{}{
            "type": "string",
            "enum": []string{"success", "error"},
        },
        "data": map[string]interface{}{"type": "object"},
        "timestamp": map[string]interface{}{
            "type": "integer",
            "minimum": 0,
        },
    },
    "required": []string{"status"},
})
```

### 3. Configuration Validation

Validate configuration files:

```go
configSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "port": map[string]interface{}{
            "type": "integer",
            "minimum": 1024,
            "maximum": 65535,
        },
        "host": map[string]interface{}{
            "type": "string",
            "format": "hostname",
        },
        "debug": map[string]interface{}{
            "type": "boolean",
            "default": false,
        },
    },
})
```

## Troubleshooting

### Validation Failures

If the AI generates output that doesn't match your schema:

1. **Add descriptions** to help the AI understand constraints:
```go
"age": map[string]interface{}{
    "type": "integer",
    "minimum": 18,
    "description": "Must be at least 18 years old",
}
```

2. **Be explicit in prompts**:
```go
Prompt: "Generate a user profile. Age must be between 18 and 120."
```

3. **Use stricter models**: gpt-4 follows schemas better than gpt-4o-mini

### Pattern Matching Issues

If regex patterns aren't respected:
- Escape special characters properly: `\\d` not `\d`
- Keep patterns simple
- Provide examples in the prompt

### Enum Not Followed

If the AI generates values outside your enum:
- Make the enum values clear in descriptions
- Mention the allowed values in the prompt
- Use a more capable model

## Next Steps

- **[complex](../complex)** - Deep nesting and optional fields
- **[basic](../basic)** - Simple structured output
- **[stream-object](../../stream-object)** - Stream validated objects
- **[providers/openai/structured-outputs](../../providers/openai/structured-outputs)** - Native validation

## Documentation

- [JSON Schema Specification](https://json-schema.org/)
- [JSON Schema Validation](https://json-schema.org/understanding-json-schema/reference/string.html)
- [Go AI SDK Docs](../../../docs)
