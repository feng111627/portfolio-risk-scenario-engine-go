package ledger

import "testing"

func mustOrder(t *testing.T, l *Ledger, id, sym string, side Side, qty float64) {
	t.Helper()
	if err := l.PlaceOrder(Order{ID: id, Symbol: sym, Side: side, Qty: qty}); err != nil {
		t.Fatalf("PlaceOrder(%s) 失败: %v", id, err)
	}
}

func mustFill(t *testing.T, l *Ledger, fid, oid, sym string, side Side, qty, price float64) FillResult {
	t.Helper()
	r, err := l.ApplyFill(Fill{ID: fid, OrderID: oid, Symbol: sym, Side: side, Qty: qty, Price: price})
	if err != nil {
		t.Fatalf("ApplyFill(%s) 失败: %v", fid, err)
	}
	if !r.Applied {
		t.Fatalf("ApplyFill(%s) 未入账", fid)
	}
	return r
}

// 业务：开仓与部分成交 —— 头寸与平均成本随部分成交逐步累积。
func TestPartialFillsAccumulate(t *testing.T) {
	t.Log("业务: 开仓 + 两次部分成交，校验头寸数量与加权平均成本")
	l := New(false)
	mustOrder(t, l, "O1", "AAA", Buy, 100)

	r := mustFill(t, l, "F1", "O1", "AAA", Buy, 40, 10.0)
	if r.Position.Qty != 40 || r.Position.AvgCost != 10.0 {
		t.Fatalf("首次部分成交后头寸错误: %+v", r.Position)
	}
	o, _ := l.Order("O1")
	if o.Status != StatusPartiallyFilled {
		t.Fatalf("部分成交后订单状态应为 PartiallyFilled, 实际 %v", o.Status)
	}

	r = mustFill(t, l, "F2", "O1", "AAA", Buy, 60, 12.0)
	wantCost := (40*10.0 + 60*12.0) / 100
	if r.Position.Qty != 100 || r.Position.AvgCost != wantCost {
		t.Fatalf("加权平均成本错误: got %+v want cost %v", r.Position, wantCost)
	}
	o, _ = l.Order("O1")
	if o.Status != StatusFilled {
		t.Fatalf("全部成交后订单状态应为 Filled, 实际 %v", o.Status)
	}
	t.Logf("用例通过: 头寸=%v 平均成本=%v 订单状态=%v", r.Position.Qty, r.Position.AvgCost, o.Status)
}

// 业务：平仓产生已实现盈亏，未实现盈亏由估值侧按成本价计算。
func TestClosePositionRealizedPnL(t *testing.T) {
	t.Log("业务: 买入后部分卖出，校验平均成本法结转的已实现盈亏")
	l := New(false)
	mustOrder(t, l, "O1", "AAA", Buy, 100)
	mustFill(t, l, "F1", "O1", "AAA", Buy, 100, 10.0)

	mustOrder(t, l, "O2", "AAA", Sell, 30)
	r := mustFill(t, l, "F2", "O2", "AAA", Sell, 30, 15.0)
	if r.RealizedDelta != 30*(15.0-10.0) {
		t.Fatalf("已实现盈亏错误: got %v want %v", r.RealizedDelta, 150.0)
	}
	if r.Position.Qty != 70 || r.Position.AvgCost != 10.0 {
		t.Fatalf("减仓后头寸错误: %+v", r.Position)
	}
	t.Logf("用例通过: 已实现盈亏=%v 剩余头寸=%v", r.RealizedDelta, r.Position.Qty)
}

// 业务：重复到达的成交不得重复计价（幂等）。
func TestDuplicateFillIdempotent(t *testing.T) {
	t.Log("业务: 同一成交回报重复到达，第二次必须幂等忽略且不计价")
	l := New(false)
	mustOrder(t, l, "O1", "AAA", Buy, 100)
	mustFill(t, l, "F1", "O1", "AAA", Buy, 50, 10.0)

	r, err := l.ApplyFill(Fill{ID: "F1", OrderID: "O1", Symbol: "AAA", Side: Buy, Qty: 50, Price: 10.0})
	if err != nil {
		t.Fatalf("重复成交不应报错（幂等），实际: %v", err)
	}
	if !r.Duplicate || r.Applied {
		t.Fatalf("重复成交应标记 Duplicate 且不入账: %+v", r)
	}
	pos := l.Position("AAA")
	if pos.Qty != 50 {
		t.Fatalf("重复成交被重复计价: 头寸=%v 应为 50", pos.Qty)
	}
	o, _ := l.Order("O1")
	if o.FilledQty != 50 {
		t.Fatalf("订单累计成交被重复累加: %v", o.FilledQty)
	}
	t.Log("用例通过: 重复成交被幂等忽略，头寸与订单累计未变")
}

// 业务：迟到成交（订单已撤销后到达）必须被拒绝。
func TestLateFillAfterCancelRejected(t *testing.T) {
	t.Log("业务: 撤单后到达的成交为迟到成交，必须以 LATE_FILL 拒绝")
	l := New(false)
	mustOrder(t, l, "O1", "AAA", Buy, 100)
	mustFill(t, l, "F1", "O1", "AAA", Buy, 40, 10.0)
	if _, err := l.CancelOrder("O1"); err != nil {
		t.Fatalf("撤单失败: %v", err)
	}

	_, err := l.ApplyFill(Fill{ID: "F2", OrderID: "O1", Symbol: "AAA", Side: Buy, Qty: 60, Price: 10.0})
	if !IsReason(err, ReasonLateFill) {
		t.Fatalf("迟到成交应返回 LATE_FILL, 实际: %v", err)
	}
	if pos := l.Position("AAA"); pos.Qty != 40 {
		t.Fatalf("迟到成交污染了头寸: %v", pos.Qty)
	}
	t.Log("用例通过: 迟到成交被拒绝，已有头寸未被污染")
}

