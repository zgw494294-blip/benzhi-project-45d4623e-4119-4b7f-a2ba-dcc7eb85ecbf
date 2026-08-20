package report_statistics_integrity

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

func TestReportSnapshotRejectsMismatchedStatistics(t *testing.T) {
	createdAt := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	sites := []domain.InspectionSite{{ID: "site-1", Name: "照明带", Category: "公共照明", Location: "河岸步道"}}
	plan, err := domain.NewPlan("plan-1", "河岸巡检", "南片区", "2026-08-20", sites, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := domain.NewObservation("observation-1", "site-1", "亮灯数", "8/8", "盏", "", "巡检员", domain.SeverityNormal, createdAt, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.AppendObservation(observation); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Close(createdAt.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	plan.Report.SeverityCounts[domain.SeverityNormal] = 0
	plan.Report.SeverityCounts[domain.SeverityMajor] = 1
	plan.Report.Checksum = reportChecksum(*plan.Report)
	snapshot := store.Snapshot{SchemaVersion: 1, SavedAt: createdAt, Plans: []domain.InspectionPlan{plan}}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	err = store.NewFileStore(path).Load(context.Background())
	if !errors.Is(err, domain.ErrSnapshotInvalid) {
		t.Fatalf("expected mismatched report statistics to be rejected, got %v", err)
	}
}

func reportChecksum(report domain.InspectionReport) string {
	report.Checksum = ""
	data, _ := json.Marshal(report)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
