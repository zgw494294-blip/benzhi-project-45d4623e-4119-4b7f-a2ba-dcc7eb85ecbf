package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type PlanStatus string

const (
	PlanStatusActive PlanStatus = "active"
	PlanStatusClosed PlanStatus = "closed"
)

type SiteStatus string

const (
	SiteStatusPending    SiteStatus = "pending"
	SiteStatusInProgress SiteStatus = "in_progress"
	SiteStatusCompleted  SiteStatus = "completed"
)

type Severity string

const (
	SeverityNormal   Severity = "normal"
	SeverityMinor    Severity = "minor"
	SeverityMajor    Severity = "major"
	SeverityCritical Severity = "critical"
)

type ReviewStatus string

const (
	ReviewNotRequired ReviewStatus = "not_required"
	ReviewPending     ReviewStatus = "pending"
	ReviewApproved    ReviewStatus = "approved"
	ReviewRejected    ReviewStatus = "rejected"
)

type ReviewEventType string

const (
	ReviewEventApproved ReviewEventType = "approved"
	ReviewEventRejected ReviewEventType = "rejected"
	ReviewEventReopened ReviewEventType = "reopened"
)

func (t ReviewEventType) IsValid() bool {
	switch t {
	case ReviewEventApproved, ReviewEventRejected, ReviewEventReopened:
		return true
	default:
		return false
	}
}

func (s Severity) IsValid() bool {
	switch s {
	case SeverityNormal, SeverityMinor, SeverityMajor, SeverityCritical:
		return true
	default:
		return false
	}
}

func (s ReviewStatus) IsValid() bool {
	switch s {
	case ReviewNotRequired, ReviewPending, ReviewApproved, ReviewRejected:
		return true
	default:
		return false
	}
}

type InspectionPlan struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Area          string            `json:"area"`
	ScheduledDate string            `json:"scheduledDate"`
	Status        PlanStatus        `json:"status"`
	Version       int64             `json:"version"`
	CreatedAt     time.Time         `json:"createdAt"`
	ClosedAt      *time.Time        `json:"closedAt,omitempty"`
	SiteIDs       []string          `json:"siteIDs"`
	Sites         []InspectionSite  `json:"sites"`
	Report        *InspectionReport `json:"report,omitempty"`
}

type InspectionSite struct {
	ID           string        `json:"id"`
	PlanID       string        `json:"planID"`
	Name         string        `json:"name"`
	Category     string        `json:"category"`
	Location     string        `json:"location"`
	Sequence     int           `json:"sequence"`
	Status       SiteStatus    `json:"status"`
	Observations []Observation `json:"observations"`
}

type Observation struct {
	ID             string        `json:"id"`
	SiteID         string        `json:"siteID"`
	Kind           string        `json:"kind"`
	Value          string        `json:"value"`
	Unit           string        `json:"unit"`
	Note           string        `json:"note"`
	ObservedAt     time.Time     `json:"observedAt"`
	Observer       string        `json:"observer"`
	Severity       Severity      `json:"severity"`
	ReviewStatus   ReviewStatus  `json:"reviewStatus"`
	ReviewedAt     *time.Time    `json:"reviewedAt,omitempty"`
	Reviewer       string        `json:"reviewer,omitempty"`
	ReviewNote     string        `json:"reviewNote,omitempty"`
	ReviewHistory  []ReviewEvent `json:"reviewHistory"`
	IdempotencyKey string        `json:"idempotencyKey,omitempty"`
}

type ReviewEvent struct {
	Event    ReviewEventType `json:"event"`
	At       time.Time       `json:"at"`
	Operator string          `json:"operator"`
	Note     string          `json:"note"`
}

type InspectionReport struct {
	PlanID         string               `json:"planID"`
	GeneratedAt    time.Time            `json:"generatedAt"`
	Summary        string               `json:"summary"`
	SiteResults    []SiteResult         `json:"siteResults"`
	ExceptionCount int                  `json:"exceptionCount"`
	SeverityCounts map[Severity]int     `json:"severityCounts,omitempty"`
	ReviewCounts   map[ReviewStatus]int `json:"reviewCounts,omitempty"`
	Checksum       string               `json:"checksum"`
}

type SiteResult struct {
	SiteID      string       `json:"siteID"`
	Name        string       `json:"name"`
	Category    string       `json:"category"`
	Location    string       `json:"location"`
	Status      SiteStatus   `json:"status"`
	Observation *Observation `json:"observation,omitempty"`
}

