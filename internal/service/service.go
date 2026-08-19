package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"community-inspection/internal/domain"
	"community-inspection/internal/store"
)

type Clock func() time.Time
type Identifier func(string) string

type Service struct {
	repo  store.Repository
	now   Clock
	newID Identifier
}

type Config struct {
	Now   Clock
	NewID Identifier
}

type CreatePlanInput struct {
	Name          string
	Area          string
	ScheduledDate string
	Sites         []CreateSiteInput
}

type CopyPlanInput struct {
	Name          string
	Area          string
	ScheduledDate string
}

type CreateSiteInput struct {
	Name     string
	Category string
	Location string
}

type ObservationInput struct {
	SiteID          string
	Kind            string
	Value           string
	Unit            string
	Note            string
	Observer        string
	Severity        domain.Severity
	ObservedAt      time.Time
	IdempotencyKey  string
	ExpectedVersion int64
}

type ReviewInput struct {
	Decision        domain.ReviewStatus
	Reviewer        string
	Note            string
	ExpectedVersion int64
}

type ReopenInput struct {
	Operator        string
	Note            string
	ExpectedVersion int64
}

type BatchReviewInput struct {
	ObservationIDs  []string
	Decision        domain.ReviewStatus
	Reviewer        string
	Note            string
	ExpectedVersion int64
}

type Filter struct {
	Date         string
	DateFrom     string
	DateTo       string
	Category     string
	Status       domain.PlanStatus
	Severity     domain.Severity
	ReviewStatus domain.ReviewStatus
	Query        string
}

type PlanSummary struct {
	TotalPlans         int `json:"totalPlans"`
	ActivePlans        int `json:"activePlans"`
	ClosedPlans        int `json:"closedPlans"`
	TotalSites         int `json:"totalSites"`
	CompletedSites     int `json:"completedSites"`
	ExceptionCount     int `json:"exceptionCount"`
	PendingReviewCount int `json:"pendingReviewCount"`
}

type ObservationSummary struct {
	Total        int                         `json:"total"`
	Severity     map[domain.Severity]int     `json:"severity"`
	ReviewStatus map[domain.ReviewStatus]int `json:"reviewStatus"`
}

type ObservationRecord struct {
	PlanID        string                `json:"planID"`
	PlanName      string                `json:"planName"`
	ScheduledDate string                `json:"scheduledDate"`
	Site          domain.InspectionSite `json:"site"`
	Observation   domain.Observation    `json:"observation"`
}

type ObservationExportRow struct {
	PlanID        string
	PlanName      string
	ScheduledDate string
	SiteID        string
	SiteName      string
	SiteCategory  string
	SiteLocation  string
	ObservationID string
	ObservedAt    string
	Kind          string
	Value         string
	Unit          string
	Note          string
	Observer      string
	Severity      string
	ReviewStatus  string
	Reviewer      string
	ReviewNote    string
	ReviewedAt    string
}

