package api

import "fmt"

type EnvVar struct {
	ID               string `json:"id"`
	Key              string `json:"key"`
	Value            string `json:"value"`
	ResolvedValue    string `json:"resolvedValue,omitempty"`
	IsSecret         bool   `json:"isSecret"`
	HasInterpolation bool   `json:"hasInterpolation"`
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
