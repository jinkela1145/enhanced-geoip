// Package config loads config.json, the single place that holds the database
// name prefix, the repository name and the upstream URLs.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Source keys used by the builder.
const (
	SrcDBIPCity       = "dbip_city"
	SrcDBIPASN        = "dbip_asn"
	SrcIPtoASN        = "iptoasn"
	SrcCloudflareV4   = "cloudflare_v4"
	SrcCloudflareV6   = "cloudflare_v6"
	SrcFastly         = "fastly"
	SrcAWS            = "aws"
	SrcGCP            = "gcp"
	SrcOracle         = "oracle"
	SrcAzurePage      = "azure_page"
	SrcAzure          = "azure" // resolved from SrcAzurePage at fetch time
	SrcAPNIC          = "apnic_delegated"
	SrcGeoNamesAdmin1 = "geonames_admin1"
	SrcGeoNamesCN     = "geonames_cn"
	SrcGeoNamesCNAlt  = "geonames_cn_altnames"
)

// DailySources are downloaded by every build, in this order.
var DailySources = []string{
	SrcDBIPCity, SrcIPtoASN,
	SrcCloudflareV4, SrcCloudflareV6, SrcFastly,
	SrcAWS, SrcGCP, SrcOracle, SrcAzurePage,
	SrcAPNIC,
}

// Radius holds default accuracy radii in kilometres.
type Radius struct {
	City        uint16 `json:"city"`
	Subdivision uint16 `json:"subdivision"`
	Country     uint16 `json:"country"`
	Anycast     uint16 `json:"anycast"`
}

// Lite holds settings of the map-oriented Lite database.
type Lite struct {
	CoordStep   float64  `json:"coord_step"`
	RadiusTiers []uint16 `json:"radius_tiers_km"`
	MaxSizeMB   int      `json:"max_size_mb"`
}

// Release holds publishing settings used by the workflow.
type Release struct {
	Keep          int    `json:"keep"`
	Branch        string `json:"branch"`
	ReportsBranch string `json:"reports_branch"`
}

// Config is the content of config.json.
type Config struct {
	Name            string            `json:"name"`
	Repo            string            `json:"repo"`
	Description     map[string]string `json:"description"`
	RadiusKM        Radius            `json:"radius_km"`
	Lite            Lite              `json:"lite"`
	Release         Release           `json:"release"`
	DisabledSources []string          `json:"disabled_sources"`
	Sources         map[string]string `json:"sources"`
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// Validate checks that required values are present and sane.
func (c *Config) Validate() error {
	var errs []error
	if c.Name == "" || strings.ContainsAny(c.Name, " /\\<>:\"|?*") {
		errs = append(errs, errors.New("name must be non-empty and usable in file names"))
	}
	if !strings.Contains(c.Repo, "/") {
		errs = append(errs, errors.New("repo must look like owner/name"))
	}
	if c.Description["en"] == "" {
		errs = append(errs, errors.New("description.en is required"))
	}
	r := c.RadiusKM
	if r.City == 0 || r.Subdivision == 0 || r.Country == 0 || r.Anycast == 0 {
		errs = append(errs, errors.New("radius_km values must be > 0"))
	}
	if c.Lite.CoordStep <= 0 || c.Lite.CoordStep > 10 {
		errs = append(errs, errors.New("lite.coord_step must be in (0, 10]"))
	}
	if len(c.Lite.RadiusTiers) == 0 || !slices.IsSorted(c.Lite.RadiusTiers) {
		errs = append(errs, errors.New("lite.radius_tiers_km must be a non-empty ascending list"))
	}
	for _, s := range DailySources {
		if c.Enabled(s) && c.Sources[s] == "" {
			errs = append(errs, fmt.Errorf("sources.%s is missing", s))
		}
	}
	if !c.Enabled(SrcDBIPCity) {
		errs = append(errs, errors.New("dbip_city is the base layer and cannot be disabled"))
	}
	return errors.Join(errs...)
}

// Enabled reports whether a source is enabled.
func (c *Config) Enabled(name string) bool {
	if name == SrcAzure {
		name = SrcAzurePage
	}
	return !slices.Contains(c.DisabledSources, name)
}

// FullType is the metadata database_type of the full database.
func (c *Config) FullType() string { return c.Name + "-City" }

// LiteType is the metadata database_type of the Lite database.
func (c *Config) LiteType() string { return c.Name + "-City-Lite" }

// FullFile is the file name of the full database.
func (c *Config) FullFile() string { return c.FullType() + ".mmdb" }

// LiteFile is the file name of the Lite database.
func (c *Config) LiteFile() string { return c.LiteType() + ".mmdb" }

// UserAgent identifies the builder to upstream servers.
func (c *Config) UserAgent() string {
	return fmt.Sprintf("%s-builder/1 (+https://github.com/%s)", strings.ToLower(c.Name), c.Repo)
}
