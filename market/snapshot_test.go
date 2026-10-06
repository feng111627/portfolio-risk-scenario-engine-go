package market

import (
	"testing"
	"time"
)

var base = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)

// 业务：正常行情更新 —— 序号递增即被接受，估值可取到最新价。
func TestApplyInOrderQuotes(t *testing.T) {
	t.Log("业务: 序号递增的行情被接受，Price 返回最新有效价")
	s := NewSnapshot(time.Minute)
	if err := s.Apply(Quote{Symbol: "AAA", Price: 10, Ts: base, Seq: 1}); err != nil {
		t.Fatalf("首条行情应被接受: %v", err)
	}
	if err := s.Apply(Quote{Symbol: "AAA", Price: 11, Ts: base.Add(time.Second), Seq: 2}); err != nil {
		t.Fatalf("序号递增的行情应被接受: %v", err)
	}
	q, err := s.Price("AAA", base.Add(2*time.Second))
	if err != nil || q.Price != 11 {
		t.Fatalf("应取到最新价 11, 实际 %+v err=%v", q, err)
	}
	t.Logf("用例通过: 最新有效价=%v", q.Price)
}

// 业务：乱序/重复到达的行情被拒绝，不得覆盖更新价格。
func TestOutOfOrderRejected(t *testing.T) {
	t.Log("业务: Seq 不递增的乱序/重复行情被拒绝，快照保持最新价")
	s := NewSnapshot(time.Minute)
	mustApply(t, s, Quote{Symbol: "AAA", Price: 11, Ts: base, Seq: 5})

	for _, q := range []Quote{
		{Symbol: "AAA", Price: 9, Ts: base, Seq: 5}, // 重复序号
		{Symbol: "AAA", Price: 8, Ts: base, Seq: 3}, // 乱序（更旧）
	} {
		if err := s.Apply(q); !IsReason(err, ReasonOutOfOrder) {
			t.Fatalf("乱序行情应返回 OUT_OF_ORDER, 实际: %v", err)
		}
	}
	q, err := s.Price("AAA", base)
	if err != nil || q.Price != 11 {
		t.Fatalf("乱序行情污染了快照: %+v err=%v", q, err)
	}
	t.Log("用例通过: 重复与乱序行情均被拒绝，快照未被污染")
}

// 业务：过期行情不得用于估值 —— 超过 maxAge 后 Price 返回 STALE_QUOTE。
func TestStaleQuoteRejected(t *testing.T) {
	t.Log("业务: 行情年龄超过 maxAge 视为过期，估值侧拿到明确错误而非旧价格")
	s := NewSnapshot(30 * time.Second)
	mustApply(t, s, Quote{Symbol: "AAA", Price: 10, Ts: base, Seq: 1})

	if _, err := s.Price("AAA", base.Add(31*time.Second)); !IsReason(err, ReasonStale) {
		t.Fatalf("过期行情应返回 STALE_QUOTE, 实际: %v", err)
	}
	if q, err := s.Price("AAA", base.Add(29*time.Second)); err != nil || q.Price != 10 {
		t.Fatalf("有效期内的行情应可用: %+v err=%v", q, err)
	}
	t.Log("用例通过: 过期行情被明确拒绝，有效期内行情正常可用")
}

// 业务：缺失行情与非法价格的可区分处理。
func TestMissingAndInvalid(t *testing.T) {
	t.Log("业务: 无行情标的返回 NO_QUOTE；非法价格返回 INVALID_PRICE")
	s := NewSnapshot(time.Minute)
	if _, err := s.Price("MISSING", base); !IsReason(err, ReasonNoQuote) {
		t.Fatalf("缺失行情应返回 NO_QUOTE, 实际: %v", err)
	}
	if err := s.Apply(Quote{Symbol: "AAA", Price: 0, Ts: base, Seq: 1}); !IsReason(err, ReasonInvalidPrice) {
		t.Fatalf("零价格应返回 INVALID_PRICE, 实际: %v", err)
	}
	if err := s.Apply(Quote{Symbol: "AAA", Price: -5, Ts: base, Seq: 1}); !IsReason(err, ReasonInvalidPrice) {
		t.Fatalf("负价格应返回 INVALID_PRICE, 实际: %v", err)
	}
	t.Log("用例通过: 缺失行情与非法价格均被可区分地拒绝")
}

// 业务：Clone 是深拷贝 —— 拷贝上的修改不影响原快照（并发评估前提）。
func TestCloneIsolation(t *testing.T) {
	t.Log("业务: Clone 深拷贝，拷贝上的 Apply 不影响原快照")
	s := NewSnapshot(time.Minute)
	mustApply(t, s, Quote{Symbol: "AAA", Price: 10, Ts: base, Seq: 1})

	cp := s.Clone()
	mustApply(t, cp, Quote{Symbol: "AAA", Price: 99, Ts: base, Seq: 2})

	q, err := s.Price("AAA", base)
	if err != nil || q.Price != 10 {
		t.Fatalf("原快照被拷贝污染: %+v err=%v", q, err)
	}
	t.Log("用例通过: 原快照未受拷贝修改影响")
}

func mustApply(t *testing.T, s *Snapshot, q Quote) {
	t.Helper()
	if err := s.Apply(q); err != nil {
		t.Fatalf("Apply(%+v) 失败: %v", q, err)
	}
}
