package api

import "net/url"

// Workspace é uma membership do usuário autenticado. "Workspace" é o vocabulário
// do produto; a API modela como `Tenant` (rotas /tenant, claim tenantId) e o
// mapeamento fica confinado a este arquivo — o resto do CLI só fala workspace.
type Workspace struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Plan   string `json:"plan"`
	Active bool   `json:"active"`
	// Role é o papel do caller NESTE workspace (OWNER | ADMIN | MEMBER). Vem
	// preenchido em GET /tenant, que lista memberships.
	Role string `json:"role,omitempty"`
}

// ResolvedProjectWorkspace responde "a qual workspace este projeto pertence",
// respeitando a membership do caller. É o que permite curar um .upuai/config.json
// linkado antes do pin de workspace existir.
type ResolvedProjectWorkspace struct {
	ProjectID   string `json:"projectId"`
	ProjectName string `json:"projectName"`
	TenantID    string `json:"tenantId"`
	TenantName  string `json:"tenantName"`
	TenantSlug  string `json:"tenantSlug"`
	Role        string `json:"role"`
	Active      bool   `json:"active"`
}

// SwitchWorkspaceRequest carrega o refresh token atual para que a API revogue a
// linha de sessão anterior no mesmo passo em que emite a nova (mesma rotação do
// /auth/refresh). Omiti-lo deixaria a sessão antiga viva até expirar.
type SwitchWorkspaceRequest struct {
	TenantID     string `json:"tenantId"`
	RefreshToken string `json:"refreshToken,omitempty"`
}

// ListWorkspaces returns every workspace the authenticated user belongs to.
func (c *Client) ListWorkspaces() ([]Workspace, error) {
	var resp []Workspace
	if err := c.Get("/tenant", &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// SwitchWorkspace troca o workspace ativo da sessão: a API valida a membership
// real no banco e devolve um par access+refresh novo, escopado ao destino. O
// caller DEVE persistir o par imediatamente — a API revoga o refresh anterior
// antes de responder, então perder a resposta deixa a sessão local sem rotação.
func (c *Client) SwitchWorkspace(tenantID, refreshToken string) (*LoginResponse, error) {
	var resp LoginResponse
	body := &SwitchWorkspaceRequest{TenantID: tenantID, RefreshToken: refreshToken}
	if err := c.Post("/auth/switch-tenant", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ResolveProjectWorkspace descobre a qual workspace um projeto pertence, mesmo
// quando o workspace ativo da sessão é outro. Um projeto inexistente, um projeto
// de workspace onde o caller não é membro, e um projeto sem ProjectMember para
// um MEMBER produzem 404 idênticos por design da API (não vira oráculo de
// enumeração de projectIds) — o caller não deve tentar distinguir os três.
func (c *Client) ResolveProjectWorkspace(projectID string) (*ResolvedProjectWorkspace, error) {
	var resp ResolvedProjectWorkspace
	path := "/tenant/resolve?projectId=" + url.QueryEscape(projectID)
	if err := c.Get(path, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
