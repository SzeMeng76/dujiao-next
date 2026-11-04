package retention

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/dujiao-next/internal/logger"
)

// Target 是一个日志表的清理目标。
type Target struct {
	// Name 用于日志标识，通常就是表名。
	Name string
	// RetentionDays 保留天数，<=0 表示跳过该目标。
	RetentionDays int
	// Delete 删除早于给定时间的记录，返回删除条数。
	Delete func(before time.Time) (int64, error)
}

// Runner 统一调度各日志表的过期清理。
//
// 设计取舍：
//   - 不依赖 asynq / Redis：README 说明服务可以无 Redis 运行，清理不应因此失效。
//   - 单 goroutine 串行执行：清理是低频后台任务，串行可避免同时打满
//     SQLite 的唯一连接（MaxOpenConns=1）。
//   - 首次延迟启动：进程刚起来时业务在预热，不要立刻大批 DELETE。
type Runner struct {
	targets []Target
	// stopCh 由 Stop() 关闭，用于通知循环退出。
	stopCh chan struct{}
	// loopDone 由 loop() 在退出时关闭，用于让 Stop() 等待循环真正结束。
	// 两者必须分开：若共用同一个 channel，Stop 与 loop 会重复 close 而 panic。
	loopDone chan struct{}
	// stopOnce 保证 stopCh 只被关闭一次；doneOnce 保证 loopDone 只被关闭一次。
	// 必须分开：Start 与 Stop 各自关闭不同的 channel，共用 once 会让
	// 其中一个 channel 永远关不掉，另一个被重复关闭而 panic。
	stopOnce sync.Once
	doneOnce sync.Once
	// launched 记录 loop goroutine 是否真的启动过。
	// Stop() 据此决定要不要自己关闭 loopDone，避免在从未 Start 时永久阻塞。
	launched atomic.Bool

	// firstDelay 首次执行前的等待时间；interval 为后续执行间隔。
	firstDelay time.Duration
	interval   time.Duration
}

// Options 调度器配置。零值时使用默认值。
type Options struct {
	FirstDelay time.Duration
	Interval   time.Duration
}

const (
	defaultFirstDelay = time.Minute
	defaultInterval   = 24 * time.Hour
)

// NewRunner 创建调度器。targets 中 RetentionDays<=0 的项会被忽略。
func NewRunner(targets []Target, opts Options) *Runner {
	enabled := make([]Target, 0, len(targets))
	for _, t := range targets {
		if t.Delete == nil || t.RetentionDays <= 0 {
			continue
		}
		enabled = append(enabled, t)
	}

	r := &Runner{
		targets:    enabled,
		stopCh:     make(chan struct{}),
		loopDone:   make(chan struct{}),
		firstDelay: opts.FirstDelay,
		interval:   opts.Interval,
	}
	if r.firstDelay <= 0 {
		r.firstDelay = defaultFirstDelay
	}
	if r.interval <= 0 {
		r.interval = defaultInterval
	}
	return r
}

// Enabled 报告是否有任何清理目标，便于调用方决定是否启动。
func (r *Runner) Enabled() bool {
	return r != nil && len(r.targets) > 0
}

// Start 启动后台清理循环；无目标时直接把 loopDone 标记为已完成。
func (r *Runner) Start() {
	if r == nil {
		return
	}
	if !r.Enabled() {
		// 无目标时没有 loop 会来关闭 loopDone，这里直接关闭，
		// 使随后的 Stop() 不会阻塞等待。
		r.doneOnce.Do(func() { close(r.loopDone) })
		return
	}
	r.launched.Store(true)
	go r.loop()
}

// Stop 停止清理循环，可重复调用。
func (r *Runner) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() { close(r.stopCh) })
	// 等待 loop 真正退出。loop 退出时会关闭 loopDone；
	// 若从未 Start 或 Enabled()==false，Start 已提前关闭它。
	// 若调用方从未 Start 过，loopDone 永远不会关闭 —— 这里补关一次，
	// 避免 Stop 无条件阻塞。
	if !r.launched.Load() {
		r.doneOnce.Do(func() { close(r.loopDone) })
	}
	<-r.loopDone
}

func (r *Runner) loop() {
	defer close(r.loopDone)

	timer := time.NewTimer(r.firstDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.stopCh:
		return
	}
	r.RunOnce()

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.RunOnce()
		case <-r.stopCh:
			return
		}
	}
}

// RunOnce 对所有目标执行一次清理。导出以便测试直接调用。
func (r *Runner) RunOnce() {
	if r == nil {
		return
	}
	for _, t := range r.targets {
		before := time.Now().AddDate(0, 0, -t.RetentionDays)
		n, err := t.Delete(before)
		if err != nil {
			logger.Warnw("retention_cleanup_failed",
				"target", t.Name,
				"retention_days", t.RetentionDays,
				"error", err,
			)
			continue
		}
		if n > 0 {
			logger.Infow("retention_cleanup_ok",
				"target", t.Name,
				"deleted", n,
				"retention_days", t.RetentionDays,
			)
		}
	}
}
