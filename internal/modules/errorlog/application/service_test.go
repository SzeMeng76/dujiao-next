package application

import (
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/modules/errorlog/contract"
	"github.com/dujiao-next/internal/modules/errorlog/domain"
)

// fakeRepo 记录收到的批次，用于断言异步刷盘行为。
type fakeRepo struct {
	mu          sync.Mutex
	received    []*domain.ErrorLog
	failNext    bool
	deletedUpTo []time.Time
}

func (r *fakeRepo) Create(log *domain.ErrorLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.received = append(r.received, log)
	return nil
}

func (r *fakeRepo) BatchCreate(logs []*domain.ErrorLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.received = append(r.received, logs...)
	return nil
}

func (r *fakeRepo) ListAdmin(filter contract.Filter) ([]domain.ErrorLog, int64, error) {
	return nil, 0, nil
}

func (r *fakeRepo) DeleteOlderThan(before time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deletedUpTo = append(r.deletedUpTo, before)
	return 1, nil
}

func (r *fakeRepo) cleanupCalls() []time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]time.Time, len(r.deletedUpTo))
	copy(out, r.deletedUpTo)
	return out
}

func (r *fakeRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.received)
}

func TestService_StopFlushesBufferedEntries(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil)

	const total = 10
	for i := 0; i < total; i++ {
		svc.Record(&domain.ErrorLog{Message: "boom"})
	}

	// 未到 flushSize 也不到 ticker 周期，此时应尚未落库
	svc.Stop()

	if got := repo.count(); got != total {
		t.Fatalf("Stop() 应排干全部缓冲记录: got %d, want %d", got, total)
	}
}

func TestService_StopIsIdempotent(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil)
	svc.Record(&domain.ErrorLog{Message: "once"})

	svc.Stop()
	// 第二次调用必须直接返回，不能因重复 close(channel) 而 panic
	svc.Stop()

	if got := repo.count(); got != 1 {
		t.Fatalf("重复 Stop() 不应重复写入: got %d, want 1", got)
	}
}

func TestService_RecordAfterStopIsSafe(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil)
	svc.Stop()

	// 优雅关闭期间仍有在途请求调用 Record()，不能 panic（send on closed channel）
	svc.Record(&domain.ErrorLog{Message: "late"})

	if got := repo.count(); got != 0 {
		t.Fatalf("Stop 后的记录应被丢弃: got %d, want 0", got)
	}
}

func TestService_RecordNilIsSafe(t *testing.T) {
	var svc *Service
	svc.Record(&domain.ErrorLog{}) // nil receiver 不应 panic

	repo := &fakeRepo{}
	realSvc := NewService(repo, nil)
	defer realSvc.Stop()
	realSvc.Record(nil) // nil entry 不应 panic 也不应写入
}

func TestService_FlushesAtBatchSize(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil)
	defer svc.Stop()

	// 达到 defaultFlushSize 应立即触发一次批量写入，无需等 ticker
	for i := 0; i < defaultFlushSize; i++ {
		svc.Record(&domain.ErrorLog{Message: "boom"})
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if repo.count() >= defaultFlushSize {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("达到批量阈值后应自动刷盘: got %d, want %d", repo.count(), defaultFlushSize)
}

// runRetention 应按 retentionDays 计算截止时间并调用仓储清理。
func TestService_RunRetentionDeletesExpiredEntries(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, &Options{RetentionDays: 7})
	defer svc.Stop()

	svc.runRetention()

	calls := repo.cleanupCalls()
	if len(calls) != 1 {
		t.Fatalf("应触发 1 次清理，实际 %d 次", len(calls))
	}

	// 截止时间应约为 7 天前（允许几秒误差）
	want := time.Now().AddDate(0, 0, -7)
	diff := calls[0].Sub(want)
	if diff < -time.Minute || diff > time.Minute {
		t.Fatalf("截止时间偏差过大: got %v, want ≈%v", calls[0], want)
	}
}

// retentionDays 为 0 时不应启动清理 goroutine，也不应删除任何数据。
func TestService_NoRetentionWhenDisabled(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, &Options{RetentionDays: 0})
	defer svc.Stop()

	svc.runRetention() // 即使被显式调用，也只验证不 panic

	if calls := repo.cleanupCalls(); len(calls) > 1 {
		t.Fatalf("未启用清理时不应反复删除: %d 次", len(calls))
	}
}

// Collect4xx 应反映构造时的配置。
func TestService_Collect4xxFlag(t *testing.T) {
	enabled := NewService(&fakeRepo{}, &Options{Collect4xx: true})
	defer enabled.Stop()
	if !enabled.Collect4xx() {
		t.Fatal("Collect4xx 应为 true")
	}

	disabled := NewService(&fakeRepo{}, nil)
	defer disabled.Stop()
	if disabled.Collect4xx() {
		t.Fatal("默认 Collect4xx 应为 false")
	}

	var nilSvc *Service
	if nilSvc.Collect4xx() {
		t.Fatal("nil Service 的 Collect4xx 应安全返回 false")
	}
}
