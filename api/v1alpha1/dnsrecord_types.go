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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RecordTags string

// Settings for the DNS record.
type RecordSettings struct {
	// When enabled, only A records will be generated, and AAAA records will not be created.
	// This setting is intended for exceptional cases.
	// Note that this option only applies to proxied records and it has no effect on whether Cloudflare communicates with the origin using IPv4 or IPv6.
	//
	// +optional
	Ipv4Only bool `json:"ipv4Only,omitempty"`

	// When enabled, only AAAA records will be generated, and A records will not be created.
	// This setting is intended for exceptional cases.
	// Note that this option only applies to proxied records and it has no effect on whether Cloudflare communicates with the origin using IPv4 or IPv6.
	//
	// +optional
	Ipv6Only bool `json:"ipv6Only,omitempty"`
}

// Components of a CAA record.
type RecordData struct {
	// Flags for the CAA record.
	//
	// +kubebuilder:validation:Maximum:=255
	// +kubebuilder:validation:Minimum:=0
	// +optional
	Flags int64 `json:"flags,omitempty"`

	// Name of the property controlled by this record (e.g.: issue, issuewild, iodef).
	//
	// +optional
	Tag string `json:"tag,omitempty"`

	// Value of the record. This field's semantics depend on the chosen tag.
	//
	// +optional
	Value string `json:"value,omitempty"`
}

type Record struct {
	// Comments or notes about the DNS record. This field has no effect on DNS responses.
	//
	// +optional
	Comment string `json:"comment,omitempty"`

	// A valid IPv4 address.
	//
	// +optional
	Content string `json:"content,omitempty"`

	// Components of a CAA record.
	//
	// +optional
	Data RecordData `json:"data,omitempty"`

	// DNS record name (or @ for the zone apex) in Punycode.
	//
	// +kubebuilder:validation:MaxLength:=255
	// +kubebuilder:validation:MinLength:=1
	// +optional
	Name string `json:"name,omitempty"`

	// Whether the record is receiving the performance and security benefits of Cloudflare.
	//
	// +optional
	Proxied bool `json:"proxied,omitempty"`

	// Settings for the DNS record.
	//
	// +optional
	Settings RecordSettings `json:"settings,omitempty"`

	// Custom tags for the DNS record.
	// This field has no effect on DNS responses.
	//
	// +optional
	Tags []RecordTags `json:"tags,omitempty"`

	// Time To Live (TTL) of the DNS record in seconds. Setting to 1 means 'automatic'.
	// Value must be between 60 and 86400, with the minimum reduced to 30 for Enterprise zones.
	//
	// +optional
	Ttl int64 `json:"ttl,omitempty"`

	// Record type.
	//
	// +optional
	Type string `json:"type,omitempty"`
}

// DnsRecordSpec defines the desired state of DnsRecord.
type DnsRecordSpec struct {
	// Identifier.
	//
	// +kubebuilder:validation:MaxLength:=32
	// +required
	ZoneId string `json:"zoneId"`

	// Record details.
	//
	// +required
	Record Record `json:"record"`
}

// DnsRecordStatus defines the observed state of DnsRecord.
type DnsRecordStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// DnsRecord is the Schema for the dnsrecords API.
type DnsRecord struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DnsRecordSpec   `json:"spec,omitempty"`
	Status DnsRecordStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DnsRecordList contains a list of DnsRecord.
type DnsRecordList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DnsRecord `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DnsRecord{}, &DnsRecordList{})
}
