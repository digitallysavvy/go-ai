package anthropic

import (
	"testing"
)

func TestCreateBedrockCachePoint(t *testing.T) {
	tests := []struct {
		name        string
		ttl         *BedrockCacheTTL
		expectedTTL *BedrockCacheTTL
	}{
		{
			name:        "no TTL",
			ttl:         nil,
			expectedTTL: nil,
		},
		{
			name:        "5 minute TTL",
			ttl:         ptr(CacheTTL5Minutes),
			expectedTTL: ptr(CacheTTL5Minutes),
		},
		{
			name:        "1 hour TTL",
			ttl:         ptr(CacheTTL1Hour),
			expectedTTL: ptr(CacheTTL1Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CreateBedrockCachePoint(tt.ttl)

			if result.Type != "default" {
				t.Errorf("Expected type 'default', got '%s'", result.Type)
			}

			if tt.expectedTTL == nil {
				if result.TTL != nil {
					t.Errorf("Expected nil TTL, got %v", result.TTL)
				}
			} else {
				if result.TTL == nil {
					t.Errorf("Expected TTL %v, got nil", *tt.expectedTTL)
				} else if *result.TTL != *tt.expectedTTL {
					t.Errorf("Expected TTL %v, got %v", *tt.expectedTTL, *result.TTL)
				}
			}
		})
	}
}

func TestNewCacheConfig(t *testing.T) {
	t.Run("empty config", func(t *testing.T) {
		config := NewCacheConfig()

		if config.TTL != nil {
			t.Errorf("Expected nil TTL, got %v", config.TTL)
		}
		if config.CacheSystem {
			t.Error("Expected CacheSystem to be false")
		}
		if config.CacheTools {
			t.Error("Expected CacheTools to be false")
		}
		if len(config.CacheMessageIndices) != 0 {
			t.Errorf("Expected empty CacheMessageIndices, got %v", config.CacheMessageIndices)
		}
	})

	t.Run("with TTL", func(t *testing.T) {
		config := NewCacheConfig(WithCacheTTL(CacheTTL1Hour))

		if config.TTL == nil {
			t.Error("Expected TTL to be set")
		} else if *config.TTL != CacheTTL1Hour {
			t.Errorf("Expected TTL %v, got %v", CacheTTL1Hour, *config.TTL)
		}
	})

	t.Run("with system cache", func(t *testing.T) {
		config := NewCacheConfig(WithSystemCache())

		if !config.CacheSystem {
			t.Error("Expected CacheSystem to be true")
		}
	})

	t.Run("with tool cache", func(t *testing.T) {
		config := NewCacheConfig(WithToolCache())

		if !config.CacheTools {
			t.Error("Expected CacheTools to be true")
		}
	})

	t.Run("with message indices", func(t *testing.T) {
		indices := []int{0, 2, 5}
		config := NewCacheConfig(WithMessageCacheIndices(indices...))

		if len(config.CacheMessageIndices) != len(indices) {
			t.Errorf("Expected %d indices, got %d", len(indices), len(config.CacheMessageIndices))
		}

		for i, expected := range indices {
			if config.CacheMessageIndices[i] != expected {
				t.Errorf("Expected index %d at position %d, got %d", expected, i, config.CacheMessageIndices[i])
			}
		}
	})

	t.Run("combined options", func(t *testing.T) {
		config := NewCacheConfig(
			WithCacheTTL(CacheTTL5Minutes),
			WithSystemCache(),
			WithToolCache(),
			WithMessageCacheIndices(0, 3),
		)

		if config.TTL == nil || *config.TTL != CacheTTL5Minutes {
			t.Errorf("Expected TTL %v, got %v", CacheTTL5Minutes, config.TTL)
		}
		if !config.CacheSystem {
			t.Error("Expected CacheSystem to be true")
		}
		if !config.CacheTools {
			t.Error("Expected CacheTools to be true")
		}
		if len(config.CacheMessageIndices) != 2 {
			t.Errorf("Expected 2 indices, got %d", len(config.CacheMessageIndices))
		}
	})
}

// Helper function to create a pointer to a BedrockCacheTTL
func ptr(ttl BedrockCacheTTL) *BedrockCacheTTL {
	return &ttl
}