var (
	ErrInvalidInput        = errors.New("输入不符合要求")
	ErrPlanNotFound        = errors.New("巡检计划不存在")
	ErrSiteNotFound        = errors.New("巡检点位不存在")
	ErrObservationNotFound = errors.New("观测记录不存在")
	ErrPlanClosed          = errors.New("巡检计划已经关闭")
	ErrVersionConflict     = errors.New("计划版本已变化，请刷新后重试")
	ErrOutOfOrder          = errors.New("请按点位顺序完成巡检")
	ErrNotReadyToClose     = errors.New("所有点位完成且异常复核后才能关闭")
	ErrAlreadyReviewed     = errors.New("该异常已经完成复核")
	ErrReportNotFound      = errors.New("巡检报告不存在")
	ErrSnapshotInvalid     = errors.New("本地数据快照无效")
)

func NewPlan(id, name, area, scheduledDate string, sites []InspectionSite, createdAt time.Time) (InspectionPlan, error) {
	name = strings.TrimSpace(name)
	area = strings.TrimSpace(area)
	scheduledDate = strings.TrimSpace(scheduledDate)
	if id == "" || name == "" || area == "" || scheduledDate == "" {
		return InspectionPlan{}, fmt.Errorf("%w：计划信息不能为空", ErrInvalidInput)
	}
	if len([]rune(name)) > 80 || len([]rune(area)) > 80 {
		return InspectionPlan{}, fmt.Errorf("%w：计划名称和区域不能超过 80 个字符", ErrInvalidInput)
	}
	if _, err := time.Parse("2006-01-02", scheduledDate); err != nil {
		return InspectionPlan{}, fmt.Errorf("%w：计划日期格式应为 YYYY-MM-DD", ErrInvalidInput)
	}
	if len(sites) == 0 || len(sites) > 100 {
		return InspectionPlan{}, fmt.Errorf("%w：点位数量应为 1 到 100", ErrInvalidInput)
	}
	plan := InspectionPlan{
		ID: id, Name: name, Area: area, ScheduledDate: scheduledDate,
		Status: PlanStatusActive, Version: 1, CreatedAt: createdAt.UTC(),
		SiteIDs: make([]string, 0, len(sites)), Sites: make([]InspectionSite, len(sites)),
	}
	seenSiteLocations := make(map[string]struct{}, len(sites))
	for i, source := range sites {
		site, err := normalizeSite(source, id, i+1)
		if err != nil {
			return InspectionPlan{}, err
		}
		locationKey := strings.ToLower(site.Name) + "\x00" + strings.ToLower(site.Location)
		if _, exists := seenSiteLocations[locationKey]; exists {
			return InspectionPlan{}, fmt.Errorf("%w：同一计划内点位名称和位置不能重复", ErrInvalidInput)
		}
		seenSiteLocations[locationKey] = struct{}{}
		for _, existing := range plan.SiteIDs {
			if existing == site.ID {
				return InspectionPlan{}, fmt.Errorf("%w：点位标识重复", ErrInvalidInput)
			}
		}
		plan.SiteIDs = append(plan.SiteIDs, site.ID)
		plan.Sites[i] = site
	}
	plan.Sites[0].Status = SiteStatusInProgress
	return plan, nil
}

func NewPlanFromRoute(id, name, area, scheduledDate string, source InspectionPlan, siteIDs []string, createdAt time.Time) (InspectionPlan, error) {
	if source.Status != PlanStatusActive && source.Status != PlanStatusClosed {
		return InspectionPlan{}, fmt.Errorf("%w：源计划状态无效", ErrInvalidInput)
	}
	if len(siteIDs) != len(source.Sites) {
		return InspectionPlan{}, fmt.Errorf("%w：复制点位标识数量无效", ErrInvalidInput)
	}
	sites := make([]InspectionSite, len(source.Sites))
	for i, sourceSite := range source.Sites {
		sites[i] = InspectionSite{
			ID: siteIDs[i], Name: sourceSite.Name, Category: sourceSite.Category, Location: sourceSite.Location,
			Sequence: i + 1,
		}
	}
	return NewPlan(id, name, area, scheduledDate, sites, createdAt)
}

