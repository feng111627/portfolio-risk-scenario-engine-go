// Package scenario 提供情景/压力场景的串行与并发评估。
package scenario

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/feng111627/portfolio-risk-scenario-engine-go/ledger"
	"github.com/feng111627/portfolio-risk-scenario-engine-go/market"
	"github.com/feng111627/portfolio-risk-scenario-engine-go/risk"
)

// Shock 是对单个标的价格的冲击，Pct 为相对变动（如 -0.1 表示跌 10%）。
// 冲击作用于基准快照中的有效价格；同一标的出现多个冲击时以最后一个为准。
type Shock struct {
	Symbol string
	Pct    float64
}

// Scenario 是一组价格冲击的命名集合。
type Scenario struct {
	Name   string
	Shocks []Shock
}

// Result 是单个情景的评估结果。Err 非空表示该情景评估失败，
// 不影响其他情景的结果。
type Result struct {
	Scenario  string
	Report    risk.Report
	PnLImpact float64 // 情景总市值 - 基准总市值
	Err       error
}

// EvaluateAll 串行评估所有情景，结果顺序与输入一致。
func EvaluateAll(ctx context.Context, positions []ledger.Position, snap *market.Snapshot, asOf time.Time, scenarios []Scenario) ([]Result, error) {
	base, err := risk.Value(positions, snap, asOf)
	if err != nil {
		return nil, fmt.Errorf("基准估值失败: %w", err)
	}
	results := make([]Result, len(scenarios))
	for i, sc := range scenarios {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("情景评估被取消: %w", err)
		}
		results[i] = evaluate(positions, snap, asOf, sc, base.TotalValue)
	}
	return results, nil
}

// EvaluateParallel 并发评估所有情景。
// 结果与 EvaluateAll 完全一致（同顺序、同数值）；
// ctx 取消时返回错误且不产出任何结果。
// 每个情景在独立的快照副本上评估，输入只读，无共享可变状态。
func EvaluateParallel(ctx context.Context, positions []ledger.Position, snap *market.Snapshot, asOf time.Time, scenarios []Scenario, workers int) ([]Result, error) {
	if workers < 1 {
		workers = 1
	}
	base, err := risk.Value(positions, snap, asOf)
	if err != nil {
		return nil, fmt.Errorf("基准估值失败: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("情景评估被取消: %w", err)
	}

	results := make([]Result, len(scenarios))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil { // 已取消：丢弃剩余任务，等待统一返回错误
					continue
				}
				results[i] = evaluate(positions, snap, asOf, scenarios[i], base.TotalValue)
			}
		}()
	}
feed:
	for i := range scenarios {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("情景评估被取消: %w", err)
	}
	return results, nil
}

// evaluate 在快照副本上施加冲击并估值，单个情景的失败以 Result.Err 返回。
func evaluate(positions []ledger.Position, snap *market.Snapshot, asOf time.Time, sc Scenario, baseValue float64) Result {
	shocked := snap.Clone()
	for _, sh := range sc.Shocks {
		if sh.Pct <= -1 {
			return Result{Scenario: sc.Name, Err: fmt.Errorf("情景 %s: 标的 %s 冲击幅度 %v 非法（必须 > -1）", sc.Name, sh.Symbol, sh.Pct)}
		}
		q, err := snap.Price(sh.Symbol, asOf)
		if err != nil {
			return Result{Scenario: sc.Name, Err: fmt.Errorf("情景 %s: 无法获取标的 %s 基准行情: %w", sc.Name, sh.Symbol, err)}
		}
		err = shocked.Apply(market.Quote{
			Symbol: sh.Symbol,
			Price:  q.Price * (1 + sh.Pct),
			Ts:     q.Ts, // 保持原行情时间戳，有效性口径与基准一致
			Seq:    q.Seq + 1,
		})
		if err != nil {
			return Result{Scenario: sc.Name, Err: fmt.Errorf("情景 %s: 施加冲击失败: %w", sc.Name, err)}
		}
	}
	rep, err := risk.Value(positions, shocked, asOf)
	if err != nil {
		return Result{Scenario: sc.Name, Err: fmt.Errorf("情景 %s: 估值失败: %w", sc.Name, err)}
	}
	return Result{Scenario: sc.Name, Report: rep, PnLImpact: rep.TotalValue - baseValue}
}