// 业务：冲突与不合法操作必须给出可区分原因。
func TestConflictingAndInvalidOps(t *testing.T) {
	t.Log("业务: 各类非法/冲突操作的可区分拒绝原因")
	l := New(false)
	mustOrder(t, l, "O1", "AAA", Buy, 100)

	cases := []struct {
		name string
		run  func() error
		want Reason
	}{
		{"重复订单", func() error { return l.PlaceOrder(Order{ID: "O1", Symbol: "AAA", Side: Buy, Qty: 1}) }, ReasonDuplicateOrder},
		{"未知订单的成交", func() error { _, e := l.ApplyFill(Fill{ID: "F9", OrderID: "NOPE", Symbol: "AAA", Side: Buy, Qty: 1, Price: 1}); return e }, ReasonUnknownOrder},
		{"超量成交", func() error { _, e := l.ApplyFill(Fill{ID: "F2", OrderID: "O1", Symbol: "AAA", Side: Buy, Qty: 101, Price: 1}); return e }, ReasonOverFill},
		{"标的不匹配", func() error { _, e := l.ApplyFill(Fill{ID: "F3", OrderID: "O1", Symbol: "BBB", Side: Buy, Qty: 1, Price: 1}); return e }, ReasonSymbolMismatch},
		{"非法数量", func() error { _, e := l.ApplyFill(Fill{ID: "F4", OrderID: "O1", Symbol: "AAA", Side: Buy, Qty: 0, Price: 1}); return e }, ReasonInvalidQuantity},
		{"非法价格", func() error { _, e := l.ApplyFill(Fill{ID: "F5", OrderID: "O1", Symbol: "AAA", Side: Buy, Qty: 1, Price: -1}); return e }, ReasonInvalidPrice},
		{"撤销未知订单", func() error { _, e := l.CancelOrder("NOPE"); return e }, ReasonUnknownOrder},
	}
	for _, c := range cases {
		if err := c.run(); !IsReason(err, c.want) {
			t.Errorf("%s: 期望原因 %s, 实际 %v", c.name, c.want, err)
		} else {
			t.Logf("用例通过: %s -> %s", c.name, c.want)
		}
	}
}

// 业务：不允许卖空时，超仓卖出被拒绝；已完成订单不可再成交或撤销。
func TestOversellAndClosedOrder(t *testing.T) {
	t.Log("业务: 禁止卖空时的超仓卖出拒绝；订单关闭后的操作拒绝")
	l := New(false)
	mustOrder(t, l, "O1", "AAA", Buy, 50)
	mustFill(t, l, "F1", "O1", "AAA", Buy, 50, 10.0)

	mustOrder(t, l, "O2", "AAA", Sell, 60)
	if _, err := l.ApplyFill(Fill{ID: "F2", OrderID: "O2", Symbol: "AAA", Side: Sell, Qty: 60, Price: 11}); !IsReason(err, ReasonInsufficientPosition) {
		t.Fatalf("超仓卖出应返回 INSUFFICIENT_POSITION, 实际: %v", err)
	}
	if _, err := l.ApplyFill(Fill{ID: "F3", OrderID: "O1", Symbol: "AAA", Side: Buy, Qty: 1, Price: 10}); !IsReason(err, ReasonOrderNotOpen) {
		t.Fatalf("已完成订单再成交应返回 ORDER_NOT_OPEN, 实际: %v", err)
	}
	if _, err := l.CancelOrder("O1"); !IsReason(err, ReasonOrderNotOpen) {
		t.Fatalf("已完成订单撤销应返回 ORDER_NOT_OPEN, 实际: %v", err)
	}
	t.Log("用例通过: 超仓卖出/已关闭订单操作均被可区分地拒绝")
}

// 业务：翻仓（反向成交超过持仓）时先结清旧方向盈亏，再以成交价开新仓。
func TestFlipPosition(t *testing.T) {
	t.Log("业务: 允许卖空时翻仓，旧方向盈亏结清、新方向以成交价建仓")
	l := New(true)
	mustOrder(t, l, "O1", "AAA", Buy, 100)
	mustFill(t, l, "F1", "O1", "AAA", Buy, 100, 10.0)

	mustOrder(t, l, "O2", "AAA", Sell, 130)
	r := mustFill(t, l, "F2", "O2", "AAA", Sell, 130, 12.0)
	if r.RealizedDelta != 100*(12.0-10.0) {
		t.Fatalf("翻仓已实现盈亏错误: got %v want 200", r.RealizedDelta)
	}
	if r.Position.Qty != -30 || r.Position.AvgCost != 12.0 {
		t.Fatalf("翻仓后头寸错误: %+v", r.Position)
	}
	t.Logf("用例通过: 翻仓后头寸=%v 成本=%v 已实现=%v", r.Position.Qty, r.Position.AvgCost, r.RealizedDelta)
}
