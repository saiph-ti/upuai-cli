package api

import "fmt"

type DatabaseTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Engine      string `json:"engine"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type DeployTemplateRequest struct {
	TemplateID    string `json:"templateId"`
	Name          string `json:"name,omitempty"`
	EnvironmentID string `json:"environmentId"`
}

type DeployTemplateService struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DeployTemplateResponse struct {
	ProjectID string                  `json:"projectId"`
	Services  []DeployTemplateService `json:"services"`
}

func (c *Client) ListTemplates() ([]DatabaseTemplate, error) {
	var templates []DatabaseTemplate
	err := c.Get("/templates", &templates)
	if err != nil {
		return nil, err
	}
	return templates, nil
}

func (c *Client) DeployTemplate(projectID string, req *DeployTemplateRequest) (*DeployTemplateResponse, error) {
	var result DeployTemplateResponse
	err := c.Post(fmt.Sprintf("/projects/%s/templates/deploy", projectID), req, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
