package api

import (
	"context"
	"fmt"
)

type DeploymentMeta struct {
	GitBranch  string `json:"gitBranch,omitempty"`
	GitSha     string `json:"gitSha,omitempty"`
	GitMessage string `json:"gitMessage,omitempty"`
}

type Deployment struct {
	ID             string          `json:"id"`
	ServiceID      string          `json:"serviceId"`
	EnvironmentID  string          `json:"environmentId"`
	Status         string          `json:"status"`
	Trigger        string          `json:"trigger"`
	IsActive       bool            `json:"isActive"`
	CanRedeploy    bool            `json:"canRedeploy"`
	CanRollback    bool            `json:"canRollback"`
	URL            string          `json:"url,omitempty"`
	Meta           *DeploymentMeta `json:"meta,omitempty"`
	Builder        string          `json:"builder,omitempty"`
	DockerfilePath string          `json:"dockerfilePath,omitempty"`
	CreatedBy      string          `json:"createdBy,omitempty"`
	StartedAt      string          `json:"startedAt,omitempty"`
	FinishedAt     string          `json:"finishedAt,omitempty"`
	CreatedAt      string          `json:"createdAt"`
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

// LatestDeployment returns the most recent deployment for a service in an env,
// or nil if none exist.
func (c *Client) LatestDeployment(envID, serviceID string) (*Deployment, error) {
	deployments, err := c.ListDeployments(envID, serviceID)
	if err != nil {
		return nil, err
	}
	if len(deployments) == 0 {
		return nil, nil
	}
	return &deployments[0], nil
}

// StreamBuildLogs streams the build log SSE for a deployment. The server
// returns the durable log from MinIO when available (terminating with
// `event: end`); otherwise live-streams from the build Job pod.
func (c *Client) StreamBuildLogs(ctx context.Context, deployID string, onLine func(string)) error {
	return c.StreamSSE(ctx, fmt.Sprintf("/deployments/%s/build-logs", deployID), onLine)
}

// StreamDeployLogs streams the release-phase + rollout log SSE for a deployment.
func (c *Client) StreamDeployLogs(ctx context.Context, deployID string, onLine func(string)) error {
	return c.StreamSSE(ctx, fmt.Sprintf("/deployments/%s/deploy-logs", deployID), onLine)
}

// StreamRuntimeLogs streams runtime logs (live tail) of a service instance.
func (c *Client) StreamRuntimeLogs(ctx context.Context, envID, serviceID string, onLine func(string)) error {
	return c.StreamSSE(
		ctx,
		fmt.Sprintf("/environments/%s/services/%s/instance/logs/stream", envID, serviceID),
		onLine,
	)
}

// DeploymentTimelineEnvelope mirrors orchestratorv1.GetDeploymentTimelineResponse.
// Decoded shape from GET /deployments/{id}/timeline. Stack-agnostic — fields
// reflect buildkit grammar / build script tags / K8s objects. The CLI
// renders selected pieces; full schema lives in
// proto/orchestrator/v1/timeline.proto.
type DeploymentTimelineEnvelope struct {
	Timeline *DeploymentTimeline `json:"timeline"`
}

type DeploymentTimeline struct {
	DeploymentID   string                  `json:"deploymentId"`
	Status         string                  `json:"status"`
	StartedAt      string                  `json:"startedAt"`
	FinishedAt     string                  `json:"finishedAt"`
	FailureSummary *TimelineFailureSummary `json:"failureSummary,omitempty"`
	Stages         []TimelineStage         `json:"stages,omitempty"`
	Partial        bool                    `json:"partial"`
}

type TimelineFailureSummary struct {
	Stage     string   `json:"stage"`
	Step      string   `json:"step"`
	ExitCode  *int32   `json:"exitCode,omitempty"`
	LastLines []string `json:"lastLines,omitempty"`
}

type TimelineStage struct {
	Kind       string            `json:"kind"`
	Status     string            `json:"status"`
	StartedAt  string            `json:"startedAt"`
	FinishedAt string            `json:"finishedAt"`
	DurationMs int64             `json:"durationMs"`
	Partial    bool              `json:"partial"`
	GitClone   *TimelineGitClone `json:"gitClone,omitempty"`
	Build      *TimelineBuild    `json:"build,omitempty"`
	Release    *TimelineRelease  `json:"release,omitempty"`
	Deploy     *TimelineDeploy   `json:"deploy,omitempty"`
}

type TimelineGitClone struct {
	ExitCode           *int32   `json:"exitCode,omitempty"`
	TerminationReason  string   `json:"terminationReason"`
	TerminationMessage string   `json:"terminationMessage"`
	LogTail            []string `json:"logTail,omitempty"`
}

type TimelineBuild struct {
	Builder       string                 `json:"builder"`
	Detected      *TimelineDetected      `json:"detected,omitempty"`
	BuildkitSteps []TimelineBuildkitStep `json:"buildkitSteps,omitempty"`
	ExitCode      *int32                 `json:"exitCode,omitempty"`
}

type TimelineDetected struct {
	Language       string `json:"language"`
	Framework      string `json:"framework"`
	Port           *int32 `json:"port,omitempty"`
	ReleaseCommand string `json:"releaseCommand"`
	DockerfilePath string `json:"dockerfilePath"`
	JdkVersion     string `json:"jdkVersion"`
}

type TimelineBuildkitStep struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	DurationMs int64    `json:"durationMs"`
	ExitCode   *int32   `json:"exitCode,omitempty"`
	LastLines  []string `json:"lastLines,omitempty"`
}

type TimelineRelease struct {
	Command  string   `json:"command"`
	ExitCode *int32   `json:"exitCode,omitempty"`
	LogTail  []string `json:"logTail,omitempty"`
}

type TimelineDeploy struct {
	RolloutPhase string        `json:"rolloutPhase"`
	Pods         []TimelinePod `json:"pods,omitempty"`
}

type TimelinePod struct {
	Name       string              `json:"name"`
	Phase      string              `json:"phase"`
	Containers []TimelineContainer `json:"containers,omitempty"`
}

type TimelineContainer struct {
	Name            string               `json:"name"`
	Ready           bool                 `json:"ready"`
	RestartCount    int32                `json:"restartCount"`
	LastTermination *TimelineTermination `json:"lastTermination,omitempty"`
}

type TimelineTermination struct {
	ExitCode int32  `json:"exitCode"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
}

// GetDeploymentTimeline fetches the structured timeline for a deployment.
// Stack-agnostic projection composed by the orchestrator from buildkit/k8s
// signals. See runbook 2026-05-05-deployment-timeline.md.
func (c *Client) GetDeploymentTimeline(deployID string) (*DeploymentTimeline, error) {
	var env DeploymentTimelineEnvelope
	if err := c.Get(fmt.Sprintf("/deployments/%s/timeline", deployID), &env); err != nil {
		return nil, err
	}
	return env.Timeline, nil
}