func normalizeSite(source InspectionSite, planID string, sequence int) (InspectionSite, error) {
	name := strings.TrimSpace(source.Name)
	category := strings.TrimSpace(source.Category)
	location := strings.TrimSpace(source.Location)
	if source.ID == "" || name == "" || category == "" || location == "" {
		return InspectionSite{}, fmt.Errorf("%w：点位信息不能为空", ErrInvalidInput)
	}
	if len([]rune(name)) > 80 || len([]rune(category)) > 40 || len([]rune(location)) > 120 {
		return InspectionSite{}, fmt.Errorf("%w：点位文本超过长度限制", ErrInvalidInput)
	}
	return InspectionSite{ID: source.ID, PlanID: planID, Name: name, Category: category, Location: location, Sequence: sequence, Status: SiteStatusPending, Observations: []Observation{}}, nil
}

func (p *InspectionPlan) AppendObservation(observation Observation) error {
	if p == nil {
		return fmt.Errorf("%w：计划为空", ErrInvalidInput)
	}
	if p.Status == PlanStatusClosed {
		return ErrPlanClosed
	}
	siteIndex := -1
	for i := range p.Sites {
		if p.Sites[i].ID == observation.SiteID {
			siteIndex = i
			break
		}
	}
	if siteIndex < 0 {
		return ErrSiteNotFound
	}
	if err := validateObservation(observation); err != nil {
		return err
	}
	for i := range p.Sites {
		for _, prior := range p.Sites[i].Observations {
			if observation.ID == prior.ID {
				return fmt.Errorf("%w：观测标识重复", ErrInvalidInput)
			}
			if observation.IdempotencyKey != "" && prior.IdempotencyKey == observation.IdempotencyKey {
				if prior.SiteID == observation.SiteID && prior.Value == observation.Value && prior.Note == observation.Note {
					return nil
				}
				return fmt.Errorf("%w：幂等标识已用于其他观测", ErrInvalidInput)
			}
		}
	}
	for i := 0; i < siteIndex; i++ {
		if p.Sites[i].Status != SiteStatusCompleted {
			return ErrOutOfOrder
		}
	}
	site := &p.Sites[siteIndex]
	if site.Status == SiteStatusCompleted || len(site.Observations) > 0 {
		return fmt.Errorf("%w：该点位已有观测", ErrInvalidInput)
	}
	if siteIndex > 0 && p.Sites[siteIndex-1].Status != SiteStatusCompleted {
		return ErrOutOfOrder
	}
	site.Status = SiteStatusCompleted
	site.Observations = append(site.Observations, observation)
	if siteIndex+1 < len(p.Sites) {
		p.Sites[siteIndex+1].Status = SiteStatusInProgress
	}
	p.Version++
	return nil
}

func (p *InspectionPlan) ReviewObservation(observationID string, decision ReviewStatus, reviewer, note string, reviewedAt time.Time) error {
	if p == nil {
		return fmt.Errorf("%w：计划为空", ErrInvalidInput)
	}
	if p.Status == PlanStatusClosed {
		return ErrPlanClosed
	}
	var err error
	reviewer, note, err = normalizeReviewInput(decision, reviewer, note)
	if err != nil {
		return err
	}
	for siteIndex := range p.Sites {
		for observationIndex := range p.Sites[siteIndex].Observations {
			observation := &p.Sites[siteIndex].Observations[observationIndex]
			if observation.ID != observationID {
				continue
			}
			if observation.ReviewStatus != ReviewPending {
				return ErrAlreadyReviewed
			}
			at := reviewedAt.UTC()
			observation.ReviewStatus = decision
			observation.Reviewer = reviewer
			observation.ReviewNote = note
			observation.ReviewedAt = &at
			observation.ReviewHistory = append(observation.ReviewHistory, ReviewEvent{Event: ReviewEventType(decision), At: at, Operator: reviewer, Note: note})
			p.Version++
			return nil
		}
	}
	return ErrObservationNotFound
}

