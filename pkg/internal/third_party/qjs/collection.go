package qjs

import "fmt"

// Array provides a wrapper around JavaScript arrays with Go-like methods.
type Array struct {
	*Value
}

// NewArray wraps a JavaScript array value in a Go Array type.
func NewArray(value *Value) *Array {
	if value == nil || !value.IsArray() {
		return nil
	}

	return &Array{Value: value}
}

func (a *Array) ForEach(forFn func(key, value *Value)) {
	if a == nil || a.Value == nil || forFn == nil {
		return
	}

	a.Value.ForEach(forFn)
}

// HasIndex returns true if the given index exists in the array.
func (a *Array) HasIndex(i int64) bool {
	return a.HasPropertyIndex(i)
}

// Get returns the element at the given index.
func (a *Array) Get(index int64) *Value {
	if a == nil || a.Value == nil {
		return nil
	}

	return a.GetPropertyIndex(index)
}

// Push appends elements to the array and returns the new length.
func (a *Array) Push(elements ...*Value) int64 {
	ret, err := a.InvokeJS("push", elements...)
	if err != nil {
		panic(fmt.Errorf("failed to invoke push on Array: %w", err))
	}

	defer ret.Free()

	return ret.Int64()
}

// Set updates the element at the given index.
func (a *Array) Set(index int64, value *Value) {
	if a == nil || a.Value == nil {
		return
	}

	a.SetPropertyIndex(index, value)
}

// Delete removes the element at the given index.
func (a *Array) Delete(index int64) bool {
	if a == nil || a.Value == nil {
		return false
	}

	indexValue := a.context.NewInt64(index)
	defer indexValue.Free()

	countValue := a.context.NewInt64(1)
	defer countValue.Free()

	removeList, err := a.InvokeJS("splice", indexValue, countValue)
	if err != nil {
		panic(fmt.Errorf("failed to invoke splice on Array: %w", err))
	}

	defer removeList.Free()

	return removeList.IsArray() && removeList.Len() > 0
}

// Map provides a wrapper around JavaScript Map objects with Go-like methods.
type Map struct {
	*Value
}

// NewMap wraps a JavaScript Map value in a Go Map type.
func NewMap(value *Value) *Map {
	if value == nil || !value.IsMap() {
		return nil
	}

	return &Map{Value: value}
}

// JSONStringify returns the JSON representation of the Map as an object.
func (m *Map) JSONStringify() (string, error) {
	object := m.CreateObject()
	defer object.Free()

	return object.JSONStringify()
}

// IsMap returns true if this is a valid Map.
// Mainly used to satisfy ObjectOrMap.
func (m *Map) IsMap() bool {
	return m != nil && m.Value != nil
}

// IsObject returns true if this is a valid Map, mainly used to satisfy ObjectOrMap.
func (m *Map) IsObject() bool {
	return m != nil && m.Value != nil
}

// ToMap returns this Map instance, mainly used to satisfy ObjectOrMap.
func (m *Map) ToMap() *Map {
	return m
}

// Get retrieves the value for the given key.
func (m *Map) Get(key *Value) *Value {
	if m == nil || m.Value == nil || key == nil {
		return nil
	}

	v, err := m.InvokeJS("get", key)
	if err != nil {
		panic(fmt.Errorf("failed to invoke get on Map: %w", err))
	}

	return v
}

// Set sets the value for the given key.
func (m *Map) Set(key, value *Value) {
	if m == nil || m.Value == nil || key == nil || value == nil {
		return
	}

	result, err := m.InvokeJS("set", key, value)
	if err != nil {
		panic(fmt.Errorf("failed to invoke set on Map: %w", err))
	}

	result.Free()
}

// Delete removes the key-value pair.
func (m *Map) Delete(key *Value) {
	if m == nil || m.Value == nil || key == nil {
		return
	}

	result, err := m.InvokeJS("delete", key)
	if err != nil {
		panic(fmt.Errorf("failed to invoke delete on Map: %w", err))
	}

	result.Free()
}

// Has returns true if the key exists.
func (m *Map) Has(key *Value) bool {
	if m == nil || m.Value == nil || key == nil {
		return false
	}

	boolValue, err := m.InvokeJS("has", key)
	if err != nil {
		panic(fmt.Errorf("failed to invoke has on Map: %w", err))
	}

	defer boolValue.Free()

	return boolValue.Bool()
}

// ForEach calls forFn for each key-value pair.
func (m *Map) ForEach(forFn func(key, value *Value)) {
	if m == nil || m.Value == nil || forFn == nil {
		return
	}

	forEachFn := m.context.Function(func(t *This) (*Value, error) {
		args := t.Args()
		if len(args) >= MinMapForeachArgs {
			forFn(args[1], args[0])
		}

		return nil, nil
	})
	defer forEachFn.Free()

	val, err := m.InvokeJS("forEach", forEachFn)
	if err != nil {
		panic(fmt.Errorf("failed to invoke forEach on Map: %w", err))
	}

	val.Free()
}

// CreateObject converts the Map to a JavaScript object.
func (m *Map) CreateObject() *Value {
	if m == nil || m.Value == nil {
		return nil
	}

	object := m.context.NewObject()
	m.ForEach(func(key, value *Value) {
		object.SetProperty(key, value)
	})

	return object
}

// Set provides a wrapper around JavaScript Set objects with Go-like methods.
type Set struct {
	*Value
}

// NewSet wraps a JavaScript Set value in a Go Set type.
func NewSet(value *Value) *Set {
	if value == nil || !value.IsSet() {
		return nil
	}

	return &Set{Value: value}
}

// JSONStringify returns the JSON representation of the Set as an array.
func (s *Set) JSONStringify() (string, error) {
	arr := s.ToArray()

	defer arr.Free()

	return arr.JSONStringify()
}

// Add adds a value to the Set.
func (s *Set) Add(value *Value) {
	if s == nil || s.Value == nil || value == nil {
		return
	}

	v, err := s.InvokeJS("add", value)
	if err != nil {
		panic(fmt.Errorf("failed to invoke add on Set: %w", err))
	}

	defer v.Free()
}

// Delete removes the value from the Set.
func (s *Set) Delete(value *Value) {
	if s == nil || s.Value == nil || value == nil {
		return
	}

	v, err := s.InvokeJS("delete", value)
	if err != nil {
		panic(fmt.Errorf("failed to invoke delete on Set: %w", err))
	}

	defer v.Free()
}

// Has returns true if the value exists.
func (s *Set) Has(value *Value) bool {
	if s == nil || s.Value == nil || value == nil {
		return false
	}

	v, err := s.InvokeJS("has", value)
	if err != nil {
		panic(fmt.Errorf("failed to invoke has on Set: %w", err))
	}

	defer v.Free()

	return v.Bool()
}

// ToArray converts the Set to an Array.
func (s *Set) ToArray() *Array {
	if s == nil || s.Value == nil {
		return nil
	}

	array := s.context.NewArray()
	s.ForEach(func(value *Value) {
		array.Push(value.Clone())
	})

	return array
}

// ForEach calls forFn for each value.
func (s *Set) ForEach(forFn func(value *Value)) {
	if s == nil || s.Value == nil || forFn == nil {
		return
	}

	forEachFn := s.context.Function(func(t *This) (*Value, error) {
		args := t.Args()
		if len(args) > 0 {
			forFn(args[0])
		}

		return t.NewUndefined(), nil
	})
	defer forEachFn.Free()

	value, err := s.InvokeJS("forEach", forEachFn)
	if err != nil {
		panic(fmt.Errorf("failed to invoke forEach on Set: %w", err))
	}

	defer value.Free()
}
