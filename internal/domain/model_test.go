package domain

import (
	"errors"
	"testing"
	"time"
)

func testPlan(t *testing.T) InspectionPlan {
	t.Helper()
	plan, err := NewPlan("plan-1", "早班巡检", "东片区", "2026-08-20", []InspectionSite{
		{ID: "site-1", Name: "健身点", Category: "健身器材", Location: "东门"},
		{ID: "site-2", Name: "路灯带", Category: "公共照明", Location: "花园路"},
	}, time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestPlanRequiresOrderedObservationsAndReviewedExceptions(t *testing.T) {
	plan := testPlan(t)
	first, err := NewObservation("observation-1", "site-1", "外观", "正常", "", "", "巡检员", SeverityNormal, time.Now(), "key-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewObservation("observation-2", "site-2", "亮灯数", "7/8", "盏", "西南角未亮", "巡检员", SeverityMajor, time.Now(), "key-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.AppendObservation(second); !errors.Is(err, ErrOutOfOrder) {
		t.Fatalf("expected order error, got %v", err)
	}
	if err := plan.AppendObservation(first); err != nil {
		t.Fatal(err)
	}
	if err := plan.AppendObservation(second); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Close(time.Now()); !errors.Is(err, ErrNotReadyToClose) {
		t.Fatalf("expected pending review error, got %v", err)
	}
	if err := plan.ReviewObservation(second.ID, ReviewApproved, "主管", "已登记维修", time.Now()); err != nil {
		t.Fatal(err)
	}
	report, err := plan.Close(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.ExceptionCount != 1 || report.Checksum == "" || plan.Status != PlanStatusClosed {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.SeverityCounts[SeverityNormal] != 1 || report.SeverityCounts[SeverityMajor] != 1 || report.SeverityCounts[SeverityMinor] != 0 || report.SeverityCounts[SeverityCritical] != 0 {
		t.Fatalf("unexpected severity counts: %+v", report.SeverityCounts)
	}
	if report.ReviewCounts[ReviewNotRequired] != 1 || report.ReviewCounts[ReviewApproved] != 1 || report.ReviewCounts[ReviewPending] != 0 || report.ReviewCounts[ReviewRejected] != 0 {
		t.Fatalf("unexpected review counts: %+v", report.ReviewCounts)
	}
	originalChecksum := report.Checksum
	report.Summary = "修改后的文本"
	report.SeverityCounts[SeverityMajor] = 99
	loaded, err := plan.Close(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Checksum != originalChecksum || loaded.Summary == report.Summary || loaded.SeverityCounts[SeverityMajor] != 1 {
		t.Fatal("closed report should be immutable")
	}
}

func TestNewPlanFromRouteCreatesFreshLifecycle(t *testing.T) {
	source := testPlan(t)
	first, err := NewObservation("observation-1", "site-1", "外观", "正常", "", "", "巡检员", SeverityNormal, time.Now(), "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := source.AppendObservation(first); err != nil {
		t.Fatal(err)
	}
	copied, err := NewPlanFromRoute("plan-copy", "复制路线", "西片区", "2026-08-27", source, []string{"site-copy-1", "site-copy-2"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if copied.Status != PlanStatusActive || copied.Version != 1 || copied.Report != nil || copied.Sites[0].Status != SiteStatusInProgress {
		t.Fatalf("unexpected copied lifecycle: %+v", copied)
	}
	if copied.Sites[0].ID == source.Sites[0].ID || copied.Sites[0].Observations == nil || len(copied.Sites[0].Observations) != 0 || copied.Sites[1].Observations == nil || len(copied.Sites[1].Observations) != 0 {
		t.Fatalf("copied route should have fresh point state: %+v", copied.Sites)
	}
	if copied.Sites[0].Name != source.Sites[0].Name || copied.Sites[1].Location != source.Sites[1].Location {
		t.Fatalf("route definition was not preserved: %+v", copied.Sites)
	}
}

func TestObservationRejectsBlankAndUnknownSeverity(t *testing.T) {
	if _, err := NewObservation("id", "site", "", "1", "", "", "巡检员", SeverityNormal, time.Now(), "key"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid input, got %v", err)
	}
	if _, err := NewObservation("id", "site", "项目", "1", "", "", "巡检员", Severity("unknown"), time.Now(), "key"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid severity, got %v", err)
	}
}
