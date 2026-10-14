// Package secrets reads the TLS Secrets Gateway listeners terminate TLS with,
// watching only the namespaces a Gateway may take certificates from.
package secrets

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// UnavailableError reports that a Secret cannot be read yet, and why.
type UnavailableError struct {
	Namespace string
	Reason    string
}

func (e *UnavailableError) Error() string {
	return e.Reason
}

// Access holds one cache of TLS Secrets per namespace it is told to watch.
//
// The operator has no cluster-wide access to Secrets. It is granted read access
// one namespace at a time, by a RoleBinding the Gateway controllers create in
// each namespace a Gateway may take certificates from, so a single
// cluster-wide cache would never sync. Access starts and stops a cache per
// namespace instead.
//
// Access is a manager Runnable, and runs on every replica, since every replica
// serves xDS.
type Access struct {
	Config     *rest.Config
	HTTPClient *http.Client
	Mapper     meta.RESTMapper
	Scheme     *runtime.Scheme
	Log        logr.Logger

	mu          sync.Mutex
	ctx         context.Context
	started     chan struct{}
	once        sync.Once
	namespaces  map[string]*namespaceCache
	subscribers []subscriber
}

type namespaceCache struct {
	cache  cache.Cache
	cancel context.CancelFunc
	synced bool
}

type subscriber struct {
	ctx     context.Context
	handler handler.EventHandler
	queue   workqueue.TypedRateLimitingInterface[reconcile.Request]
}

func (a *Access) init() {
	a.once.Do(func() {
		a.started = make(chan struct{})
		a.namespaces = map[string]*namespaceCache{}
	})
}

// Start records the context namespace caches run under and blocks until it is
// done, which stops them all.
func (a *Access) Start(ctx context.Context) error {
	a.init()
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	close(a.started)

	<-ctx.Done()
	return nil
}

// NeedLeaderElection reports false: every replica reads Secrets.
func (a *Access) NeedLeaderElection() bool {
	return false
}

// Namespaces lists the namespaces Access watches.
func (a *Access) Namespaces() sets.Set[string] {
	a.init()
	a.mu.Lock()
	defer a.mu.Unlock()

	return sets.KeySet(a.namespaces)
}

// SetNamespaces starts a cache in each namespace of want that has none, and
// stops the caches of namespaces not in want.
func (a *Access) SetNamespaces(ctx context.Context, want sets.Set[string]) error {
	a.init()
	select {
	case <-a.started:
	case <-ctx.Done():
		return ctx.Err()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	changed := sets.New[string]()
	for ns, nc := range a.namespaces {
		if want.Has(ns) {
			continue
		}
		nc.cancel()
		delete(a.namespaces, ns)
		changed.Insert(ns)
		a.Log.Info("Stopped watching TLS Secrets", "namespace", ns)
	}

	for ns := range want {
		if _, ok := a.namespaces[ns]; ok {
			continue
		}
		if err := a.watch(ns); err != nil {
			return fmt.Errorf("watching Secrets in %s: %w", ns, err)
		}
		a.Log.Info("Watching TLS Secrets", "namespace", ns)
	}

	for ns := range changed {
		a.notifyLocked(ns)
	}

	return nil
}

// watch starts a cache of the TLS Secrets in ns. a.mu must be held.
func (a *Access) watch(ns string) error {
	c, err := cache.New(a.Config, cache.Options{
		HTTPClient:        a.HTTPClient,
		Mapper:            a.Mapper,
		Scheme:            a.Scheme,
		DefaultNamespaces: map[string]cache.Config{ns: {}},
		// Listeners need only TLS Secrets, and caching every Secret in the
		// namespace would hold far more than that.
		DefaultFieldSelector: fields.OneTermEqualSelector("type", string(corev1.SecretTypeTLS)),
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(a.ctx)
	informer, err := c.GetInformer(ctx, &corev1.Secret{})
	if err != nil {
		cancel()
		return err
	}
	notify := func(any) { a.notify(ns) }
	if _, err := informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc:    notify,
		UpdateFunc: func(_, obj any) { notify(obj) },
		DeleteFunc: notify,
	}); err != nil {
		cancel()
		return err
	}

	nc := &namespaceCache{cache: c, cancel: cancel}
	a.namespaces[ns] = nc

	go func() {
		if err := c.Start(ctx); err != nil {
			a.Log.Error(err, "Failed to watch TLS Secrets", "namespace", ns)
		}
	}()
	go func() {
		// The informer retries until the operator may list Secrets here,
		// which waits on the RoleBinding granting it.
		if !c.WaitForCacheSync(ctx) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.namespaces[ns] != nc {
			return
		}
		nc.synced = true
		a.notifyLocked(ns)
	}()

	return nil
}

// Get reads the TLS Secret named by key. It returns an UnavailableError when
// the namespace is not watched or its cache has not synced yet, and a NotFound
// error when the Secret does not exist or is not of type kubernetes.io/tls.
func (a *Access) Get(ctx context.Context, key types.NamespacedName) (*corev1.Secret, error) {
	a.init()
	a.mu.Lock()
	nc, ok := a.namespaces[key.Namespace]
	synced := ok && nc.synced
	a.mu.Unlock()

	if !ok {
		return nil, &UnavailableError{
			Namespace: key.Namespace,
			Reason:    fmt.Sprintf("The operator is not watching Secrets in namespace %s yet", key.Namespace),
		}
	}
	if !synced {
		return nil, &UnavailableError{
			Namespace: key.Namespace,
			Reason:    fmt.Sprintf("Waiting for access to Secrets in namespace %s", key.Namespace),
		}
	}

	secret := &corev1.Secret{}
	if err := nc.cache.Get(ctx, key, secret); err != nil {
		return nil, err
	}

	return secret, nil
}

// Source returns a source that hands h an event whenever a watched Secret
// changes, or a namespace starts or stops being readable.
func (a *Access) Source(h handler.EventHandler) source.Source {
	return &accessSource{access: a, handler: h}
}

type accessSource struct {
	access  *Access
	handler handler.EventHandler
}

func (s *accessSource) Start(ctx context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	s.access.init()
	s.access.mu.Lock()
	defer s.access.mu.Unlock()
	s.access.subscribers = append(s.access.subscribers, subscriber{ctx: ctx, handler: s.handler, queue: q})

	return nil
}

func (s *accessSource) String() string {
	return "gateway TLS Secrets"
}

func (a *Access) notify(ns string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notifyLocked(ns)
}

// notifyLocked hands every subscriber a generic event for ns. The object is a
// placeholder: Gateways are rebuilt whole, so which Secret changed does not
// matter, only that one did.
func (a *Access) notifyLocked(ns string) {
	obj := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns}}
	for _, s := range a.subscribers {
		if s.ctx.Err() != nil {
			continue
		}
		s.handler.Generic(s.ctx, event.GenericEvent{Object: obj}, s.queue)
	}
}
