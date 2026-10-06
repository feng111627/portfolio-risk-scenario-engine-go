package ledger

import (
	"fmt"
	"sort"
)

// Ledger 是记账核心：维护订单、成交与头寸。
// 非并发安全；并发使用由调用方加锁。
type Ledger struct {
	orders     map[string]*Order
	fills      map[string]struct{}
	positions  map[string]*Position
	allowShort bool
}

// New 创建账本。allowShort 为 false 时，卖出超过持仓将被拒绝。
func New(allowShort bool) *Ledger {
	return &Ledger{
		orders:     make(map[string]*Order),
		fills:      make(map[string]struct{}),
		positions:  make(map[string]*Position),
		allowShort: allowShort,
	}
}

// PlaceOrder 登记一笔委托。
func (l *Ledger) PlaceOrder(o Order) error {
	if o.ID == "" {
		return &Error{Reason: ReasonInvalidQuantity, Msg: "订单 ID 不能为空"}
	}
	if _, dup := l.orders[o.ID]; dup {
		return &Error{Reason: ReasonDuplicateOrder, Msg: fmt.Sprintf("订单 %s 已存在", o.ID)}
	}
	if o.Side != Buy && o.Side != Sell {
		return &Error{Reason: ReasonInvalidSide, Msg: fmt.Sprintf("非法买卖方向: %d", o.Side)}
	}
	if o.Qty <= 0 {
		return &Error{Reason: ReasonInvalidQuantity, Msg: fmt.Sprintf("委托数量必须为正: %v", o.Qty)}
	}
	if o.Symbol == "" {
		return &Error{Reason: ReasonInvalidQuantity, Msg: "标的代码不能为空"}
	}
	cp := o
	cp.FilledQty = 0
	cp.Status = StatusOpen
	l.orders[o.ID] = &cp
	return nil
}

// CancelOrder 撤销一笔未完成的委托，返回撤销后的订单快照。
func (l *Ledger) CancelOrder(id string) (Order, error) {
	o, ok := l.orders[id]
	if !ok {
		return Order{}, &Error{Reason: ReasonUnknownOrder, Msg: fmt.Sprintf("订单 %s 不存在", id)}
	}
	if o.Status != StatusOpen && o.Status != StatusPartiallyFilled {
		return Order{}, &Error{Reason: ReasonOrderNotOpen, Msg: fmt.Sprintf("订单 %s 状态为 %v，不可撤销", id, o.Status)}
	}
	o.Status = StatusCancelled
	return *o, nil
}

