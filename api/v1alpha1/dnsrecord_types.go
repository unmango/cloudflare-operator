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

type CNAMERecordSettings struct {
	RecordSettings `json:",inline"`

	// If enabled, causes the CNAME record to be resolved externally and the resulting address records (e.g., A and AAAA) to be returned instead of the CNAME record itself.
	// This setting is unavailable for proxied records, since they are always flattened.
	FlattenCname bool `json:"flattenCname,omitempty"`
}

type ARecord struct {
	// Comments or notes about the DNS record. This field has no effect on DNS responses.
	//
	// +optional
	Comment string `json:"comment,omitempty"`

	// A valid IPv4 address.
	//
	// +optional
	Content string `json:"content,omitempty"`

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
	// +kubebuilder:default:=A
	// +optional
	Type string `json:"type,omitempty"`
}

type AAAARecord struct {
	// Comments or notes about the DNS record. This field has no effect on DNS responses.
	//
	// +optional
	Comment string `json:"comment,omitempty"`

	// A valid IPv4 address.
	//
	// +optional
	Content string `json:"content,omitempty"`

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
	// +kubebuilder:default:=AAAA
	// +optional
	Type string `json:"type,omitempty"`
}

// Components of a CAA record.
type CAARecordData struct {
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

type CAARecord struct {
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
	Data CAARecordData `json:"data,omitempty"`

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
	// +kubebuilder:default:=CAA
	// +optional
	Type string `json:"type,omitempty"`
}

// Components of a CERT record.
type CERTRecordData struct {
	// Algorithm.
	//
	// +optional
	Algorithm int64 `json:"algorithm,omitempty"`

	// Certificate.
	//
	// +optional
	Certificate string `json:"certificate,omitempty"`

	// Key tag.
	//
	// +optional
	KeyTag int64 `json:"keyTag,omitempty"`

	// Type.
	//
	// +optional
	Type int64 `json:"type,omitempty"`
}

type CERTRecord struct {
	// Comments or notes about the DNS record. This field has no effect on DNS responses.
	//
	// +optional
	Comment string `json:"comment,omitempty"`

	// Formatted CERT content. See 'data' to set CERT properties.
	//
	// +optional
	Content string `json:"content,omitempty"`

	// Components of a CERT record.
	//
	// +optional
	Data CERTRecordData `json:"data,omitempty"`

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
	// +kubebuilder:default:=CERT
	// +optional
	Type string `json:"type,omitempty"`
}

type CNAMERecord struct {
	// Comments or notes about the DNS record. This field has no effect on DNS responses.
	//
	// +optional
	Comment string `json:"comment,omitempty"`

	// A valid IPv4 address.
	//
	// +optional
	Content string `json:"content,omitempty"`

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
	Settings CNAMERecordSettings `json:"settings,omitempty"`

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
	// +kubebuilder:default:=CNAME
	// +optional
	Type string `json:"type,omitempty"`
}

type TXTRecord struct {
	// Comments or notes about the DNS record. This field has no effect on DNS responses.
	//
	// +optional
	Comment string `json:"comment,omitempty"`

	// Text content for the record. The content must consist of quoted "character strings" (RFC 1035), each with a length of up to 255 bytes.
	// Strings exceeding this allowed maximum length are automatically split.
	// Learn more at https://www.cloudflare.com/learning/dns/dns-records/dns-txt-record/.
	//
	// +optional
	Content string `json:"content,omitempty"`

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
	// +kubebuilder:default:=TXT
	// +optional
	Type string `json:"type,omitempty"`
}

// +kubebuilder:validation:MaxProperties:=1
// +kubebuilder:validation:MinProperties:=1
type Record struct {
	// +optional
	ARecord *ARecord `json:"aRecord,omitempty"`

	// +optional
	AAAARecord *AAAARecord `json:"aaaaRecord,omitempty"`

	// +optional
	CAARecord *CAARecord `json:"caaRecord,omitempty"`

	// +optional
	CNAMERecord *CNAMERecord `json:"cnameRecord,omitempty"`

	// +optional
	TXTRecord *TXTRecord `json:"txtRecrod,omitempty"`
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
	Record `json:"record"`
}

// DnsRecordStatus defines the observed state of DnsRecord.
type DnsRecordStatus struct {
	// +optional
	Id *string `json:"id,omitempty"`

	// +optional
	Name *string `json:"name,omitempty"`

	// +optional
	Comment *string `json:"comment,omitempty"`

	// +optional
	Content *string `json:"content,omitempty"`

	// +optional
	Type *string `json:"type,omitempty"`
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
