// Package valuation 基于最新“有效”行情对持仓进行重估，
// 产出每标的与组合层面的市值、未实现盈亏；行情缺失/过期时默认整体报错，
// 绝不静默使用陈旧或错误价格。
package valuation

import (
	"context"
	"errors"

	"github.com/feng111627/portfolio-risk-scenario-engine-go/internal/booking"
)

// PriceSource 是估值使用的只读价格来源（now 时刻的有效价格）。
type PriceSource interface {
	PriceAt(symbol string, now interface{}) (float64, bool)
}

// Result 是组合层面的估值结果。
type Result struct {
	Positions map[string]PositionValue
	MV        float64 // 组合总市值（带方向，空头为负）
	CostBasis float64 // 剩余持仓总成本（按 |Qty|*AvgCost 汇总）
	Unrealized float64 // 组合未实现盈亏合计
	Time      interface{}
}

// PositionValue 是单标的估值结果。
type PositionValue struct {
	Position booking.Position
	Price     float64
	MV        float64 // Qty * Price（空头为负）
	CostBasis float64 // |Qty| * AvgCost
	Unrealized float64 // 平仓口径的未实现盈亏：(Price-AvgCost)*Qty
}

// Option 配置估值器。
type Option func(*Valuator)

// Valuator 按严格行情口径重估持仓。
type Valuator struct {
	// 空实现占位。
}

// NewValuator 创建估值器；默认严格口径（任何标的缺价即整体失败）。
func NewValuator(market interface{}, now interface{}, opts ...Option) *Valuator {
	return &Valuator{}
}

// WithAllowMissing 允许部分标的缺价：缺失标的被跳过并在结果中显式列出。
func WithAllowMissing() Option { return func(*Valuator) {} }

// Value 对给定持仓快照进行重估。
func (v *Valuator) Value(context.Context, map[string]booking.Position) (Result, error) {
	return Result{}, nil
}

// Missing 返回最近一次估值中因缺失/过期行情而被跳过的标的。
func (v *Valuator) Missing() []string { return nil }

// 估值错误。
var (
	// ErrNoPrice 某持仓标的在 now 时刻没有有效行情（严格口径下整体失败）。
	ErrNoPrice = errors.New("valuation: no valid market price for position")
)