// ApplyFill 应用一笔成交回报；重复到达的成交幂等忽略。
func (l *Ledger) ApplyFill(f Fill) (FillResult, error) {
	// 1. 幂等去重：同一 Fill.ID 只计价一次。
	if _, seen := l.fills[f.ID]; seen {
		pos := l.Position(f.Symbol)
		return FillResult{Applied: false, Duplicate: true, Position: pos}, nil
	}
	// 2. 基本校验。
	if f.Qty <= 0 {
		return FillResult{}, &Error{Reason: ReasonInvalidQuantity, Msg: fmt.Sprintf("成交数量必须为正: %v", f.Qty)}
	}
	if f.Price <= 0 {
		return FillResult{}, &Error{Reason: ReasonInvalidPrice, Msg: fmt.Sprintf("成交价格必须为正: %v", f.Price)}
	}
	// 3. 关联订单。
	o, ok := l.orders[f.OrderID]
	if !ok {
		return FillResult{}, &Error{Reason: ReasonUnknownOrder, Msg: fmt.Sprintf("成交 %s 引用的订单 %s 不存在", f.ID, f.OrderID)}
	}
	if o.Symbol != f.Symbol || o.Side != f.Side {
		return FillResult{}, &Error{Reason: ReasonSymbolMismatch, Msg: fmt.Sprintf("成交 %s 的标的/方向与订单 %s 不一致", f.ID, f.OrderID)}
	}
	// 4. 订单状态：已撤销的订单再收到成交视为迟到成交，拒绝。
	if o.Status == StatusCancelled {
		return FillResult{}, &Error{Reason: ReasonLateFill, Msg: fmt.Sprintf("订单 %s 已撤销，成交 %s 为迟到成交", f.OrderID, f.ID)}
	}
	if o.Status == StatusFilled {
		return FillResult{}, &Error{Reason: ReasonOrderNotOpen, Msg: fmt.Sprintf("订单 %s 已完全成交", f.OrderID)}
	}
	// 5. 数量约束：不得超量成交。
	if f.Qty > o.Remaining() {
		return FillResult{}, &Error{Reason: ReasonOverFill, Msg: fmt.Sprintf("成交数量 %v 超过订单 %s 剩余数量 %v", f.Qty, o.ID, o.Remaining())}
	}
	// 6. 头寸约束：不允许卖空时，卖出不得超过当前持仓。
	signed := f.Qty
	if f.Side == Sell {
		signed = -signed
	}
	pos := l.position(f.Symbol)
	if !l.allowShort && pos.Qty+signed < 0 {
		return FillResult{}, &Error{Reason: ReasonInsufficientPosition, Msg: fmt.Sprintf("卖出 %v 后头寸将为 %v，不允许卖空", f.Qty, pos.Qty+signed)}
	}

	// 7. 入账：更新订单、头寸与盈亏。
	realized := pos.apply(signed, f.Price)
	o.FilledQty += f.Qty
	switch {
	case o.FilledQty == o.Qty:
		o.Status = StatusFilled
	default:
		o.Status = StatusPartiallyFilled
	}
	l.fills[f.ID] = struct{}{}
	return FillResult{Applied: true, Position: *pos, RealizedDelta: realized}, nil
}

// Order 查询订单。
func (l *Ledger) Order(id string) (Order, bool) {
	o, ok := l.orders[id]
	if !ok {
		return Order{}, false
	}
	return *o, true
}

// Position 查询单个标的的头寸。
func (l *Ledger) Position(symbol string) Position {
	return *l.position(symbol)
}

// Positions 返回全部非零头寸（按标的排序，保证可复现）。
func (l *Ledger) Positions() []Position {
	out := make([]Position, 0, len(l.positions))
	for _, p := range l.positions {
		if p.Qty != 0 || p.RealizedPnL != 0 {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out
}

// position 返回内部头寸对象（不存在则创建零头寸）。
func (l *Ledger) position(symbol string) *Position {
	p, ok := l.positions[symbol]
	if !ok {
		p = &Position{Symbol: symbol}
		l.positions[symbol] = p
	}
	return p
}

// apply 按平均成本法把一笔带符号成交应用到头寸上，返回本次新增的已实现盈亏。
//
// 规则：
//   - 同向加仓：更新平均成本，无已实现盈亏；
//   - 反向减仓/平仓：按平均成本结转已实现盈亏；
//   - 反向超过持仓（翻仓）：先按平均成本结清旧方向的全部盈亏，
//     剩余数量以成交价作为新方向的开仓成本。
func (p *Position) apply(signedQty, price float64) (realized float64) {
	switch {
	case p.Qty == 0 || (p.Qty > 0) == (signedQty > 0):
		// 开仓或同向加仓：加权平均成本。
		total := abs(p.Qty) + abs(signedQty)
		p.AvgCost = (p.AvgCost*abs(p.Qty) + price*abs(signedQty)) / total
		p.Qty += signedQty
		return 0
	default:
		// 反向成交。
		closing := min(abs(signedQty), abs(p.Qty))
		if p.Qty > 0 {
			realized = closing * (price - p.AvgCost)
		} else {
			realized = closing * (p.AvgCost - price)
		}
		p.RealizedPnL += realized
		p.Qty += signedQty
		switch {
		case p.Qty == 0:
			p.AvgCost = 0
		case (p.Qty > 0) != (signedQty > 0):
			// 未翻仓，仅减仓：平均成本不变。
		default:
			// 翻仓：剩余数量以成交价重新开仓。
			p.AvgCost = price
		}
		return realized
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
