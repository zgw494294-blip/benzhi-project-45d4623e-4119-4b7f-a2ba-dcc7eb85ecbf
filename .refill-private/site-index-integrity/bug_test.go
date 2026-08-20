package siteindexintegrity_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"community-inspection/internal/domain"
	"community-inspection/internal/store"
)

func TestSnapshotRejectsDuplicateSiteIndex(t *testing.T) {
	plan, err := domain.NewPlan(
		"plan-site-index",
		"点位索引校验",
		"东片区",
		"2026-08-20",
		[]domain.InspectionSite{
			{ID: "site-a", Name: "入口", Category: "公共设施", Location: "东门"},
			{ID: "site-b", Name: "广场", Category: "公共设施", Location: "中心"},
		},
		time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan.SiteIDs = []string{plan.Sites[0].ID, plan.Sites[0].ID}
	snapshot := store.Snapshot{
		SchemaVersion: 1,
		SavedAt:       time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC),
		Plans:         []domain.InspectionPlan{plan},
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "inspection.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	err = store.NewFileStore(path).Load(context.Background())
	if !errors.Is(err, domain.ErrSnapshotInvalid) {
		t.Fatalf("expected duplicate site index snapshot to be rejected, got %v", err)
	}
}
