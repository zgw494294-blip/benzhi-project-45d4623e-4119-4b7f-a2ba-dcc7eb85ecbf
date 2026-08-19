package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"community-inspection/internal/domain"
)

const currentSchemaVersion = 1

type Snapshot struct {
	SchemaVersion int                     `json:"schemaVersion"`
	SavedAt       time.Time               `json:"savedAt"`
	Plans         []domain.InspectionPlan `json:"plans"`
}

type Repository interface {
	Load(context.Context) error
	Snapshot(context.Context) (Snapshot, error)
	Create(context.Context, domain.InspectionPlan) error
	Update(context.Context, string, int64, func(*domain.InspectionPlan) error) (domain.InspectionPlan, error)
	Get(context.Context, string) (domain.InspectionPlan, error)
}

type FileStore struct {
	mu       sync.RWMutex
	path     string
	now      func() time.Time
	loaded   bool
	snapshot Snapshot
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path, now: time.Now}
}

func (s *FileStore) Load(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.mu.Lock()
		s.snapshot = Snapshot{SchemaVersion: currentSchemaVersion, SavedAt: s.now().UTC(), Plans: []domain.InspectionPlan{}}
		s.loaded = true
		s.mu.Unlock()
		return nil
	}
	if err != nil {
		return fmt.Errorf("打开数据快照失败：%w", err)
	}
	defer file.Close()
	var snapshot Snapshot
	decoder := json.NewDecoder(io.LimitReader(file, 16<<20))
	if err := decoder.Decode(&snapshot); err != nil {
		return fmt.Errorf("解析数据快照失败：%w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%w：快照包含多余内容", domain.ErrSnapshotInvalid)
	}
	if snapshot.SchemaVersion != currentSchemaVersion || snapshot.Plans == nil {
		return fmt.Errorf("%w：schemaVersion 不受支持", domain.ErrSnapshotInvalid)
	}
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	s.mu.Lock()
	s.snapshot = cloneSnapshot(snapshot)
	s.loaded = true
	s.mu.Unlock()
	return nil
}

func (s *FileStore) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := contextErr(ctx); err != nil {
		return Snapshot{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.loaded {
		return Snapshot{}, fmt.Errorf("%w：数据快照尚未加载", domain.ErrSnapshotInvalid)
	}
	return cloneSnapshot(s.snapshot), nil
}

func (s *FileStore) Create(ctx context.Context, plan domain.InspectionPlan) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		return fmt.Errorf("%w：数据快照尚未加载", domain.ErrSnapshotInvalid)
	}
	for _, existing := range s.snapshot.Plans {
		if existing.ID == plan.ID {
			return fmt.Errorf("%w：计划标识已存在", domain.ErrInvalidInput)
		}
	}
	next := cloneSnapshot(s.snapshot)
	next.Plans = append(next.Plans, clonePlan(plan))
	if err := s.persistLocked(ctx, next); err != nil {
		return err
	}
	s.snapshot = next
	return nil
}

