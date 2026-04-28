package api

import "fmt"

type APIError struct {
	StatusCode int    `json:"statusCode"`
	Message    string `json:"message"`
	Path       string `json:"path,omitempty"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
}
