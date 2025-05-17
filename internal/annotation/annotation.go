// Over-enginerring at its finest
package annotation

import (
	"errors"

	"github.com/unmango/go/maybe"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type (
	Prefix string
	Value  maybe.Maybe[string]
)

func (v Value) UnmarshalYAML(obj any) error {
	if a, err := v(); err != nil {
		return errors.Join(maybe.ErrNone, err)
	} else {
		return yaml.Unmarshal([]byte(a), obj)
	}
}

func (p Prefix) Annotation(name string) Annotation {
	return Annotation{p, name}
}

func (p Prefix) Get(obj client.Object, name string) Value {
	if value, ok := p.Lookup(obj, name); ok {
		return Value(maybe.Ok(value))
	} else {
		return Value(maybe.None[string])
	}
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
	return a.prefix.Get(obj, a.name)
}

func (a Annotation) Lookup(obj client.Object) (string, bool) {
	return lookup(obj, a.String())
}

func (a Annotation) String() string {
	return string(a.prefix) + a.name
}

type Reader struct {
	prefix Prefix
	meta   client.Object
}

func (r Reader) Get(name string) Value {
	return r.prefix.Get(r.meta, name)
}

func lookup(obj client.Object, name string) (string, bool) {
	annotation, ok := obj.GetAnnotations()[name]
	return annotation, ok
}
