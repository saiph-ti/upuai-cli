package cmd

import (
	"errors"
	"slices"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
)

// Um proxy restrito nunca é aberto por omissão: sem --allow/--any a lista atual
// vai de novo; num proxy desligado a escolha é obrigatória (E2E 23/11: desligar
// e religar sem flag abria para qualquer IP); --any abre; --allow substitui.
func TestResolveTcpProxyAllowList(t *testing.T) {
	reset := func() { tcpProxyAllow, tcpProxyAllowAny = nil, false }
	t.Cleanup(reset)
	restricted := &api.TcpProxyInfo{Enabled: true, AllowedCidrs: []string{"203.0.113.7/32"}}

	reset()
	if got, err := resolveTcpProxyAllowList(restricted); err != nil || !slices.Equal(got, restricted.AllowedCidrs) {
		t.Fatalf("omissão = %v, %v; want a lista atual", got, err)
	}
	reset()
	if _, err := resolveTcpProxyAllowList(&api.TcpProxyInfo{}); !errors.Is(err, errTcpProxyAllowRequired) {
		t.Fatalf("desligado sem flags = %v; want errTcpProxyAllowRequired", err)
	}
	reset()
	tcpProxyAllowAny = true
	if got, err := resolveTcpProxyAllowList(&api.TcpProxyInfo{}); err != nil || len(got) != 0 {
		t.Fatalf("desligado com --any = %v, %v; want aberto", got, err)
	}
	reset()
	tcpProxyAllowAny = true
	if got, err := resolveTcpProxyAllowList(restricted); err != nil || len(got) != 0 {
		t.Fatalf("--any = %v, %v; want vazio (aberto)", got, err)
	}
	reset()
	tcpProxyAllow = []string{"198.51.100.9", "198.51.100.0/24"}
	if got, err := resolveTcpProxyAllowList(restricted); err != nil || !slices.Equal(got, []string{"198.51.100.9/32", "198.51.100.0/24"}) {
		t.Fatalf("--allow = %v, %v", got, err)
	}
	reset()
	tcpProxyAllow, tcpProxyAllowAny = []string{"198.51.100.9"}, true
	if _, err := resolveTcpProxyAllowList(restricted); err == nil {
		t.Fatal("--any com --allow tem de falhar")
	}
}
