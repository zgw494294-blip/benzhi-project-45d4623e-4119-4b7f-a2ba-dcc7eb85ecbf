package http_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	web "community-inspection/internal/http"
	"community-inspection/internal/service"
	"community-inspection/internal/store"
)

func TestHTTPServesWorkbenchAndCompletesWorkflow(t *testing.T) {
	repository := store.NewFileStore(filepath.Join(t.TempDir(), "inspection.json"))
	if err := repository.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	svc, err := service.New(repository, service.Config{Now: func() time.Time { return time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC) }, NewID: func(prefix string) string { sequence++; return prefix + "-" + string(rune('a'+sequence)) }})
	if err != nil {
		t.Fatal(err)
	}
	handler := web.NewHandler(svc)
	page := perform(t, handler, http.MethodGet, "/", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "社区设施巡检") || !strings.Contains(page.Body.String(), "导出观测") || !strings.Contains(page.Body.String(), "<body>") {
		t.Fatalf("unexpected page: %d", page.Code)
	}
	create := perform(t, handler, http.MethodPost, "/api/plans", map[string]any{"name": "路口巡检", "area": "北片区", "scheduledDate": "2026-08-20", "sites": []map[string]string{{"name": "路口照明", "category": "公共照明", "location": "北门"}}})
	if create.Code != http.StatusCreated {
		t.Fatalf("create status: %d %s", create.Code, create.Body.String())
	}
	var createBody struct {
		Plan struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
			Sites   []struct {
				ID string `json:"id"`
			} `json:"sites"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &createBody); err != nil {
		t.Fatal(err)
	}
	planID, siteID, version := createBody.Plan.ID, createBody.Plan.Sites[0].ID, createBody.Plan.Version
	t.Logf("created plan=%q site=%q version=%d", planID, siteID, version)
	observation := perform(t, handler, http.MethodPost, "/api/plans/"+planID+"/observations", map[string]any{"siteID": siteID, "kind": "亮灯状态", "value": "正常", "observer": "巡检员", "severity": "normal", "expectedVersion": version})
	if observation.Code != http.StatusCreated {
		t.Fatalf("observation status: %d %s", observation.Code, observation.Body.String())
	}
	var observationBody struct {
		Plan struct {
			Version int64 `json:"version"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(observation.Body.Bytes(), &observationBody); err != nil {
		t.Fatal(err)
	}
	closed := perform(t, handler, http.MethodPost, "/api/plans/"+planID+"/close", map[string]any{"expectedVersion": observationBody.Plan.Version})
	if closed.Code != http.StatusOK || !strings.Contains(closed.Body.String(), "checksum") {
		t.Fatalf("close response: %d %s", closed.Code, closed.Body.String())
	}
	report := perform(t, handler, http.MethodGet, "/api/plans/"+planID+"/report", nil)
	if report.Code != http.StatusOK || !strings.Contains(report.Body.String(), "generatedAt") || !strings.Contains(report.Body.String(), "severityCounts") || !strings.Contains(report.Body.String(), "reviewCounts") {
		t.Fatalf("report response: %d %s", report.Code, report.Body.String())
	}
	planBeforeExport := perform(t, handler, http.MethodGet, "/api/plans/"+planID, nil).Body.String()
	export := perform(t, handler, http.MethodGet, "/api/observations/export?date=2026-08-20&dateFrom=2026-08-20&dateTo=2026-08-20&category=%E5%85%AC%E5%85%B1%E7%85%A7%E6%98%8E&status=closed&severity=normal&reviewStatus=not_required&q=%E4%BA%AE%E7%81%AF", nil)
	if export.Code != http.StatusOK || export.Header().Get("Content-Type") != "text/csv; charset=utf-8" || export.Header().Get("Content-Disposition") != `attachment; filename="inspection-observations.csv"` {
		t.Fatalf("export response: %d headers=%v body=%s", export.Code, export.Header(), export.Body.String())
	}
	reader := csv.NewReader(bytes.NewReader(export.Body.Bytes()))
	header, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(header) != 19 || header[0] != "计划标识" || header[7] != "观测标识" || header[15] != "复核状态" {
		t.Fatalf("unexpected export header: %v", header)
	}
	row, err := reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if row[0] != planID || row[1] != "路口巡检" || row[4] != "路口照明" || row[9] != "亮灯状态" || row[10] != "正常" || row[15] != "not_required" {
		t.Fatalf("unexpected export row: %v", row)
	}
	if _, err := reader.Read(); err != io.EOF {
		t.Fatalf("expected one exported row, got %v", err)
	}
	planAfterExport := perform(t, handler, http.MethodGet, "/api/plans/"+planID, nil).Body.String()
	if planBeforeExport != planAfterExport {
		t.Fatal("export changed the plan snapshot")
	}
	copyResponse := perform(t, handler, http.MethodPost, "/api/plans/"+planID+"/copy", map[string]any{"name": "路口巡检副本", "area": "北片区", "scheduledDate": "2026-08-27"})
	if copyResponse.Code != http.StatusCreated {
		t.Fatalf("copy status: %d %s", copyResponse.Code, copyResponse.Body.String())
	}
	var copyBody struct {
		Plan struct {
			ID            string `json:"id"`
			Status        string `json:"status"`
			Version       int64  `json:"version"`
			ScheduledDate string `json:"scheduledDate"`
			Sites         []struct {
				ID           string                   `json:"id"`
				Observations []map[string]interface{} `json:"observations"`
			} `json:"sites"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(copyResponse.Body.Bytes(), &copyBody); err != nil {
		t.Fatal(err)
	}
	if copyBody.Plan.ID == planID || copyBody.Plan.Status != "active" || copyBody.Plan.Version != 1 || copyBody.Plan.ScheduledDate != "2026-08-27" || len(copyBody.Plan.Sites) != 1 || copyBody.Plan.Sites[0].ID == siteID || len(copyBody.Plan.Sites[0].Observations) != 0 {
		t.Fatalf("unexpected copied plan: %+v", copyBody.Plan)
	}
	filtered := perform(t, handler, http.MethodGet, "/api/plans?category=%E5%85%AC%E5%85%B1%E7%85%A7%E6%98%8E&status=closed", nil)
	if filtered.Code != http.StatusOK || !strings.Contains(filtered.Body.String(), planID) {
		t.Fatalf("filter response: %d %s", filtered.Code, filtered.Body.String())
	}
}

func perform(t *testing.T, handler http.Handler, method, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if value == nil {
		body = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, path, body)
	if value != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
