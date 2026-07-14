package api

// ApiToken mirrors the API's serialized token (never includes the sha256 hash).
// Token holds the raw secret and is populated ONLY by the create response.
type ApiToken struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	ProjectID  *string  `json:"projectId"`
	ExpiresAt  *string  `json:"expiresAt"`
	LastUsedAt *string  `json:"lastUsedAt"`
	RevokedAt  *string  `json:"revokedAt"`
	CreatedAt  string   `json:"createdAt"`
	Token      string   `json:"token,omitempty"`
}

// CreateTokenRequest is the body for POST /tokens.
type CreateTokenRequest struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ProjectID     string   `json:"projectId,omitempty"`
	ExpiresInDays int      `json:"expiresInDays,omitempty"`
}

// CreateToken mints a scoped machine token. The returned Token is the secret,
// shown exactly once.
func (c *Client) CreateToken(req CreateTokenRequest) (*ApiToken, error) {
	var result ApiToken
	if err := c.Post("/tokens", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTokens returns the tenant's tokens (prefix + metadata only).
func (c *Client) ListTokens() ([]ApiToken, error) {
	var result []ApiToken
	if err := c.Get("/tokens", &result); err != nil {
		return nil, err
	}
	return result, nil
}

// RevokeToken revokes a token by id. Idempotent server-side.
func (c *Client) RevokeToken(id string) error {
	return c.Delete("/tokens/" + id)
}
