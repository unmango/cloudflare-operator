package controller

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/zones"
	cfv1alpha1 "github.com/unmango/cloudflare-operator/api/v1alpha1"
	cfclient "github.com/unmango/cloudflare-operator/internal/client"
)

const typeDnsReadyCloudflareTunnel = "DnsReady"

const (
	reasonDnsRecordsReady   = "RecordsReady"
	reasonDnsRecordsPending = "RecordsPending"
)

// tunnelDns is what reconciling a tunnel's DNS records found, for the status.
type tunnelDns struct {
	desired, ready int32

	// problems are manifest errors, one per hostname that was skipped.
	problems []string
}

// apply writes the record counts and the DnsReady condition. A tunnel that asks
// for no records carries no condition, so DNS stays invisible until it is used.
func (d tunnelDns) apply(obj *cfv1alpha1.CloudflareTunnel) {
	obj.Status.DnsRecords = d.desired
	obj.Status.DnsRecordsReady = d.ready

	if d.desired == 0 {
		_ = meta.RemoveStatusCondition(&obj.Status.Conditions, typeDnsReadyCloudflareTunnel)
		return
	}

	condition := metav1.Condition{
		Type:    typeDnsReadyCloudflareTunnel,
		Status:  metav1.ConditionTrue,
		Reason:  reasonDnsRecordsReady,
		Message: fmt.Sprintf("%d of %d DNS records point at the tunnel", d.ready, d.desired),
	}
	if d.ready < d.desired {
		condition.Status = metav1.ConditionFalse
		condition.Reason = reasonDnsRecordsPending
	}
	_ = meta.SetStatusCondition(&obj.Status.Conditions, condition)
}

// problem is the Degraded message for the hostnames that were skipped, or an
// empty string when there were none.
func (d tunnelDns) problem() string {
	return strings.Join(d.problems, "; ")
}

// desiredDnsRecords groups the tunnel's ingress entries by hostname and resolves
// the DNS settings each hostname gets. Hostnames whose entries resolve to
// different settings are reported and left out rather than settled by ingress
// order. Entries without a hostname, and hostnames no zone resolves for, get no
// record.
func desiredDnsRecords(tunnel *cfv1alpha1.CloudflareTunnel) (map[string]resolvedDns, []string) {
	if tunnel.Spec.Config == nil {
		return nil, nil
	}

	settings := map[string][]resolvedDns{}
	var order []string
	for _, entry := range tunnel.Spec.Config.Ingress {
		if entry.Hostname == "" {
			continue
		}

		host := strings.ToLower(entry.Hostname)
		if _, seen := settings[host]; !seen {
			order = append(order, host)
		}

		// An entry with no zone still counts: a hostname that is in DNS for one
		// path and not another has no single answer either.
		resolved, _ := resolveDns(tunnel.Spec.Dns, entry.Dns)
		settings[host] = append(settings[host], resolved)
	}

	desired := map[string]resolvedDns{}
	var problems []string
	for _, host := range order {
		first := settings[host][0]
		if slices.ContainsFunc(settings[host][1:], func(r resolvedDns) bool { return r != first }) {
			problems = append(problems, fmt.Sprintf("the entries for hostname %s resolve to different DNS settings", host))
			continue
		}
		if first.ZoneId != "" {
			desired[host] = first
		}
	}

	return desired, problems
}

// inZone reports whether host is the zone apex or a name below it.
func inZone(host, zone string) bool {
	zone = strings.ToLower(zone)
	return host == zone || strings.HasSuffix(host, "."+zone)
}

// dnsRecordName is the deterministic name of the DnsRecord a tunnel owns for
// host, so that every reconcile converges on the same object.
func dnsRecordName(tunnel *cfv1alpha1.CloudflareTunnel, host string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(host))
	suffix := fmt.Sprintf("-%08x", h.Sum32())

	// An object name is at most 253 characters.
	prefix := tunnel.Name
	if limit := 253 - len(suffix); len(prefix) > limit {
		prefix = strings.TrimRight(prefix[:limit], ".-")
	}

	return prefix + suffix
}

