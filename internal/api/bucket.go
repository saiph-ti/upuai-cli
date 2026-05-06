package api

import "fmt"

// Bucket mirrors the platform Bucket DTO returned by GET /projects/:id/buckets.
// Mantido conscientemente narrow — só os campos consumidos pelo CLI hoje
// (resolução name → id pra `upuai bucket public`). Acrescentar campos quando
// outros comandos precisarem; tipos compartilhados por wire-format vivem na API
// (Prisma/Zod), não aqui.
type Bucket struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	Region    string `json:"region"`
	Endpoint  string `json:"endpoint"`
	IsPublic  bool   `json:"isPublic"`
}

// BucketPublicAccessInfo é a resposta de GET/PUT /buckets/:id/public-access.
// Espelha o tipo BucketPublicAccessInfo do shared package.
// Runbook: upuai-core/docs/runbooks/2026-05-05-public-bucket-access.md
type BucketPublicAccessInfo struct {
	Enabled   bool   `json:"enabled"`
	PublicURL string `json:"publicUrl"`
}

type setBucketPublicAccessRequest struct {
	Enabled bool `json:"enabled"`
}

func (c *Client) ListProjectBuckets(projectID string) ([]Bucket, error) {
	var buckets []Bucket
	if err := c.Get(fmt.Sprintf("/projects/%s/buckets", projectID), &buckets); err != nil {
		return nil, err
	}
	return buckets, nil
}

func (c *Client) GetBucketPublicAccess(bucketID string) (*BucketPublicAccessInfo, error) {
	var info BucketPublicAccessInfo
	if err := c.Get(fmt.Sprintf("/buckets/%s/public-access", bucketID), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) SetBucketPublicAccess(bucketID string, enabled bool) (*BucketPublicAccessInfo, error) {
	var info BucketPublicAccessInfo
	if err := c.Put(
		fmt.Sprintf("/buckets/%s/public-access", bucketID),
		setBucketPublicAccessRequest{Enabled: enabled},
		&info,
	); err != nil {
		return nil, err
	}
	return &info, nil
}
