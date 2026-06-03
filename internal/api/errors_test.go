package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func respWith(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestAPIError_SurfacesValidationDetails(t *testing.T) {
	c := &Client{}
	// Validation error: details is Record<string,string> (Zod parse). Should be surfaced.
	err := c.parseError(respWith(400, `{"statusCode":400,"error":"ValidationError","message":"Invalid data","details":{"hostname":"Hostname is required"},"requestId":"req-6j8"}`))
	got := err.Error()
	for _, want := range []string{"400", "Invalid data", "hostname: Hostname is required", "req-6j8"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestAPIError_PlanLimitNumericDetails_NoRegression(t *testing.T) {
	c := &Client{}
	// PlanLimit: details has non-string (numeric) values. Must NOT lose message.
	err := c.parseError(respWith(403, `{"statusCode":403,"code":"PLAN_LIMIT_DOMAINS","message":"Domain limit reached","details":{"current":3,"limit":3,"plan":"FREE"},"requestId":"req-x"}`))
	got := err.Error()
	if !strings.Contains(got, "Domain limit reached") || !strings.Contains(got, "req-x") {
		t.Fatalf("message/requestId lost on numeric details: %q", got)
	}
	if ae, ok := err.(*APIError); ok && ae.Details != nil {
		t.Fatalf("numeric details should be dropped, got %#v", ae.Details)
	}
}

func TestAPIError_ArrayDetails_NoRegression(t *testing.T) {
	c := &Client{}
	// details as string[] — must not break message parsing.
	err := c.parseError(respWith(400, `{"statusCode":400,"message":"Bad input","details":["a","b"],"requestId":"req-y"}`))
	got := err.Error()
	if !strings.Contains(got, "Bad input") || !strings.Contains(got, "req-y") {
		t.Fatalf("message/requestId lost on array details: %q", got)
	}
}

func TestAPIError_NoDetails(t *testing.T) {
	c := &Client{}
	err := c.parseError(respWith(404, `{"statusCode":404,"message":"Not found","requestId":"req-z"}`))
	got := err.Error()
	if got != "API error 404: Not found (requestId: req-z)" {
		t.Fatalf("unexpected: %q", got)
	}
}
