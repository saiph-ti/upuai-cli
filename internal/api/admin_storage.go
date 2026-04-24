package api

// Admin storage observability endpoints.
// Platform runbook: upuai-core/docs/runbooks/2026-04-24-storage-architecture.md.

type AdminStorageTier struct {
	Name           string   `json:"name"`
	StorageClass   string   `json:"storageClass"`
	Description    string   `json:"description"`
	CapacityBytes  int64    `json:"capacityBytes"`
	UsedBytes      int64    `json:"usedBytes"`
	AvailableBytes int64    `json:"availableBytes"`
	ReservedBytes  int64    `json:"reservedBytes"`
	ScheduledBytes int64    `json:"scheduledBytes"`
	UsagePercent   int      `json:"usagePercent"`
	PVCCount       int      `json:"pvcCount"`
	DiskTags       []string `json:"diskTags,omitempty"`
	Healthy        bool     `json:"healthy"`
}

type AdminStoragePVC struct {
	Namespace     string   `json:"namespace"`
	Name          string   `json:"name"`
	StorageClass  string   `json:"storageClass"`
	Tier          string   `json:"tier"`
	DeclaredBytes int64    `json:"declaredBytes"`
	ActualBytes   int64    `json:"actualBytes"`
	UsagePercent  int      `json:"usagePercent"`
	Phase         string   `json:"phase"`
	VolumeName    string   `json:"volumeName"`
	ConsumerPods  []string `json:"consumerPods,omitempty"`
	CreatedAt     string   `json:"createdAt"`
}

type AdminStorageAlert struct {
	Name        string            `json:"name"`
	Severity    string            `json:"severity"`
	Summary     string            `json:"summary"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels,omitempty"`
	StartsAt    string            `json:"startsAt"`
	FiringFor   string            `json:"firingFor"`
}

type AdminStorageSummary struct {
	TotalCapacityBytes int64 `json:"totalCapacityBytes"`
	TotalUsedBytes     int64 `json:"totalUsedBytes"`
	TotalPVCCount      int   `json:"totalPvcCount"`
	UnhealthyVolumes   int   `json:"unhealthyVolumes"`
	ActiveAlerts       int   `json:"activeAlerts"`
	CriticalAlerts     int   `json:"criticalAlerts"`
}

type AdminStorageOverview struct {
	Tiers   []AdminStorageTier  `json:"tiers"`
	Alerts  []AdminStorageAlert `json:"alerts"`
	Summary AdminStorageSummary `json:"summary"`
}

func (c *Client) GetAdminStorageOverview() (*AdminStorageOverview, error) {
	var out AdminStorageOverview
	if err := c.Get("/admin/cluster/storage/overview", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListAdminStoragePVCs(namespace string) ([]AdminStoragePVC, error) {
	path := "/admin/cluster/storage/pvcs"
	if namespace != "" {
		path += "?namespace=" + namespace
	}
	var out []AdminStoragePVC
	if err := c.Get(path, &out); err != nil {
		return nil, err
	}
	return out, nil
}
