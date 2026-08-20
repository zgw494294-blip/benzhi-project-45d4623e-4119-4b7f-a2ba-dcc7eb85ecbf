package duplicateobservationid

import (
	"errors"
	"testing"
	"time"

	"community-inspection/internal/domain"
)

func TestObservationIDsMustBeUniqueAcrossPlan(t *testing.T) {
	plan, err := domain.NewPlan("plan", "巡检", "东区", "2026-08-20", []domain.InspectionSite{{ID: "site-a", Name: "路灯A", Category: "照明", Location: "东门"}, {ID: "site-b", Name: "路灯B", Category: "照明", Location: "西门"}}, time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	first, err := domain.NewObservation("same-id", "site-a", "外观", "正常", "", "", "甲", domain.SeverityNormal, time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC), "key-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := domain.NewObservation("same-id", "site-b", "外观", "正常", "", "", "甲", domain.SeverityNormal, time.Date(2026, 8, 20, 9, 1, 0, 0, time.UTC), "key-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.AppendObservation(first); err != nil {
		t.Fatal(err)
	}
	if err := plan.AppendObservation(second); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("TestObservationIDsMustBeUniqueAcrossPlan: expected duplicate observation ID rejection, got %v", err)
	}
}
