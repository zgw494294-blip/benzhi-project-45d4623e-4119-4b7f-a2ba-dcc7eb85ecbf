package report_count_integrity_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"community-inspection/internal/domain"
	"community-inspection/internal/store"
)

func TestSnapshotRejectsReportSiteResultMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inspection.json")
	repository := store.NewFileStore(path)
	if err := repository.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	plan, err := domain.NewPlan("plan-report-count", "报告统计", "东片区", "2026-08-20", []domain.InspectionSite{{
		ID: "site-report-count", Name: "饮水台", Category: "公共服务", Location: "东门",
	}}, time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	observation, err := domain.NewObservation("observation-report-count", plan.Sites[0].ID, "外观", "正常", "", "", "巡检员", domain.SeverityNormal, time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC), "report-count-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.AppendObservation(observation); err != nil {
		t.Fatal(err)
	}
	report, err := plan.Close(time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	plan.Report = &report
	if err := repository.Create(context.Background(), plan); err != nil {
		t.Fatal(err)
	}

	snapshot, err := repository.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Plans[0].Report.SiteResults[0].SiteID = "ghost-site"
	snapshot.Plans[0].Report.Checksum = checksumReport(*snapshot.Plans[0].Report)
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := store.NewFileStore(path)
	err = loaded.Load(context.Background())
	if !errors.Is(err, domain.ErrSnapshotInvalid) {
		t.Fatalf("expected invalid report snapshot, got %v", err)
	}
}

func checksumReport(report domain.InspectionReport) string {
	report.Checksum = ""
	data, _ := json.Marshal(report)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
