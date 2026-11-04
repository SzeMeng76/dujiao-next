package retention

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNewRunner_FiltersDisabledTargets(t *testing.T) {
	noop := func(time.Time) (int64, error) { return 0, nil }

	r := NewRunner([]Target{
		{Name: "enabled", RetentionDays: 30, Delete: noop},
		{Name: "zero_days", RetentionDays: 0, Delete: noop},
		{Name: "negative", RetentionDays: -1, Delete: noop},
		{Name: "nil_delete", RetentionDays: 30},
	}, Options{})

	if !r.Enabled() {
		t.Fatal("存在有效目标时应为 enabled")
	}
	if len(r.targets) != 1 {
		t.Fatalf("应只保留 1 个有效目标，实际 %d", len(r.targets))
	}
	if r.targets[0].Name != "enabled" {
		t.Fatalf("保留的目标错误: %s", r.targets[0].Name)
	}
}

func TestRunner_DisabledWhenNoValidTargets(t *testing.T) {
	r := NewRunner(nil, Options{})
	if r.Enabled() {
		t.Fatal("无目标时应为 disabled")
	}

	// 未 Enabled 时 Stop 不能死锁
	done := make(chan struct{})
	go func() {
		r.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() 在无目标时阻塞了")
	}
}

func TestRunner_RunOnceDeletesWithCorrectCutoff(t *testing.T) {
	var mu sync.Mutex
	got := map[string]time.Time{}
	callCount := map[string]int{}

	mk := func(name string) func(time.Time) (int64, error) {
		return func(before time.Time) (int64, error) {
			mu.Lock()
			defer mu.Unlock()
			got[name] = before
			callCount[name]++
			return 1, nil
		}
	}

	r := NewRunner([]Target{
		{Name: "a", RetentionDays: 7, Delete: mk("a")},
		{Name: "b", RetentionDays: 180, Delete: mk("b")},
	}, Options{})

	r.RunOnce()

	now := time.Now()
	for name, days := range map[string]int{"a": 7, "b": 180} {
		mu.Lock()
		cutoff, called := got[name], callCount[name]
		mu.Unlock()
		if called == 0 {
			t.Fatalf("目标 %s 未被调用", name)
		}
		want := now.AddDate(0, 0, -days)
		if d := cutoff.Sub(want); d < -time.Minute || d > time.Minute {
			t.Fatalf("目标 %s 截止时间偏差过大: got %v, want ≈%v", name, cutoff, want)
		}
	}
}

func TestRunner_RunOnceContinuesAfterError(t *testing.T) {
	var mu sync.Mutex
	called := []string{}

	r := NewRunner([]Target{
		{Name: "fails", RetentionDays: 7, Delete: func(time.Time) (int64, error) {
			mu.Lock()
			called = append(called, "fails")
			mu.Unlock()
			return 0, errors.New("boom")
		}},
		{Name: "succeeds", RetentionDays: 7, Delete: func(time.Time) (int64, error) {
			mu.Lock()
			called = append(called, "succeeds")
			mu.Unlock()
			return 3, nil
		}},
	}, Options{})

	r.RunOnce()

	if len(called) != 2 {
		t.Fatalf("单个目标失败不应中断其余目标，实际执行 %v", called)
	}
}

func TestRunner_StopIsIdempotent(t *testing.T) {
	r := NewRunner([]Target{
		{Name: "x", RetentionDays: 1, Delete: func(time.Time) (int64, error) { return 0, nil }},
	}, Options{FirstDelay: time.Hour})

	r.Start()

	done := make(chan struct{})
	go func() {
		r.Stop()
		r.Stop() // 第二次必须安全返回
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("重复 Stop() 阻塞了")
	}
}

func TestRunner_DefaultsApplied(t *testing.T) {
	r := NewRunner([]Target{
		{Name: "x", RetentionDays: 1, Delete: func(time.Time) (int64, error) { return 0, nil }},
	}, Options{})

	if r.firstDelay != defaultFirstDelay {
		t.Fatalf("firstDelay 默认值错误: %v", r.firstDelay)
	}
	if r.interval != defaultInterval {
		t.Fatalf("interval 默认值错误: %v", r.interval)
	}
}
