package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/upuai-cloud/cli/internal/api"
)

// --secret é tri-estado no wire: ausente não manda o campo (a API preserva o que
// a variável já é), true marca, false desmarca. Mandar false por default
// desmascararia todo secret cujo valor fosse trocado sem repetir a flag.
func TestBuildVariableInputs_SecretOnTheWire(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name   string
		secret *bool
		want   string
	}{
		{"flag ausente omite o campo", nil, `{"key":"API_KEY","value":"v"}`},
		{"--secret marca", &yes, `{"key":"API_KEY","value":"v","isSecret":true}`},
		{"--secret=false desmarca", &no, `{"key":"API_KEY","value":"v","isSecret":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vars, err := buildVariableInputs([]string{"API_KEY=v"}, "", tc.secret)
			if err != nil {
				t.Fatalf("buildVariableInputs: %v", err)
			}
			got, err := json.Marshal(vars[0])
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("payload = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestBuildVariableInputs(t *testing.T) {
	yes := true
	vars, err := buildVariableInputs([]string{"A=1", "B=x=y"}, "RUNTIME", &yes)
	if err != nil {
		t.Fatalf("buildVariableInputs: %v", err)
	}
	if len(vars) != 2 || vars[0].Key != "A" || vars[1].Key != "B" || vars[1].Value != "x=y" {
		t.Fatalf("unexpected vars: %+v", vars)
	}
	for _, v := range vars {
		if v.Scope != "RUNTIME" || v.IsSecret == nil || !*v.IsSecret {
			t.Errorf("%s: scope/secret not applied to every variable: %+v", v.Key, v)
		}
	}

	for name, args := range map[string][]string{
		"sem =":           {"NOVALUE"},
		"chave duplicada": {"A=1", "A=2"},
	} {
		if _, err := buildVariableInputs(args, "", nil); err == nil {
			t.Errorf("%s: expected an error for %v", name, args)
		}
	}
}

func TestSetVariableMessage(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		in   api.VariableInput
		want string
	}{
		{"simples", api.VariableInput{Key: "A"}, "Set A [service]"},
		{"scope both não aparece", api.VariableInput{Key: "A", Scope: "BOTH"}, "Set A [service]"},
		{"scope", api.VariableInput{Key: "A", Scope: "BUILD"}, "Set A (scope: build) [service]"},
		{"secret", api.VariableInput{Key: "A", IsSecret: &yes}, "Set A (secret) [service]"},
		{"desmarcado", api.VariableInput{Key: "A", IsSecret: &no}, "Set A (not secret) [service]"},
		{"secret e scope", api.VariableInput{Key: "A", IsSecret: &yes, Scope: "RUNTIME"}, "Set A (secret, scope: runtime) [service]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := setVariableMessage(tc.in, "service"); got != tc.want {
				t.Errorf("setVariableMessage = %q, want %q", got, tc.want)
			}
		})
	}
}

// O valor de um secret nunca aparece no output do `set`.
func TestSetVariableMessage_NeverEchoesTheValue(t *testing.T) {
	yes := true
	msg := setVariableMessage(api.VariableInput{Key: "API_KEY", Value: "s3cr3t-value", IsSecret: &yes}, "service")
	if strings.Contains(msg, "s3cr3t-value") {
		t.Errorf("message leaks the value: %q", msg)
	}
}

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
