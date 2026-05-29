package api

import "testing"

// Regressão #6: down/rollback escolhiam por índice cru ([0]/[1]), podendo
// agir no deployment errado (ex: [0] é um build em andamento, [1] falhou).
// Agora selecionam por IsActive / CanRollback.

func TestActiveDeployment(t *testing.T) {
	// [0] é um build em andamento (não-ativo); o ativo é [1].
	ds := []Deployment{
		{ID: "d3", Status: "building", IsActive: false},
		{ID: "d2", Status: "success", IsActive: true},
		{ID: "d1", Status: "success", IsActive: false},
	}
	got := ActiveDeployment(ds)
	if got == nil || got.ID != "d2" {
		t.Fatalf("esperado d2 (ativo), veio %v", got)
	}
	if ActiveDeployment([]Deployment{{IsActive: false}}) != nil {
		t.Fatal("esperado nil quando nenhum ativo")
	}
	if ActiveDeployment(nil) != nil {
		t.Fatal("esperado nil para lista vazia")
	}
}

func TestRollbackTarget(t *testing.T) {
	// [0] é o ativo (não elegível), [1] falhou, [2] é o último success elegível.
	ds := []Deployment{
		{ID: "d3", IsActive: true, CanRollback: false},
		{ID: "d2", Status: "failed", CanRollback: false},
		{ID: "d1", Status: "success", CanRollback: true},
	}
	got := RollbackTarget(ds)
	if got == nil || got.ID != "d1" {
		t.Fatalf("esperado d1 (último success elegível), veio %v", got)
	}
	if RollbackTarget([]Deployment{{CanRollback: false}}) != nil {
		t.Fatal("esperado nil quando nenhum elegível")
	}
}
