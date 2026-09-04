package annotation

import (
	"errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// ErrNotFound reports that an annotation was absent from the object.
var ErrNotFound = errors.New("annotation not found")

type Prefix string

// Value resolves an annotation that may be absent.
type Value func() (string, bool)

// UnmarshalYAML decodes the annotation into obj, which must be a non-nil
// pointer. Field names come from the json tags so annotations spell fields the
// same way a manifest does. It reports ErrNotFound when the annotation is
// absent.
func (v Value) UnmarshalYAML(obj any) error {
	value, ok := v()
	if !ok {
		return ErrNotFound
	}

	return yaml.Unmarshal([]byte(value), obj)
}

func (p Prefix) Annotation(name string) Annotation {
	return Annotation{p, name}
}

func (p Prefix) Get(obj client.Object, name string) Value {
	return p.Annotation(name).Get(obj)
}

func (p Prefix) Lookup(obj client.Object, name string) (string, bool) {
	return p.Annotation(name).Lookup(obj)
}

func Get(obj client.Object, prefix, name string) Value {
	return Prefix(prefix).Get(obj, name)
}

func Lookup(obj client.Object, prefix, name string) (string, bool) {
	return Prefix(prefix).Annotation(name).Lookup(obj)
}

type Annotation struct {
	prefix Prefix
	name   string
}

func (a Annotation) Get(obj client.Object) Value {
	return func() (string, bool) { return a.Lookup(obj) }
}

func (a Annotation) Lookup(obj client.Object) (string, bool) {
	value, ok := obj.GetAnnotations()[a.String()]
	return value, ok
}

func (a Annotation) String() string {
	return string(a.prefix) + a.name
}
