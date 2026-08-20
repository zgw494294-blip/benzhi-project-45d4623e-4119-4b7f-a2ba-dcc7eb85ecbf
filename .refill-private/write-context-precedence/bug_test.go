package writecontextprecedence

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"community-inspection/internal/service"
	"community-inspection/internal/store"
)

func TestCanceledWritesReturnContextErrorBeforeInputValidation(t *testing.T) {
	repository := store.NewFileStore(filepath.Join(t.TempDir(), "inspection.json"))
	if err := repository.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(repository, service.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.CreatePlan(ctx, service.CreatePlanInput{}); !errors.Is(err, context.Canceled) {
		t.Errorf("创建计划预期 context.Canceled，实际为 %v", err)
	}
	if _, _, err := svc.AddObservation(ctx, "missing-plan", service.ObservationInput{}); !errors.Is(err, context.Canceled) {
		t.Errorf("追加观测预期 context.Canceled，实际为 %v", err)
	}
}
