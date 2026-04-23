package api

import "fmt"

type Environment struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	Name        string `json:"name"`
	IsEphemeral bool   `json:"isEphemeral"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type CreateEnvironmentRequest struct {
	Name        string `json:"name"`
	IsEphemeral bool   `json:"isEphemeral,omitempty"`
}

func (c *Client) ListEnvironments(projectID string) ([]Environment, error) {
	var environments []Environment
	err := c.Get(fmt.Sprintf("/projects/%s/environments", projectID), &environments)
	if err != nil {
		return nil, err
	}
	return environments, nil
}

func (c *Client) CreateEnvironment(projectID string, req *CreateEnvironmentRequest) (*Environment, error) {
	var environment Environment
	err := c.Post(fmt.Sprintf("/projects/%s/environments", projectID), req, &environment)
	if err != nil {
		return nil, err
	}
	return &environment, nil
}

func (c *Client) DeleteEnvironment(projectID, envID string) error {
	return c.Delete(fmt.Sprintf("/projects/%s/environments/%s", projectID, envID))
}
