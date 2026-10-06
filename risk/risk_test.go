package risk

import (
	"testing"
	"time"

	"github.com/feng111627/portfolio-risk-scenario-engine-go/ledger"
	"github.com/feng111627/portfolio-risk-scenario-engine-go/market"
)

var base = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)

func snapWith(t *testing.T, quotes ...market.Quote) *market.Snapshot {
	t.Helper()
	s := market.NewSnapshot(time.Minute)
	for _, q := range quotes {
		if err := s.Apply(q); err != nil {
			t.Fatalf("Apply(%+v) 失败: %v", q, err)
		}
	}
	return s
}

// 业务：基于最新有效行情的估值 —— 市值、敞口与盈亏可复现。
func TestValuationDeterministic(t *testing.T) {
	t.Log("业务: 多头+空头组合按快照估值，两次估值结果完全一致（可复现）")
	positions := []ledger.Position{
		{Symbol: "AAA", Qty: 100, AvgCost: 10, RealizedPnL: 50},
		{Symbol: "BBB", Qty: -40, AvgCost: 20},
	}
	snap := snapWith(t,
		market.Quote{Symbol: "AAA", Price: 12, Ts: base, Seq: 1},
		market.Quote{Symbol: "BBB", Price: 18, Ts: base, Seq: 1},
	)
	r1, err := Value(positions, snap, base)
	if err != nil {
		t.Fatalf("估值失败: %v", err)
	}
	r2, err := Value(positions, snap, base)
	if err != nil {
		t.Fatalf("估值失败: %v", err)
	}
	if r1.TotalValue != r2.TotalValue || r1.GrossExposure != r2.GrossExposure || r1.UnrealizedPnL != r2.UnrealizedPnL {
		t.Fatalf("两次估值不一致: %+v vs %+v", r1, r2)
	}
	// AAA: 100*12=1200; BBB: -40*18=-720
	if r1.TotalValue != 480 || r1.NetExposure != 480 || r1.GrossExposure != 1920 {
		t.Fatalf("敞口计算错误: %+v", r1)
	}
	// AAA 未实现: 100*(12-10)=200; BBB 未实现: -40*(18-20)=80
	if r1.UnrealizedPnL != 280 || r1.RealizedPnL != 50 {
		t.Fatalf("盈亏计算错误: %+v", r1)
	}
	if r1.Positions[0].Symbol != "AAA" || r1.Positions[1].Symbol != "BBB" {
		t.Fatalf("头寸输出未按标的排序: %+v", r1.Positions)
	}
	t.Logf("用例通过: TotalValue=%v Gross=%v Unrealized=%v", r1.TotalValue, r1.GrossExposure, r1.UnrealizedPnL)
}

// 业务：行情缺失或过期时估值必须失败，不得静默使用错误价格。
func TestValuationFailsOnBadQuotes(t *testing.T) {
	t.Log("业务: 缺失行情与过期行情都使估值返回可区分错误")
	positions := []ledger.Position{{Symbol: "AAA", Qty: 100, AvgCost: 10}}

	snapMissing := snapWith(t)
	if _, err := Value(positions, snapMissing, base); !market.IsReason(err, market.ReasonNoQuote) {
		t.Fatalf("缺失行情应导致 NO_QUOTE, 实际: %v", err)
	}

	snapStale := snapWith(t, market.Quote{Symbol: "AAA", Price: 10, Ts: base.Add(-2 * time.Minute), Seq: 1})
	if _, err := Value(positions, snapStale, base); !market.IsReason(err, market.ReasonStale) {
		t.Fatalf("过期行情应导致 STALE_QUOTE, 实际: %v", err)
	}
	t.Log("用例通过: 缺失/过期行情均被明确拒绝，未静默计价")
}

// 业务：风险指标随行情单调 —— 多头市值与未实现盈亏随价格上升而上升。
func TestMonotonicInPrice(t *testing.T) {
	t.Log("业务: 多头组合的总市值与未实现盈亏随行情价格单调递增")
	positions := []ledger.Position{{Symbol: "AAA", Qty: 100, AvgCost: 10}}
	var prevVal, prevPnL float64
	for i, price := range []float64{8, 10, 12, 15} {
		snap := snapWith(t, market.Quote{Symbol: "AAA", Price: price, Ts: base, Seq: 1})
		r, err := Value(positions, snap, base)
		if err != nil {
			t.Fatalf("估值失败: %v", err)
		}
		if i > 0 && (r.TotalValue <= prevVal || r.UnrealizedPnL <= prevPnL) {
			t.Fatalf("价格 %v 处单调性被破坏: val=%v pnl=%v", price, r.TotalValue, r.UnrealizedPnL)
		}
		prevVal, prevPnL = r.TotalValue, r.UnrealizedPnL
	}
	t.Log("用例通过: 价格 8->10->12->15 下市值与盈亏严格单调递增")
}
