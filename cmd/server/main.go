package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	web "community-inspection/internal/http"
	"community-inspection/internal/service"
	"community-inspection/internal/store"
)

func Main() {
	if len(os.Args) > 1 && os.Args[1] == "selfcheck" {
		if err := RunSelfCheck(); err != nil {
			fmt.Fprintf(os.Stderr, "自检失败：%v\n", err)
			os.Exit(1)
		}
		fmt.Println("自检通过：创建、观测、复核、关闭和报告读取流程正常。")
		return
	}
	if err := runServer(); err != nil {
		fmt.Fprintf(os.Stderr, "服务退出：%v\n", err)
		os.Exit(1)
	}
}

func runServer() error {
	dataPath := os.Getenv("INSPECTION_DATA")
	if dataPath == "" {
		dataPath = filepath.Join("data", "inspection.json")
	}
	address := os.Getenv("INSPECTION_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	repository := store.NewFileStore(dataPath)
	if err := repository.Load(context.Background()); err != nil {
		return err
	}
	svc, err := service.New(repository, service.Config{})
	if err != nil {
		return err
	}
	server := &http.Server{Addr: address, Handler: web.NewHandler(svc), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	stopContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-stopContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("优雅关闭失败：%w", err)
		}
		return nil
	}
}

func RunSelfCheck() error {
	temporary, err := os.MkdirTemp("", "community-inspection-selfcheck-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	repository := store.NewFileStore(filepath.Join(temporary, "inspection.json"))
	if err := repository.Load(context.Background()); err != nil {
		return err
	}
	fixedNow := time.Date(2026, 8, 19, 9, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	sequence := 0
	svc, err := service.New(repository, service.Config{
		Now:   func() time.Time { return fixedNow },
		NewID: func(prefix string) string { sequence++; return fmt.Sprintf("%s-self-%02d", prefix, sequence) },
	})
	if err != nil {
		return err
	}
	plan, err := svc.CreatePlan(context.Background(), service.CreatePlanInput{
		Name: "东片区晨间巡检", Area: "华宁街道东片区", ScheduledDate: "2026-08-20",
		Sites: []service.CreateSiteInput{{Name: "东门健身点", Category: "健身器材", Location: "东门广场北侧"}, {Name: "中心花园照明", Category: "公共照明", Location: "中心花园环路"}},
	})
	if err != nil {
		return fmt.Errorf("创建计划：%w", err)
	}
	first, _, err := svc.AddObservation(context.Background(), plan.ID, service.ObservationInput{SiteID: plan.Sites[0].ID, Kind: "器材外观", Value: "正常", Observer: "巡检员甲", Severity: "normal", ExpectedVersion: plan.Version})
	if err != nil {
		return fmt.Errorf("记录首个观测：%w", err)
	}
	second, observation, err := svc.AddObservation(context.Background(), plan.ID, service.ObservationInput{SiteID: plan.Sites[1].ID, Kind: "灯具亮灯数", Value: "7/8", Unit: "盏", Note: "西南角灯具未亮", Observer: "巡检员甲", Severity: "major", ExpectedVersion: first.Version})
	if err != nil {
		return fmt.Errorf("记录异常观测：%w", err)
	}
	reviewed, err := svc.ReviewObservation(context.Background(), plan.ID, observation.ID, service.ReviewInput{Decision: "approved", Reviewer: "区域主管", Note: "已登记维修工单", ExpectedVersion: second.Version})
	if err != nil {
		return fmt.Errorf("提交复核：%w", err)
	}
	closed, report, err := svc.ClosePlan(context.Background(), plan.ID, reviewed.Version)
	if err != nil {
		return fmt.Errorf("关闭计划：%w", err)
	}
	if closed.Status != "closed" || report.Checksum == "" || report.ExceptionCount != 1 || report.SeverityCounts["normal"] != 1 || report.SeverityCounts["major"] != 1 || report.ReviewCounts["not_required"] != 1 || report.ReviewCounts["approved"] != 1 {
		return errors.New("报告结果不完整")
	}
	loadedReport, err := svc.GetReport(context.Background(), plan.ID)
	if err != nil {
		return fmt.Errorf("读取报告：%w", err)
	}
	if loadedReport.Checksum != report.Checksum || loadedReport.Summary != report.Summary || loadedReport.SeverityCounts["major"] != 1 || loadedReport.ReviewCounts["approved"] != 1 {
		return errors.New("报告读取结果不一致")
	}
	return nil
}
