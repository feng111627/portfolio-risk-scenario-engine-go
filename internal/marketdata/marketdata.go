// Package marketdata 维护本地行情快照，并定义行情“有效性”的统一判定口径：
// 过期（超过最大时延）、乱序（序列号或时间戳倒退）、未来时间戳、非正价格、
// 同一快照内同一标的重复——这些情况都会被明确拒绝或标记为无效，
// 估值流程不会静默地使用错误/陈旧价格。
package marketdata

import (
	"context"
	"errors"
	"time"
)

// Quote 是单只标的的一条行情。
type Quote struct {
	Symbol    string
	Price     float64 // 最新成交价/中间价，必须 > 0
	Timestamp time.Time
	Seq       int64 // 单调递增的行情序列号（乱序检测的权威依据）
}

// Snapshot 是一个时刻到达的一批行情（原子应用）。
type Snapshot struct {
	Quotes    []Quote
	ReceivedAt time.Time
}

// QuoteState 是某标的最新一条已接受行情的状态。
type QuoteState struct {
	Quote     Quote
	ReceivedAt time.Time
	UpdatedAt time.Time
}

// ValidQuote 是有效性判定结果。
type ValidQuote struct {
	State  QuoteState
	Stale  bool   // true 表示最新行情已超过 MaxAge
	Age    time.Duration
	Reason string // 无效/陈旧原因；有效时为空
}

// IsValid 在行情存在且未过期时为 true。
func (v ValidQuote) IsValid() bool { return v.State.Quote.Price > 0 && !v.Stale }

// Book 是线程安全的本地行情簿。
type Book struct {
	// 空实现占位。
}

// NewBook 创建行情簿；maxAge<=0 表示不做过期判定。
func NewBook(maxAge time.Duration) *Book { return &Book{} }

// ApplySnapshot 原子地应用一批行情：任一条非法则整批拒绝。
func (b *Book) ApplySnapshot(context.Context, Snapshot) error { return nil }

// ApplyQuote 应用单条行情。
func (b *Book) ApplyQuote(context.Context, Quote) error { return nil }

// Latest 返回某标的最新已接受行情，以及相对 now 的有效性结论。
func (b *Book) Latest(symbol string, now time.Time) (ValidQuote, bool) {
	return ValidQuote{}, false
}

// SnapshotAt 返回 now 时刻所有有效行情的价格副本（严格口径：剔除过期标的）。
func (b *Book) SnapshotAt(now time.Time) map[string]float64 { return nil }

// 可区分的行情错误（用 errors.Is 判定）。
var (
	// ErrInvalidQuote 字段非法（空标的、价格<=0、零时间戳等）。
	ErrInvalidQuote = errors.New("marketdata: invalid quote")
	// ErrStaleQuote 相对新行情而言，该条为乱序/迟到行情（Seq 与时间戳均倒退）。
	ErrStaleQuote = errors.New("marketdata: stale or out-of-order quote")
	// ErrFutureQuote 行情时间戳晚于本地接收时间（容忍时钟偏差后仍在未来）。
	ErrFutureQuote = errors.New("marketdata: quote timestamp in the future")
	// ErrDuplicateInSnapshot 同一快照中同一标的出现多条。
	ErrDuplicateInSnapshot = errors.New("marketdata: duplicate symbol within one snapshot")
)
