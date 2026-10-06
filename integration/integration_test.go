// Package integration 端到端验证：记账 -> 行情 -> 估值 -> 情景评估
// 各入口产出的头寸、估值与风险结论保持一致。
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/feng111627/portfolio-risk-scenario-engine-go/ledger"
	"github.com/feng111627/portfolio-risk-scenario-engine-go/market"
	"github.com/feng111627/portfolio-risk-scenario-engine-go/risk"
	"github.com/feng111627/portfolio-risk-scenario-engine-go/scenario"
)

var base = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)

// 业务：同一批事件流经记账、估值、情景评估三个入口后结论一致。
func TestEndToEndConsistency(t *testing.T) {
	t.Log("业务: 事件流(含重复/迟到成交) -> 头寸 -> 估值 -> 情景，各入口结论一致")
	l := ledger.New(false)

	// 事件流：开仓、部分成交、撤单、迟到成交、重复成交。
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("事件流执行失败: %v", err)
		}
	}
	must(l.PlaceOrder(ledger.Order{ID: "O1", Symbol: "AAA", Side: ledger.Buy, Qty: 100}))
	if _, err := l.ApplyFill(ledger.Fill{ID: "F1", OrderID: "O1", Symbol: "AAA", Side: ledger.Buy, Qty: 60, Price: 10}); err != nil {
		t.Fatalf("F1 失败: %v", err)
	}
	if _, err := l.CancelOrder("O1"); err != nil {
		t.Fatalf("撤单失败: %v", err)
	}
	// 迟到成交：必须被拒绝且不影响头寸。
	if _, err := l.ApplyFill(ledger.Fill{ID: "F2", OrderID: "O1", Symbol: "AAA", Side: ledger.Buy, Qty: 40, Price: 10}); !ledger.IsReason(err, ledger.ReasonLateFill) {
		t.Fatalf("迟到成交应被拒绝, 实际: %v", err)
	}
	// 重复成交：幂等忽略。
	r, err := l.ApplyFill(ledger.Fill{ID: "F1", OrderID: "O1", Symbol: "AAA", Side: ledger.Buy, Qty: 60, Price: 10})
	if err != nil || !r.Duplicate {
		t.Fatalf("重复成交应幂等忽略: r=%+v err=%v", r, err)
	}
	// 平仓一半。
	must(l.PlaceOrder(ledger.Order{ID: "O2", Symbol: "AAA", Side: ledger.Sell, Qty: 30}))
	if _, err := l.ApplyFill(ledger.Fill{ID: "F3", OrderID: "O2", Symbol: "AAA", Side: ledger.Sell, Qty: 30, Price: 14}); err != nil {
		t.Fatalf("F3 失败: %v", err)
	}

	// 入口一：账本直接给出的头寸与已实现盈亏。
	pos := l.Position("AAA")
	if pos.Qty != 30 || pos.AvgCost != 10 || pos.RealizedPnL != 30*(14.0-10.0) {
		t.Fatalf("账本头寸错误: %+v", pos)
	}
	t.Logf("入口一(账本): 头寸=%v 成本=%v 已实现=%v", pos.Qty, pos.AvgCost, pos.RealizedPnL)

	// 入口二：估值报告中的同一头寸必须与账本一致。
	snap := market.NewSnapshot(time.Minute)
	must(snap.Apply(market.Quote{Symbol: "AAA", Price: 15, Ts: base, Seq: 1}))
	rep, err := risk.Value(l.Positions(), snap, base)
	if err != nil {
		t.Fatalf("估值失败: %v", err)
	}
	pv := rep.Positions[0]
	if pv.Qty != pos.Qty || pv.RealizedPnL != pos.RealizedPnL {
		t.Fatalf("估值入口与账本入口不一致: %+v vs %+v", pv, pos)
	}
	if pv.UnrealizedPnL != 30*(15.0-10.0) || rep.TotalValue != 30*15.0 {
		t.Fatalf("估值数值错误: %+v", pv)
	}
	t.Logf("入口二(估值): 市值=%v 未实现=%v 已实现=%v", pv.MarketValue, pv.UnrealizedPnL, pv.RealizedPnL)

	// 入口三：情景评估基于同一头寸与行情，冲击影响可解释。
	scs := []scenario.Scenario{{Name: "跌20%", Shocks: []scenario.Shock{{Symbol: "AAA", Pct: -0.2}}}}
	results, err := scenario.EvaluateAll(context.Background(), l.Positions(), snap, base, scs)
	if err != nil {
		t.Fatalf("情景评估失败: %v", err)
	}
	if results[0].Err != nil {
		t.Fatalf("情景评估失败: %v", results[0].Err)
	}
	if results[0].PnLImpact != 30*15.0*(-0.2) {
		t.Fatalf("情景冲击不可解释: got %v want %v", results[0].PnLImpact, -90.0)
	}
	if results[0].Report.RealizedPnL != rep.RealizedPnL {
		t.Fatalf("情景入口的已实现盈亏与估值入口不一致: %v vs %v", results[0].Report.RealizedPnL, rep.RealizedPnL)
	}
	t.Logf("入口三(情景): PnLImpact=%v 已实现=%v", results[0].PnLImpact, results[0].Report.RealizedPnL)
	t.Log("用例通过: 三个入口下的头寸、估值与风险结论完全一致")
}

// 业务：行情过期时所有估值入口一致拒绝，不得静默使用旧价。
func TestStaleQuoteRejectedEverywhere(t *testing.T) {
	t.Log("业务: 行情过期后，估值与情景评估两个入口都明确失败")
	l := ledger.New(true)
	if err := l.PlaceOrder(ledger.Order{ID: "O1", Symbol: "AAA", Side: ledger.Buy, Qty: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ApplyFill(ledger.Fill{ID: "F1", OrderID: "O1", Symbol: "AAA", Side: ledger.Buy, Qty: 10, Price: 10}); err != nil {
		t.Fatal(err)
	}
	snap := market.NewSnapshot(time.Minute)
	if err := snap.Apply(market.Quote{Symbol: "AAA", Price: 10, Ts: base, Seq: 1}); err != nil {
		t.Fatal(err)
	}
	asOf := base.Add(2 * time.Minute) // 超过 maxAge

	if _, err := risk.Value(l.Positions(), snap, asOf); !market.IsReason(err, market.ReasonStale) {
		t.Fatalf("估值入口应拒绝过期行情, 实际: %v", err)
	}
	scs := []scenario.Scenario{{Name: "s", Shocks: []scenario.Shock{{Symbol: "AAA", Pct: 0.1}}}}
	if _, err := scenario.EvaluateAll(context.Background(), l.Positions(), snap, asOf, scs); !market.IsReason(err, market.ReasonStale) {
		t.Fatalf("情景入口应拒绝过期行情, 实际: %v", err)
	}
	t.Log("用例通过: 两个入口一致拒绝过期行情，未静默计价")
}
