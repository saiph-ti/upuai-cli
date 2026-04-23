package api

import "fmt"

type AppService struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type CreateServiceRequest struct {
	Name          string               `json:"name"`
	Type          string               `json:"type"`
	EnvironmentID string               `json:"environmentId"`
	Source        *ServiceSourceConfig `json:"source,omitempty"`
}

type ServiceSourceConfig struct {
	Repo          string `json:"repo,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Image         string `json:"image,omitempty"`
	RootDirectory string `json:"rootDirectory,omitempty"`
}

func (c *Client) ListServices(projectID string) ([]AppService, error) {
	var services []AppService
	err := c.Get(fmt.Sprintf("/projects/%s/services", projectID), &services)
	if err != nil {
		return nil, err
	}
	return services, nil
}

func (c *Client) CreateService(projectID string, req *CreateServiceRequest) (*AppService, error) {
	var service AppService
	err := c.Post(fmt.Sprintf("/projects/%s/services", projectID), req, &service)
	if err != nil {
		return nil, err
	}
	return &service, nil
}
