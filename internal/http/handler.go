package http

import (
	"bytes"
	"context"
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"community-inspection/internal/domain"
	"community-inspection/internal/service"
)

//go:embed static/*
var staticFiles embed.FS

type Handler struct {
	service *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

func (h *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Referrer-Policy", "same-origin")
	if request.URL.Path == "/" {
		if request.Method != http.MethodGet {
			methodNotAllowed(writer, http.MethodGet)
			return
		}
		h.serveStatic(writer, "static/index.html")
		return
	}
	if strings.HasPrefix(request.URL.Path, "/static/") {
		if request.Method != http.MethodGet {
			methodNotAllowed(writer, http.MethodGet)
			return
		}
		h.serveStatic(writer, "static/"+path.Base(request.URL.Path))
		return
	}
	if request.URL.Path == "/api/healthz" {
		if request.Method != http.MethodGet {
			methodNotAllowed(writer, http.MethodGet)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if request.URL.Path == "/api/plans" {
		switch request.Method {
		case http.MethodGet:
			h.listPlans(writer, request)
		case http.MethodPost:
			h.createPlan(writer, request)
		default:
			methodNotAllowed(writer, http.MethodGet, http.MethodPost)
		}
		return
	}
	if request.URL.Path == "/api/observations" {
		if request.Method != http.MethodGet {
			methodNotAllowed(writer, http.MethodGet)
			return
		}
		h.listObservations(writer, request)
		return
	}
	if request.URL.Path == "/api/observations/export" {
		if request.Method != http.MethodGet {
			methodNotAllowed(writer, http.MethodGet)
			return
		}
		h.exportObservations(writer, request)
		return
	}
	if strings.HasPrefix(request.URL.Path, "/api/plans/") {
		h.planRoute(writer, request)
		return
	}
	h.writeDomainError(writer, http.StatusNotFound, errors.New("请求路径不存在"))
}

func (h *Handler) planRoute(writer http.ResponseWriter, request *http.Request) {
	parts := splitPath(request.URL.Path)
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "plans" || parts[2] == "" {
		h.writeDomainError(writer, http.StatusNotFound, errors.New("请求路径不存在"))
		return
	}
	planID := parts[2]
	if len(parts) == 3 {
		if request.Method != http.MethodGet {
			methodNotAllowed(writer, http.MethodGet)
			return
		}
		plan, err := h.service.GetPlan(request.Context(), planID)
		if err != nil {
			h.writeDomainError(writer, statusForError(err), err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]domain.InspectionPlan{"plan": plan})
		return
	}
	switch parts[3] {
	case "observations":
		if len(parts) == 4 && request.Method == http.MethodPost {
			h.addObservation(writer, request, planID)
			return
		}
		if len(parts) == 5 && parts[4] == "review-batch" && request.Method == http.MethodPost {
			h.reviewObservations(writer, request, planID)
			return
		}
		if len(parts) == 6 && parts[5] == "review" && request.Method == http.MethodPost {
			h.reviewObservation(writer, request, planID, parts[4])
			return
		}
		if len(parts) == 6 && parts[5] == "reopen" && request.Method == http.MethodPost {
			h.reopenObservation(writer, request, planID, parts[4])
			return
		}
	case "copy":
		if len(parts) == 4 && request.Method == http.MethodPost {
			h.copyPlan(writer, request, planID)
			return
		}
	case "close":
		if len(parts) == 4 && request.Method == http.MethodPost {
			h.closePlan(writer, request, planID)
			return
		}
	case "report":
		if len(parts) == 4 && request.Method == http.MethodGet {
			h.getReport(writer, request, planID)
			return
		}
	}
	h.writeDomainError(writer, http.StatusNotFound, errors.New("请求路径不存在"))
}

func (h *Handler) listPlans(writer http.ResponseWriter, request *http.Request) {
	filter := parseFilter(request)
	plans, err := h.service.ListPlans(request.Context(), filter)
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"plans": plans, "count": len(plans), "summary": service.SummarizePlans(plans)})
}

func (h *Handler) createPlan(writer http.ResponseWriter, request *http.Request) {
	var input createPlanRequest
	if err := decodeJSON(request, &input); err != nil {
		h.writeDomainError(writer, http.StatusBadRequest, err)
		return
	}
	if len(input.Sites) == 0 {
		h.writeDomainError(writer, http.StatusBadRequest, errors.New("至少需要登记一个设施点位"))
		return
	}
	sites := make([]service.CreateSiteInput, len(input.Sites))
	for i, site := range input.Sites {
		sites[i] = service.CreateSiteInput{Name: site.Name, Category: site.Category, Location: site.Location}
	}
	plan, err := h.service.CreatePlan(request.Context(), service.CreatePlanInput{Name: input.Name, Area: input.Area, ScheduledDate: input.ScheduledDate, Sites: sites})
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]domain.InspectionPlan{"plan": plan})
}

