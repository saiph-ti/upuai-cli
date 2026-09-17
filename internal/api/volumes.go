package api

import "fmt"

// Volume é um disco persistente do projeto, montado num serviço de um ambiente.
// ReadWriteOnce: um pod por vez (ver `upuai volume add`).
type Volume struct {
	ID        string           `json:"id"`
	ProjectID string           `json:"projectId"`
	Name      string           `json:"name"`
	SizeMb    int              `json:"sizeMb,omitempty"`
	Instances []VolumeInstance `json:"instances,omitempty"`
	CreatedAt string           `json:"createdAt,omitempty"`
}

type VolumeInstance struct {
	ID            string `json:"id"`
	VolumeID      string `json:"volumeId"`
	EnvironmentID string `json:"environmentId"`
	ServiceID     string `json:"serviceId"`
	MountPath     string `json:"mountPath"`
	Status        string `json:"status"`
}

type CreateVolumeRequest struct {
	Name          string `json:"name,omitempty"`
	MountPath     string `json:"mountPath"`
	ServiceID     string `json:"serviceId"`
	EnvironmentID string `json:"environmentId"`
	SizeMb        int    `json:"sizeMb,omitempty"`
}

func (c *Client) ListProjectVolumes(projectID string) ([]Volume, error) {
	var volumes []Volume
	if err := c.Get(fmt.Sprintf("/projects/%s/volumes", projectID), &volumes); err != nil {
		return nil, err
	}
	return volumes, nil
}

func (c *Client) CreateVolume(projectID string, req CreateVolumeRequest) (*Volume, error) {
	var volume Volume
	if err := c.Post(fmt.Sprintf("/projects/%s/volumes", projectID), req, &volume); err != nil {
		return nil, err
	}
	return &volume, nil
}

func (c *Client) DeleteVolume(volumeID string) error {
	return c.Delete(fmt.Sprintf("/volumes/%s", volumeID))
}
