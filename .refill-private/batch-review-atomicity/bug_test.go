package batchreviewatomicity

import (
	"errors"
	"testing"
	"time"

	"community-inspection/internal/domain"
)

func TestBatchReviewFailureLeavesEveryObservationUnchanged(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	plan, err := domain.NewPlan("plan", "批量复核", "东区", "2026-08-20", []domain.InspectionSite{
		{ID: "site-a", Name: "路灯甲", Category: "照明", Location: "东门"},
		{ID: "site-b", Name: "路灯乙", Category: "照明", Location: "西门"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := domain.NewObservation("observation-a", "site-a", "亮灯数", "7/8", "盏", "一盏未亮", "巡检员", domain.SeverityMajor, now, "key-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := domain.NewObservation("observation-b", "site-b", "灯杆状态", "倾斜", "", "需要加固", "巡检员", domain.SeverityMinor, now, "key-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.AppendObservation(first); err != nil {
		t.Fatal(err)
	}
	if err := plan.AppendObservation(second); err != nil {
		t.Fatal(err)
	}
	version := plan.Version
	err = plan.ReviewObservations([]string{first.ID, "missing-observation"}, domain.ReviewApproved, "区域主管", "统一安排维修", now.Add(time.Hour))
	if !errors.Is(err, domain.ErrObservationNotFound) {
		t.Fatalf("预期缺失观测错误，实际为 %v", err)
	}
	stored := plan.Sites[0].Observations[0]
	if plan.Version != version || stored.ReviewStatus != domain.ReviewPending || stored.ReviewedAt != nil || stored.Reviewer != "" || stored.ReviewNote != "" || len(stored.ReviewHistory) != 0 {
		t.Fatalf("TestBatchReviewFailureLeavesEveryObservationUnchanged: 批量复核失败后首条观测已被修改：version=%d observation=%+v", plan.Version, stored)
	}
}
