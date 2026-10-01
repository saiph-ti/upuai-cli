package api

import (
	"fmt"
	"net/url"
	"time"
)

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

// ─── Extensões gerenciadas (Postgres) ────────────────────────────────────────
//
// A plataforma gerencia uma allowlist curada de extensões no banco `app`
// (a da connection string). Ativar = CREATE EXTENSION ... CASCADE, desativar =
// DROP EXTENSION ... RESTRICT (nunca CASCADE), atualizar = ALTER EXTENSION ...
// UPDATE. Toda mutação devolve a lista completa: o CASCADE pode instalar
// dependências (ex: postgis_topology traz postgis).

// databaseExtensionTimeout cobre o teto que a API impõe à chamada ao
// orchestrator nas mutações de extensão (55s — statement_timeout de 45s no
// Postgres + margem). Com o default de 30s do client, um CREATE EXTENSION
// lento seria cortado aqui enquanto ainda roda no servidor.
const databaseExtensionTimeout = 75 * time.Second

// DatabaseExtension espelha o contrato da API. Strings vazias quando a extensão
// não está instalada — nunca null.
type DatabaseExtension struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	// Description é o comment do próprio Postgres (inglês).
	Description string `json:"description"`
	// Available: a imagem do banco traz a extensão. False numa imagem antiga
	// (ex: PostGIS antes da atualização de manutenção).
	Available        bool   `json:"available"`
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installedVersion"`
	DefaultVersion   string `json:"defaultVersion"`
	// UpdateAvailable: instalada numa versão anterior à que a imagem traz.
	UpdateAvailable bool `json:"updateAvailable"`
}

type DatabaseExtensionList struct {
	Extensions []DatabaseExtension `json:"extensions"`
}

func databaseExtensionsPath(envID, serviceID string) string {
	return fmt.Sprintf("/environments/%s/services/%s/database/extensions", envID, serviceID)
}

func databaseExtensionPath(envID, serviceID, name string) string {
	return databaseExtensionsPath(envID, serviceID) + "/" + url.PathEscape(name)
}

func (c *Client) ListDatabaseExtensions(envID, serviceID string) (*DatabaseExtensionList, error) {
	var list DatabaseExtensionList
	if err := c.Get(databaseExtensionsPath(envID, serviceID), &list); err != nil {
		return nil, err
	}
	return &list, nil
}

func (c *Client) EnableDatabaseExtension(envID, serviceID, name string) (*DatabaseExtensionList, error) {
	var list DatabaseExtensionList
	if err := c.withTimeout(databaseExtensionTimeout).Put(databaseExtensionPath(envID, serviceID, name), nil, &list); err != nil {
		return nil, err
	}
	return &list, nil
}

func (c *Client) DisableDatabaseExtension(envID, serviceID, name string) (*DatabaseExtensionList, error) {
	var list DatabaseExtensionList
	if err := c.withTimeout(databaseExtensionTimeout).DeleteJSON(databaseExtensionPath(envID, serviceID, name), &list); err != nil {
		return nil, err
	}
	return &list, nil
}

func (c *Client) UpdateDatabaseExtension(envID, serviceID, name string) (*DatabaseExtensionList, error) {
	var list DatabaseExtensionList
	if err := c.withTimeout(databaseExtensionTimeout).Post(databaseExtensionPath(envID, serviceID, name)+"/update", nil, &list); err != nil {
		return nil, err
	}
	return &list, nil
}

// ─── Versão e atualização de manutenção ──────────────────────────────────────

// DatabaseVersion é a versão VIVA do banco (lida da imagem em execução).
// UpdateAvailable só é populado para Postgres: a imagem em execução difere da
// imagem fixada pela plataforma para aquele major (atualização de manutenção
// pendente). Os demais engines devolvem false.
type DatabaseVersion struct {
	Engine          string `json:"engine"`
	Version         string `json:"version"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

// DatabaseMaintenanceResult: Status "accepted" (202, rollout assíncrono
// acompanhável pelo DeploymentID) ou "noop" (200, já está na imagem atual).
type DatabaseMaintenanceResult struct {
	Status       string `json:"status"`
	DeploymentID string `json:"deploymentId,omitempty"`
	Version      string `json:"version"`
}

func (c *Client) GetDatabaseVersion(envID, serviceID string) (*DatabaseVersion, error) {
	var v DatabaseVersion
	if err := c.Get(fmt.Sprintf("/environments/%s/services/%s/instance/version", envID, serviceID), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Client) ApplyDatabaseMaintenance(envID, serviceID string) (*DatabaseMaintenanceResult, error) {
	var res DatabaseMaintenanceResult
	if err := c.Post(fmt.Sprintf("/environments/%s/services/%s/instance/maintenance", envID, serviceID), nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
