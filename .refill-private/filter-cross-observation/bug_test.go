package filtercrossobservation

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"community-inspection/internal/domain"
	"community-inspection/internal/service"
	"community-inspection/internal/store"
)

func TestPlanFilterRequiresOneObservationToMatchAllConditions(t *testing.T) {
	repository := store.NewFileStore(filepath.Join(t.TempDir(), "inspection.json"))
	if err := repository.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	svc, err := service.New(repository, service.Config{
		Now:   func() time.Time { return now },
		NewID: func(prefix string) string { sequence++; return prefix + "-" + string(rune('a'+sequence)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.CreatePlan(context.Background(), service.CreatePlanInput{
		Name: "组合筛选", Area: "东区", ScheduledDate: "2026-08-20",
		Sites: []service.CreateSiteInput{{Name: "路灯甲", Category: "照明", Location: "东门"}, {Name: "路灯乙", Category: "照明", Location: "西门"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	firstPlan, first, err := svc.AddObservation(context.Background(), plan.ID, service.ObservationInput{
		SiteID: plan.Sites[0].ID, Kind: "亮灯数", Value: "7/8", Observer: "巡检员", Severity: domain.SeverityMajor, ExpectedVersion: plan.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	reviewedFirst, err := svc.ReviewObservation(context.Background(), plan.ID, first.ID, service.ReviewInput{
		Decision: domain.ReviewApproved, Reviewer: "区域主管", Note: "已安排维修", ExpectedVersion: firstPlan.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, second, err := svc.AddObservation(context.Background(), plan.ID, service.ObservationInput{
		SiteID: plan.Sites[1].ID, Kind: "灯杆状态", Value: "轻微倾斜", Observer: "巡检员", Severity: domain.SeverityMinor, ExpectedVersion: reviewedFirst.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReviewObservation(context.Background(), plan.ID, second.ID, service.ReviewInput{
		Decision: domain.ReviewRejected, Reviewer: "区域主管", Note: "需要现场复查", ExpectedVersion: secondPlan.Version,
	}); err != nil {
		t.Fatal(err)
	}
	plans, err := svc.ListPlans(context.Background(), service.Filter{Severity: domain.SeverityMajor, ReviewStatus: domain.ReviewRejected})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 0 {
		t.Fatalf("TestPlanFilterRequiresOneObservationToMatchAllConditions: 不同观测分别满足条件时仍返回了计划：%+v", plans)
	}
}
