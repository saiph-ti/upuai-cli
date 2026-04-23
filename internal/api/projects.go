package api

import "fmt"

type PaginatedResponse[T any] struct {
	Content       []T  `json:"content"`
	TotalElements int  `json:"totalElements"`
	TotalPages    int  `json:"totalPages"`
	Number        int  `json:"number"`
	Size          int  `json:"size"`
	First         bool `json:"first"`
	Last          bool `json:"last"`
}

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
	IsPublic    bool   `json:"isPublic,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

type StatusServiceInstance struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
}

type StatusLastDeployment struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Trigger   string `json:"trigger"`
	CreatedAt string `json:"createdAt"`
}

type StatusService struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Type           string                `json:"type"`
	Instance       StatusServiceInstance `json:"instance"`
	LastDeployment *StatusLastDeployment `json:"lastDeployment,omitempty"`
}

type StatusEnvironment struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Services []StatusService `json:"services"`
}

type StatusProject struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
}

type ProjectStatus struct {
	Project      StatusProject       `json:"project"`
	Environments []StatusEnvironment `json:"environments"`
}

type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func (c *Client) ListProjects() ([]Project, error) {
	var resp PaginatedResponse[Project]
	err := c.Get("/projects?size=100", &resp)
	if err != nil {
		return nil, err
	}
	return resp.Content, nil
}

func (c *Client) GetProject(id string) (*Project, error) {
	var project Project
	err := c.Get(fmt.Sprintf("/projects/%s", id), &project)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (c *Client) CreateProject(req *CreateProjectRequest) (*Project, error) {
	var project Project
	err := c.Post("/projects", req, &project)
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (c *Client) DeleteProject(projectID string) error {
	return c.Delete(fmt.Sprintf("/projects/%s", projectID))
}

func (c *Client) GetProjectStatus(id, environment string) (*ProjectStatus, error) {
	var status ProjectStatus
	path := fmt.Sprintf("/projects/%s/status", id)
	if environment != "" {
		path += "?environment=" + environment
	}
	err := c.Get(path, &status)
	if err != nil {
		return nil, err
	}
	return &status, nil
}
