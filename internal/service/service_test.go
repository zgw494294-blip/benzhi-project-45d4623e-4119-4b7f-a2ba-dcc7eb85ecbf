package service

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"community-inspection/internal/domain"
	"community-inspection/internal/store"
)

func newTestService(t *testing.T) (*Service, *store.FileStore) {
	t.Helper()
	repository := store.NewFileStore(filepath.Join(t.TempDir(), "inspection.json"))
	if err := repository.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	svc, err := New(repository, Config{
		Now:   func() time.Time { return time.Date(2026, 8, 19, 8, 0, 0, 0, time.UTC) },
		NewID: func(prefix string) string { sequence++; return prefix + "-" + string(rune('a'+sequence)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, repository
}

func TestServiceRunsClosedLoopAndFilters(t *testing.T) {
	svc, repository := newTestService(t)
	plan, err := svc.CreatePlan(context.Background(), CreatePlanInput{Name: "河岸设施巡检", Area: "南片区", ScheduledDate: "2026-08-20", Sites: []CreateSiteInput{{Name: "饮水台", Category: "公共服务", Location: "河岸入口"}, {Name: "照明带", Category: "公共照明", Location: "河岸步道"}}})
	if err != nil {
		t.Fatal(err)
	}
	first, firstObservation, err := svc.AddObservation(context.Background(), plan.ID, ObservationInput{SiteID: plan.Sites[0].ID, Kind: "外观", Value: "正常", Observer: "巡检员", Severity: domain.SeverityNormal, ExpectedVersion: plan.Version, IdempotencyKey: "same-request"})
	if err != nil {
		t.Fatal(err)
	}
	repeated, repeatedObservation, err := svc.AddObservation(context.Background(), plan.ID, ObservationInput{SiteID: plan.Sites[0].ID, Kind: "外观", Value: "正常", Observer: "巡检员", Severity: domain.SeverityNormal, ExpectedVersion: first.Version, IdempotencyKey: "same-request"})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Version != first.Version || repeatedObservation.ID != firstObservation.ID {
		t.Fatal("same idempotency key should return the stored observation")
	}
	second, exception, err := svc.AddObservation(context.Background(), plan.ID, ObservationInput{SiteID: plan.Sites[1].ID, Kind: "亮灯数", Value: "7/8", Unit: "盏", Observer: "巡检员", Severity: domain.SeverityMajor, ExpectedVersion: first.Version, IdempotencyKey: "exception-request"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ClosePlan(context.Background(), plan.ID, second.Version); !errors.Is(err, domain.ErrNotReadyToClose) {
		t.Fatalf("expected close guard, got %v", err)
	}
	reviewed, err := svc.ReviewObservation(context.Background(), plan.ID, exception.ID, ReviewInput{Decision: domain.ReviewApproved, Reviewer: "主管", Note: "安排更换", ExpectedVersion: second.Version})
	if err != nil {
		t.Fatal(err)
	}
	closed, report, err := svc.ClosePlan(context.Background(), plan.ID, reviewed.Version)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != domain.PlanStatusClosed || report.ExceptionCount != 1 {
		t.Fatalf("unexpected closed result: %+v %+v", closed, report)
	}
	if _, _, err := svc.AddObservation(context.Background(), plan.ID, ObservationInput{SiteID: plan.Sites[0].ID, Kind: "复测", Value: "正常", Observer: "巡检员", Severity: domain.SeverityNormal}); !errors.Is(err, domain.ErrPlanClosed) {
		t.Fatalf("expected closed guard, got %v", err)
	}
	plans, err := svc.ListPlans(context.Background(), Filter{Date: "2026-08-20", Category: "公共照明", Status: domain.PlanStatusClosed})
	if err != nil || len(plans) != 1 {
		t.Fatalf("unexpected filtered plans: %v %+v", err, plans)
	}
	rows, err := svc.ListObservations(context.Background(), Filter{Category: "公共照明"})
	if err != nil || len(rows) != 1 || rows[0].Observation.ID != exception.ID {
		t.Fatalf("unexpected observation rows: %v %+v", err, rows)
	}
	if _, err := repository.Update(context.Background(), plan.ID, 1, func(current *domain.InspectionPlan) error { return nil }); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestServiceExportsFilteredObservationsInServiceOrder(t *testing.T) {
	svc, repository := newTestService(t)
	target, err := svc.CreatePlan(context.Background(), CreatePlanInput{
		Name: "夜间照明巡检", Area: "东片区", ScheduledDate: "2026-08-20",
		Sites: []CreateSiteInput{{Name: "东门灯带", Category: "公共照明", Location: "东门入口"}, {Name: "花园灯带", Category: "公共照明", Location: "中心花园"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, exception, err := svc.AddObservation(context.Background(), target.ID, ObservationInput{
		SiteID: target.Sites[0].ID, Kind: "灯具亮灯数", Value: "7/8", Unit: "盏", Note: "西南角未亮", Observer: "巡检员甲", Severity: domain.SeverityMajor, ExpectedVersion: target.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := svc.ReviewObservation(context.Background(), target.ID, exception.ID, ReviewInput{Decision: domain.ReviewApproved, Reviewer: "区域主管", Note: "已登记维修工单", ExpectedVersion: first.Version})
	if err != nil {
		t.Fatal(err)
	}
	_, normal, err := svc.AddObservation(context.Background(), target.ID, ObservationInput{
		SiteID: target.Sites[1].ID, Kind: "灯具外观", Value: "正常", Observer: "巡检员甲", Severity: domain.SeverityNormal, ExpectedVersion: reviewed.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreatePlan(context.Background(), CreatePlanInput{
		Name: "次日照明巡检", Area: "西片区", ScheduledDate: "2026-08-21",
		Sites: []CreateSiteInput{{Name: "西门灯带", Category: "公共照明", Location: "西门入口"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AddObservation(context.Background(), other.ID, ObservationInput{
		SiteID: other.Sites[0].ID, Kind: "灯具亮灯数", Value: "6/8", Unit: "盏", Observer: "巡检员乙", Severity: domain.SeverityMajor, ExpectedVersion: other.Version,
	}); err != nil {
		t.Fatal(err)
	}

	before, err := repository.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ExportObservations(context.Background(), Filter{
		DateFrom: "2026-08-20", DateTo: "2026-08-20", Category: "公共照明", Status: domain.PlanStatusActive,
		Severity: domain.SeverityMajor, ReviewStatus: domain.ReviewApproved, Query: "灯具",
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := repository.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("export should not change the stored snapshot")
	}
	if len(rows) != 1 {
		t.Fatalf("unexpected exported rows: %+v", rows)
	}
	row := rows[0]
	if row.PlanID != target.ID || row.PlanName != target.Name || row.ScheduledDate != target.ScheduledDate || row.SiteID != target.Sites[0].ID || row.SiteName != "东门灯带" || row.SiteCategory != "公共照明" || row.ObservationID != exception.ID || row.Kind != "灯具亮灯数" || row.Value != "7/8" || row.Unit != "盏" || row.Note != "西南角未亮" || row.Observer != "巡检员甲" || row.Severity != string(domain.SeverityMajor) || row.ReviewStatus != string(domain.ReviewApproved) || row.Reviewer != "区域主管" || row.ReviewNote != "已登记维修工单" || row.ReviewedAt == "" {
		t.Fatalf("unexpected exported row: %+v", row)
	}

	ordered, err := svc.ExportObservations(context.Background(), Filter{Date: target.ScheduledDate})
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 2 || ordered[0].ObservationID != exception.ID || ordered[1].ObservationID != normal.ID {
		t.Fatalf("export order changed: %+v", ordered)
	}
}

func TestServiceCopiesActiveAndClosedRoutes(t *testing.T) {
	svc, _ := newTestService(t)
	active, err := svc.CreatePlan(context.Background(), CreatePlanInput{
		Name: "周巡路线", Area: "北片区", ScheduledDate: "2026-08-20",
		Sites: []CreateSiteInput{{Name: "儿童活动场", Category: "活动设施", Location: "北门广场"}, {Name: "休息座椅", Category: "公共家具", Location: "北门广场东侧"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	copiedActive, err := svc.CopyPlan(context.Background(), active.ID, CopyPlanInput{Name: "周巡路线副本", Area: "北片区", ScheduledDate: "2026-08-27"})
	if err != nil {
		t.Fatal(err)
	}
	assertCopiedRoute(t, active, copiedActive)

	first, _, err := svc.AddObservation(context.Background(), active.ID, ObservationInput{SiteID: active.Sites[0].ID, Kind: "外观", Value: "正常", Observer: "巡检员", Severity: domain.SeverityNormal, ExpectedVersion: active.Version})
	if err != nil {
		t.Fatal(err)
	}
	closed, exception, err := svc.AddObservation(context.Background(), active.ID, ObservationInput{SiteID: active.Sites[1].ID, Kind: "稳固性", Value: "需加固", Observer: "巡检员", Severity: domain.SeverityMinor, ExpectedVersion: first.Version})
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := svc.ReviewObservation(context.Background(), active.ID, exception.ID, ReviewInput{Decision: domain.ReviewApproved, Reviewer: "主管", Note: "已安排处理", ExpectedVersion: closed.Version})
	if err != nil {
		t.Fatal(err)
	}
	closed, _, err = svc.ClosePlan(context.Background(), active.ID, reviewed.Version)
	if err != nil {
		t.Fatal(err)
	}
	copiedClosed, err := svc.CopyPlan(context.Background(), closed.ID, CopyPlanInput{Name: closed.Name, Area: closed.Area, ScheduledDate: "2026-09-03"})
	if err != nil {
		t.Fatal(err)
	}
	assertCopiedRoute(t, closed, copiedClosed)
	if copiedClosed.ScheduledDate != "2026-09-03" || copiedClosed.Report != nil || copiedClosed.Version != 1 {
		t.Fatalf("copy did not initialize independent lifecycle: %+v", copiedClosed)
	}
	if closed.Status != domain.PlanStatusClosed || closed.Report == nil || closed.Report.SeverityCounts[domain.SeverityMinor] != 1 {
		t.Fatalf("source plan changed during copy: %+v", closed)
	}
}

func assertCopiedRoute(t *testing.T, source, copied domain.InspectionPlan) {
	t.Helper()
	if copied.ID == source.ID || copied.Status != domain.PlanStatusActive || copied.Version != 1 || len(copied.Sites) != len(source.Sites) {
		t.Fatalf("unexpected copied plan: %+v", copied)
	}
	for i, sourceSite := range source.Sites {
		copiedSite := copied.Sites[i]
		if copiedSite.ID == sourceSite.ID || copiedSite.PlanID != copied.ID || copiedSite.Name != sourceSite.Name || copiedSite.Category != sourceSite.Category || copiedSite.Location != sourceSite.Location || copiedSite.Sequence != sourceSite.Sequence || len(copiedSite.Observations) != 0 {
			t.Fatalf("route definition was not copied independently: source=%+v copied=%+v", sourceSite, copiedSite)
		}
	}
}
