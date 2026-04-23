package api

import "fmt"

type Domain struct {
	ID        string `json:"id"`
	Domain    string `json:"hostname"` // API returns "hostname"
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

type AddDomainRequest struct {
	Domain string `json:"domain"`
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
	err := c.Post(fmt.Sprintf("/environments/%s/services/%s/domains", envID, serviceID), &AddDomainRequest{Domain: domain}, &result)
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
