package api

import "fmt"

// DatabaseRecoveryWindow é a janela de restore para um ponto no tempo
// (Postgres): do backup base mais antigo retido ao último WAL arquivado.
type DatabaseRecoveryWindow struct {
	Available                bool   `json:"available"`
	FirstRecoverabilityPoint string `json:"firstRecoverabilityPoint"`
	LatestRestorableTime     string `json:"latestRestorableTime"`
	UnavailableReason        string `json:"unavailableReason"`
}

// RestoreDatabaseResult é o banco NOVO criado por um restore.
type RestoreDatabaseResult struct {
	ServiceID  string `json:"serviceId"`
	Name       string `json:"name"`
	TargetTime string `json:"targetTime"`
}

type restoreDatabaseRequest struct {
	TargetTime string `json:"targetTime"`
	Name       string `json:"name,omitempty"`
}

func (c *Client) GetDatabaseRecoveryWindow(envID, serviceID string) (*DatabaseRecoveryWindow, error) {
	var w DatabaseRecoveryWindow
	if err := c.Get(fmt.Sprintf("/environments/%s/services/%s/database/recovery-window", envID, serviceID), &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// RestoreDatabase cria um banco NOVO no mesmo ambiente com os dados da origem
// em targetTime (RFC 3339). A origem não é tocada.
func (c *Client) RestoreDatabase(envID, serviceID, targetTime, name string) (*RestoreDatabaseResult, error) {
	var r RestoreDatabaseResult
	if err := c.Post(
		fmt.Sprintf("/environments/%s/services/%s/database/restore", envID, serviceID),
		restoreDatabaseRequest{TargetTime: targetTime, Name: name},
		&r,
	); err != nil {
		return nil, err
	}
	return &r, nil
}