func New(repo store.Repository, config Config) (*Service, error) {
	if repo == nil {
		return nil, errors.New("巡检仓储不能为空")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	newID := config.NewID
	if newID == nil {
		newID = defaultIdentifier
	}
	return &Service{repo: repo, now: now, newID: newID}, nil
}

func (s *Service) CreatePlan(ctx context.Context, input CreatePlanInput) (domain.InspectionPlan, error) {
	sites := make([]domain.InspectionSite, len(input.Sites))
	for i, source := range input.Sites {
		sites[i] = domain.InspectionSite{
			ID: s.newID("site"), Name: source.Name, Category: source.Category, Location: source.Location,
			Sequence: i + 1,
		}
	}
	plan, err := domain.NewPlan(s.newID("plan"), input.Name, input.Area, input.ScheduledDate, sites, s.now())
	if err != nil {
		return domain.InspectionPlan{}, err
	}
	if err := s.repo.Create(ctx, plan); err != nil {
		return domain.InspectionPlan{}, err
	}
	return plan, nil
}

func (s *Service) CopyPlan(ctx context.Context, sourceID string, input CopyPlanInput) (domain.InspectionPlan, error) {
	if err := checkContext(ctx); err != nil {
		return domain.InspectionPlan{}, err
	}
	source, err := s.repo.Get(ctx, sourceID)
	if err != nil {
		return domain.InspectionPlan{}, err
	}
	siteIDs := make([]string, len(source.Sites))
	for i := range source.Sites {
		siteIDs[i] = s.newID("site")
	}
	plan, err := domain.NewPlanFromRoute(s.newID("plan"), input.Name, input.Area, input.ScheduledDate, source, siteIDs, s.now())
	if err != nil {
		return domain.InspectionPlan{}, err
	}
	if err := s.repo.Create(ctx, plan); err != nil {
		return domain.InspectionPlan{}, err
	}
	return plan, nil
}

func (s *Service) ListPlans(ctx context.Context, filter Filter) ([]domain.InspectionPlan, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	filter = normalizeFilter(filter)
	if err := filter.validate(); err != nil {
		return nil, err
	}
	snapshot, err := s.repo.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]domain.InspectionPlan, 0, len(snapshot.Plans))
	for _, plan := range snapshot.Plans {
		if filter.Date != "" && plan.ScheduledDate != filter.Date {
			continue
		}
		if filter.DateFrom != "" && plan.ScheduledDate < filter.DateFrom {
			continue
		}
		if filter.DateTo != "" && plan.ScheduledDate > filter.DateTo {
			continue
		}
		if filter.Status != "" && plan.Status != filter.Status {
			continue
		}
		if filter.Category != "" && !planHasCategory(plan, filter.Category) {
			continue
		}
		if filter.Query != "" && !planMatches(plan, filter.Query) {
			continue
		}
		if !planHasObservation(plan, filter) {
			continue
		}
		result = append(result, plan)
	}
	return result, nil
}

func (s *Service) GetPlan(ctx context.Context, id string) (domain.InspectionPlan, error) {
	if strings.TrimSpace(id) == "" {
		return domain.InspectionPlan{}, domain.ErrPlanNotFound
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) AddObservation(ctx context.Context, planID string, input ObservationInput) (domain.InspectionPlan, domain.Observation, error) {
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = s.newID("observation-key")
	}
	observedAt := input.ObservedAt
	if observedAt.IsZero() {
		observedAt = s.now()
	}
	observation, err := domain.NewObservation(s.newID("observation"), input.SiteID, input.Kind, input.Value, input.Unit, input.Note, input.Observer, input.Severity, observedAt, idempotencyKey)
	if err != nil {
		return domain.InspectionPlan{}, domain.Observation{}, err
	}
	var saved domain.Observation
	plan, err := s.repo.Update(ctx, planID, input.ExpectedVersion, func(plan *domain.InspectionPlan) error {
		if existing := findObservationByKey(*plan, idempotencyKey); existing != nil {
			if existing.SiteID == observation.SiteID && existing.Value == observation.Value && existing.Note == observation.Note {
				saved = *existing
				return nil
			}
			return fmt.Errorf("%w：幂等标识已用于其他观测", domain.ErrInvalidInput)
		}
		if err := plan.AppendObservation(observation); err != nil {
			return err
		}
		saved = observation
		return nil
	})
	if err != nil {
		return domain.InspectionPlan{}, domain.Observation{}, err
	}
	return plan, saved, nil
}

func (s *Service) ReviewObservation(ctx context.Context, planID, observationID string, input ReviewInput) (domain.InspectionPlan, error) {
	if err := checkContext(ctx); err != nil {
		return domain.InspectionPlan{}, err
	}
	return s.repo.Update(ctx, planID, input.ExpectedVersion, func(plan *domain.InspectionPlan) error {
		return plan.ReviewObservation(observationID, input.Decision, input.Reviewer, input.Note, s.now())
	})
}

func (s *Service) ReviewObservations(ctx context.Context, planID string, input BatchReviewInput) (domain.InspectionPlan, error) {
	if err := checkContext(ctx); err != nil {
		return domain.InspectionPlan{}, err
	}
	return s.repo.Update(ctx, planID, input.ExpectedVersion, func(plan *domain.InspectionPlan) error {
		return plan.ReviewObservations(input.ObservationIDs, input.Decision, input.Reviewer, input.Note, s.now())
	})
}

