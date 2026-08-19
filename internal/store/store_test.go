package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"community-inspection/internal/domain"
)

func makeStorePlan() domain.InspectionPlan {
	plan, _ := domain.NewPlan("plan-store", "夜间巡检", "西片区", "2026-08-20", []domain.InspectionSite{{ID: "site-store", Name: "路灯", Category: "公共照明", Location: "西门"}}, time.Now())
	return plan
}

func TestFileStorePersistsAndRestoresSnapshot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nested", "inspection.json")
	first := NewFileStore(file)
	if err := first.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	plan := makeStorePlan()
	if err := plan.Validate(); err != nil {
		t.Fatalf("test plan should be valid: %+v", plan)
	}
	if err := first.Create(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	updated, err := first.Update(context.Background(), plan.ID, plan.Version, func(current *domain.InspectionPlan) error {
		current.Name = "夜间巡检已更新"
		current.Version++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "夜间巡检已更新" {
		t.Fatalf("unexpected update: %+v", updated)
	}
	second := NewFileStore(file)
	if err := second.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, err := second.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Name != updated.Name || restored.Version != updated.Version {
		t.Fatalf("unexpected restored plan: %+v", restored)
	}
}

func TestFileStoreRejectsUnsupportedSchema(t *testing.T) {
	file := filepath.Join(t.TempDir(), "inspection.json")
	data, err := json.Marshal(Snapshot{SchemaVersion: 99, Plans: []domain.InspectionPlan{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	err = NewFileStore(file).Load(context.Background())
	if !errors.Is(err, domain.ErrSnapshotInvalid) {
		t.Fatalf("expected snapshot error, got %v", err)
	}
}

func TestFileStorePersistsReportCounts(t *testing.T) {
	file := filepath.Join(t.TempDir(), "inspection.json")
	first := NewFileStore(file)
	if err := first.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	plan, err := domain.NewPlan("plan-report", "报告统计", "东片区", "2026-08-20", []domain.InspectionSite{
		{ID: "site-report-1", Name: "活动场", Category: "活动设施", Location: "东门"},
		{ID: "site-report-2", Name: "路灯", Category: "公共照明", Location: "东门路"},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	firstObservation, err := domain.NewObservation("observation-report-1", "site-report-1", "外观", "正常", "", "", "巡检员", domain.SeverityNormal, time.Now(), "key-report-1")
	if err != nil {
		t.Fatal(err)
	}
	secondObservation, err := domain.NewObservation("observation-report-2", "site-report-2", "稳固性", "需加固", "", "", "巡检员", domain.SeverityMajor, time.Now(), "key-report-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.AppendObservation(firstObservation); err != nil {
		t.Fatal(err)
	}
	if err := plan.AppendObservation(secondObservation); err != nil {
		t.Fatal(err)
	}
	if err := plan.ReviewObservation(secondObservation.ID, domain.ReviewRejected, "主管", "补充加固", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Close(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := first.Create(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	second := NewFileStore(file)
	if err := second.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, err := second.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Report == nil || restored.Report.SeverityCounts[domain.SeverityNormal] != 1 || restored.Report.SeverityCounts[domain.SeverityMajor] != 1 || restored.Report.SeverityCounts[domain.SeverityMinor] != 0 || restored.Report.ReviewCounts[domain.ReviewRejected] != 1 || restored.Report.ReviewCounts[domain.ReviewApproved] != 0 {
		t.Fatalf("report counts were not restored: %+v", restored.Report)
	}
}
