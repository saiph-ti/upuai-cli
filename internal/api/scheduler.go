package api

import "fmt"

type ScheduledJob struct {
	ID             string `json:"id"`
	ServiceID      string `json:"serviceId"`
	EnvironmentID  string `json:"environmentId"`
	Name           string `json:"name"`
	Command        string `json:"command"`
	Schedule       string `json:"schedule"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Status         string `json:"status"`
	LastRunAt      string `json:"lastRunAt,omitempty"`
}

type CreateScheduledJobRequest struct {
	Name           string `json:"name"`
	Command        string `json:"command"`
	Schedule       string `json:"schedule"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

type UpdateScheduledJobRequest struct {
	Command        string `json:"command,omitempty"`
	Schedule       string `json:"schedule,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	Status         string `json:"status,omitempty"`
}

func (c *Client) ListScheduledJobs(envID, serviceID string) ([]ScheduledJob, error) {
	var jobs []ScheduledJob
	if err := c.Get(fmt.Sprintf("/environments/%s/services/%s/scheduled-jobs", envID, serviceID), &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (c *Client) CreateScheduledJob(envID, serviceID string, req *CreateScheduledJobRequest) (*ScheduledJob, error) {
	var job ScheduledJob
	if err := c.Post(fmt.Sprintf("/environments/%s/services/%s/scheduled-jobs", envID, serviceID), req, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *Client) UpdateScheduledJob(envID, serviceID, id string, req *UpdateScheduledJobRequest) (*ScheduledJob, error) {
	var job ScheduledJob
	if err := c.Patch(fmt.Sprintf("/environments/%s/services/%s/scheduled-jobs/%s", envID, serviceID, id), req, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *Client) DeleteScheduledJob(envID, serviceID, id string) error {
	return c.Delete(fmt.Sprintf("/environments/%s/services/%s/scheduled-jobs/%s", envID, serviceID, id))
}

func (c *Client) RunScheduledJob(envID, serviceID, id string) (*ScheduledJob, error) {
	var job ScheduledJob
	if err := c.Post(fmt.Sprintf("/environments/%s/services/%s/scheduled-jobs/%s/run", envID, serviceID, id), nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}
