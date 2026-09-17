package api

import "fmt"

// PublicAccessInfo mirrors the platform API contract for the Public DB Endpoint feature.
// Runbook: upuai-core/docs/runbooks/2026-04-24-public-db-endpoint.md
type PublicAccessInfo struct {
	Enabled          bool   `json:"enabled"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	ConnectionString string `json:"connectionString"`
	// AllowedCidrs: origens autorizadas a conectar. Vazio = qualquer IP, que é o
	// comportamento histórico do toggle.
	AllowedCidrs []string `json:"allowedCidrs,omitempty"`
}

type setPublicAccessRequest struct {
	Enabled bool `json:"enabled"`
	// Sempre serializado (sem omitempty): a API distingue "lista vazia" (abrir
	// para qualquer IP) de campo ausente só pelo valor, e mandar o campo torna a
	// intenção explícita no wire.
	AllowedCidrs []string `json:"allowedCidrs"`
}

func (c *Client) GetDatabasePublicAccess(envID, serviceID string) (*PublicAccessInfo, error) {
	var info PublicAccessInfo
	if err := c.Get(fmt.Sprintf("/environments/%s/services/%s/database/public-access", envID, serviceID), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// SetDatabasePublicAccess publica (ou retira) o endpoint público. allowedCIDRs
// vazio abre para qualquer IP; com valores, só eles conectam.
func (c *Client) SetDatabasePublicAccess(envID, serviceID string, enabled bool, allowedCIDRs []string) (*PublicAccessInfo, error) {
	if allowedCIDRs == nil {
		allowedCIDRs = []string{}
	}
	var info PublicAccessInfo
	if err := c.Put(
		fmt.Sprintf("/environments/%s/services/%s/database/public-access", envID, serviceID),
		setPublicAccessRequest{Enabled: enabled, AllowedCidrs: allowedCIDRs},
		&info,
	); err != nil {
		return nil, err
	}
	return &info, nil
}