func (p *InspectionPlan) ReviewObservations(observationIDs []string, decision ReviewStatus, reviewer, note string, reviewedAt time.Time) error {
	if p == nil {
		return fmt.Errorf("%w：计划为空", ErrInvalidInput)
	}
	if p.Status == PlanStatusClosed {
		return ErrPlanClosed
	}
	reviewer, note, err := normalizeReviewInput(decision, reviewer, note)
	if err != nil {
		return err
	}
	if len(observationIDs) == 0 {
		return fmt.Errorf("%w：至少选择一条待复核观测", ErrInvalidInput)
	}

	seen := make(map[string]struct{}, len(observationIDs))
	reviewedAt = reviewedAt.UTC()
	for _, observationID := range observationIDs {
		observationID = strings.TrimSpace(observationID)
		if observationID == "" {
			return fmt.Errorf("%w：观测记录标识不能为空", ErrInvalidInput)
		}
		if _, exists := seen[observationID]; exists {
			return fmt.Errorf("%w：观测记录标识不能重复", ErrInvalidInput)
		}
		seen[observationID] = struct{}{}

		found := false
		for siteIndex := range p.Sites {
			for observationIndex := range p.Sites[siteIndex].Observations {
				observation := &p.Sites[siteIndex].Observations[observationIndex]
				if observation.ID != observationID {
					continue
				}
				if observation.ReviewStatus != ReviewPending {
					return ErrAlreadyReviewed
				}
				at := reviewedAt
				observation.ReviewStatus = decision
				observation.Reviewer = reviewer
				observation.ReviewNote = note
				observation.ReviewedAt = &at
				observation.ReviewHistory = append(observation.ReviewHistory, ReviewEvent{Event: ReviewEventType(decision), At: at, Operator: reviewer, Note: note})
				found = true
				break
			}
			if found {
				break
			}
		}
		if !found {
			return ErrObservationNotFound
		}
	}
	p.Version++
	return nil
}

func (p *InspectionPlan) ReopenObservation(observationID, operator, note string, reopenedAt time.Time) error {
	if p == nil {
		return fmt.Errorf("%w：计划为空", ErrInvalidInput)
	}
	if p.Status == PlanStatusClosed {
		return ErrPlanClosed
	}
	operator, note, err := normalizeReopenInput(operator, note)
	if err != nil {
		return err
	}
	for siteIndex := range p.Sites {
		for observationIndex := range p.Sites[siteIndex].Observations {
			observation := &p.Sites[siteIndex].Observations[observationIndex]
			if observation.ID != observationID {
				continue
			}
			if observation.ReviewStatus != ReviewRejected {
				return ErrAlreadyReviewed
			}
			at := reopenedAt.UTC()
			observation.ReviewStatus = ReviewPending
			observation.ReviewedAt = nil
			observation.Reviewer = ""
			observation.ReviewNote = ""
			observation.ReviewHistory = append(observation.ReviewHistory, ReviewEvent{Event: ReviewEventReopened, At: at, Operator: operator, Note: note})
			p.Version++
			return nil
		}
	}
	return ErrObservationNotFound
}

func (p *InspectionPlan) Close(now time.Time) (InspectionReport, error) {
	if p == nil {
		return InspectionReport{}, fmt.Errorf("%w：计划为空", ErrInvalidInput)
	}
	if p.Status == PlanStatusClosed && p.Report != nil {
		return cloneReport(*p.Report), nil
	}
	if p.Status == PlanStatusClosed {
		return InspectionReport{}, ErrPlanClosed
	}
	for _, site := range p.Sites {
		if site.Status != SiteStatusCompleted || len(site.Observations) == 0 {
			return InspectionReport{}, ErrNotReadyToClose
		}
		for _, observation := range site.Observations {
			if observation.ReviewStatus == ReviewPending {
				return InspectionReport{}, ErrNotReadyToClose
			}
		}
	}
	results := make([]SiteResult, 0, len(p.Sites))
	exceptionCount := 0
	for _, site := range p.Sites {
		result := SiteResult{SiteID: site.ID, Name: site.Name, Category: site.Category, Location: site.Location, Status: site.Status}
		if len(site.Observations) > 0 {
			observation := site.Observations[len(site.Observations)-1]
			result.Observation = &observation
			if observation.Severity != SeverityNormal {
				exceptionCount++
			}
		}
		results = append(results, result)
	}
	report := InspectionReport{
		PlanID:         p.ID,
		GeneratedAt:    now.UTC(),
		Summary:        fmt.Sprintf("共完成 %d 个点位，发现 %d 项异常。", len(p.Sites), exceptionCount),
		SiteResults:    results,
		ExceptionCount: exceptionCount,
		SeverityCounts: newSeverityCounts(),
		ReviewCounts:   newReviewCounts(),
	}
	for _, site := range p.Sites {
		for _, observation := range site.Observations {
			report.SeverityCounts[observation.Severity]++
			report.ReviewCounts[observation.ReviewStatus]++
		}
	}
	report.Checksum = checksumReport(report)
	closedAt := now.UTC()
	p.ClosedAt = &closedAt
	p.Status = PlanStatusClosed
	p.Report = &report
	p.Version++
	return cloneReport(report), nil
}

