package api

import "fmt"

// Process is one declared process of a multi-process service (web + worker +
// clock + release from a single repo/build — Procfile parity). The platform
// derives these from the service's Procfile/release config; the CLI is
// read-only here (scaling per-process goes through the existing scale endpoint).
type Process struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"` // WEB | WORKER | RELEASE | CLOCK
	Command       string `json:"command"`
	InstanceCount int    `json:"instanceCount"`
}

// listProcessesResponse is the envelope returned by the processes endpoint.
type listProcessesResponse struct {
	Processes []Process `json:"processes"`
}

// ListProcesses returns the declared processes of a service in an environment.
func (c *Client) ListProcesses(envID, serviceID string) ([]Process, error) {
	var resp listProcessesResponse
	if err := c.Get(fmt.Sprintf("/environments/%s/services/%s/processes", envID, serviceID), &resp); err != nil {
		return nil, err
	}
	return resp.Processes, nil
}