func (s *Service) ReopenObservation(ctx context.Context, planID, observationID string, input ReopenInput) (domain.InspectionPlan, error) {
	if err := checkContext(ctx); err != nil {
		return domain.InspectionPlan{}, err
	}
	return s.repo.Update(ctx, planID, input.ExpectedVersion, func(plan *domain.InspectionPlan) error {
		return plan.ReopenObservation(observationID, input.Operator, input.Note, s.now())
	})
}

func (s *Service) ClosePlan(ctx context.Context, planID string, expectedVersion int64) (domain.InspectionPlan, domain.InspectionReport, error) {
	if err := checkContext(ctx); err != nil {
		return domain.InspectionPlan{}, domain.InspectionReport{}, err
	}
	var report domain.InspectionReport
	plan, err := s.repo.Update(ctx, planID, expectedVersion, func(plan *domain.InspectionPlan) error {
		var closeErr error
		report, closeErr = plan.Close(s.now())
		return closeErr
	})
	if err != nil {
		return domain.InspectionPlan{}, domain.InspectionReport{}, err
	}
	return plan, report, nil
}

func (s *Service) GetReport(ctx context.Context, planID string) (domain.InspectionReport, error) {
	plan, err := s.GetPlan(ctx, planID)
	if err != nil {
		return domain.InspectionReport{}, err
	}
	if plan.Report == nil {
		return domain.InspectionReport{}, domain.ErrReportNotFound
	}
	return *plan.Report, nil
}

func (s *Service) ListObservations(ctx context.Context, filter Filter) ([]ObservationRecord, error) {
	filter = normalizeFilter(filter)
	if err := filter.validate(); err != nil {
		return nil, err
	}
	planFilter := filter
	planFilter.Category = ""
	plans, err := s.ListPlans(ctx, planFilter)
	if err != nil {
		return nil, err
	}
	result := make([]ObservationRecord, 0)
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	for _, plan := range plans {
		for _, site := range plan.Sites {
			if filter.Category != "" && strings.ToLower(site.Category) != strings.ToLower(strings.TrimSpace(filter.Category)) {
				continue
			}
			for _, observation := range site.Observations {
				if filter.Severity != "" && observation.Severity != filter.Severity {
					continue
				}
				if filter.ReviewStatus != "" && observation.ReviewStatus != filter.ReviewStatus {
					continue
				}
				if query != "" && !observationMatches(plan, site, observation, query) {
					continue
				}
				result = append(result, ObservationRecord{PlanID: plan.ID, PlanName: plan.Name, ScheduledDate: plan.ScheduledDate, Site: site, Observation: observation})
			}
		}
	}
	return result, nil
}

func (s *Service) ExportObservations(ctx context.Context, filter Filter) ([]ObservationExportRow, error) {
	observations, err := s.ListObservations(ctx, filter)
	if err != nil {
		return nil, err
	}
	rows := make([]ObservationExportRow, len(observations))
	for i, record := range observations {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		observation := record.Observation
		row := ObservationExportRow{
			PlanID:        record.PlanID,
			PlanName:      record.PlanName,
			ScheduledDate: record.ScheduledDate,
			SiteID:        record.Site.ID,
			SiteName:      record.Site.Name,
			SiteCategory:  record.Site.Category,
			SiteLocation:  record.Site.Location,
			ObservationID: observation.ID,
			ObservedAt:    observation.ObservedAt.Format(time.RFC3339Nano),
			Kind:          observation.Kind,
			Value:         observation.Value,
			Unit:          observation.Unit,
			Note:          observation.Note,
			Observer:      observation.Observer,
			Severity:      string(observation.Severity),
			ReviewStatus:  string(observation.ReviewStatus),
			Reviewer:      observation.Reviewer,
			ReviewNote:    observation.ReviewNote,
		}
		if observation.ReviewedAt != nil {
			row.ReviewedAt = observation.ReviewedAt.Format(time.RFC3339Nano)
		}
		rows[i] = row
	}
	return rows, nil
}

