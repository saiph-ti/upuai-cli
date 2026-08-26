package cmd

import (
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
)

func TestMatchDomainRef(t *testing.T) {
	domains := []api.Domain{
		{ID: "cmraxvcnm00ln0mqazza9b7eq", Domain: "petalia.com.br"},
		{ID: "cmraxvco900lo0mqa2vkfrrqf", Domain: "www.petalia.com.br"},
	}
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{"exact id", "cmraxvco900lo0mqa2vkfrrqf", "cmraxvco900lo0mqa2vkfrrqf"},
		{"hostname exact", "www.petalia.com.br", "cmraxvco900lo0mqa2vkfrrqf"},
		{"hostname case-insensitive", "WWW.Petalia.COM.BR", "cmraxvco900lo0mqa2vkfrrqf"},
		{"apex is not the www", "petalia.com.br", "cmraxvcnm00ln0mqazza9b7eq"},
		{"unknown id passes through", "cmnotinthislist0000000000x", "cmnotinthislist0000000000x"},
		{"unknown hostname passes through", "ghost.example.com", "ghost.example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchDomainRef(domains, tc.ref); got != tc.want {
				t.Fatalf("matchDomainRef(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

func TestFormatDomainRedirect(t *testing.T) {
	plain := api.Domain{Domain: "petalia.com.br"}
	if got := formatDomainRedirect(plain); got != "-" {
		t.Fatalf("sem redirect deve ser \"-\", veio %q", got)
	}
	www := api.Domain{
		Domain:     "www.petalia.com.br",
		RedirectTo: &api.DomainRedirect{DomainID: "cmraxvcnm00ln0mqazza9b7eq", Hostname: "petalia.com.br", Status: 301},
	}
	if got := formatDomainRedirect(www); got != "→ petalia.com.br (301)" {
		t.Fatalf("formatDomainRedirect = %q", got)
	}
}

func TestValidateRedirectStatus(t *testing.T) {
	for _, ok := range []int{301, 302} {
		if err := validateRedirectStatus(ok); err != nil {
			t.Fatalf("%d deveria ser aceito: %v", ok, err)
		}
	}
	for _, bad := range []int{0, 200, 307, 308} {
		if err := validateRedirectStatus(bad); err == nil {
			t.Fatalf("%d deveria ser rejeitado", bad)
		}
	}
}