func validateObservation(observation Observation) error {
	if observation.ID == "" || observation.SiteID == "" || strings.TrimSpace(observation.Kind) == "" || strings.TrimSpace(observation.Value) == "" || strings.TrimSpace(observation.Observer) == "" {
		return fmt.Errorf("%w：观测类型、数值和巡检员不能为空", ErrInvalidInput)
	}
	if len([]rune(observation.Kind)) > 60 || len([]rune(observation.Value)) > 100 || len([]rune(observation.Unit)) > 20 || len([]rune(observation.Note)) > 300 || len([]rune(observation.Observer)) > 40 {
		return fmt.Errorf("%w：观测内容超过长度限制", ErrInvalidInput)
	}
	if observation.ObservedAt.IsZero() {
		return fmt.Errorf("%w：观测时间不能为空", ErrInvalidInput)
	}
	switch observation.Severity {
	case SeverityNormal:
		if observation.ReviewStatus != ReviewNotRequired {
			return fmt.Errorf("%w：正常观测无需复核", ErrInvalidInput)
		}
	case SeverityMinor, SeverityMajor, SeverityCritical:
		if observation.ReviewStatus != ReviewPending && observation.ReviewStatus != ReviewApproved && observation.ReviewStatus != ReviewRejected {
			return fmt.Errorf("%w：异常观测必须进入待复核", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w：异常等级无效", ErrInvalidInput)
	}
	for _, event := range observation.ReviewHistory {
		if !event.Event.IsValid() || event.At.IsZero() || strings.TrimSpace(event.Operator) == "" || len([]rune(event.Operator)) > 40 || len([]rune(event.Note)) > 300 {
			return fmt.Errorf("%w：复核历史无效", ErrInvalidInput)
		}
	}
	return nil
}

func normalizeReviewInput(decision ReviewStatus, reviewer, note string) (string, string, error) {
	if decision != ReviewApproved && decision != ReviewRejected {
		return "", "", fmt.Errorf("%w：复核结论无效", ErrInvalidInput)
	}
	reviewer = strings.TrimSpace(reviewer)
	note = strings.TrimSpace(note)
	if reviewer == "" || len([]rune(reviewer)) > 40 || len([]rune(note)) > 300 {
		return "", "", fmt.Errorf("%w：复核人不能为空且备注不能超过 300 个字符", ErrInvalidInput)
	}
	return reviewer, note, nil
}

func normalizeReopenInput(operator, note string) (string, string, error) {
	operator = strings.TrimSpace(operator)
	note = strings.TrimSpace(note)
	if operator == "" || len([]rune(operator)) > 40 || note == "" || len([]rune(note)) > 300 {
		return "", "", fmt.Errorf("%w：整改人不能为空且整改说明应为 1 到 300 个字符", ErrInvalidInput)
	}
	return operator, note, nil
}

func checksumReport(report InspectionReport) string {
	copyReport := report
	copyReport.Checksum = ""
	data, _ := json.Marshal(copyReport)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func newSeverityCounts() map[Severity]int {
	return map[Severity]int{
		SeverityNormal: 0, SeverityMinor: 0, SeverityMajor: 0, SeverityCritical: 0,
	}
}

func newReviewCounts() map[ReviewStatus]int {
	return map[ReviewStatus]int{
		ReviewNotRequired: 0, ReviewPending: 0, ReviewApproved: 0, ReviewRejected: 0,
	}
}

func validateReportCounts(report InspectionReport) error {
	severityCounts := newSeverityCounts()
	if len(report.SeverityCounts) != len(severityCounts) {
		return fmt.Errorf("%w：报告异常等级统计键不完整", ErrSnapshotInvalid)
	}
	for severity := range severityCounts {
		count, exists := report.SeverityCounts[severity]
		if !exists || count < 0 {
			return fmt.Errorf("%w：报告异常等级统计值无效", ErrSnapshotInvalid)
		}
	}
	reviewCounts := newReviewCounts()
	if len(report.ReviewCounts) != len(reviewCounts) {
		return fmt.Errorf("%w：报告复核统计键不完整", ErrSnapshotInvalid)
	}
	for status := range reviewCounts {
		count, exists := report.ReviewCounts[status]
		if !exists || count < 0 {
			return fmt.Errorf("%w：报告复核统计值无效", ErrSnapshotInvalid)
		}
	}
	return nil
}

func (p InspectionPlan) Validate() error {
	if p.ID == "" || p.Name == "" || p.Area == "" || p.ScheduledDate == "" || p.Version < 1 || p.CreatedAt.IsZero() {
		return fmt.Errorf("%w：计划快照缺少必填字段", ErrSnapshotInvalid)
	}
	if p.Status != PlanStatusActive && p.Status != PlanStatusClosed {
		return fmt.Errorf("%w：计划状态无效", ErrSnapshotInvalid)
	}
	if len(p.SiteIDs) != len(p.Sites) || len(p.Sites) == 0 {
		return fmt.Errorf("%w：计划点位集合无效", ErrSnapshotInvalid)
	}
	seen := make(map[string]bool, len(p.Sites))
	seenObservations := make(map[string]bool)
	ordered := append([]InspectionSite(nil), p.Sites...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Sequence < ordered[j].Sequence })
	for i, site := range ordered {
		if site.ID == "" || site.PlanID != p.ID || site.Sequence != i+1 || seen[site.ID] {
			return fmt.Errorf("%w：点位顺序或归属无效", ErrSnapshotInvalid)
		}
		seen[site.ID] = true
		if site.Status != SiteStatusPending && site.Status != SiteStatusInProgress && site.Status != SiteStatusCompleted {
			return fmt.Errorf("%w：点位状态无效", ErrSnapshotInvalid)
		}
		for _, observation := range site.Observations {
			if observation.SiteID != site.ID {
				return fmt.Errorf("%w：观测记录归属无效", ErrSnapshotInvalid)
			}
			if observation.ID == "" || seenObservations[observation.ID] {
				return fmt.Errorf("%w：观测标识重复", ErrSnapshotInvalid)
			}
			seenObservations[observation.ID] = true
			if err := validateObservation(observation); err != nil {
				return err
			}
			if observation.ReviewStatus == ReviewPending && p.Status == PlanStatusClosed {
				return fmt.Errorf("%w：已关闭计划不能保留待复核异常", ErrSnapshotInvalid)
			}
		}
	}
	for _, id := range p.SiteIDs {
		if !seen[id] {
			return fmt.Errorf("%w：计划点位索引无效", ErrSnapshotInvalid)
		}
	}
	if p.Status == PlanStatusClosed && (p.ClosedAt == nil || p.Report == nil) {
		return fmt.Errorf("%w：已关闭计划必须带有报告", ErrSnapshotInvalid)
	}
	if p.Report != nil && p.Report.PlanID != p.ID {
		return fmt.Errorf("%w：报告归属无效", ErrSnapshotInvalid)
	}
	if p.Report != nil && (p.Report.SeverityCounts != nil || p.Report.ReviewCounts != nil) {
		if err := validateReportCounts(*p.Report); err != nil {
			return err
		}
	}
	if p.Report != nil && p.Report.Checksum != checksumReport(*p.Report) {
		return fmt.Errorf("%w：报告校验值无效", ErrSnapshotInvalid)
	}
	return nil
}

func cloneReport(report InspectionReport) InspectionReport {
	copyReport := report
	copyReport.SiteResults = append([]SiteResult(nil), report.SiteResults...)
	copyReport.SeverityCounts = cloneSeverityCounts(report.SeverityCounts)
	copyReport.ReviewCounts = cloneReviewCounts(report.ReviewCounts)
	for i := range copyReport.SiteResults {
		if report.SiteResults[i].Observation != nil {
			observation := cloneObservation(*report.SiteResults[i].Observation)
			copyReport.SiteResults[i].Observation = &observation
		}
	}
	return copyReport
}

func cloneSeverityCounts(counts map[Severity]int) map[Severity]int {
	if counts == nil {
		return nil
	}
	clone := make(map[Severity]int, len(counts))
	for severity, count := range counts {
		clone[severity] = count
	}
	return clone
}

func cloneReviewCounts(counts map[ReviewStatus]int) map[ReviewStatus]int {
	if counts == nil {
		return nil
	}
	clone := make(map[ReviewStatus]int, len(counts))
	for status, count := range counts {
		clone[status] = count
	}
	return clone
}

func cloneObservation(observation Observation) Observation {
	if observation.ReviewHistory != nil {
		observation.ReviewHistory = append([]ReviewEvent{}, observation.ReviewHistory...)
	}
	return observation
}
