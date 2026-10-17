// Package provider is the Carbon Cloud provider catalog: data, not code.
//
// Six descriptors (hetzner, aws, gcp, digitalocean, proxmox, generic) describe
// regions, credential requirements and bootstrap capability. A provider whose
// credentials are absent is listed with Available() == false plus the missing
// key names; it is never hidden and never crashes boot (see ADR 0006).
package provider

import (
	"context"
	"sort"

	"github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
	"github.com/athNdev/carbon-panel/cloud/internal/cloud/secrets"
)

// Region is one schedulable provider region.
type Region struct {
	// Name is the provider-native region id (e.g. "fsn1", "us-east-1", "onprem").
	Name string
	// Title is the human label.
	Title string
}

// Descriptor describes one infrastructure provider.
type Descriptor struct {
	Name             string
	Title            string
	Regions          []Region
	StorageBackends  []string
	SupportsUserData bool
	CredentialKeys   []string
	Notes            string
}

// RegionNames returns the region ids in declared order.
func (d Descriptor) RegionNames() []string {
	out := make([]string, 0, len(d.Regions))
	for _, r := range d.Regions {
		out = append(out, r.Name)
	}
	return out
}

// HasRegion reports whether region is a known region for this provider.
func (d Descriptor) HasRegion(region string) bool {
	for _, r := range d.Regions {
		if r.Name == region {
			return true
		}
	}
	return false
}

// MissingKeys returns the credential key names absent from r.
func (d Descriptor) MissingKeys(ctx context.Context, r *secrets.Resolver) []string {
	if r == nil {
		return append([]string(nil), d.CredentialKeys...)
	}
	return r.Missing(ctx, d.CredentialKeys)
}

// Available reports whether all credential keys resolve. A provider with
// absent credentials is listed but unavailable.
func (d Descriptor) Available(ctx context.Context, r *secrets.Resolver) bool {
	return len(d.MissingKeys(ctx, r)) == 0
}

func regions(names ...string) []Region {
	out := make([]Region, 0, len(names))
	for _, n := range names {
		out = append(out, Region{Name: n, Title: n})
	}
	return out
}

var catalog = []Descriptor{
	{
		Name:             "hetzner",
		Title:            "Hetzner Cloud",
		Regions:          regions("nbg1", "fsn1", "hel1", "ash", "hil"),
		StorageBackends:  []string{"local", "s3"},
		SupportsUserData: true,
		CredentialKeys:   []string{keys.ProviderHetznerToken},
		Notes:            "Hetzner Cloud servers via hcloud_server; module cloud/terraform/modules/node/hetzner.",
	},
	{
		Name:             "aws",
		Title:            "Amazon Web Services",
		Regions:          regions("us-east-1", "us-west-2", "eu-west-1", "eu-central-1", "ap-southeast-1"),
		StorageBackends:  []string{"local", "s3"},
		SupportsUserData: true,
		CredentialKeys:   []string{keys.ProviderAWSAccessKeyID, keys.ProviderAWSSecretAccessKey},
		Notes:            "EC2 via aws_instance; module cloud/terraform/modules/node/aws.",
	},
	{
		Name:             "gcp",
		Title:            "Google Cloud Platform",
		Regions:          regions("us-central1", "europe-west1", "europe-west3", "asia-east1"),
		StorageBackends:  []string{"local", "s3"},
		SupportsUserData: true,
		CredentialKeys:   []string{keys.ProviderGCPCredentials},
		Notes:            "GCE via google_compute_instance; module cloud/terraform/modules/node/gcp.",
	},
	{
		Name:             "digitalocean",
		Title:            "DigitalOcean",
		Regions:          regions("nyc1", "nyc3", "ams3", "sgp1", "fra1"),
		StorageBackends:  []string{"local", "s3"},
		SupportsUserData: true,
		CredentialKeys:   []string{keys.ProviderDigitalOceanToken},
		Notes:            "Droplets via digitalocean_droplet; module cloud/terraform/modules/node/digitalocean.",
	},
	{
		Name:             "proxmox",
		Title:            "Proxmox VE",
		Regions:          regions("onprem", "local"),
		StorageBackends:  []string{"local", "s3"},
		SupportsUserData: true,
		CredentialKeys:   []string{keys.ProviderProxmoxEndpoint, keys.ProviderProxmoxToken},
		Notes:            "Proxmox VMs via proxmox_virtual_environment_vm; module cloud/terraform/modules/node/proxmox.",
	},
	{
		Name:             "generic",
		Title:            "Generic SSH host",
		Regions:          regions("onprem", "any"),
		StorageBackends:  []string{"local", "s3"},
		SupportsUserData: false,
		CredentialKeys:   []string{keys.ProviderGenericSSHKey},
		Notes:            "BYO host bootstrapped over SSH with shellscript user-data; module cloud/terraform/modules/node/generic.",
	},
}

// List returns all six provider descriptors in name order.
func List() []Descriptor {
	out := append([]Descriptor(nil), catalog...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns the descriptor for name, or false when unknown.
func Get(name string) (Descriptor, bool) {
	for _, d := range catalog {
		if d.Name == name {
			return d, true
		}
	}
	return Descriptor{}, false
}

// Registry is the query surface wave 3 wires against.
type Registry struct {
	resolver *secrets.Resolver
}

// NewRegistry returns a Registry that gates availability on r (nil = all unavailable).
func NewRegistry(r *secrets.Resolver) *Registry { return &Registry{resolver: r} }

// Get returns the descriptor for name, or false when unknown.
func (r *Registry) Get(name string) (Descriptor, bool) { return Get(name) }

// List returns all descriptors in name order.
func (r *Registry) List() []Descriptor { return List() }

// Available reports whether name is known and credentialed.
func (r *Registry) Available(ctx context.Context, name string) bool {
	d, ok := Get(name)
	if !ok {
		return false
	}
	return d.Available(ctx, r.resolver)
}

// MissingKeys returns the absent credential key names for a known provider,
// or nil for an unknown provider.
func (r *Registry) MissingKeys(ctx context.Context, name string) []string {
	d, ok := Get(name)
	if !ok {
		return nil
	}
	return d.MissingKeys(ctx, r.resolver)
}
