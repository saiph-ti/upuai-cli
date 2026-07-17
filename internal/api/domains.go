package api

import "fmt"

type Domain struct {
	ID     string `json:"id"`
	Domain string `json:"hostname"` // API returns "hostname"
	Type   string `json:"type"`
	Status string `json:"status"` // DNS: pending | configuring | active | error
	// TLS é rastreado separado do DNS: um domain pode estar com DNS active e o
	// certificado ainda em issuing/failed (ex: failed-backoff do cert-manager).
	SslStatus string `json:"sslStatus,omitempty"` // pending | issuing | active | failed; vazio até a 1ª emissão
	SslError  string `json:"sslError,omitempty"`  // última falha de emissão (failed, ou issuing em retry-backoff)
	CreatedAt string `json:"createdAt"`
}

// AddDomainRequest mirrors createDomainSchema on the API: the canonical field
// is `hostname` (same as the web SPA's CreateDomainRequest). `targetPort` is
// omitted on purpose — the API inherits it from the service's generated domain.
type AddDomainRequest struct {
	Hostname string `json:"hostname"`
}

func (c *Client) ListDomains(envID, serviceID string) ([]Domain, error) {
	var domains []Domain
	err := c.Get(fmt.Sprintf("/environments/%s/services/%s/domains", envID, serviceID), &domains)
	if err != nil {
		return nil, err
	}
	return domains, nil
}

func (c *Client) AddDomain(envID, serviceID, domain string) (*Domain, error) {
	var result Domain
	err := c.Post(fmt.Sprintf("/environments/%s/services/%s/domains", envID, serviceID), &AddDomainRequest{Hostname: domain}, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GenerateDomain(envID, serviceID string, targetPort int) (*Domain, error) {
	var result Domain
	body := map[string]int{"targetPort": targetPort}
	err := c.Post(fmt.Sprintf("/environments/%s/services/%s/domains/generate", envID, serviceID), body, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) DeleteDomain(envID, serviceID, domainID string) error {
	return c.Delete(fmt.Sprintf("/environments/%s/services/%s/domains/%s", envID, serviceID, domainID))
}

// DNSRecord espelha DnsRecord/DnsTxtRecord de apps/shared/src/types/domain-types.ts.
// Note carrega uma dica opcional (ex: "acmeDelegation" para o CNAME de delegação
// do wildcard, "wildcardRoot" para o registro curinga).
type DNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
	TTL   int    `json:"ttl"`
	Note  string `json:"note,omitempty"`
}

// DNSInstructions espelha DnsInstructions da API. TxtRecord é ausente para
// domínios WILDCARD (posse provada pela delegação ACME, sem TXT).
type DNSInstructions struct {
	Hostname    string      `json:"hostname"`
	IsApex      bool        `json:"isApex"`
	DNSRecords  []DNSRecord `json:"dnsRecords"`
	TxtRecord   *DNSRecord  `json:"txtRecord,omitempty"`
	DNSProvider string      `json:"dnsProvider"`
}

func (c *Client) GetDNSInstructions(envID, serviceID, domainID string) (*DNSInstructions, error) {
	var result DNSInstructions
	err := c.Get(fmt.Sprintf("/environments/%s/services/%s/domains/%s/dns-instructions", envID, serviceID, domainID), &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
