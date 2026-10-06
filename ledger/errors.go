package ledger

import "errors"

// Reason 是可区分的拒绝原因。
type Reason string

const (
	ReasonDuplicateOrder        Reason = "DUPLICATE_ORDER"
	ReasonUnknownOrder          Reason = "UNKNOWN_ORDER"
	ReasonOrderNotOpen          Reason = "ORDER_NOT_OPEN"
	ReasonLateFill              Reason = "LATE_FILL"
	ReasonOverFill              Reason = "OVER_FILL"
	ReasonInvalidQuantity       Reason = "INVALID_QUANTITY"
	ReasonInvalidPrice          Reason = "INVALID_PRICE"
	ReasonInvalidSide           Reason = "INVALID_SIDE"
	ReasonSymbolMismatch        Reason = "SYMBOL_MISMATCH"
	ReasonInsufficientPosition  Reason = "INSUFFICIENT_POSITION"
)

// Error 是带原因的记账错误。
type Error struct {
	Reason Reason
	Msg    string
}

func (e *Error) Error() string { return string(e.Reason) + ": " + e.Msg }

// IsReason 判断 err 是否为指定原因的记账错误。
func IsReason(err error, r Reason) bool {
	var le *Error
	return errors.As(err, &le) && le.Reason == r
}
