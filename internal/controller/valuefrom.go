package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
)

type reader interface {
	Get(ctx context.Context, key kclient.ObjectKey, obj kclient.Object, opts ...kclient.GetOption) error
}

// errValueUnset reports a reference to a source that does not exist, or to a key
// the source does not contain, where the selector marked it optional. Callers
// treat it as "no value" rather than as a failure.
var errValueUnset = fmt.Errorf("no value")

// resolveTunnelSecret reads the tunnel secret out of the cluster. An inline
// value is returned as-is; a reference is read from the Secret or ConfigMap it
// names in the given namespace.
func resolveTunnelSecret(ctx context.Context, c reader, namespace string, secret *cfv1alpha1.CloudflareTunnelSecret) (string, error) {
	if secret == nil {
		return "", errValueUnset
	}
	if secret.Value != nil {
		return *secret.Value, nil
	}
	if secret.ValueFrom == nil {
		return "", fmt.Errorf("tunnel secret has neither value nor valueFrom")
	}

	if c == nil {
		return "", fmt.Errorf("no reader configured for tunnel secret references")
	}

	secretRef, configMapRef := secret.ValueFrom.SecretKeyRef, secret.ValueFrom.ConfigMapKeyRef
	switch {
	case secretRef != nil && configMapRef != nil:
		return "", fmt.Errorf("valueFrom has both secretKeyRef and configMapKeyRef")
	case secretRef != nil:
		return resolveSecretKey(ctx, c, namespace, secretRef)
	case configMapRef != nil:
		return resolveConfigMapKey(ctx, c, namespace, configMapRef)
	default:
		return "", fmt.Errorf("valueFrom has neither secretKeyRef nor configMapKeyRef")
	}
}

func resolveSecretKey(ctx context.Context, c reader, namespace string, ref *corev1.SecretKeySelector) (string, error) {
	source := &corev1.Secret{}
	key := kclient.ObjectKey{Namespace: namespace, Name: ref.Name}

	if err := c.Get(ctx, key, source); err != nil {
		if apierrors.IsNotFound(err) && optional(ref.Optional) {
			return "", errValueUnset
		}

		return "", fmt.Errorf("get secret %s: %w", key, err)
	}

	value, ok := source.Data[ref.Key]
	if !ok {
		if optional(ref.Optional) {
			return "", errValueUnset
		}

		return "", fmt.Errorf("secret %s has no key %s", key, ref.Key)
	}

	return string(value), nil
}

func resolveConfigMapKey(ctx context.Context, c reader, namespace string, ref *corev1.ConfigMapKeySelector) (string, error) {
	source := &corev1.ConfigMap{}
	key := kclient.ObjectKey{Namespace: namespace, Name: ref.Name}

	if err := c.Get(ctx, key, source); err != nil {
		if apierrors.IsNotFound(err) && optional(ref.Optional) {
			return "", errValueUnset
		}

		return "", fmt.Errorf("get configmap %s: %w", key, err)
	}

	value, ok := source.Data[ref.Key]
	if !ok {
		if optional(ref.Optional) {
			return "", errValueUnset
		}

		return "", fmt.Errorf("configmap %s has no key %s", key, ref.Key)
	}

	return value, nil
}

func optional(o *bool) bool {
	return o != nil && *o
}
