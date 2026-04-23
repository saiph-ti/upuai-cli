package api

import "fmt"

type DeploymentMeta struct {
	GitBranch  string `json:"gitBranch,omitempty"`
	GitSha     string `json:"gitSha,omitempty"`
	GitMessage string `json:"gitMessage,omitempty"`
}

type Deployment struct {
	ID            string          `json:"id"`
	ServiceID     string          `json:"serviceId"`
	EnvironmentID string          `json:"environmentId"`
	Status        string          `json:"status"`
	Trigger       string          `json:"trigger"`
	IsActive      bool            `json:"isActive"`
	CanRedeploy   bool            `json:"canRedeploy"`
	CanRollback   bool            `json:"canRollback"`
	URL           string          `json:"url,omitempty"`
	Meta          *DeploymentMeta `json:"meta,omitempty"`
	CreatedBy     string          `json:"createdBy,omitempty"`
	StartedAt     string          `json:"startedAt,omitempty"`
	FinishedAt    string          `json:"finishedAt,omitempty"`
	CreatedAt     string          `json:"createdAt"`
}

type DeployRequest struct {
	Environment string `json:"environment"`
	ServiceID   string `json:"serviceId,omitempty"`
	GitBranch   string `json:"gitBranch,omitempty"`
	GitSha      string `json:"gitSha,omitempty"`
}

func (c *Client) Deploy(projectID string, req *DeployRequest) (*Deployment, error) {
	var deployment Deployment
	err := c.Post(fmt.Sprintf("/projects/%s/deploy", projectID), req, &deployment)
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (c *Client) ListDeployments(envID, serviceID string) ([]Deployment, error) {
	var resp PaginatedResponse[Deployment]
	err := c.Get(fmt.Sprintf("/environments/%s/services/%s/deployments?size=20", envID, serviceID), &resp)
	if err != nil {
		return nil, err
	}
	return resp.Content, nil
}

func (c *Client) GetDeployment(deployID string) (*Deployment, error) {
	var deployment Deployment
	err := c.Get(fmt.Sprintf("/deployments/%s", deployID), &deployment)
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (c *Client) Rollback(deployID string) (*Deployment, error) {
	var deployment Deployment
	err := c.Post(fmt.Sprintf("/deployments/%s/rollback", deployID), nil, &deployment)
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (c *Client) Redeploy(deployID string) (*Deployment, error) {
	var deployment Deployment
	err := c.Post(fmt.Sprintf("/deployments/%s/redeploy", deployID), nil, &deployment)
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (c *Client) RemoveDeployment(deployID string) error {
	return c.Delete(fmt.Sprintf("/deployments/%s", deployID))
}