func (h *Handler) copyPlan(writer http.ResponseWriter, request *http.Request, sourceID string) {
	var input copyPlanRequest
	if err := decodeJSON(request, &input); err != nil {
		h.writeDomainError(writer, http.StatusBadRequest, err)
		return
	}
	plan, err := h.service.CopyPlan(request.Context(), sourceID, service.CopyPlanInput{
		Name: input.Name, Area: input.Area, ScheduledDate: input.ScheduledDate,
	})
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]domain.InspectionPlan{"plan": plan})
}

func (h *Handler) addObservation(writer http.ResponseWriter, request *http.Request, planID string) {
	var input observationRequest
	if err := decodeJSON(request, &input); err != nil {
		h.writeDomainError(writer, http.StatusBadRequest, err)
		return
	}
	observedAt := time.Time{}
	if input.ObservedAt != "" {
		var err error
		observedAt, err = time.Parse(time.RFC3339, input.ObservedAt)
		if err != nil {
			h.writeDomainError(writer, http.StatusBadRequest, errors.New("观测时间格式应为 RFC3339"))
			return
		}
	}
	plan, observation, err := h.service.AddObservation(request.Context(), planID, service.ObservationInput{
		SiteID: input.SiteID, Kind: input.Kind, Value: input.Value, Unit: input.Unit, Note: input.Note,
		Observer: input.Observer, Severity: domain.Severity(input.Severity), ObservedAt: observedAt,
		IdempotencyKey: input.IdempotencyKey, ExpectedVersion: input.ExpectedVersion,
	})
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"plan": plan, "observation": observation})
}

func (h *Handler) reviewObservation(writer http.ResponseWriter, request *http.Request, planID, observationID string) {
	var input reviewRequest
	if err := decodeJSON(request, &input); err != nil {
		h.writeDomainError(writer, http.StatusBadRequest, err)
		return
	}
	plan, err := h.service.ReviewObservation(request.Context(), planID, observationID, service.ReviewInput{
		Decision: domain.ReviewStatus(input.Decision), Reviewer: input.Reviewer, Note: input.Note, ExpectedVersion: input.ExpectedVersion,
	})
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]domain.InspectionPlan{"plan": plan})
}

func (h *Handler) reviewObservations(writer http.ResponseWriter, request *http.Request, planID string) {
	var input reviewBatchRequest
	if err := decodeJSON(request, &input); err != nil {
		h.writeDomainError(writer, http.StatusBadRequest, err)
		return
	}
	plan, err := h.service.ReviewObservations(request.Context(), planID, service.BatchReviewInput{
		ObservationIDs:  input.ObservationIDs,
		Decision:        domain.ReviewStatus(input.Decision),
		Reviewer:        input.Reviewer,
		Note:            input.Note,
		ExpectedVersion: input.ExpectedVersion,
	})
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]domain.InspectionPlan{"plan": plan})
}

func (h *Handler) reopenObservation(writer http.ResponseWriter, request *http.Request, planID, observationID string) {
	var input reopenRequest
	if err := decodeJSON(request, &input); err != nil {
		h.writeDomainError(writer, http.StatusBadRequest, err)
		return
	}
	plan, err := h.service.ReopenObservation(request.Context(), planID, observationID, service.ReopenInput{
		Operator: input.Operator, Note: input.Note, ExpectedVersion: input.ExpectedVersion,
	})
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]domain.InspectionPlan{"plan": plan})
}

func (h *Handler) closePlan(writer http.ResponseWriter, request *http.Request, planID string) {
	var input versionRequest
	if err := decodeJSON(request, &input); err != nil {
		h.writeDomainError(writer, http.StatusBadRequest, err)
		return
	}
	plan, report, err := h.service.ClosePlan(request.Context(), planID, input.ExpectedVersion)
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"plan": plan, "report": report})
}

func (h *Handler) getReport(writer http.ResponseWriter, request *http.Request, planID string) {
	report, err := h.service.GetReport(request.Context(), planID)
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]domain.InspectionReport{"report": report})
}

func (h *Handler) listObservations(writer http.ResponseWriter, request *http.Request) {
	rows, err := h.service.ListObservations(request.Context(), parseFilter(request))
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"observations": rows, "count": len(rows), "summary": service.SummarizeObservations(rows)})
}

