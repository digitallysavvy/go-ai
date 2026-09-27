package qjs

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"time"
)

// ToJsValue converts any Go value to a QuickJS value.
func ToJsValue(c *Context, v any) (*Value, error) {
	jsVal, err := NewTracker[uintptr]().toJsValue(c, v)
	if err != nil {
		return nil, fmt.Errorf("[ToJSValue] %w", err)
	}

	if !jsVal.IsNull() && jsVal.IsObject() {
		goValueType := reflect.TypeOf(v).String()
		jsVal.SetPropertyStr("__go_type", c.NewString(goValueType))
		jsVal.SetPropertyStr("__registry_id", c.NewInt64(int64(c.runtime.registry.Register(v))))
	}

	return jsVal, nil
}

// toJsValue converts any Go value to a QuickJS Value using the conversion context.
func (tracker *Tracker[T]) toJsValue(c *Context, v any) (*Value, error) {
	if v == nil {
		return c.NewNull(), nil
	}

	if err, ok := v.(error); ok {
		return c.NewError(err), nil
	}

	if jsVal := tryConvertBuiltinTypes(c, v); jsVal != nil {
		return jsVal, nil
	}

	if jsVal := tryConvertNumeric(c, v); jsVal != nil {
		return jsVal, nil
	}

	return tracker.convertReflectValue(c, v)
}

// GoStructToJs converts a Go struct to a JavaScript object.
// Includes both fields and methods as object properties.
func (tracker *Tracker[T]) GoStructToJs(
	c *Context,
	rtype reflect.Type,
	rval reflect.Value,
) (*Value, error) {
	return withJSObject(c, func(obj *Value) error {
		// obj.SetPropertyStr("__go_type", c.NewString(GetGoTypeName(rtype)))

		// Determine the struct type and value for field processing:
		// - pointer to struct: dereference for field processing.
		// - direct struct: use as-is.
		var (
			structType reflect.Type
			structVal  reflect.Value
		)

		if rtype.Kind() == reflect.Pointer {
			structType = rtype.Elem()
			structVal = rval.Elem()
		} else {
			structType = rtype
			structVal = rval
		}

		err := tracker.addStructFieldsToObject(c, obj, structType, structVal)
		if err != nil {
			return err
		}

		// For methods, use the original type and value to preserve pointer receiver methods
		return tracker.addStructMethodsToObject(c, obj, rtype, rval)
	})
}

// GoSliceToJs converts a Go slice to a JavaScript array.
func (tracker *Tracker[T]) GoSliceToJs(c *Context, rval reflect.Value) (*Value, error) {
	return tracker.arrayLikeToJS(c, rval, "slice")
}

// GoArrayToJs converts a Go array to a JavaScript array without unnecessary copying.
func (tracker *Tracker[T]) GoArrayToJs(c *Context, rval reflect.Value) (*Value, error) {
	return tracker.arrayLikeToJS(c, rval, "array")
}

// GoMapToJs converts a Go map to a JavaScript object.
// Non-string keys are converted to string representation.
func (tracker *Tracker[T]) GoMapToJs(
	c *Context,
	rval reflect.Value,
) (*Value, error) {
	return withJSObject(c, func(obj *Value) error {
		for _, key := range rval.MapKeys() {
			value := rval.MapIndex(key)

			var keyStr string
			if key.Kind() == reflect.String {
				keyStr = key.String()
			} else {
				keyStr = fmt.Sprintf("%v", key.Interface())
			}

			jsValue, err := tracker.toJsValue(c, value.Interface())
			if err != nil {
				return newGoToJsErr("map key: "+keyStr, err)
			}

			obj.SetPropertyStr(keyStr, jsValue)
		}

		return nil
	})
}

