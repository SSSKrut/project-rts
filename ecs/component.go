package ecs

import "reflect"

// Component represents a unit of data attached to an entity.
type Component interface{}

// ComponentType identifies a component by its Go type.
type ComponentType = reflect.Type

// TypeOf returns the ComponentType for a generic component type.
func TypeOf[T any]() ComponentType {
	return reflect.TypeOf((*T)(nil)).Elem()
}
