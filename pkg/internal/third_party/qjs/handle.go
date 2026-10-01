package qjs

import (
	"fmt"
	"math"
	"sync/atomic"
)

// Handle represents a reference to a QuickJS value.
// It manages raw pointer values from WebAssembly memory and provides safe
// type conversion methods with proper resource management.
type Handle struct {
	raw     uint64
	runtime *Runtime
	freed   int32 // atomic flag to prevent double-free
}

// NewHandle creates a new Handle wrapping the given pointer value.
// The handle maintains a reference to the runtime for proper memory management.
func NewHandle(runtime *Runtime, ptr uint64) *Handle {
	if runtime == nil {
		panic("handle: runtime cannot be nil")
	}

	return &Handle{
		raw:     ptr,
		runtime: runtime,
		freed:   0,
	}
}

// Free releases the memory associated with this handle.
// Only used with C values such as: QJS_ToCString, QJS_JSONStringify.
// Do not use this method for JsValue.
func (h *Handle) Free() {
	if h == nil || h.runtime == nil {
		return
	}

	// Use atomic compare-and-swap to ensure single free
	if atomic.CompareAndSwapInt32(&h.freed, 0, 1) && h.raw != 0 {
		h.runtime.FreeHandle(h.raw)
	}
}

// IsFreed returns true if the handle has been freed.
func (h *Handle) IsFreed() bool {
	return h == nil || atomic.LoadInt32(&h.freed) != 0
}

// Raw returns the underlying raw pointer or 0 if the handle is nil or freed.
func (h *Handle) Raw() uint64 {
	if h == nil || h.IsFreed() {
		return 0
	}

	return h.raw
}

// Bool converts the handle value to bool using zero/non-zero semantics.
func (h *Handle) Bool() bool {
	if h == nil || h.IsFreed() {
		return false
	}

	return int32(h.raw) != 0
}

// Signed integer conversion methods with bounds checking.
type Signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type Unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type Integer interface {
	Signed | Unsigned
}

type Float interface {
	~float32 | ~float64
}

// ConvertToSigned performs safe conversion to signed integer types with bounds checking.
func ConvertToSigned[T Signed](h *Handle) T {
	if h == nil || h.IsFreed() {
		return T(0)
	}

	// For signed integers, we need to handle sign extension properly
	// WebAssembly returns values as uint64, but they may represent signed values
	var result T

	switch any(result).(type) {
	case int8:
		// Extract lower 8 bits and sign extend
		val := int8(uint8(h.raw))
		result = T(val)
	case int16:
		// Extract lower 16 bits and sign extend
		val := int16(uint16(h.raw))
		result = T(val)
	case int32:
		// Extract lower 32 bits and sign extend
		val := int32(uint32(h.raw))
		result = T(val)
	case int64:
		// For int64, direct cast is appropriate
		result = T(int64(h.raw))
	case int:
		// For int (platform dependent), handle like int32 or int64 based on size
		if Is32BitPlatform() {
			result = T(int32(uint32(h.raw)))
		} else {
			result = T(int64(h.raw))
		}
	default:
		// Fallback for any other signed integer types
		result = T(h.raw)
	}

	return result
}

// ConvertToUnsigned performs safe conversion to unsigned integer types with bounds checking.
func ConvertToUnsigned[T Unsigned](h *Handle) T {
	if h == nil || h.IsFreed() {
		return T(0)
	}

	value := h.raw

	var result = T(value)

	// Check for overflow by converting back and comparing
	if uint64(result) != value {
		panic(fmt.Sprintf("handle: overflow error - value %d exceeds range for %T", value, result))
	}

	return result
}

func (h *Handle) Int() int         { return ConvertToSigned[int](h) }
func (h *Handle) Int8() int8       { return ConvertToSigned[int8](h) }
func (h *Handle) Int16() int16     { return ConvertToSigned[int16](h) }
func (h *Handle) Int32() int32     { return ConvertToSigned[int32](h) }
func (h *Handle) Int64() int64     { return ConvertToSigned[int64](h) }
func (h *Handle) Uint() uint       { return ConvertToUnsigned[uint](h) }
func (h *Handle) Uint8() uint8     { return ConvertToUnsigned[uint8](h) }
func (h *Handle) Uint16() uint16   { return ConvertToUnsigned[uint16](h) }
func (h *Handle) Uint32() uint32   { return ConvertToUnsigned[uint32](h) }
func (h *Handle) Uint64() uint64   { return ConvertToUnsigned[uint64](h) }
func (h *Handle) Uintptr() uintptr { return ConvertToUnsigned[uintptr](h) }

// Float32 converts the handle value to float32 by interpreting the lower 32 bits
// as IEEE 754 single-precision floating point representation.
// Returns 0.0 if the handle is nil or freed.
func (h *Handle) Float32() float32 {
	if h == nil || h.IsFreed() {
		return 0.0
	}

	return math.Float32frombits(uint32(h.raw))
}

// Float64 converts the handle value to float64 by interpreting the raw bits
// as IEEE 754 double-precision floating point representation.
// Returns 0.0 if the handle is nil or freed.
func (h *Handle) Float64() float64 {
	if h == nil || h.IsFreed() {
		return 0.0
	}

	return math.Float64frombits(h.raw)
}

// String converts the handle value to string by unpacking a pointer
// to string data in QuickJS memory. Returns empty string if handle is nil or freed.
// If there's a JavaScript exception in the context, it will panic with the exception.
func (h *Handle) String() string {
	if h == nil || h.IsFreed() {
		return ""
	}

	// Check for exceptions in the JavaScript context
	if h.raw == 0 {
		if h.runtime != nil && h.runtime.context != nil && h.runtime.context.HasException() {
			panic(h.runtime.context.Exception())
		}

		return ""
	}

	return h.runtime.mem.StringFromPackedPtr(h.raw)
}

// Bytes converts the handle value to []byte by reading from QuickJS memory.
// Returns empty slice for zero handles or if the handle is freed.
// The returned bytes are a copy and safe to modify.
func (h *Handle) Bytes() []byte {
	if h == nil || h.IsFreed() || h.raw == 0 {
		return nil
	}

	addr, size := h.runtime.mem.UnpackPtr(h.raw)
	if addr == 0 || size == 0 {
		return nil
	}

	// Ensure we free the address after reading
	defer h.runtime.FreeHandle(uint64(addr))

	// Read from WebAssembly memory
	data := h.runtime.mem.MustRead(addr, uint64(size))

	// Create a copy to ensure the returned slice is safe to use
	result := make([]byte, size)
	copy(result, data)

	return result
}