// GoNumberToJs converts Go numeric types to appropriate JS number types.
func GoNumberToJs[T NumberType](c *Context, i T) *Value {
	switch v := any(i).(type) {
	case int32:
		return c.NewInt32(v)
	case uint32:
		return c.NewUint32(v)
	case int64:
		return c.NewInt64(v)
	case float64:
		return c.NewFloat64(v)
	case float32:
		return c.NewFloat64(float64(v))
	default:
		// Convert other integer types to int64, floats to float64
		switch any(i).(type) {
		case int, int8, int16:
			return c.NewInt64(int64(i))
		case uint, uint8, uint16, uint64:
			if uint64(i) > math.MaxInt64 {
				return c.NewBigUint64(uint64(i))
			}

			return c.NewInt64(int64(i))
		default:
			return c.NewFloat64(float64(i))
		}
	}
}

// GoComplexToJs converts Go complex numbers to JS objects with real/imag properties.
func GoComplexToJs[T complex64 | complex128](c *Context, z T) *Value {
	obj := c.NewObject()

	var realPart, imagPart float64

	switch v := any(z).(type) {
	case complex64:
		realPart = float64(real(v))
		imagPart = float64(imag(v))
	case complex128:
		realPart = real(v)
		imagPart = imag(v)
	}

	obj.SetPropertyStr("real", c.NewFloat64(realPart))
	obj.SetPropertyStr("imag", c.NewFloat64(imagPart))

	return obj
}

// GoStructToJs converts Go structs to JavaScript objects.
func GoStructToJs(
	c *Context,
	rtype reflect.Type,
	rval reflect.Value,
) (*Value, error) {
	return NewTracker[uint64]().GoStructToJs(c, rtype, rval)
}

// GoSliceToJs converts Go slices to JavaScript arrays.
func GoSliceToJs(c *Context, rval reflect.Value) (*Value, error) {
	return NewTracker[uint64]().GoSliceToJs(c, rval)
}

// GoMapToJs converts Go maps to JavaScript objects.
func GoMapToJs(c *Context, rval reflect.Value) (*Value, error) {
	return NewTracker[uint64]().GoMapToJs(c, rval)
}

// tryConvertBuiltinTypes handles built-in Go types that don't require reflection.
func tryConvertBuiltinTypes(c *Context, v any) *Value {
	switch val := v.(type) {
	case bool:
		return c.NewBool(val)
	case string:
		return c.NewString(val)
	case []byte:
		if val == nil {
			return c.NewNull()
		}

		return c.NewArrayBuffer(val)
	case time.Time:
		return c.NewDate(&val)
	case *time.Time:
		if val == nil {
			return c.NewNull()
		}

		return c.NewDate(val)
	case uintptr:
		return c.NewInt64(int64(val))
	}

	return nil
}

// tryConvertNumeric handles all numeric types in a consolidated way.
func tryConvertNumeric(c *Context, v any) *Value {
	switch val := v.(type) {
	case int8:
		return GoNumberToJs(c, val)
	case int16:
		return GoNumberToJs(c, val)
	case int32:
		return GoNumberToJs(c, val)
	case int:
		return GoNumberToJs(c, val)
	case int64:
		return GoNumberToJs(c, val)
	case uint8:
		return GoNumberToJs(c, val)
	case uint16:
		return GoNumberToJs(c, val)
	case uint32:
		return GoNumberToJs(c, val)
	case uint:
		return GoNumberToJs(c, val)
	case uint64:
		// Large uint64 values use float64 to avoid overflow
		if val&(uint64(1)<<Uint64SignBitPosition) == 0 {
			return GoNumberToJs(c, int64(val))
		}

		return c.NewFloat64(float64(val))
	case float32:
		return GoNumberToJs(c, val)
	case float64:
		return GoNumberToJs(c, val)
	case complex64:
		return GoComplexToJs(c, val)
	case complex128:
		return GoComplexToJs(c, val)
	}

	return nil
}

