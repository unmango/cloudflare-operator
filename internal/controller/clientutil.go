package controller

// https://github.com/actions/actions-runner-controller/blob/32f19acc66da617f57cf6960b9ca283958d40768/controllers/actions.github.com/clientutil.go

import (
	"context"

	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type object[T kclient.Object] interface {
	kclient.Object
	DeepCopy() T
}

type patcher interface {
	Patch(ctx context.Context, obj kclient.Object, patch kclient.Patch, opts ...kclient.PatchOption) error
}

func patch[T object[T]](ctx context.Context, client patcher, obj T, update func(obj T)) error {
	original := obj.DeepCopy()
	update(obj)
	return client.Patch(ctx, obj, kclient.MergeFrom(original))
}

type subResourcePatcher interface {
	Patch(ctx context.Context, obj kclient.Object, patch kclient.Patch, opts ...kclient.SubResourcePatchOption) error
}

func patchSubResource[T object[T]](ctx context.Context, client subResourcePatcher, obj T, update func(obj T)) error {
	original := obj.DeepCopy()
	update(obj)
	return client.Patch(ctx, obj, kclient.MergeFrom(original))
}
