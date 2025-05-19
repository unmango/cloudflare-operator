/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cloudflare/cloudflare-go/v4"
	"github.com/cloudflare/cloudflare-go/v4/dns"
	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	cfclient "github.com/unmango/cloudflare-operator/internal/client"
)

const (
	dnsRecordFinalizer = "dnsrecord.cloudflare.unmango.dev/finalizer"
)

// DnsRecordReconciler reconciles a DnsRecord object
type DnsRecordReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Cloudflare cfclient.Client
}

// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=dnsrecords,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=dnsrecords/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cloudflare.unmango.dev,resources=dnsrecords/finalizers,verbs=update

func (r *DnsRecordReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	record := &cfv1alpha1.DnsRecord{}
	if err := r.Get(ctx, req.NamespacedName, record); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !controllerutil.ContainsFinalizer(record, dnsRecordFinalizer) {
		if err := patch(ctx, r, record, func(obj *cfv1alpha1.DnsRecord) {
			_ = controllerutil.AddFinalizer(record, dnsRecordFinalizer)
		}); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
	}

	if id := record.Status.Id; id == nil {
		log.Info("Creating DnsRecord")
		res, err := r.Cloudflare.CreateDnsRecord(ctx, dns.RecordNewParams{
			ZoneID: cloudflare.F(record.Spec.ZoneId),
			Record: r.toCloudflare(record),
		})
		if err != nil {
			log.Error(err, "Failed to create DNS record")
			return ctrl.Result{}, nil
		}

		if err := patchSubResource(ctx, r.Status(), record, func(obj *cfv1alpha1.DnsRecord) {
			obj.Status.Id = &res.ID
			obj.Status.Comment = &res.Comment
			obj.Status.Content = &res.Content
			obj.Status.Name = &res.Name
			obj.Status.Type = ptr.To(string(res.Type))
		}); err != nil {
			return ctrl.Result{}, nil
		} else {
			return ctrl.Result{Requeue: true}, nil
		}
	} else if !record.DeletionTimestamp.IsZero() {
		log.Info("Deleting DnsRecord", "id", id)
		res, err := r.Cloudflare.DeleteDnsRecord(ctx, *id, dns.RecordDeleteParams{
			ZoneID: cloudflare.F(record.Spec.ZoneId),
		})
		if err != nil {
			log.Error(err, "Failed to delete DNS record")
			return ctrl.Result{}, cfclient.IgnoreNotFound(err)
		}

		if controllerutil.RemoveFinalizer(record, dnsRecordFinalizer) {
			if err := r.Update(ctx, record); err != nil {
				return ctrl.Result{}, nil
			}
		}

		log.Info("Successfully deleted DnsRecord", "id", res.ID)
		return ctrl.Result{}, nil
	} else {
		log.Info("Refreshing DnsRecord")
		res, err := r.Cloudflare.GetDnsRecord(ctx, *id, dns.RecordGetParams{
			ZoneID: cloudflare.F(record.Spec.ZoneId),
		})
		if err != nil {
			log.Error(err, "Failed to read DNS record")
			return ctrl.Result{}, nil
		}

		if err := patchSubResource(ctx, r.Status(), record, func(obj *cfv1alpha1.DnsRecord) {
			obj.Status.Comment = &res.Comment
			obj.Status.Content = &res.Content
			obj.Status.Id = &res.ID
			obj.Status.Name = &res.Name
			obj.Status.Type = ptr.To(string(res.Type))
		}); err != nil {
			return ctrl.Result{}, nil
		}
	}

	if r.diff(record) {
		log.Info("Updating DnsRecord")
		res, err := r.Cloudflare.UpdateDnsRecord(ctx, *record.Status.Id, dns.RecordUpdateParams{
			ZoneID: cloudflare.F(record.Spec.ZoneId),
			Record: r.toCloudflare(record),
		})
		if err != nil {
			log.Error(err, "Failed to update DNS record")
			return ctrl.Result{}, nil
		}

		if err := patchSubResource(ctx, r.Status(), record, func(obj *cfv1alpha1.DnsRecord) {
			obj.Status.Id = &res.ID
			obj.Status.Comment = &res.Comment
			obj.Status.Content = &res.Content
			obj.Status.Name = &res.Name
			obj.Status.Type = ptr.To(string(res.Type))
		}); err != nil {
			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DnsRecordReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cfv1alpha1.DnsRecord{}).
		Named("dnsrecord").
		Complete(r)
}

func (r *DnsRecordReconciler) diff(record *cfv1alpha1.DnsRecord) bool {
	status := record.Status
	var conditions []bool

	if spec := record.Spec.AAAARecord; spec != nil {
		conditions = []bool{
			ptr.Equal(status.Comment, &spec.Comment),
			ptr.Equal(status.Content, &spec.Content),
			ptr.Equal(status.Name, &spec.Name),
			ptr.Equal(status.Type, &spec.Type),
		}
	}
	if spec := record.Spec.ARecord; spec != nil {
		conditions = []bool{
			ptr.Equal(status.Comment, &spec.Comment),
			ptr.Equal(status.Content, &spec.Content),
			ptr.Equal(status.Name, &spec.Name),
			ptr.Equal(status.Type, &spec.Type),
		}
	}
	if spec := record.Spec.CAARecord; spec != nil {
		conditions = []bool{
			ptr.Equal(status.Comment, &spec.Comment),
			ptr.Equal(status.Content, &spec.Content),
			ptr.Equal(status.Name, &spec.Name),
			ptr.Equal(status.Type, &spec.Type),
		}
	}
	if spec := record.Spec.CNAMERecord; spec != nil {
		conditions = []bool{
			ptr.Equal(status.Comment, &spec.Comment),
			ptr.Equal(status.Content, &spec.Content),
			ptr.Equal(status.Name, &spec.Name),
			ptr.Equal(status.Type, &spec.Type),
		}
	}
	if spec := record.Spec.TXTRecord; spec != nil {
		conditions = []bool{
			ptr.Equal(status.Comment, &spec.Comment),
			ptr.Equal(status.Content, &spec.Content),
			ptr.Equal(status.Name, &spec.Name),
			ptr.Equal(status.Type, &spec.Type),
		}
	}

	// Logical AND
	return slices.Contains(conditions, false)
}

func (DnsRecordReconciler) toCloudflare(record *cfv1alpha1.DnsRecord) dns.RecordUnionParam {
	if r := record.Spec.AAAARecord; r != nil {
		return dns.AAAARecordParam{
			Comment: cloudflare.F(r.Comment),
			Content: cloudflare.F(r.Content),
			Name:    cloudflare.F(r.Name),
			Proxied: cloudflare.F(r.Proxied),
			Settings: cloudflare.F(dns.AAAARecordSettingsParam{
				IPV4Only: cloudflare.F(r.Settings.Ipv4Only),
				IPV6Only: cloudflare.F(r.Settings.Ipv6Only),
			}),
			Tags: cloudflare.F(toRecordTags(r.Tags)),
			TTL:  cloudflare.F(dns.TTL(r.Ttl)),
			Type: cloudflare.F(dns.AAAARecordType(r.Type)),
		}
	}
	if r := record.Spec.ARecord; r != nil {
		return dns.ARecordParam{
			Comment: cloudflare.F(r.Comment),
			Content: cloudflare.F(r.Content),
			Name:    cloudflare.F(r.Name),
			Proxied: cloudflare.F(r.Proxied),
			Settings: cloudflare.F(dns.ARecordSettingsParam{
				IPV4Only: cloudflare.F(r.Settings.Ipv4Only),
				IPV6Only: cloudflare.F(r.Settings.Ipv6Only),
			}),
			Tags: cloudflare.F(toRecordTags(r.Tags)),
			TTL:  cloudflare.F(dns.TTL(r.Ttl)),
			Type: cloudflare.F(dns.ARecordType(r.Type)),
		}
	}
	if r := record.Spec.CAARecord; r != nil {
		return dns.CAARecordParam{
			Comment: cloudflare.F(r.Comment),
			Data: cloudflare.F(dns.CAARecordDataParam{
				Flags: cloudflare.F(float64(r.Data.Flags)),
				Tag:   cloudflare.F(r.Data.Tag),
				Value: cloudflare.F(r.Data.Value),
			}),
			Name:    cloudflare.F(r.Name),
			Proxied: cloudflare.F(r.Proxied),
			Settings: cloudflare.F(dns.CAARecordSettingsParam{
				IPV4Only: cloudflare.F(r.Settings.Ipv4Only),
				IPV6Only: cloudflare.F(r.Settings.Ipv6Only),
			}),
			Tags: cloudflare.F(toRecordTags(r.Tags)),
			TTL:  cloudflare.F(dns.TTL(r.Ttl)),
			Type: cloudflare.F(dns.CAARecordType(r.Type)),
		}
	}
	if r := record.Spec.CNAMERecord; r != nil {
		return dns.CNAMERecordParam{
			Comment: cloudflare.F(r.Comment),
			Content: cloudflare.F(r.Content),
			Name:    cloudflare.F(r.Name),
			Proxied: cloudflare.F(r.Proxied),
			Settings: cloudflare.F(dns.CNAMERecordSettingsParam{
				IPV4Only: cloudflare.F(r.Settings.Ipv4Only),
				IPV6Only: cloudflare.F(r.Settings.Ipv6Only),
			}),
			Tags: cloudflare.F(toRecordTags(r.Tags)),
			TTL:  cloudflare.F(dns.TTL(r.Ttl)),
			Type: cloudflare.F(dns.CNAMERecordType(r.Type)),
		}
	}
	if r := record.Spec.TXTRecord; r != nil {
		return dns.TXTRecordParam{
			Comment: cloudflare.F(r.Comment),
			Content: cloudflare.F(r.Content),
			Name:    cloudflare.F(r.Name),
			Proxied: cloudflare.F(r.Proxied),
			Settings: cloudflare.F(dns.TXTRecordSettingsParam{
				IPV4Only: cloudflare.F(r.Settings.Ipv4Only),
				IPV6Only: cloudflare.F(r.Settings.Ipv6Only),
			}),
			Tags: cloudflare.F(toRecordTags(r.Tags)),
			TTL:  cloudflare.F(dns.TTL(r.Ttl)),
			Type: cloudflare.F(dns.TXTRecordType(r.Type)),
		}
	}

	return nil
}

func toRecordTags[T ~string](tags []T) (out []dns.RecordTags) {
	for _, t := range tags {
		out = append(out, dns.RecordTags(t))
	}

	return out
}
