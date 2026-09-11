package cmd

import (
	"strings"
	"testing"
)

func TestParseReplicaCount(t *testing.T) {
	cases := []struct {
		raw     string
		want    int
		wantErr string
	}{
		{raw: "1", want: 1},
		{raw: "3", want: 3},
		{raw: "0", wantErr: "at least 1 replica"},
		{raw: "-2", wantErr: "at least 1 replica"},
		{raw: "abc", wantErr: "must be an integer"},
	}
	for _, tc := range cases {
		got, err := parseReplicaCount(tc.raw)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("parseReplicaCount(%q) err = %v, want containing %q", tc.raw, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("parseReplicaCount(%q) = %d, %v; want %d", tc.raw, got, err, tc.want)
		}
	}
}