func (h *Handler) exportObservations(writer http.ResponseWriter, request *http.Request) {
	rows, err := h.service.ExportObservations(request.Context(), parseFilter(request))
	if err != nil {
		h.writeDomainError(writer, statusForError(err), err)
		return
	}
	body, err := encodeObservationCSV(rows)
	if err != nil {
		h.writeDomainError(writer, http.StatusInternalServerError, err)
		return
	}
	writer.Header().Set("Content-Type", "text/csv; charset=utf-8")
	writer.Header().Set("Content-Disposition", `attachment; filename="inspection-observations.csv"`)
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func encodeObservationCSV(rows []service.ObservationExportRow) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write([]string{"计划标识", "计划名称", "计划日期", "点位标识", "点位名称", "设施类别", "点位位置", "观测标识", "观测时间", "观测项目", "现场数值", "单位", "备注", "记录人", "异常等级", "复核状态", "复核人", "复核意见", "复核时间"}); err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err := writer.Write([]string{
			row.PlanID, row.PlanName, row.ScheduledDate,
			row.SiteID, row.SiteName, row.SiteCategory, row.SiteLocation,
			row.ObservationID, row.ObservedAt, row.Kind, row.Value, row.Unit,
			row.Note, row.Observer, row.Severity, row.ReviewStatus,
			row.Reviewer, row.ReviewNote, row.ReviewedAt,
		}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (h *Handler) serveStatic(writer http.ResponseWriter, name string) {
	data, err := staticFiles.ReadFile(name)
	if err != nil {
		h.writeDomainError(writer, http.StatusNotFound, errors.New("页面资源不存在"))
		return
	}
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	contentType = strings.Split(contentType, ";")[0]
	writer.Header().Set("Content-Type", contentType+"; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(data)
}

func (h *Handler) writeDomainError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"message": err.Error()}})
}

type createPlanRequest struct {
	Name          string              `json:"name"`
	Area          string              `json:"area"`
	ScheduledDate string              `json:"scheduledDate"`
	Sites         []createSiteRequest `json:"sites"`
}

type createSiteRequest struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Location string `json:"location"`
}

type copyPlanRequest struct {
	Name          string `json:"name"`
	Area          string `json:"area"`
	ScheduledDate string `json:"scheduledDate"`
}

type observationRequest struct {
	SiteID          string `json:"siteID"`
	Kind            string `json:"kind"`
	Value           string `json:"value"`
	Unit            string `json:"unit"`
	Note            string `json:"note"`
	Observer        string `json:"observer"`
	Severity        string `json:"severity"`
	ObservedAt      string `json:"observedAt"`
	IdempotencyKey  string `json:"idempotencyKey"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type reviewRequest struct {
	Decision        string `json:"decision"`
	Reviewer        string `json:"reviewer"`
	Note            string `json:"note"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type reopenRequest struct {
	Operator        string `json:"operator"`
	Note            string `json:"note"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type reviewBatchRequest struct {
	ObservationIDs  []string `json:"observationIDs"`
	Decision        string   `json:"decision"`
	Reviewer        string   `json:"reviewer"`
	Note            string   `json:"note"`
	ExpectedVersion int64    `json:"expectedVersion"`
}

type versionRequest struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}

func decodeJSON(request *http.Request, target any) error {
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(target); err != nil {
		return errors.New("请求 JSON 格式无效")
	}
	return nil
}

func parseFilter(request *http.Request) service.Filter {
	values := request.URL.Query()
	return service.Filter{
		Date:         strings.TrimSpace(values.Get("date")),
		DateFrom:     strings.TrimSpace(values.Get("dateFrom")),
		DateTo:       strings.TrimSpace(values.Get("dateTo")),
		Category:     strings.TrimSpace(values.Get("category")),
		Status:       domain.PlanStatus(strings.TrimSpace(values.Get("status"))),
		Severity:     domain.Severity(strings.TrimSpace(values.Get("severity"))),
		ReviewStatus: domain.ReviewStatus(strings.TrimSpace(values.Get("reviewStatus"))),
		Query:        values.Get("q"),
	}
}

func splitPath(value string) []string {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrPlanNotFound), errors.Is(err, domain.ErrSiteNotFound), errors.Is(err, domain.ErrObservationNotFound), errors.Is(err, domain.ErrReportNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrVersionConflict), errors.Is(err, domain.ErrPlanClosed), errors.Is(err, domain.ErrOutOfOrder), errors.Is(err, domain.ErrNotReadyToClose), errors.Is(err, domain.ErrAlreadyReviewed):
		return http.StatusConflict
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func methodNotAllowed(writer http.ResponseWriter, methods ...string) {
	writer.Header().Set("Allow", strings.Join(methods, ", "))
	writeJSON(writer, http.StatusMethodNotAllowed, map[string]any{"error": map[string]string{"message": "请求方法不支持"}})
}
