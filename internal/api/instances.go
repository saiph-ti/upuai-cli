package api

import "fmt"

type InstanceSourceConfig struct {
	RootDirectory string `json:"rootDirectory,omitempty"`
}

type InstanceBuildConfig struct {
	Builder        string `json:"builder,omitempty"`
	DockerfilePath string `json:"dockerfilePath,omitempty"`
	BuildCommand   string `json:"buildCommand,omitempty"`
}

type InstanceDeployConfig struct {
	StartCommand       string `json:"startCommand,omitempty"`
	HealthCheckPath    string `json:"healthCheckPath,omitempty"`
	HealthCheckTimeout int    `json:"healthCheckTimeout,omitempty"`
}

type UpdateInstanceRequest struct {
	Source *InstanceSourceConfig `json:"source,omitempty"`
	Build  *InstanceBuildConfig  `json:"build,omitempty"`
	Deploy *InstanceDeployConfig `json:"deploy,omitempty"`
}

// InstanceConfig mirrors the `Service.config` shape returned by the API
// (see apps/api: ServiceInstance.config — only fields the CLI surfaces).
type InstanceConfig struct {
	Source *InstanceSourceConfig `json:"source,omitempty"`
	Build  *InstanceBuildConfig  `json:"build,omitempty"`
	Deploy *InstanceDeployConfig `json:"deploy,omitempty"`
}

type Instance struct {
	ID            string          `json:"id"`
	ServiceID     string          `json:"serviceId"`
	EnvironmentID string          `json:"environmentId"`
	Name          string          `json:"name,omitempty"`
	Type          string          `json:"type,omitempty"`
	Status        string          `json:"status,omitempty"`
	Config        *InstanceConfig `json:"config,omitempty"`
}

func (c *Client) GetInstance(envID, serviceID string) (*Instance, error) {
	var inst Instance
	err := c.Get(fmt.Sprintf("/environments/%s/services/%s/instance", envID, serviceID), &inst)
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func (c *Client) UpdateInstance(envID, serviceID string, req *UpdateInstanceRequest) error {
	return c.Patch(fmt.Sprintf("/environments/%s/services/%s/instance", envID, serviceID), req, nil)
}

func (c *Client) GetLogs(envID, serviceID string, lines int) (string, error) {
	path := fmt.Sprintf("/environments/%s/services/%s/instance/logs?lines=%d", envID, serviceID, lines)
	data, err := c.GetRaw(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *Client) RestartInstance(envID, serviceID string) error {
	return c.Post(fmt.Sprintf("/environments/%s/services/%s/instance/restart", envID, serviceID), nil, nil)
}

func (c *Client) ScaleInstance(envID, serviceID string, count int) error {
	body := map[string]int{"replicas": count}
	return c.Post(fmt.Sprintf("/environments/%s/services/%s/instance/scale", envID, serviceID), body, nil)
}
