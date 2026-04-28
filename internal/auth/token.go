package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

type TokenClaims struct {
	Sub        string   `json:"sub"`
	ID         string   `json:"id"`
	Roles      []string `json:"roles"`
	TenantID   string   `json:"tenantId"`
	TenantName string   `json:"tenantName"`
	Name       string   `json:"name"`
	Email      string   `json:"email"`
	Exp        int64    `json:"exp"`
	Iat        int64    `json:"iat"`
}

func DecodeToken(tokenStr string) (*TokenClaims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode token payload: %w", err)
	}

	var claims TokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse token claims: %w", err)
	}

	return &claims, nil
}
