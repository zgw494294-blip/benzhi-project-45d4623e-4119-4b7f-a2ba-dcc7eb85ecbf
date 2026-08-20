package listcontextcancel

import (
	"context"
	"errors"
	"testing"
	"time"

	"community-inspection/internal/domain"
	"community-inspection/internal/service"
	"community-inspection/internal/store"
)

type cancelOnSnapshotRepo struct {
	cancel context.CancelFunc
	plan   domain.InspectionPlan
}

func (r *cancelOnSnapshotRepo) Load(context.Context) error { return nil }
func (r *cancelOnSnapshotRepo) Snapshot(context.Context) (store.Snapshot, error) {
	r.cancel()
	return store.Snapshot{SchemaVersion: 1, Plans: []domain.InspectionPlan{r.plan}}, nil
}
func (r *cancelOnSnapshotRepo) Create(context.Context, domain.InspectionPlan) error { return nil }
func (r *cancelOnSnapshotRepo) Update(context.Context, string, int64, func(*domain.InspectionPlan) error) (domain.InspectionPlan, error) {
	return domain.InspectionPlan{}, errors.New("未实现")
}
func (r *cancelOnSnapshotRepo) Get(context.Context, string) (domain.InspectionPlan, error) {
	return r.plan, nil
}

func TestListObservationsStopsWhenContextIsCanceledDuringSnapshot(t *testing.T) {
	plan, err := domain.NewPlan("plan", "巡检", "东区", "2026-08-20", []domain.InspectionSite{{ID: "site", Name: "路灯", Category: "照明", Location: "东门"}}, time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	repo := &cancelOnSnapshotRepo{cancel: cancel, plan: plan}
	svc, err := service.New(repo, service.Config{Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ListObservations(cancelCtx, service.Filter{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("TestListObservationsStopsWhenContextIsCanceledDuringSnapshot: expected context cancellation, got %v", err)
	}
}