func (s *FileStore) Update(ctx context.Context, id string, expectedVersion int64, mutate func(*domain.InspectionPlan) error) (domain.InspectionPlan, error) {
	if err := contextErr(ctx); err != nil {
		return domain.InspectionPlan{}, err
	}
	if mutate == nil {
		return domain.InspectionPlan{}, fmt.Errorf("%w：更新操作不能为空", domain.ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		return domain.InspectionPlan{}, fmt.Errorf("%w：数据快照尚未加载", domain.ErrSnapshotInvalid)
	}
	index := -1
	for i := range s.snapshot.Plans {
		if s.snapshot.Plans[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return domain.InspectionPlan{}, domain.ErrPlanNotFound
	}
	current := clonePlan(s.snapshot.Plans[index])
	if expectedVersion > 0 && current.Version != expectedVersion {
		return domain.InspectionPlan{}, domain.ErrVersionConflict
	}
	if err := mutate(&current); err != nil {
		return domain.InspectionPlan{}, err
	}
	if err := contextErr(ctx); err != nil {
		return domain.InspectionPlan{}, err
	}
	if err := current.Validate(); err != nil {
		return domain.InspectionPlan{}, err
	}
	next := cloneSnapshot(s.snapshot)
	next.Plans[index] = current
	if err := s.persistLocked(ctx, next); err != nil {
		return domain.InspectionPlan{}, err
	}
	s.snapshot = next
	return clonePlan(current), nil
}

func (s *FileStore) Get(ctx context.Context, id string) (domain.InspectionPlan, error) {
	if err := contextErr(ctx); err != nil {
		return domain.InspectionPlan{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.loaded {
		return domain.InspectionPlan{}, fmt.Errorf("%w：数据快照尚未加载", domain.ErrSnapshotInvalid)
	}
	for _, plan := range s.snapshot.Plans {
		if plan.ID == id {
			return clonePlan(plan), nil
		}
	}
	return domain.InspectionPlan{}, domain.ErrPlanNotFound
}

func (s *FileStore) persistLocked(ctx context.Context, snapshot Snapshot) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	snapshot.SchemaVersion = currentSchemaVersion
	snapshot.SavedAt = s.now().UTC()
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败：%w", err)
	}
	temporary, err := os.CreateTemp(directory, ".inspection-snapshot-*")
	if err != nil {
		return fmt.Errorf("创建临时快照失败：%w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("写入数据快照失败：%w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("同步数据快照失败：%w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭数据快照失败：%w", err)
	}
	if err := os.Rename(temporaryName, s.path); err != nil {
		return fmt.Errorf("替换数据快照失败：%w", err)
	}
	return nil
}

func validateSnapshot(snapshot Snapshot) error {
	seen := make(map[string]bool, len(snapshot.Plans))
	for _, plan := range snapshot.Plans {
		if seen[plan.ID] {
			return fmt.Errorf("%w：计划标识重复", domain.ErrSnapshotInvalid)
		}
		seen[plan.ID] = true
		if err := plan.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	return snapshot
}

func clonePlan(plan domain.InspectionPlan) domain.InspectionPlan {
	plan.SiteIDs = append([]string(nil), plan.SiteIDs...)
	sourceSites := plan.Sites
	plan.Sites = make([]domain.InspectionSite, len(sourceSites))
	for i, site := range sourceSites {
		plan.Sites[i] = site
		plan.Sites[i].Observations = make([]domain.Observation, len(site.Observations))
		for observationIndex, observation := range site.Observations {
			plan.Sites[i].Observations[observationIndex] = observation
			if observation.ReviewHistory != nil {
				plan.Sites[i].Observations[observationIndex].ReviewHistory = append([]domain.ReviewEvent{}, observation.ReviewHistory...)
			}
		}
	}
	if plan.Report != nil {
		report := *plan.Report
		report.SiteResults = append([]domain.SiteResult(nil), plan.Report.SiteResults...)
		report.SeverityCounts = cloneSeverityCounts(plan.Report.SeverityCounts)
		report.ReviewCounts = cloneReviewCounts(plan.Report.ReviewCounts)
		for i := range report.SiteResults {
			if plan.Report.SiteResults[i].Observation != nil {
				observation := *plan.Report.SiteResults[i].Observation
				if observation.ReviewHistory != nil {
					observation.ReviewHistory = append([]domain.ReviewEvent{}, observation.ReviewHistory...)
				}
				report.SiteResults[i].Observation = &observation
			}
		}
		plan.Report = &report
	}
	return plan
}

func cloneSeverityCounts(counts map[domain.Severity]int) map[domain.Severity]int {
	if counts == nil {
		return nil
	}
	clone := make(map[domain.Severity]int, len(counts))
	for severity, count := range counts {
		clone[severity] = count
	}
	return clone
}

func cloneReviewCounts(counts map[domain.ReviewStatus]int) map[domain.ReviewStatus]int {
	if counts == nil {
		return nil
	}
	clone := make(map[domain.ReviewStatus]int, len(counts))
	for status, count := range counts {
		clone[status] = count
	}
	return clone
}

func contextErr(ctx context.Context) error {
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
