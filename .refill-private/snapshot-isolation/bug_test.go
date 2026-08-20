package snapshotisolation

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"community-inspection/internal/domain"
	"community-inspection/internal/store"
)

func TestSnapshotMutationDoesNotChangeStoredPlan(t *testing.T) {
	repository := store.NewFileStore(filepath.Join(t.TempDir(), "inspection.json"))
	if err := repository.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	plan, err := domain.NewPlan("plan", "快照隔离", "东区", "2026-08-20", []domain.InspectionSite{
		{ID: "site", Name: "中心路灯", Category: "照明", Location: "中心广场"},
	}, time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Create(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Plans[0].Name = "外部修改的计划"
	snapshot.Plans[0].SiteIDs[0] = "external-site"
	snapshot.Plans[0].Sites[0].Name = "外部修改的点位"
	stored, err := repository.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != plan.Name || stored.SiteIDs[0] != plan.SiteIDs[0] || stored.Sites[0].Name != plan.Sites[0].Name {
		t.Fatalf("TestSnapshotMutationDoesNotChangeStoredPlan: 修改返回快照污染了仓储状态：%+v", stored)
	}
}
