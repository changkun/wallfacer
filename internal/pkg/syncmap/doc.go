// Package syncmap provides a type-safe generic wrapper around sync.Map.
//
// The stdlib sync.Map uses interface{} for keys and values, requiring type
// assertions at every call site. [Map] wraps it with generics to provide
// compile-time type safety for Store, Load, Delete, and Range operations.
// The zero value is ready to use, matching sync.Map semantics.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Available as a general-purpose
// concurrent map for any package that needs lock-free concurrent access
// with type safety.
//
// # Usage
//
//	var m syncmap.Map[string, *Connection]
//	m.Store("key", conn)
//	if val, ok := m.Load("key"); ok { ... }
//	m.Range(func(k string, v *Connection) bool { return true })
package syncmap
