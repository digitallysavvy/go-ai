package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/golang-jwt/jwt/v5"
)

// MCPAuthServer implements MCP with authentication
type MCPAuthServer struct {
	provider provider.Provider
	apiKey   string
	jwtSecret []byte
	apiKeys   map[string]string // API key -> user mapping
}

// Claims represents JWT claims
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})

	server := &MCPAuthServer{
		provider:  p,
		apiKey:    apiKey,
		jwtSecret: []byte(os.Getenv("JWT_SECRET")),
		apiKeys: map[string]string{
			"key_test123":     "user1",
			"key_demo456":     "user2",
			"key_production":  "prod_user",
		},
	}

	if len(server.jwtSecret) == 0 {
		server.jwtSecret = []byte("default-secret-change-in-production")
	}

	// Routes
	http.HandleFunc("/auth/login", server.handleLogin)
	http.HandleFunc("/auth/refresh", server.handleRefresh)
	http.HandleFunc("/mcp/tools", server.authenticate(server.handleTools))
	http.HandleFunc("/mcp/generate", server.authenticate(server.handleGenerate))
	http.HandleFunc("/health", server.handleHealth)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("🔐 MCP server with auth on :%s\n", port)
	fmt.Println("\nAuthentication methods:")
	fmt.Println("  1. API Key (Header: X-API-Key)")
	fmt.Println("  2. JWT Token (Header: Authorization: Bearer <token>)")
	fmt.Println("\nTest credentials:")
	fmt.Println("  API Key: key_test123")
	fmt.Println("  Username: testuser")
	fmt.Println("  Password: testpass")

	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// handleLogin authenticates user and returns JWT token
func (s *MCPAuthServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate credentials (simplified - use proper auth in production)
	if req.Username != "testuser" || req.Password != "testpass" {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Generate JWT token
	token, err := s.generateToken(req.Username)
	if err != nil {
		http.Error(w, "Token generation failed", http.StatusInternalServerError)
		return
	}

	// Generate refresh token
	refreshToken, err := s.generateRefreshToken(req.Username)
	if err != nil {
		http.Error(w, "Refresh token generation failed", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token":  token,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    3600, // 1 hour
	})
}

// handleRefresh refreshes an expired token
func (s *MCPAuthServer) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate refresh token
	token, err := jwt.ParseWithClaims(req.RefreshToken, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		http.Error(w, "Invalid refresh token", http.StatusUnauthorized)
		return
	}

	claims := token.Claims.(*Claims)

	// Generate new access token
	newToken, err := s.generateToken(claims.Username)
	if err != nil {
		http.Error(w, "Token generation failed", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token": newToken,
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}

// authenticate middleware checks API key or JWT token
func (s *MCPAuthServer) authenticate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Try API key first
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "" {
			if username, ok := s.apiKeys[apiKey]; ok {
				// Valid API key - add username to context
				ctx := context.WithValue(r.Context(), "username", username)
				next(w, r.WithContext(ctx))
				return
			}
			http.Error(w, "Invalid API key", http.StatusUnauthorized)
			return
		}

		// Try JWT token
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing authentication", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
			return
		}

		token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return s.jwtSecret, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		claims := token.Claims.(*Claims)
		ctx := context.WithValue(r.Context(), "username", claims.Username)
		next(w, r.WithContext(ctx))
	}
}

// generateToken creates a new JWT access token
func (s *MCPAuthServer) generateToken(username string) (string, error) {
	claims := &Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "mcp-auth-server",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

// generateRefreshToken creates a refresh token
func (s *MCPAuthServer) generateRefreshToken(username string) (string, error) {
	claims := &Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)), // 7 days
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "mcp-auth-server-refresh",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

// handleTools returns available tools
func (s *MCPAuthServer) handleTools(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value("username").(string)

	tools := []map[string]interface{}{
		{
			"name":        "greet",
			"description": "Greet the user",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Name to greet",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			"name":        "get_user_info",
			"description": "Get information about the authenticated user",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"tools":    tools,
		"user":     username,
		"authenticated": true,
	})
}

// handleGenerate processes text generation requests
func (s *MCPAuthServer) handleGenerate(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value("username").(string)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	model, err := s.provider.LanguageModel("gpt-4")
	if err != nil {
		http.Error(w, "Model initialization failed", http.StatusInternalServerError)
		return
	}

	// Create user-specific tools
	tools := []types.Tool{
		{
			Name:        "greet",
			Description: "Greet the user",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"required": []string{"name"},
			},
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				name := params["name"].(string)
				return fmt.Sprintf("Hello, %s!", name), nil
			},
		},
		{
			Name:        "get_user_info",
			Description: "Get authenticated user information",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				return map[string]string{
					"username": username,
					"auth_method": "jwt",
					"access_level": "standard",
				}, nil
			},
		},
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: fmt.Sprintf("User %s asks: %s", username, req.Prompt),
		Tools:  tools,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"text":  result.Text,
		"usage": result.Usage,
		"user":  username,
	})
}

// handleHealth returns server health status
func (s *MCPAuthServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"auth":   "enabled",
	})
}

// constantTimeCompare safely compares two strings
func constantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
