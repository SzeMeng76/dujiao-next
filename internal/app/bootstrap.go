package app

import (
	"errors"

	"github.com/dujiao-next/internal/app/container"
	"github.com/dujiao-next/internal/app/httpserver"
	"github.com/dujiao-next/internal/app/jobs"
	jobconsumer "github.com/dujiao-next/internal/app/jobs/consumer"
	"github.com/dujiao-next/internal/config"
)

// BuildRunner 构建服务运行器
func BuildRunner(cfg *config.Config, mode string) (*Runner, error) {
	runner, _, err := buildRunnerWithDependencies(cfg, mode)
	return runner, err
}

// buildRunnerWithDependencies 同时返回 container，供 Run 在收尾时释放后台资源
// （例如错误日志的异步写入队列）。
func buildRunnerWithDependencies(cfg *config.Config, mode string) (*Runner, *container.Container, error) {
	if cfg == nil {
		return nil, nil, errors.New("config is nil")
	}

	dependencies, err := container.NewContainer(cfg)
	if err != nil {
		return nil, nil, err
	}

	var services []Service

	// 初始化 HTTP 服务
	if mode == ModeAll || mode == ModeAPI {
		engine := httpserver.SetupRouter(cfg, dependencies)
		addr := cfg.Server.Host + ":" + cfg.Server.Port
		httpService := NewHTTPService(addr, engine)
		services = append(services, httpService)
	}

	// 初始化 Worker 服务
	if mode == ModeAll || mode == ModeWorker {
		consumer := jobconsumer.New(dependencies)
		workerService, err := jobs.NewService(&cfg.Queue, consumer)
		if err != nil {
			return nil, nil, err
		}
		services = append(services, workerService)
	}

	// 如果没有服务被启动（例如模式错误或配置导致都没起），应该报错或至少打日志
	if len(services) == 0 {
		return nil, nil, errors.New("no services initialized (check mode and config)")
	}

	return NewRunner(services...), dependencies, nil
}

// Run 应用启动入口
func Run(opts Options) error {
	opts = normalizeOptions(opts)
	if opts.Config == nil {
		return errors.New("config is nil")
	}

	runner, dependencies, err := buildRunnerWithDependencies(opts.Config, opts.Mode)
	if err != nil {
		return err
	}
	// 进程退出前排干错误日志的异步写入队列，否则最后一批记录会丢。
	if dependencies != nil {
		if dependencies.ErrorLogService != nil {
			defer dependencies.ErrorLogService.Stop()
		}
		if dependencies.LogRetentionRunner != nil {
			dependencies.LogRetentionRunner.Start()
			defer dependencies.LogRetentionRunner.Stop()
		}
	}

	addr := opts.Config.Server.Host + ":" + opts.Config.Server.Port
	opts.Logger.Infow("app_start", "addr", addr, "mode", opts.Mode)
	return RunWithOptions(runner, opts)
}
