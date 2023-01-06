//+go:build go1.19

package log

import "sync/atomic"

// swapHandler wraps another handler that may be swapped out
// dynamically at runtime in a thread-safe fashion.
type swapHandler struct {
	handler atomic.Pointer[Handler]
}

func (h *swapHandler) Log(r *Record) error {
	return (*h.handler.Load()).Log(r)
}

func (h *swapHandler) Swap(newHandler Handler) {
	h.handler.Store(&newHandler)
}

func (h *swapHandler) Get() Handler {
	return *h.handler.Load()
}

func (h *swapHandler) Level() Lvl {
	return (*h.handler.Load()).Level()
}
