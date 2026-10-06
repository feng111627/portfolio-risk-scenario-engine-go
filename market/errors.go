package market

import "errors"

// Reason 是行情相关的可区分原因。
type Reason string

const (
	ReasonOutOfOrder   Reason = "OUT_OF_ORDER"
	ReasonInvalidPrice Reason = "INVALID_PRICE"
	ReasonNoQuote      Reason = "NO_QUOTE"
	ReasonStale        Reason = "STALE_QUOTE"
)

// Error 是带原因的行情错误。
type Error struct {
	Reason Reason
	Msg    string
}

func (e *Error) Error() string { return string(e.Reason) + ": " + e.Msg }

// IsReason 判断 err 是否为指定原因的行情错误。
func IsReason(err error, r Reason) bool {
	var me *Error
	return errors.As(err, &me) && me.Reason == r
}
