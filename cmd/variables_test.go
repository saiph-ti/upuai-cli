package cmd

import "testing"

// O aviso de "vale no próximo deploy" sugere um `upuai redeploy` que alcança o
// mesmo serviço e ambiente do comando de variáveis — sem -s/-e, quem o segue de
// fora do serviço linkado redeploya outro serviço.
func TestRedeployHint(t *testing.T) {
	cases := []struct {
		name, service, env, want string
	}{
		{"serviço linkado", "", "", "upuai redeploy"},
		{"serviço explícito", "worker", "", "upuai redeploy -s worker"},
		{"serviço e ambiente", "worker", "staging", "upuai redeploy -s worker -e staging"},
		{"só ambiente", "", "staging", "upuai redeploy -e staging"},
		{"valor com espaço vai entre aspas", "my worker", "", "upuai redeploy -s 'my worker'"},
		{"aspas simples escapadas", "it's", "", `upuai redeploy -s 'it'\''s'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := flagEnvironment
			t.Cleanup(func() { flagEnvironment = prev })
			flagEnvironment = tc.env

			if got := redeployHint(shellArg(tc.service)); got != tc.want {
				t.Errorf("redeployHint = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShellArg(t *testing.T) {
	for in, want := range map[string]string{
		"":                          "",
		"api-prod_2.v1":             "api-prod_2.v1",
		"cmobddc1a000v01pinklg0qo9": "cmobddc1a000v01pinklg0qo9",
		"a;rm -rf":                  "'a;rm -rf'",
		"$(id)":                     "'$(id)'",
		"<service>":                 "'<service>'",
	} {
		if got := shellArg(in); got != want {
			t.Errorf("shellArg(%q) = %q, want %q", in, got, want)
		}
	}
}
