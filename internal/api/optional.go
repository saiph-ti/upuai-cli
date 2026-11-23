package api

import (
	"bytes"
	"encoding/json"
)

// OptionalInt é um inteiro de PATCH com três estados: ausente (o campo não vai
// no corpo e a API não mexe nele), null (volta ao padrão da plataforma) e um
// valor. Um *int com omitempty só tem dois — nil some do JSON —, então não havia
// como pedir "volte ao padrão" para um campo cujo padrão não é um número válido
// (deploy.shutdownTimeoutSeconds aceita 1..300; o padrão é a ausência).
//
// Use com a tag `omitzero`: IsZero = ausente.
type OptionalInt struct {
	set   bool
	value *int
}

// OptionalIntValue é o estado "valor".
func OptionalIntValue(v int) OptionalInt { return OptionalInt{set: true, value: &v} }

// OptionalIntNull é o estado "null": volta ao padrão.
func OptionalIntNull() OptionalInt { return OptionalInt{set: true} }

// IsZero implementa a interface de `omitzero`: ausente não vai no corpo.
func (o OptionalInt) IsZero() bool { return !o.set }

// Value devolve o número e se ele existe (false para ausente e para null).
func (o OptionalInt) Value() (int, bool) {
	if o.value == nil {
		return 0, false
	}
	return *o.value, true
}

func (o OptionalInt) MarshalJSON() ([]byte, error) {
	if o.value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*o.value)
}

func (o *OptionalInt) UnmarshalJSON(b []byte) error {
	o.set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.value = nil
		return nil
	}
	var v int
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.value = &v
	return nil
}
