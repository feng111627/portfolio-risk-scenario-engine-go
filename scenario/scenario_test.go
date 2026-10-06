package scenario

import (
	"context"
	"testing"
	"time"

	"github.com/feng111627/portfolio-risk-scenario-engine-go/ledger"
	"github.com/feng111627/portfolio-risk-scenario-engine-go/market"
)

var base = time.Date(2026, 10, 7, 10, 0, 0, 0, time.UTC)

func fixture(t *testing.T) ([]ledger.Position, *market.Snapshot) {
	t.Helper()
	positions := []ledger.Position{
		{Symbol: "AAA", Qty: 100, AvgCost: 10},
		{Symbol: "BBB", Qty: -50, AvgCost: 20},
		{Symbol: "CCC", Qty: 200, AvgCost: 5},
	}
	snap := market.NewSnapshot(time.Minute)
	for i, q := range []market.Quote{
		{Symbol: "AAA", Price: 12, Ts: base},
		{Symbol: "BBB", Price: 18, Ts: base},
		{Symbol: "CCC", Price: 6, Ts: base},
	} {
		q.Seq = int64(i + 1)
		if err := snap.Apply(q); err != nil {
			t.Fatalf("构造行情失败: %v", err)
		}
	}
	return positions, snap
}

func scenarios() []Scenario {
	return []Scenario{
		{Name: "股市暴跌", Shocks: []Shock{{Symbol: "AAA", Pct: -0.2}, {Symbol: "CCC", Pct: -0.3}}},
		{Name: "全面上涨", Shocks: []Shock{{Symbol: "AAA", Pct: 0.1}, {Symbol: "BBB", Pct: 0.1}, {Symbol: "CCC", Pct: 0.1}}},
		{Name: "空头回补", Shocks: []Shock{{Symbol: "BBB", Pct: 0.25}}},
		{Name: "无冲击基准"},
	}
}

// 业务：并发评估结果必须与串行评估完全一致（同顺序、同数值）。
func TestParallelMatchesSerial(t *testing.T) {
	t.Log("业务: 同一批情景分别串行与并发评估，结果逐字段一致")
	positions, snap := fixture(t)
	scs := scenarios()

	serial, err := EvaluateAll(context.Background(), positions, snap, base, scs)
	if err != nil {
		t.Fatalf("串行评估失败: %v", err)
	}
	for _, workers := range []int{1, 2, 8} {
		parallel, err := EvaluateParallel(context.Background(), positions, snap, base, scs, workers)
		if err != nil {
			t.Fatalf("并发评估(workers=%d)失败: %v", workers, err)
		}
		if len(parallel) != len(serial) {
			t.Fatalf("workers=%d 结果数量不一致: %d vs %d", workers, len(parallel), len(serial))
		}
		for i := range serial {
			s, p := serial[i], parallel[i]
			if s.Scenario != p.Scenario ||
				s.Report.TotalValue != p.Report.TotalValue ||
				s.Report.GrossExposure != p.Report.GrossExposure ||
				s.Report.UnrealizedPnL != p.Report.UnrealizedPnL ||
				s.PnLImpact != p.PnLImpact {
				t.Fatalf("workers=%d 情景 %q 结果不一致:\n串行 %+v\n并发 %+v", workers, s.Scenario, s, p)
			}
		}
		t.Logf("用例通过: workers=%d 与串行结果完全一致", workers)
	}
}

// 业务：情景冲击的 PnL 影响可解释 —— 等于各标的价格变动 * 头寸之和。
func TestPnLImpactExplainable(t *testing.T) {
	t.Log("业务: 校验 PnLImpact 等于 sum(头寸 * 基准价 * 冲击幅度)")
	positions, snap := fixture(t)
	scs := []Scenario{{Name: "股市暴跌", Shocks: []Shock{{Symbol: "AAA", Pct: -0.2}, {Symbol: "CCC", Pct: -0.3}}}}

	results, err := EvaluateAll(context.Background(), positions, snap, base, scs)
	if err != nil {
		t.Fatalf("评估失败: %v", err)
	}
	// AAA: 100*12*(-0.2) = -240; CCC: 200*6*(-0.3) = -360; 合计 -600
	if got := results[0].PnLImpact; got != -600 {
		t.Fatalf("PnLImpact 错误: got %v want -600", got)
	}
	t.Logf("用例通过: PnLImpact=%v 与手工核算一致", results[0].PnLImpact)
}

// 业务：单个情景失败（冲击了无行情的标的）不污染其他情景的结果。
func TestFailureIsolation(t *testing.T) {
	t.Log("业务: 含非法冲击的情景单独失败，其余情景结果正常产出")
	positions, snap := fixture(t)
	scs := []Scenario{
		{Name: "正常情景", Shocks: []Shock{{Symbol: "AAA", Pct: -0.1}}},
		{Name: "坏情景-无行情", Shocks: []Shock{{Symbol: "NOPE", Pct: -0.1}}},
		{Name: "坏情景-非法幅度", Shocks: []Shock{{Symbol: "AAA", Pct: -1.5}}},
	}

	results, err := EvaluateParallel(context.Background(), positions, snap, base, scs, 3)
	if err != nil {
		t.Fatalf("单情景失败不应导致整体失败: %v", err)
	}
	if results[0].Err != nil {
		t.Fatalf("正常情景不应失败: %v", results[0].Err)
	}
	if results[1].Err == nil || !market.IsReason(results[1].Err, market.ReasonNoQuote) {
		t.Fatalf("无行情冲击应报 NO_QUOTE, 实际: %v", results[1].Err)
	}
	if results[2].Err == nil {
		t.Fatal("非法冲击幅度应报错")
	}
	if results[0].PnLImpact != -120 { // 100*12*(-0.1)
		t.Fatalf("正常情景结果被污染: PnLImpact=%v", results[0].PnLImpact)
	}
	t.Log("用例通过: 失败情景以 Err 隔离，正常情景结果正确")
}

// 业务：中途取消不得产出任何结果。
func TestCancellationYieldsNoResults(t *testing.T) {
	t.Log("业务: ctx 已取消时并发评估返回错误且无结果")
	positions, snap := fixture(t)
	scs := scenarios()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results, err := EvaluateParallel(ctx, positions, snap, base, scs, 4)
	if err == nil {
		t.Fatal("取消后应返回错误")
	}
	if results != nil {
		t.Fatalf("取消后不得产出结果, 实际: %+v", results)
	}
	if _, err := EvaluateAll(ctx, positions, snap, base, scs); err == nil {
		t.Fatal("串行评估在取消后也应返回错误")
	}
	t.Logf("用例通过: 取消时返回错误 %v，无结果产出", err)
}

// 业务：并发评估不得修改输入快照（竞态检测下验证）。
func TestParallelDoesNotMutateInputs(t *testing.T) {
	t.Log("业务: 并发评估后输入快照与头寸保持不变")
	positions, snap := fixture(t)
	scs := scenarios()

	if _, err := EvaluateParallel(context.Background(), positions, snap, base, scs, 8); err != nil {
		t.Fatalf("并发评估失败: %v", err)
	}
	for sym, want := range map[string]float64{"AAA": 12, "BBB": 18, "CCC": 6} {
		q, err := snap.Price(sym, base)
		if err != nil || q.Price != want {
			t.Fatalf("输入快照被污染: %s price=%v err=%v", sym, q.Price, err)
		}
	}
	if positions[0].Qty != 100 || positions[1].Qty != -50 || positions[2].Qty != 200 {
		t.Fatalf("输入头寸被污染: %+v", positions)
	}
	t.Log("用例通过: 输入快照与头寸在并发评估后未被修改")
}
