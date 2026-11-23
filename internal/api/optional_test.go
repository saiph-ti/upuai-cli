package api

import (
	"encoding/json"
	"testing"
)

// O PATCH …/instance precisa distinguir os três estados do tempo de
// desligamento: não mexer, voltar ao padrão (null) e um valor.
func TestUpdateInstanceRequestShutdownTimeoutStates(t *testing.T) {
	cases := []struct {
		name string
		opt  OptionalInt
		want string
	}{
		{"ausente não vai no corpo", OptionalInt{}, `{"deploy":{"startCommand":"x"}}`},
		{"null volta ao padrão", OptionalIntNull(), `{"deploy":{"startCommand":"x","shutdownTimeoutSeconds":null}}`},
		{"valor", OptionalIntValue(120), `{"deploy":{"startCommand":"x","shutdownTimeoutSeconds":120}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := UpdateInstanceRequest{Deploy: &InstanceDeployConfig{StartCommand: "x", ShutdownTimeoutSeconds: tc.opt}}
			b, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.want {
				t.Fatalf("json = %s, want %s", b, tc.want)
			}
		})
	}
}

func TestInstanceDecodesShutdownTimeout(t *testing.T) {
	var cfg InstanceConfig
	if err := json.Unmarshal([]byte(`{"deploy":{"shutdownTimeoutSeconds":90}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if v, ok := cfg.Deploy.ShutdownTimeoutSeconds.Value(); !ok || v != 90 {
		t.Fatalf("Value() = %d,%v, want 90,true", v, ok)
	}
	cfg = InstanceConfig{}
	if err := json.Unmarshal([]byte(`{"deploy":{"shutdownTimeoutSeconds":null}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Deploy.ShutdownTimeoutSeconds.Value(); ok {
		t.Fatal("null decodificado como valor")
	}
}
