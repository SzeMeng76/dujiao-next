package application

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/modules/errorlog/contract"
	"github.com/dujiao-next/internal/modules/errorlog/domain"
)

const (
	defaultBufferSize  = 512  // channel 满了就丢弃，不阻塞请求
	defaultFlushSize   = 50   // 积累到 50 条批量写一次
	defaultFlushMillis = 2000 // 或者 2 秒一次，取先到者
)

// Options 创建 Service 的可选配置。
type Options struct {
	// Collect4xx 为 true 时同时记录 4xx 响应（默认只记录 5xx）。
	Collect4xx bool
}

// Service 错误日志服务。
//
// 采用 bounded channel + 后台 goroutine 批量写入：
//   - Record() 非阻塞，channel 满时直接丢弃（dropped 计数，日志告警）
//   - 后台 worker 每 flushSize 条或 flushInterval 后批量 BatchCreate
//   - Stop() 在进程退出时 drain channel、flush 剩余条目
type Service struct {
	repo    contract.Repository
	ch      chan *domain.ErrorLog
	done    chan struct{}
	dropped atomic.Int64

	// mu/closed 保护 Stop() 与在途 Record() 的竞争：
	// channel 被 close 后再 send 会 panic，优雅关闭期间仍有请求在跑，
	// 必须用 closed 标记把这类写入挡掉。
	mu     sync.RWMutex
	closed bool

	collect4xx bool

	flushSize     int
	flushInterval time.Duration
}

// NewService 创建服务并启动后台 worker。opts 为可选，传 nil 使用全部默认值。
func NewService(repo contract.Repository, opts *Options) *Service {
	s := &Service{
		repo:          repo,
		ch:            make(chan *domain.ErrorLog, defaultBufferSize),
		done:          make(chan struct{}),
		flushSize:     defaultFlushSize,
		flushInterval: defaultFlushMillis * time.Millisecond,
	}
	if opts != nil {
		s.collect4xx = opts.Collect4xx
	}
	go s.worker()
	return s
}

// Collect4xx 报告是否收集 4xx 错误，供中间件决策。
func (s *Service) Collect4xx() bool {
	if s == nil {
		return false
	}
	return s.collect4xx
}

// Record 异步记录一条错误日志，永不阻塞调用方。
func (s *Service) Record(entry *domain.ErrorLog) {
	if s == nil || entry == nil {
		return
	}
	// 读锁存活期覆盖整次发送，确保 Stop() 不会在发送中途 close(channel)。
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- entry:
	default:
		// channel 已满：丢弃并计数，不阻塞请求路径
		s.dropped.Add(1)
		if s.dropped.Load()%100 == 0 {
			logger.Warnw("errorlog_buffer_full_dropping",
				"dropped_total", s.dropped.Load(),
			)
		}
	}
}

// Stop 优雅关闭：等后台 worker 排干剩余条目后返回。
// 调用方应在进程退出前调用。可重复调用，第二次直接返回。
func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.ch)
	s.mu.Unlock()

	<-s.done
}

// ListForAdmin 管理端分页查询。
func (s *Service) ListForAdmin(filter contract.Filter) ([]domain.ErrorLog, int64, error) {
	if s == nil || s.repo == nil {
		return nil, 0, nil
	}
	return s.repo.ListAdmin(filter)
}

// DeleteOlderThan 删除创建时间早于 before 的记录，返回删除条数。
// 供统一的 retention 调度器调用。
func (s *Service) DeleteOlderThan(before time.Time) (int64, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	return s.repo.DeleteOlderThan(before)
}

func (s *Service) worker() {
	defer close(s.done)

	buf := make([]*domain.ErrorLog, 0, s.flushSize)
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(buf) == 0 {
			return
		}
		if err := s.repo.BatchCreate(buf); err != nil {
			logger.Warnw("errorlog_batch_write_failed",
				"count", len(buf),
				"error", err,
			)
		}
		buf = buf[:0]
	}

	for {
		select {
		case entry, ok := <-s.ch:
			if !ok {
				// channel 已关闭，flush 剩余后退出
				flush()
				return
			}
			buf = append(buf, entry)
			if len(buf) >= s.flushSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
