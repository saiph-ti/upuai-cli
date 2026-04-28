package api

type LoginResponse struct {
	UserID       string  `json:"userId"`
	UserName     string  `json:"userName"`
	Login        string  `json:"login"`
	Token        string  `json:"token"`
	RefreshToken string  `json:"refreshToken,omitempty"`
	TenantPlan   string  `json:"tenantPlan,omitempty"`
	AvatarUrl    *string `json:"avatarUrl,omitempty"`
}

type OAuthRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirectUri"`
}

type MeResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Email     string  `json:"email"`
	AvatarUrl *string `json:"avatarUrl"`
	IsAdmin   bool    `json:"isAdmin"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

func (c *Client) SendEmailToken(email string) error {
	return c.Post("/auth/send-email-token/"+email, nil, nil)
}

func (c *Client) LoginWithEmailToken(email, token string) (*LoginResponse, error) {
	var resp LoginResponse
	err := c.Post("/auth/login-email-token/"+email+"/"+token, nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) LoginOAuthGitHub(code, redirectURI string) (*LoginResponse, error) {
	var resp LoginResponse
	err := c.Post("/auth/oauth/github", &OAuthRequest{Code: code, RedirectURI: redirectURI}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetMe() (*MeResponse, error) {
	var resp MeResponse
	err := c.Get("/auth/me", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

type CliSessionRequest struct {
	SessionToken string `json:"sessionToken"`
}

type CliSessionStatusResponse struct {
	Status       string  `json:"status"`
	UserID       string  `json:"userId,omitempty"`
	UserName     string  `json:"userName,omitempty"`
	Login        string  `json:"login,omitempty"`
	Token        string  `json:"token,omitempty"`
	RefreshToken string  `json:"refreshToken,omitempty"`
	TenantPlan   string  `json:"tenantPlan,omitempty"`
	AvatarUrl    *string `json:"avatarUrl,omitempty"`
}

type CliSessionInitResponse struct {
	SessionToken string `json:"sessionToken"`
}

func (c *Client) InitCliSession(sessionToken string) (string, error) {
	var resp CliSessionInitResponse
	err := c.Post("/auth/cli/session", &CliSessionRequest{SessionToken: sessionToken}, &resp)
	if err != nil {
		return "", err
	}
	if resp.SessionToken == "" {
		return sessionToken, nil
	}
	return resp.SessionToken, nil
}

func (c *Client) GetCliSessionStatus(sessionToken string) (*CliSessionStatusResponse, error) {
	var resp CliSessionStatusResponse
	err := c.Get("/auth/cli/session/"+sessionToken, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
