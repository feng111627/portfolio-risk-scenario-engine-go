// Package market 维护本地行情快照及其有效性口径。
package market

import (
	"fmt"
	"time"
)

// Quote 是一条行情。
type Quote struct {
	Symbol string
	Price  float64
	Ts     time.Time // 行情产生时间
	Seq    int64     // 单调递增的行情序号，用于识别乱序
}

// Snapshot 是行情快照：每个标的保留最新有效行情。
// 有效性口径：
//   - Apply 只接受 Seq 严格递增的行情，乱序/重复到达被拒绝（OUT_OF_ORDER）；
//   - Price 在 asOf 时刻检查行情年龄，超过 maxAge 视为过期（STALE_QUOTE）；
//   - 任何情况下都不会返回缺失或过期行情的价格。
type Snapshot struct {
	quotes map[string]Quote
	maxAge time.Duration
}

// NewSnapshot 创建快照。maxAge 为行情的最大有效时长。
func NewSnapshot(maxAge time.Duration) *Snapshot {
	return &Snapshot{quotes: make(map[string]Quote), maxAge: maxAge}
}

// MaxAge 返回快照配置的最大有效时长。
func (s *Snapshot) MaxAge() time.Duration { return s.maxAge }

// Apply 应用一条行情；乱序（Seq 不递增）或非法价格会被拒绝。
func (s *Snapshot) Apply(q Quote) error {
	if q.Price <= 0 {
		return &Error{Reason: ReasonInvalidPrice, Msg: fmt.Sprintf("标的 %s 行情价格非法: %v", q.Symbol, q.Price)}
	}
	if cur, ok := s.quotes[q.Symbol]; ok && q.Seq <= cur.Seq {
		return &Error{Reason: ReasonOutOfOrder, Msg: fmt.Sprintf("标的 %s 行情序号 %d 不晚于当前序号 %d，按乱序拒绝", q.Symbol, q.Seq, cur.Seq)}
	}
	s.quotes[q.Symbol] = q
	return nil
}

// Price 返回标的在 asOf 时刻有效的行情；缺失或过期返回错误。
func (s *Snapshot) Price(symbol string, asOf time.Time) (Quote, error) {
	q, ok := s.quotes[symbol]
	if !ok {
		return Quote{}, &Error{Reason: ReasonNoQuote, Msg: fmt.Sprintf("标的 %s 无行情", symbol)}
	}
	if asOf.Sub(q.Ts) > s.maxAge {
		return Quote{}, &Error{Reason: ReasonStale, Msg: fmt.Sprintf("标的 %s 行情年龄 %v 超过最大有效时长 %v", symbol, asOf.Sub(q.Ts), s.maxAge)}
	}
	return q, nil
}

// Clone 深拷贝快照，供并发情景评估安全使用。
func (s *Snapshot) Clone() *Snapshot {
	cp := &Snapshot{quotes: make(map[string]Quote, len(s.quotes)), maxAge: s.maxAge}
	for k, v := range s.quotes {
		cp.quotes[k] = v
	}
	return cp
}