// convertReflectValue handles types that require reflection.
func (tracker *Tracker[T]) convertReflectValue(c *Context, v any) (*Value, error) {
	rtype := reflect.TypeOf(v)
	rval := reflect.ValueOf(v)

	var ct CircularTracker[T]
	defer ct.cleanup()

	if rtype.Kind() == reflect.Ptr {
		if rval.IsNil() {
			return c.NewNull(), nil
		}

		// Track pointer address for circular reference detection.
		var addr any = (rval.Pointer())

		addrT, _ := addr.(T)
		if err := ct.trackPtr(tracker, addrT); err != nil {
			return nil, newGoToJsErr(GetGoTypeName(reflect.TypeOf(v)), err, "recursive pointer")
		}

		// If pointer points to a struct,
		// preserve the pointer type for method resolution.
		elemType := rtype.Elem()
		if elemType.Kind() == reflect.Struct {
			return tracker.GoStructToJs(c, rtype, rval)
		}

		return tracker.toJsValue(c, rval.Elem().Interface())
	}

	switch rtype.Kind() {
	case reflect.Func:
		return FuncToJS(c, v)
	case reflect.Struct:
		return tracker.GoStructToJs(c, rtype, rval)
	case reflect.Slice:
		if rval.IsNil() {
			return c.NewNull(), nil
		}

		return tracker.GoSliceToJs(c, rval)
	case reflect.Map:
		if rval.IsNil() {
			return c.NewNull(), nil
		}

		return tracker.GoMapToJs(c, rval)
	case reflect.Array:
		return tracker.GoArrayToJs(c, rval)
	case reflect.Chan:
		return ChannelToJSObjectValue(c, rtype, rval)
	case reflect.UnsafePointer:
		return nil, newGoToJsErr(GetGoTypeName(rtype), nil)

	// Handle custom types based on their underlying type.
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return GoNumberToJs(c, rval.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		uintVal := rval.Uint()
		if uintVal > math.MaxInt64 {
			return c.NewFloat64(float64(uintVal)), nil
		}

		return GoNumberToJs(c, int64(uintVal)), nil
	case reflect.Float32, reflect.Float64:
		return GoNumberToJs(c, rval.Float()), nil
	case reflect.String:
		return c.NewString(rval.String()), nil
	case reflect.Bool:
		return c.NewBool(rval.Bool()), nil
	}

	return nil, newGoToJsErr(rtype.Name(), nil)
}

// withJSObject creates a JS object and ensures it's freed on error.
func withJSObject(c *Context, fn func(*Value) error) (*Value, error) {
	obj := c.NewObject()
	if err := fn(obj); err != nil {
		obj.Free()

		return nil, err
	}

	return obj, nil
}

// addStructFieldsToObject converts struct fields to JS object properties.
// Processes embedded fields first, then regular fields to allow overriding.
func (tracker *Tracker[T]) addStructFieldsToObject(
	c *Context,
	obj *Value,
	rtype reflect.Type,
	rval reflect.Value,
) error {
	// Process embedded fields first to enable field overriding
	if err := tracker.processEmbeddedFields(c, obj, rtype, rval); err != nil {
		return err
	}

	// Process regular fields (can override embedded fields)
	return tracker.processRegularFields(c, obj, rtype, rval)
}

// processEmbeddedFields handles anonymous/embedded fields in structs.
func (tracker *Tracker[T]) processEmbeddedFields(
	c *Context,
	obj *Value,
	rtype reflect.Type,
	rval reflect.Value,
) error {
	for i := range rtype.NumField() {
		field := rtype.Field(i)
		jsonIgnore := field.Tag.Get("json") == "-"

		if !field.IsExported() || !field.Anonymous || jsonIgnore {
			continue
		}

		if err := tracker.processEmbeddedField(c, obj, field, rval.Field(i)); err != nil {
			return err
		}
	}

	return nil
}

// processEmbeddedField handles a single embedded field.
func (tracker *Tracker[T]) processEmbeddedField(
	c *Context,
	obj *Value,
	field reflect.StructField,
	fieldValue reflect.Value,
) error {
	switch fieldValue.Kind() {
	case reflect.Struct:
		return tracker.addStructFieldsToObject(c, obj, field.Type, fieldValue)
	case reflect.Ptr:
		return tracker.processEmbeddedPointer(c, obj, field.Name, field.Type, fieldValue)
	default:
		return tracker.addEmbeddedPrimitive(c, obj, field.Name, fieldValue)
	}
}

// processEmbeddedPointer handles embedded pointer fields.
func (tracker *Tracker[T]) processEmbeddedPointer(
	c *Context,
	obj *Value,
	fieldName string,
	fieldType reflect.Type,
	fieldValue reflect.Value,
) error {
	elem := fieldValue.Elem()
	if elem.Kind() == reflect.Struct {
		// Embedded pointer to struct: recursively process struct fields
		return tracker.addStructFieldsToObject(c, obj, fieldType.Elem(), elem)
	}

	// Embedded pointer to primitive: add the dereferenced value as field
	return tracker.addEmbeddedPrimitive(c, obj, fieldName, elem)
}

// addEmbeddedPrimitive adds an embedded primitive type as a field.
func (tracker *Tracker[T]) addEmbeddedPrimitive(
	c *Context,
	obj *Value,
	fieldName string,
	fieldValue reflect.Value,
) error {
	if !fieldValue.IsValid() {
		return nil
	}

	prop, err := tracker.toJsValue(c, fieldValue.Interface())
	obj.SetPropertyStr(fieldName, prop)

	return err
}

// processRegularFields handles non-anonymous fields in structs.
func (tracker *Tracker[T]) processRegularFields(
	c *Context,
	obj *Value,
	rtype reflect.Type,
	rval reflect.Value,
) error {
	for i := range rtype.NumField() {
		field := rtype.Field(i)

		if !field.IsExported() || field.Anonymous {
			continue
		}

		fieldName, skip := tracker.getFieldName(field)
		if skip {
			continue
		}

		prop, err := tracker.toJsValue(c, rval.Field(i).Interface())
		if err != nil {
			return err
		}

		obj.SetPropertyStr(fieldName, prop)
	}

	return nil
}

// getFieldName determines the field name considering JSON tags.
func (tracker *Tracker[T]) getFieldName(field reflect.StructField) (string, bool) {
	fieldName := field.Name
	if jsonTag := field.Tag.Get("json"); jsonTag != "" {
		name, _, _ := strings.Cut(jsonTag, ",")
		if name == "-" {
			return "", true
		}

		if name != "" {
			fieldName = name
		}
	}

	return fieldName, false
}

// addStructMethodsToObject adds exported struct methods as JS functions.
func (tracker *Tracker[T]) addStructMethodsToObject(
	c *Context,
	obj *Value,
	rtype reflect.Type,
	rval reflect.Value,
) error {
	for i := range rtype.NumMethod() {
		method := rtype.Method(i)
		methodValue := rval.Method(i)

		jsMethod, err := FuncToJS(c, methodValue.Interface())
		if err != nil {
			return newGoToJsErr(
				GetGoTypeName(rtype)+"."+method.Name,
				err,
				"struct method",
			)
		}

		obj.SetPropertyStr(method.Name, jsMethod)
	}

	return nil
}

// arrayLikeToJS is a helper that converts arrays and slices to JS arrays.
func (tracker *Tracker[T]) arrayLikeToJS(
	c *Context,
	rval reflect.Value,
	typeName string,
) (*Value, error) {
	arr := c.NewArray()

	for i := range rval.Len() {
		elem := rval.Index(i)

		jsElem, err := tracker.toJsValue(c, elem.Interface())
		if err != nil {
			arr.Free()

			return nil, newGoToJsErr(typeName+": "+GetGoTypeName(rval.Type()), err)
		}

		arr.SetPropertyIndex(int64(i), jsElem)
	}

	return arr.Value, nil
}
