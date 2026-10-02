package sources

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"strings"

	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
)

// NetFlags marks the type of network an address belongs to.
type NetFlags struct {
	Anycast bool
	CDN     bool
	Cloud   string // lower-case vendor code: aws, gcp, azure, oracle
	Region  string // vendor region code, e.g. ap-northeast-1
}

// IsZero reports whether no flag is set.
func (f NetFlags) IsZero() bool { return f == NetFlags{} }

// Merge combines two flag sets: booleans are OR-ed, strings from o win when
// non-empty (callers pass the more specific entry as o).
func (f NetFlags) Merge(o NetFlags) NetFlags {
	f.Anycast = f.Anycast || o.Anycast
	f.CDN = f.CDN || o.CDN
	if o.Cloud != "" {
		f.Cloud = o.Cloud
	}
	if o.Region != "" {
		f.Region = o.Region
	}
	return f
}

// FlagEntry is a network with flags, taken from a public list.
type FlagEntry struct {
	R      iprange.Range
	Bits   int // prefix length, used to let more specific entries win
	Flags  NetFlags
	Source string
}

func prefixEntry(s string, flags NetFlags, source string) (FlagEntry, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(s))
	if err != nil {
		return FlagEntry{}, fmt.Errorf("%s: %w", source, err)
	}
	p = p.Masked()
	bits := p.Bits()
	if p.Addr().Is4In6() {
		bits -= 96
	}
	return FlagEntry{R: iprange.FromPrefix(p), Bits: bits, Flags: flags, Source: source}, nil
}

func needEntries(source string, es []FlagEntry, err error) ([]FlagEntry, error) {
	if err != nil {
		return nil, err
	}
	if len(es) == 0 {
		return nil, fmt.Errorf("%s: no prefixes found (format changed?)", source)
	}
	return es, nil
}

// ParseCIDRList parses a plain text list with one CIDR per line (Cloudflare's
// ips-v4 / ips-v6).
func ParseCIDRList(r io.Reader, flags NetFlags, source string) ([]FlagEntry, error) {
	var out []FlagEntry
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		e, err := prefixEntry(line, flags, source)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return needEntries(source, out, sc.Err())
}

