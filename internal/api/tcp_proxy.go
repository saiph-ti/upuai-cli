package api

import "fmt"

// TcpProxyInfo é o proxy TCP público de um serviço web: Host:Port encaminha TCP
// cru (sem HTTP, sem TLS da plataforma) para ApplicationPort das réplicas no ar.
type TcpProxyInfo struct {
	Enabled             bool     `json:"enabled"`
	Host                string   `json:"host"`
	Port                int      `json:"port"`
	ApplicationPort     int      `json:"applicationPort"`
	AllowedCidrs        []string `json:"allowedCidrs"`
	MaxConnectionsPerIP int      `json:"maxConnectionsPerIp"`
}

type setTcpProxyRequest struct {
	Enabled bool `json:"enabled"`
	// 0 = a API usa a porta do serviço.
	ApplicationPort int      `json:"applicationPort,omitempty"`
	AllowedCidrs    []string `json:"allowedCidrs"`
}

func (c *Client) GetTcpProxy(envID, serviceID string) (*TcpProxyInfo, error) {
	var info TcpProxyInfo
	if err := c.Get(fmt.Sprintf("/environments/%s/services/%s/tcp-proxy", envID, serviceID), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// SetTcpProxy liga/ajusta (enabled) ou desliga o proxy. applicationPort 0 = a
// porta do serviço; allowedCIDRs vazio = qualquer IP.
func (c *Client) SetTcpProxy(envID, serviceID string, enabled bool, applicationPort int, allowedCIDRs []string) (*TcpProxyInfo, error) {
	if allowedCIDRs == nil {
		allowedCIDRs = []string{}
	}
	var info TcpProxyInfo
	if err := c.Put(
		fmt.Sprintf("/environments/%s/services/%s/tcp-proxy", envID, serviceID),
		setTcpProxyRequest{Enabled: enabled, ApplicationPort: applicationPort, AllowedCidrs: allowedCIDRs},
		&info,
	); err != nil {
		return nil, err
	}
	return &info, nil
}