func SummarizeObservations(rows []ObservationRecord) ObservationSummary {
	severityCounts := map[domain.Severity]int{
		domain.SeverityNormal: 0, domain.SeverityMinor: 0,
		domain.SeverityMajor: 0, domain.SeverityCritical: 0,
	}
	reviewStatusCounts := map[domain.ReviewStatus]int{
		domain.ReviewNotRequired: 0, domain.ReviewPending: 0,
		domain.ReviewApproved: 0, domain.ReviewRejected: 0,
	}
	for _, row := range rows {
		severity := row.Observation.Severity
		reviewStatus := row.Observation.ReviewStatus
		severityCounts[severity]++
		reviewStatusCounts[reviewStatus]++
	}
	return ObservationSummary{Total: len(rows), Severity: severityCounts, ReviewStatus: reviewStatusCounts}
}

func SummarizePlans(plans []domain.InspectionPlan) PlanSummary {
	summary := PlanSummary{TotalPlans: len(plans)}
	for _, plan := range plans {
		switch plan.Status {
		case domain.PlanStatusActive:
			summary.ActivePlans++
		case domain.PlanStatusClosed:
			summary.ClosedPlans++
		}
		summary.TotalSites += len(plan.Sites)
		summary.CompletedSites += plan.CompletedSiteCount()
		summary.ExceptionCount += plan.ExceptionCount()
		summary.PendingReviewCount += plan.PendingReviewCount()
	}
	return summary
}

func normalizeFilter(filter Filter) Filter {
	filter.Date = strings.TrimSpace(filter.Date)
	filter.DateFrom = strings.TrimSpace(filter.DateFrom)
	filter.DateTo = strings.TrimSpace(filter.DateTo)
	filter.Category = strings.TrimSpace(filter.Category)
	filter.Severity = domain.Severity(strings.TrimSpace(string(filter.Severity)))
	filter.ReviewStatus = domain.ReviewStatus(strings.TrimSpace(string(filter.ReviewStatus)))
	filter.Query = strings.ToLower(strings.TrimSpace(filter.Query))
	return filter
}

func (f Filter) validate() error {
	for label, value := range map[string]string{"日期": f.Date, "起始日期": f.DateFrom, "结束日期": f.DateTo} {
		if value == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return fmt.Errorf("%w：%s格式应为 YYYY-MM-DD", domain.ErrInvalidInput, label)
		}
	}
	if f.DateFrom != "" && f.DateTo != "" && f.DateFrom > f.DateTo {
		return fmt.Errorf("%w：起始日期不能晚于结束日期", domain.ErrInvalidInput)
	}
	if f.Severity != "" && !f.Severity.IsValid() {
		return fmt.Errorf("%w：异常等级筛选值无效", domain.ErrInvalidInput)
	}
	if f.ReviewStatus != "" && !f.ReviewStatus.IsValid() {
		return fmt.Errorf("%w：复核状态筛选值无效", domain.ErrInvalidInput)
	}
	return nil
}

func planHasObservation(plan domain.InspectionPlan, filter Filter) bool {
	for _, site := range plan.Sites {
		for _, observation := range site.Observations {
			severityMatched := filter.Severity == "" || observation.Severity == filter.Severity
			reviewMatched := filter.ReviewStatus == "" || observation.ReviewStatus == filter.ReviewStatus
			if severityMatched && reviewMatched {
				return true
			}
		}
	}
	return false
}

func observationMatches(plan domain.InspectionPlan, site domain.InspectionSite, observation domain.Observation, query string) bool {
	values := []string{plan.Name, plan.Area, site.Name, site.Category, site.Location, observation.Kind, observation.Value, observation.Note, observation.Observer}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func findObservationByKey(plan domain.InspectionPlan, key string) *domain.Observation {
	for _, site := range plan.Sites {
		for i := range site.Observations {
			if site.Observations[i].IdempotencyKey == key {
				observation := site.Observations[i]
				return &observation
			}
		}
	}
	return nil
}

func planHasCategory(plan domain.InspectionPlan, category string) bool {
	category = strings.ToLower(strings.TrimSpace(category))
	for _, site := range plan.Sites {
		if strings.ToLower(site.Category) == category {
			return true
		}
	}
	return false
}

func planMatches(plan domain.InspectionPlan, query string) bool {
	values := []string{plan.Name, plan.Area, plan.ID}
	for _, site := range plan.Sites {
		values = append(values, site.Name, site.Category, site.Location)
		for _, observation := range site.Observations {
			values = append(values, observation.Kind, observation.Value, observation.Note, observation.Observer)
		}
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func defaultIdentifier(prefix string) string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(bytes)
}