// ParseFastly parses https://api.fastly.com/public-ip-list.
func ParseFastly(r io.Reader, flags NetFlags) ([]FlagEntry, error) {
	var doc struct {
		Addresses     []string `json:"addresses"`
		IPv6Addresses []string `json:"ipv6_addresses"`
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("fastly: %w", err)
	}
	if len(doc.Addresses) == 0 || len(doc.IPv6Addresses) == 0 {
		return nil, errors.New("fastly: missing addresses or ipv6_addresses (format changed?)")
	}
	var out []FlagEntry
	for _, s := range append(doc.Addresses, doc.IPv6Addresses...) {
		e, err := prefixEntry(s, flags, "fastly")
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// ParseAWS parses ip-ranges.json. Every prefix is marked cloud=aws with its
// region; CLOUDFRONT prefixes are marked cdn and GLOBALACCELERATOR prefixes
// anycast.
func ParseAWS(r io.Reader) ([]FlagEntry, string, error) {
	var doc struct {
		SyncToken  string `json:"syncToken"`
		CreateDate string `json:"createDate"`
		Prefixes   []struct {
			IPPrefix string `json:"ip_prefix"`
			Region   string `json:"region"`
			Service  string `json:"service"`
		} `json:"prefixes"`
		IPv6Prefixes []struct {
			IPv6Prefix string `json:"ipv6_prefix"`
			Region     string `json:"region"`
			Service    string `json:"service"`
		} `json:"ipv6_prefixes"`
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, "", fmt.Errorf("aws: %w", err)
	}
	if len(doc.Prefixes) == 0 || len(doc.IPv6Prefixes) == 0 {
		return nil, "", errors.New("aws: missing prefixes or ipv6_prefixes (format changed?)")
	}
	flagsFor := func(region, service string) NetFlags {
		f := NetFlags{Cloud: "aws"}
		if region != "" && !strings.EqualFold(region, "GLOBAL") {
			f.Region = region
		}
		switch service {
		case "CLOUDFRONT", "CLOUDFRONT_ORIGIN_FACING":
			if service == "CLOUDFRONT" {
				f.CDN = true
			}
		case "GLOBALACCELERATOR":
			f.Anycast = true
		}
		return f
	}
	var out []FlagEntry
	for _, p := range doc.Prefixes {
		e, err := prefixEntry(p.IPPrefix, flagsFor(p.Region, p.Service), "aws:"+p.Service)
		if err != nil {
			return nil, "", err
		}
		out = append(out, e)
	}
	for _, p := range doc.IPv6Prefixes {
		e, err := prefixEntry(p.IPv6Prefix, flagsFor(p.Region, p.Service), "aws:"+p.Service)
		if err != nil {
			return nil, "", err
		}
		out = append(out, e)
	}
	return out, doc.CreateDate, nil
}

// ParseGCP parses Google's cloud.json (Google Cloud customer ranges).
func ParseGCP(r io.Reader) ([]FlagEntry, string, error) {
	var doc struct {
		CreationTime string `json:"creationTime"`
		Prefixes     []struct {
			IPv4  string `json:"ipv4Prefix"`
			IPv6  string `json:"ipv6Prefix"`
			Scope string `json:"scope"`
		} `json:"prefixes"`
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, "", fmt.Errorf("gcp: %w", err)
	}
	var out []FlagEntry
	for _, p := range doc.Prefixes {
		f := NetFlags{Cloud: "gcp"}
		if p.Scope != "" && !strings.EqualFold(p.Scope, "global") {
			f.Region = p.Scope
		}
		for _, s := range []string{p.IPv4, p.IPv6} {
			if s == "" {
				continue
			}
			e, err := prefixEntry(s, f, "gcp")
			if err != nil {
				return nil, "", err
			}
			out = append(out, e)
		}
	}
	out, err := needEntries("gcp", out, nil)
	return out, doc.CreationTime, err
}

// ParseOracle parses OCI's public_ip_ranges.json.
func ParseOracle(r io.Reader) ([]FlagEntry, string, error) {
	var doc struct {
		LastUpdated string `json:"last_updated_timestamp"`
		Regions     []struct {
			Region string `json:"region"`
			CIDRs  []struct {
				CIDR string `json:"cidr"`
			} `json:"cidrs"`
		} `json:"regions"`
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, "", fmt.Errorf("oracle: %w", err)
	}
	var out []FlagEntry
	for _, reg := range doc.Regions {
		for _, c := range reg.CIDRs {
			e, err := prefixEntry(c.CIDR, NetFlags{Cloud: "oracle", Region: reg.Region}, "oracle")
			if err != nil {
				return nil, "", err
			}
			out = append(out, e)
		}
	}
	out, err := needEntries("oracle", out, nil)
	return out, doc.LastUpdated, err
}

// ParseAzure parses the weekly ServiceTags_Public_*.json. The AzureCloud and
// AzureCloud.<region> tags give cloud=azure with the region; the
// AzureFrontDoor.Frontend tag is marked cdn.
func ParseAzure(r io.Reader) ([]FlagEntry, string, error) {
	var doc struct {
		ChangeNumber int    `json:"changeNumber"`
		Cloud        string `json:"cloud"`
		Values       []struct {
			Name       string `json:"name"`
			Properties struct {
				Region          string   `json:"region"`
				AddressPrefixes []string `json:"addressPrefixes"`
			} `json:"properties"`
		} `json:"values"`
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, "", fmt.Errorf("azure: %w", err)
	}
	var out []FlagEntry
	for _, v := range doc.Values {
		var f NetFlags
		switch {
		case v.Name == "AzureCloud":
			f = NetFlags{Cloud: "azure"}
		case strings.HasPrefix(v.Name, "AzureCloud."):
			f = NetFlags{Cloud: "azure", Region: v.Properties.Region}
		case v.Name == "AzureFrontDoor.Frontend":
			f = NetFlags{Cloud: "azure", CDN: true}
		default:
			continue
		}
		for _, s := range v.Properties.AddressPrefixes {
			e, err := prefixEntry(s, f, "azure:"+v.Name)
			if err != nil {
				return nil, "", err
			}
			out = append(out, e)
		}
	}
	out, err := needEntries("azure", out, nil)
	return out, fmt.Sprint(doc.ChangeNumber), err
}

var azureURLRe = regexp.MustCompile(`https:(?:\\?/){2}download\.microsoft\.com(?:\\?/)[^"'\s<>]*?ServiceTags_Public_\d{8}\.json`)

// FindAzureURL extracts the current ServiceTags_Public JSON link from the
// Microsoft Download Center page.
func FindAzureURL(page []byte) (string, error) {
	m := azureURLRe.Find(page)
	if m == nil {
		return "", errors.New("azure: ServiceTags_Public link not found on the download page (format changed?)")
	}
	return strings.ReplaceAll(string(m), `\/`, `/`), nil
}
