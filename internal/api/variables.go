package api

import "fmt"

type EnvVar struct {
	ID               string `json:"id"`
	Key              string `json:"key"`
	Value            string `json:"value"`
	ResolvedValue    string `json:"resolvedValue,omitempty"`
	IsSecret         bool   `json:"isSecret"`
	HasInterpolation bool   `json:"hasInterpolation"`
	// Scope: BOTH (default) | RUNTIME | BUILD — fase(s) em que a var é injetada.
	// Omitido em respostas legadas (tratar como BOTH).
	Scope string `json:"scope,omitempty"`
}

// DisplayValue returns the resolved value if available, otherwise the raw value.
func (v *EnvVar) DisplayValue() string {
	if v.ResolvedValue != "" {
		return v.ResolvedValue
	}
	return v.Value
}

type VariableInput struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"isSecret,omitempty"`
	// Scope: "BOTH" | "RUNTIME" | "BUILD". Vazio = servidor mantém o atual (update)
	// ou aplica o default BOTH (create).
	Scope string `json:"scope,omitempty"`
}

func (c *Client) ListVariables(envID, serviceID string) ([]EnvVar, error) {
	var vars []EnvVar
	err := c.Get(fmt.Sprintf("/environments/%s/services/%s/variables", envID, serviceID), &vars)
	if err != nil {
		return nil, err
	}
	return vars, nil
}

type setVariablesBody struct {
	Variables []VariableInput `json:"variables"`
}

func (c *Client) SetVariables(envID, serviceID string, vars []VariableInput) ([]EnvVar, error) {
	var result []EnvVar
	body := setVariablesBody{Variables: vars}
	err := c.Put(fmt.Sprintf("/environments/%s/services/%s/variables", envID, serviceID), body, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DeleteVariable(envID, serviceID, key string) error {
	return c.Delete(fmt.Sprintf("/environments/%s/services/%s/variables/%s", envID, serviceID, key))
}

// ─── Variáveis compartilhadas em camadas (Heroku Config Vars / Railway) ──────
// Project-level (global): mesmo valor em todos os ambientes do projeto.

func (c *Client) ListProjectVariables(projectID string) ([]EnvVar, error) {
	var vars []EnvVar
	if err := c.Get(fmt.Sprintf("/projects/%s/variables", projectID), &vars); err != nil {
		return nil, err
	}
	return vars, nil
}

func (c *Client) SetProjectVariables(projectID string, vars []VariableInput) ([]EnvVar, error) {
	var result []EnvVar
	if err := c.Put(fmt.Sprintf("/projects/%s/variables", projectID), setVariablesBody{Variables: vars}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DeleteProjectVariable(projectID, key string) error {
	return c.Delete(fmt.Sprintf("/projects/%s/variables/%s", projectID, key))
}

// Environment-level (shared): herdada por todos os serviços do ambiente.

func (c *Client) ListEnvironmentVariables(envID string) ([]EnvVar, error) {
	var vars []EnvVar
	if err := c.Get(fmt.Sprintf("/environments/%s/variables", envID), &vars); err != nil {
		return nil, err
	}
	return vars, nil
}

func (c *Client) SetEnvironmentVariables(envID string, vars []VariableInput) ([]EnvVar, error) {
	var result []EnvVar
	if err := c.Put(fmt.Sprintf("/environments/%s/variables", envID), setVariablesBody{Variables: vars}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DeleteEnvironmentVariable(envID, key string) error {
	return c.Delete(fmt.Sprintf("/environments/%s/variables/%s", envID, key))
}
