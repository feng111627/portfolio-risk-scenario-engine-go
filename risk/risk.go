// Package risk 基于行情快照对头寸做估值与风险度量。
package risk

import (
	"fmt"
	"sort"
	"time"

	"github.com/feng111627/portfolio-risk-scenario-engine-go/ledger"
	"github.com/feng111627/portfolio-risk-scenario-engine-go/market"
)

// PositionValuation 是单个头寸的估值结果。
type PositionValuation struct {
	Symbol        string
	Qty           float64
	Price         float64
	MarketValue   float64 // Qty * Price（带符号）
	UnrealizedPnL float64 // Qty * (Price - AvgCost)
	RealizedPnL   float64
}

// Report 是组合层面的估值与风险报告。字段由头寸与行情唯一确定，可复现。
type Report struct {
	Positions     []PositionValuation // 按标的排序
	AsOf          time.Time
	TotalValue    float64 // 所有头寸市值之和（净额）
	GrossExposure float64 // sum(|市值|)
	NetExposure   float64 // sum(市值)
	UnrealizedPnL float64
	RealizedPnL   float64
}

// Value 按快照中 asOf 时刻的有效行情重估头寸。
// 任何头寸缺少有效行情（缺失或过期）都会返回错误，绝不静默使用错误价格。
// 输出仅取决于输入头寸与行情快照，因此可复现；对多头而言，
// 市值与未实现盈亏随行情价格单调递增。
func Value(positions []ledger.Position, snap *market.Snapshot, asOf time.Time) (Report, error) {
	sorted := make([]ledger.Position, len(positions))
	copy(sorted, positions)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Symbol < sorted[j].Symbol })

	rep := Report{AsOf: asOf, Positions: make([]PositionValuation, 0, len(sorted))}
	for _, p := range sorted {
		q, err := snap.Price(p.Symbol, asOf)
		if err != nil {
			return Report{}, fmt.Errorf("标的 %s 估值失败: %w", p.Symbol, err)
		}
		pv := PositionValuation{
			Symbol:        p.Symbol,
			Qty:           p.Qty,
			Price:         q.Price,
			MarketValue:   p.Qty * q.Price,
			UnrealizedPnL: p.Qty * (q.Price - p.AvgCost),
			RealizedPnL:   p.RealizedPnL,
		}
		rep.Positions = append(rep.Positions, pv)
		rep.TotalValue += pv.MarketValue
		rep.NetExposure += pv.MarketValue
		if pv.MarketValue < 0 {
			rep.GrossExposure -= pv.MarketValue
		} else {
			rep.GrossExposure += pv.MarketValue
		}
		rep.UnrealizedPnL += pv.UnrealizedPnL
		rep.RealizedPnL += pv.RealizedPnL
	}
	return rep, nil
}
