package api

import (
	"fmt"
	"net/url"
)

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

// GetLogs fetches the most recent runtime log lines of a service instance. When
// process is non-empty it scopes the logs to that named process (multi-process
// service); empty keeps legacy single-process behavior.
func (c *Client) GetLogs(envID, serviceID, process string, lines int) (string, error) {
	q := url.Values{}
	q.Set("lines", fmt.Sprintf("%d", lines))
	if process != "" {
		q.Set("process", process)
	}
	path := fmt.Sprintf("/environments/%s/services/%s/instance/logs?%s", envID, serviceID, q.Encode())
	data, err := c.GetRaw(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// RestartInstance restarts the running container(s) of a service. When process
// is non-empty it restarts only that named process; empty restarts the default
// (web) process / legacy single-process service.
func (c *Client) RestartInstance(envID, serviceID, process string) error {
	path := fmt.Sprintf("/environments/%s/services/%s/instance/restart", envID, serviceID)
	if process != "" {
		path += "?process=" + url.QueryEscape(process)
	}
	return c.Post(path, nil, nil)
}

// ScaleInstance scales a service to count replicas. When processName is
// non-empty it scales only that named process (multi-process service); empty
// keeps legacy whole-service scaling.
func (c *Client) ScaleInstance(envID, serviceID, processName string, count int) error {
	// API canonical field is `instanceCount` (scaleInstanceSchema + config.deploy.instanceCount).
	body := map[string]any{"instanceCount": count}
	if processName != "" {
		body["processName"] = processName
	}
	return c.Post(fmt.Sprintf("/environments/%s/services/%s/instance/scale", envID, serviceID), body, nil)
}
