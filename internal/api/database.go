package api

import "fmt"

// PublicAccessInfo mirrors the platform API contract for the Public DB Endpoint feature.
// Runbook: upuai-core/docs/runbooks/2026-04-24-public-db-endpoint.md
type PublicAccessInfo struct {
	Enabled          bool   `json:"enabled"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	ConnectionString string `json:"connectionString"`
}

type setPublicAccessRequest struct {
	Enabled bool `json:"enabled"`
}

func (c *Client) GetDatabasePublicAccess(envID, serviceID string) (*PublicAccessInfo, error) {
	var info PublicAccessInfo
	if err := c.Get(fmt.Sprintf("/environments/%s/services/%s/database/public-access", envID, serviceID), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) SetDatabasePublicAccess(envID, serviceID string, enabled bool) (*PublicAccessInfo, error) {
	var info PublicAccessInfo
	if err := c.Put(
		fmt.Sprintf("/environments/%s/services/%s/database/public-access", envID, serviceID),
		setPublicAccessRequest{Enabled: enabled},
		&info,
	); err != nil {
		return nil, err
	}
	return &info, nil
}
