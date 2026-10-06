// Package ledger 负责投资组合的记账：订单、成交、头寸与盈亏。
package ledger

// Side 表示买卖方向。
type Side int

const (
	Buy Side = iota + 1
	Sell
)

// OrderStatus 表示订单生命周期状态。
type OrderStatus int

const (
	StatusOpen OrderStatus = iota + 1
	StatusPartiallyFilled
	StatusFilled
	StatusCancelled
)

// Order 是一笔委托。
type Order struct {
	ID        string
	Symbol    string
	Side      Side
	Qty       float64 // 原始委托数量，恒为正
	FilledQty float64 // 累计成交数量
	Status    OrderStatus
}

// Remaining 返回剩余可成交数量。
func (o Order) Remaining() float64 { return o.Qty - o.FilledQty }

// Fill 是一笔成交回报。ID 全局唯一，用于去重。
type Fill struct {
	ID      string
	OrderID string
	Symbol  string
	Side    Side
	Qty     float64
	Price   float64
}

// Position 是某个标的的净头寸与盈亏。
type Position struct {
	Symbol       string
	Qty          float64 // 带符号：正为多头，负为空头
	AvgCost      float64 // 平均持仓成本（开仓方向的价格均值）
	RealizedPnL  float64 // 累计已实现盈亏
}

// FillResult 描述一笔成交回报的处理结果。
type FillResult struct {
	Applied       bool     // 是否真正入账
	Duplicate     bool     // 是否为重复到达（幂等忽略）
	Position      Position // 处理后该标的的头寸
	RealizedDelta float64  // 本次成交新增的已实现盈亏
}