// reconcileDns converges the DnsRecords the tunnel owns on one per hostname it
// routes, each a CNAME pointing at the tunnel through tunnelRef.
func (r *CloudflareTunnelReconciler) reconcileDns(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel, tunnelId string) (tunnelDns, error) {
	log := logf.FromContext(ctx)
	desired, problems := desiredDnsRecords(tunnel)

	// Catching a hostname outside its zone here names the hostname and the zone;
	// the API rejects the record with far less context.
	zoneNames := map[string]string{}
	for _, zoneId := range slices.Sorted(maps.Keys(zoneIds(desired))) {
		zone, err := r.Cloudflare.GetZone(ctx, zones.ZoneGetParams{ZoneID: cloudflare.F(zoneId)})
		switch {
		case cfclient.IsForbidden(err):
			// A token scoped to DNS alone may not read the zone. The API still
			// rejects a record outside it, only later.
			log.V(1).Info("Cannot read the zone, skipping the hostname check", "zone", zoneId)
		case cfclient.IsNotFound(err):
			zoneNames[zoneId] = ""
		case err != nil:
			return tunnelDns{}, fmt.Errorf("reading zone %s: %w", zoneId, err)
		default:
			zoneNames[zoneId] = zone.Name
		}
	}
	for _, host := range slices.Sorted(maps.Keys(desired)) {
		zoneId := desired[host].ZoneId
		name, checked := zoneNames[zoneId]
		switch {
		case !checked:
		case name == "":
			problems = append(problems, fmt.Sprintf("hostname %s names zone %s, which does not exist", host, zoneId))
			delete(desired, host)
		case !inZone(host, name):
			problems = append(problems, fmt.Sprintf("hostname %s is not in zone %s (%s)", host, name, zoneId))
			delete(desired, host)
		}
	}

	owned, err := r.ownedDnsRecords(ctx, tunnel)
	if err != nil {
		return tunnelDns{}, err
	}

	result := tunnelDns{}
	keep := map[string]bool{}
	for _, host := range slices.Sorted(maps.Keys(desired)) {
		settings := desired[host]
		record := &cfv1alpha1.DnsRecord{ObjectMeta: metav1.ObjectMeta{
			Name:      dnsRecordName(tunnel, host),
			Namespace: tunnel.Namespace,
		}}

		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, record, func() error {
			if record.ResourceVersion != "" && !metav1.IsControlledBy(record, tunnel) {
				return errNotControlled
			}

			record.Spec = cfv1alpha1.DnsRecordSpec{
				ZoneId: settings.ZoneId,
				Record: cfv1alpha1.Record{CNAMERecord: &cfv1alpha1.CNAMERecord{
					Name:      host,
					TunnelRef: &cfv1alpha1.DnsRecordTunnelReference{Name: tunnel.Name},
					Proxied:   settings.Proxied,
					Ttl:       settings.Ttl,
					Type:      "CNAME",
				}},
			}
			return controllerutil.SetControllerReference(tunnel, record, r.Scheme)
		})
		if errors.Is(err, errNotControlled) {
			problems = append(problems, fmt.Sprintf("DnsRecord %s for hostname %s exists and is not owned by the tunnel", record.Name, host))
			continue
		}
		if err != nil {
			return tunnelDns{}, fmt.Errorf("applying DnsRecord %s: %w", record.Name, err)
		}

		keep[record.Name] = true
		result.desired++
		if dnsRecordReady(record, tunnelId) {
			result.ready++
		}
	}

	// Owner references never collect a record for a hostname that left the
	// config, because the tunnel itself is still there.
	for i := range owned {
		if keep[owned[i].Name] {
			continue
		}

		log.Info("Deleting DnsRecord for a hostname the tunnel no longer routes", "name", owned[i].Name)
		if err := client.IgnoreNotFound(r.Delete(ctx, &owned[i])); err != nil {
			return tunnelDns{}, fmt.Errorf("deleting DnsRecord %s: %w", owned[i].Name, err)
		}
	}

	result.problems = problems
	return result, nil
}

// dnsRecordReady reports whether Cloudflare holds the record pointing at the
// tunnel, which is read from the record's own status rather than from having
// created it.
func dnsRecordReady(record *cfv1alpha1.DnsRecord, tunnelId string) bool {
	status := record.Status
	return status.Id != nil && status.Content != nil && *status.Content == tunnelTarget(tunnelId)
}

func (r *CloudflareTunnelReconciler) ownedDnsRecords(ctx context.Context, tunnel *cfv1alpha1.CloudflareTunnel) ([]cfv1alpha1.DnsRecord, error) {
	records := &cfv1alpha1.DnsRecordList{}
	if err := r.List(ctx, records, client.InNamespace(tunnel.Namespace)); err != nil {
		return nil, fmt.Errorf("listing DnsRecords: %w", err)
	}

	return slices.DeleteFunc(records.Items, func(record cfv1alpha1.DnsRecord) bool {
		return !metav1.IsControlledBy(&record, tunnel)
	}), nil
}

func zoneIds(desired map[string]resolvedDns) map[string]struct{} {
	ids := map[string]struct{}{}
	for _, settings := range desired {
		ids[settings.ZoneId] = struct{}{}
	}

	return ids
}
