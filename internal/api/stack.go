package api

import (
	"fmt"
	"net/url"
)

// StackTemplateInputProperty espelha a mesma struct usada pelo backend
// (apps/shared/src/types/stack-template-types.ts). Mantido em sync manualmente.
type StackTemplateInputProperty struct {
	Type        string        `json:"type"`
	Default     interface{}   `json:"default,omitempty"`
	Enum        []interface{} `json:"enum,omitempty"`
	MinLength   *int          `json:"minLength,omitempty"`
	MaxLength   *int          `json:"maxLength,omitempty"`
	Minimum     *float64      `json:"minimum,omitempty"`
	Maximum     *float64      `json:"maximum,omitempty"`
	Pattern     string        `json:"pattern,omitempty"`
	Format      string        `json:"format,omitempty"`
	Description string        `json:"description,omitempty"`
}

type StackTemplateInputSchema struct {
	Type       string                                `json:"type"`
	Required   []string                              `json:"required,omitempty"`
	Properties map[string]StackTemplateInputProperty `json:"properties"`
}

type StackTemplate struct {
	ID           string                   `json:"id"`
	Slug         string                   `json:"slug"`
	Version      string                   `json:"version"`
	DisplayName  string                   `json:"displayName"`
	Category     string                   `json:"category"`
	Description  *string                  `json:"description,omitempty"`
	IconURL      *string                  `json:"iconUrl,omitempty"`
	DocsURL      *string                  `json:"docsUrl,omitempty"`
	Tags         []string                 `json:"tags"`
	InputsSchema StackTemplateInputSchema `json:"inputsSchema"`
}

type StackInstance struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	TemplateSlug    string `json:"templateSlug"`
	TemplateVersion string `json:"templateVersion"`
	EnvironmentID   string `json:"environmentId"`
	CreatedAt       string `json:"createdAt"`
}

type StackInstanceServiceDetail struct {
	ID          string `json:"id"`
	ServiceID   string `json:"serviceId"`
	NodeName    string `json:"nodeName"`
	Role        string `json:"role"`
	DeployOrder int    `json:"deployOrder"`
	Service     struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Type      string `json:"type"`
		Instances []struct {
			Status        string `json:"status"`
			EnvironmentID string `json:"environmentId"`
		} `json:"instances"`
		Domains []struct {
			Hostname string `json:"hostname"`
			Status   string `json:"status"`
			Type     string `json:"type"`
		} `json:"domains"`
	} `json:"service"`
}

type StackInstanceDetail struct {
	StackInstance
	TemplateID    string                       `json:"templateId"`
	ProjectID     string                       `json:"projectId"`
	Inputs        map[string]interface{}       `json:"inputs"`
	Outputs       map[string]string            `json:"outputs"`
	FailureReason *string                      `json:"failureReason,omitempty"`
	Services      []StackInstanceServiceDetail `json:"services"`
}

type DeployStackRequest struct {
	TemplateSlug    string                 `json:"templateSlug"`
	TemplateVersion string                 `json:"templateVersion"`
	EnvironmentID   string                 `json:"environmentId"`
	Name            string                 `json:"name"`
	Inputs          map[string]interface{} `json:"inputs"`
}

type DeployStackResponse struct {
	StackID       string            `json:"stackId"`
	Status        string            `json:"status"`
	Outputs       map[string]string `json:"outputs"`
	FailureReason string            `json:"failureReason,omitempty"`
}

// ListStackTemplates lista o catálogo de stack templates ativos.
// Filtros opcionais: category, tag.
func (c *Client) ListStackTemplates(category, tag string) ([]StackTemplate, error) {
	qp := url.Values{}
	if category != "" {
		qp.Set("category", category)
	}
	if tag != "" {
		qp.Set("tag", tag)
	}
	path := "/stacks/templates"
	if q := qp.Encode(); q != "" {
		path = path + "?" + q
	}
	var out []StackTemplate
	if err := c.Get(path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetStackTemplate(slug, version string) (*StackTemplate, error) {
	path := fmt.Sprintf("/stacks/templates/%s", url.PathEscape(slug))
	if version != "" {
		path = path + "?version=" + url.QueryEscape(version)
	}
	var t StackTemplate
	if err := c.Get(path, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (c *Client) DeployStack(projectID string, req *DeployStackRequest) (*DeployStackResponse, error) {
	var out DeployStackResponse
	if err := c.Post(fmt.Sprintf("/projects/%s/stacks/deploy", projectID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListStackInstances(projectID string) ([]StackInstance, error) {
	var out []StackInstance
	if err := c.Get(fmt.Sprintf("/projects/%s/stacks", projectID), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetStackInstance(projectID, stackID string) (*StackInstanceDetail, error) {
	var out StackInstanceDetail
	if err := c.Get(fmt.Sprintf("/projects/%s/stacks/%s", projectID, stackID), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteStack(projectID, stackID string) error {
	return c.Delete(fmt.Sprintf("/projects/%s/stacks/%s", projectID, stackID))
}
