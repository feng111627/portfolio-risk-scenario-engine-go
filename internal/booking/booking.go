// Package booking 负责订单、成交与头寸的记账。
//
// 记账入口（直接 Ledger 调用或 Service 的批量 API）共享同一份状态与同一套
// 校验/计价规则，保证不同入口下的结论一致；所有成交按 FillID 幂等去重。
package booking

import (
	"context"
	"errors"
	"time"
)

// Side 表示成交/持仓方向。
type Side int

const (
	// SideBuy 买入（多头方向）。
	SideBuy Side = iota + 1
	// SideSell 卖出（空头方向）。
	SideSell
)

// String 返回方向的可读名称。
func (s Side) String() string { return sideString[s] }

var sideString = map[Side]string{SideBuy: "BUY", SideSell: "SELL"}

// OrderType 订单类型。
type OrderType int

const (
	// OrderMarket 市价单：以成交回报中的价格计价。
	OrderMarket OrderType = iota + 1
	// OrderLimit 限价单：提供 LimitPrice 作为保护价校验。
	OrderLimit
)

// OrderStatus 订单生命周期状态。
type OrderStatus int

const (
	// OrderNew 已报单、尚无成交。
	OrderNew OrderStatus = iota + 1
	// OrderPartiallyFilled 部分成交，余量有效。
	OrderPartiallyFilled
	// OrderFilled 全部成交，订单终结。
	OrderFilled
	// OrderCancelled 已撤销（可能带有部分成交），订单终结。
	OrderCancelled
	// OrderRejected 报单即被拒，订单终结。
	OrderRejected
)

// Order 是一笔委托单。
type Order struct {
	ID        string
	Symbol    string
	Side      Side
	Type      OrderType
	Qty       float64
	LimitPrice float64 // 仅限价单使用；<=0 表示无限制
	CreatedAt time.Time
}

// Fill 是一笔成交回报。
type Fill struct {
	FillID    string    // 成交回报唯一 ID（幂等键）
	OrderID   string    // 关联订单
	Symbol    string    // 成交标的；必须与订单一致
	Side      Side      // 成交方向；必须与订单一致
	Qty       float64   // 本次成交数量（>0）
	Price     float64   // 本次成交价格（>0）
	Fee       float64   // 手续费（>=0）
	Timestamp time.Time // 成交时间（用于迟到判定）
}

// FillReceipt 是成交被接受后的处理结论。
type FillReceipt struct {
	FillID      string
	OrderID     string
	Duplicate   bool    // true 表示该 FillID 曾到达过，本次为重复回报
	DuplicateOf string  // 重复时，首次接受该 FillID 的订单
	Applied     bool    // true 表示本次真正参与了计价
	RealizedPnL float64 // 本次平仓部分实现的已实现盈亏（含费用）
	PositionQty float64 // 处理后该标的的净持仓数量（带方向符号，买为正）
	AvgCost     float64 // 处理后剩余持仓的摊薄成本（始终为正的单位成本）
}

// Position 是某一标的的持仓快照。
type Position struct {
	Symbol     string
	Qty        float64 // 带方向符号的净持仓：多头为正，空头为负
	AvgCost    float64 // 摊薄单位成本（>=0；平仓不改变剩余持仓成本）
	Realized   float64 // 该标的累计已实现盈亏（含费用）
	LastFillAt time.Time
}

// IsFlat 返回持仓是否为零。
func (p Position) IsFlat() bool { return p.Qty == 0 }

// Service 是对外的记账入口（批量与单操作共享同一套内部规则）。
type Service interface {
	SubmitOrder(context.Context, Order) error
	CancelOrder(ctx context.Context, orderID string, at time.Time) error
	ApplyFill(context.Context, Fill) (FillReceipt, error)
	ApplyFills(context.Context, []Fill) ([]FillReceipt, []error)
	Position(symbol string) (Position, bool)
	Positions() map[string]Position
	Order(id string) (Order, OrderStatus, bool)
}

// 可区分的拒绝原因（用 errors.Is 判定）。
var (
	// ErrInvalidArgument 字段非法（空 ID、数量/价格非正、方向或类型未知等）。
	ErrInvalidArgument = errors.New("booking: invalid argument")
	// ErrOrderExists 同 ID 订单重复提交。
	ErrOrderExists = errors.New("booking: order already exists")
	// ErrOrderNotFound 订单不存在。
	ErrOrderNotFound = errors.New("booking: order not found")
	// ErrOrderClosed 订单已终结（全成/已撤/已拒），不得再成交或撤销。
	ErrOrderClosed = errors.New("booking: order already closed")
	// ErrFillSymbolMismatch 成交标的与订单不一致。
	ErrFillSymbolMismatch = errors.New("booking: fill symbol mismatch")
	// ErrFillSideMismatch 成交方向与订单不一致。
	ErrFillSideMismatch = errors.New("booking: fill side mismatch")
	// ErrFillOverQty 累计成交超过委托数量。
	ErrFillOverQty = errors.New("booking: cumulative fill qty exceeds order qty")
	// ErrLateFill 成交时间早于该订单上一笔已接受成交（迟到/乱序成交）。
	ErrLateFill = errors.New("booking: fill timestamp is earlier than a prior accepted fill")
	// ErrLimitPriceProtection 成交价格越过限价保护（买单高于限价/卖单低于限价）。
	ErrLimitPriceProtection = errors.New("booking: fill price breaches limit price")
	// ErrFeeNegative 手续费为负。
	ErrFeeNegative = errors.New("booking: negative fee")
)

// Ledger 是线程安全的在内存账册，所有入口共享同一实例。
type Ledger struct {
	// 空实现占位，具体字段在填充阶段添加。
}

// NewLedger 创建一个空账册。
func NewLedger() *Ledger { return &Ledger{} }

// SubmitOrder 记录一笔新订单。
func (l *Ledger) SubmitOrder(context.Context, Order) error { return nil }

// CancelOrder 撤销一笔尚未终结的订单。
func (l *Ledger) CancelOrder(ctx context.Context, orderID string, at time.Time) error {
	return nil
}

// ApplyFill 应用一笔成交回报；重复 FillID 返回幂等回执而不重复计价。
func (l *Ledger) ApplyFill(context.Context, Fill) (FillReceipt, error) {
	return FillReceipt{}, nil
}

// ApplyFills 按切片顺序逐笔应用，任一笔失败不影响其他笔。
func (l *Ledger) ApplyFills(context.Context, []Fill) ([]FillReceipt, []error) {
	return nil, nil
}

// Position 查询单个标的持仓。
func (l *Ledger) Position(symbol string) (Position, bool) { return Position{}, false }

// Positions 返回全部持仓的副本快照。
func (l *Ledger) Positions() map[string]Position { return nil }

// Order 查询订单与其状态。
func (l *Ledger) Order(id string) (Order, OrderStatus, bool) {
	return Order{}, 0, false
}

// 编译期保证 Ledger 实现 Service。
var _ Service = (*Ledger)(nil)
