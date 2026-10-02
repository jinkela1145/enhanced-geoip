package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jinkela1145/enhanced-geoip/internal/build"
	"github.com/jinkela1145/enhanced-geoip/internal/config"
	"github.com/jinkela1145/enhanced-geoip/internal/fetch"
	"github.com/jinkela1145/enhanced-geoip/internal/sources"
)

// Data file names inside the data directory.
const (
	FileCNAdmin   = "cn_admin.csv"
	FileCNCities  = "cn_cities.csv"
	FileCNASN     = "cn_asn_province.csv"
	FileAnycastAS = "anycast_asns.csv"
	FileAnycastNS = "anycast_prefixes.csv"
	FileOverrides = "overrides.csv"
)

// BuildOptions control Build.
type BuildOptions struct {
	Config   *config.Config
	Inputs   *InputsFile
	CacheDir string
	DataDir  string
	OutDir   string
	Version  string // release version, e.g. 2026.10.02
	Now      time.Time
	Logf     func(format string, args ...any)
}

// Loaded holds the parsed inputs and per-source versions.
type Loaded struct {
	In       build.Inputs
	Versions map[string]string
	Layers   map[string]int
	Epoch    int64
}

// Load parses every downloaded file and curated table.
func Load(opt BuildOptions) (*Loaded, error) {
	inputs := opt.Inputs
	path := func(name string) string { return filepath.Join(opt.CacheDir, name+".data") }
	has := func(name string) bool { _, ok := inputs.Sources[name]; return ok }
	l := &Loaded{Versions: map[string]string{}, Layers: map[string]int{}}
	tmp := filepath.Join(opt.OutDir, ".tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	base, err := sources.ReadDBIPCity(path(config.SrcDBIPCity), tmp)
	if err != nil {
		return nil, fmt.Errorf("dbip_city: %w", err)
	}
	l.In.Base = base
	l.Versions[config.SrcDBIPCity] = inputs.DBIPMonth
	l.Layers["dbip_networks"] = base.Networks
	l.Layers["dbip_records"] = len(base.Records)
	l.Epoch = int64(base.BuildEpoch)

	switch {
	case has(config.SrcIPtoASN):
		l.In.ASN, err = sources.ReadIPtoASN(path(config.SrcIPtoASN))
	case has(config.SrcDBIPASN):
		l.In.ASN, err = sources.ReadDBIPASN(path(config.SrcDBIPASN), tmp)
	}
	if err != nil {
		return nil, fmt.Errorf("asn: %w", err)
	}
	if l.In.ASN != nil {
		l.Layers["asn_ranges"] = len(l.In.ASN.V4) + len(l.In.ASN.V6)
	}

	addFlags := func(name string, es []sources.FlagEntry) {
		l.In.Flags = append(l.In.Flags, es...)
		l.Layers["flags_"+name] = len(es)
	}
	open := func(name string) (*os.File, error) { return os.Open(path(name)) }
	listParsers := []struct {
		name  string
		parse func(f *os.File) ([]sources.FlagEntry, string, error)
	}{
		{config.SrcCloudflareV4, func(f *os.File) ([]sources.FlagEntry, string, error) {
			es, err := sources.ParseCIDRList(f, sources.NetFlags{Anycast: true, CDN: true}, "cloudflare")
			return es, "", err
		}},
		{config.SrcCloudflareV6, func(f *os.File) ([]sources.FlagEntry, string, error) {
			es, err := sources.ParseCIDRList(f, sources.NetFlags{Anycast: true, CDN: true}, "cloudflare")
			return es, "", err
		}},
		{config.SrcFastly, func(f *os.File) ([]sources.FlagEntry, string, error) {
			es, err := sources.ParseFastly(f, sources.NetFlags{CDN: true})
			return es, "", err
		}},
		{config.SrcAWS, func(f *os.File) ([]sources.FlagEntry, string, error) { return sources.ParseAWS(f) }},
		{config.SrcGCP, func(f *os.File) ([]sources.FlagEntry, string, error) { return sources.ParseGCP(f) }},
		{config.SrcOracle, func(f *os.File) ([]sources.FlagEntry, string, error) { return sources.ParseOracle(f) }},
		{config.SrcAzure, func(f *os.File) ([]sources.FlagEntry, string, error) { return sources.ParseAzure(f) }},
	}
	for _, p := range listParsers {
		if !has(p.name) {
			continue
		}
		f, err := open(p.name)
		if err != nil {
			return nil, err
		}
		es, ver, err := p.parse(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		addFlags(p.name, es)
		if ver != "" {
			l.Versions[p.name] = ver
			if t, ok := parseVersionTime(ver); ok && t.Unix() > l.Epoch {
				l.Epoch = t.Unix()
			}
		}
	}

	dd := func(name string) string { return filepath.Join(opt.DataDir, name) }
	curated, err := sources.ReadAnycastNets(dd(FileAnycastNS))
	if err != nil {
		return nil, err
	}
	addFlags("curated", curated)
	if l.In.ASNFlags, err = sources.ReadAnycastASNs(dd(FileAnycastAS)); err != nil {
		return nil, err
	}
	l.Layers["anycast_asns"] = len(l.In.ASNFlags)
	if l.In.Provinces, err = sources.ReadCNAdmin(dd(FileCNAdmin)); err != nil {
		return nil, err
	}
	if l.In.Cities, err = sources.ReadCNCities(dd(FileCNCities)); err != nil {
		return nil, err
	}
	if l.In.CNASN, err = sources.ReadCNASN(dd(FileCNASN)); err != nil {
		return nil, err
	}
	if l.In.Overrides, err = sources.ReadOverrides(dd(FileOverrides)); err != nil {
		return nil, err
	}
	l.Layers["cn_provinces"] = len(l.In.Provinces)
	l.Layers["cn_cities"] = len(l.In.Cities)
	l.Layers["cn_asn_province"] = len(l.In.CNASN)
	l.Layers["overrides"] = len(l.In.Overrides)

	if has(config.SrcAPNIC) {
		v4, v6, err := sources.ReadDelegated(path(config.SrcAPNIC), "CN")
		if err != nil {
			return nil, fmt.Errorf("apnic_delegated: %w", err)
		}
		l.In.CNDelegatedV4, l.In.CNDelegatedV6 = v4, v6
	}
	return l, nil
}

// parseVersionTime understands the date formats used by the cloud lists.
func parseVersionTime(v string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02-15-04-05", "2006-01-02T15:04:05", time.RFC3339, "2006-01-02T15:04:05.999999"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// Build parses the inputs, writes the databases and all reports.
func Build(opt BuildOptions) (*Manifest, error) {
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now().UTC()
	}
	if opt.Version == "" {
		opt.Version = opt.Now.Format("2006.01.02")
	}
	l, err := Load(opt)
	if err != nil {
		return nil, err
	}
	res, err := build.Build(&l.In, build.Options{Config: opt.Config, OutDir: opt.OutDir, BuildEpoch: l.Epoch, Logf: opt.Logf})
	if err != nil {
		return nil, err
	}
	m := newManifest(opt, l, res)
	for _, o := range []build.Output{res.Full, res.Lite} {
		line := fmt.Sprintf("%s  %s\n", o.SHA256, o.File)
		if err := os.WriteFile(filepath.Join(opt.OutDir, o.File+".sha256"), []byte(line), 0o644); err != nil {
			return nil, err
		}
	}
	if err := writeJSON(filepath.Join(opt.OutDir, "manifest.json"), m); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(opt.OutDir, "ACCURACY.md"), []byte(accuracyMarkdown(m)), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(opt.OutDir, "RELEASE_NOTES.md"), []byte(releaseNotes(opt.Config, m)), 0o644); err != nil {
		return nil, err
	}
	if err := writeReports(filepath.Join(opt.OutDir, "reports"), l, res); err != nil {
		return nil, err
	}
	if len(m.Warnings) > 0 {
		if err := os.WriteFile(filepath.Join(opt.OutDir, "warnings.txt"), []byte(strings.Join(m.Warnings, "\n")+"\n"), 0o644); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// SourceInfo is the per-source part of manifest.json.
type SourceInfo struct {
	URL          string `json:"url"`
	Version      string `json:"version,omitempty"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	FetchedAt    string `json:"fetched_at"`
}

// Manifest is written to manifest.json and attached to every release.
type Manifest struct {
	Name           string                  `json:"name"`
	Version        string                  `json:"version"`
	BuildEpoch     int64                   `json:"build_epoch"`
	BuiltAt        string                  `json:"built_at"`
	BuilderVersion string                  `json:"builder_version"`
	Fingerprint    string                  `json:"fingerprint"`
	Sources        map[string]SourceInfo   `json:"sources"`
	Layers         map[string]int          `json:"layers"`
	Outputs        map[string]build.Output `json:"outputs"`
	Stats          build.Stats             `json:"stats"`
	LiteMaxSizeMB  int                     `json:"lite_max_size_mb"`
	Warnings       []string                `json:"warnings,omitempty"`
	Benchmark      any                     `json:"benchmark"`
}

func newManifest(opt BuildOptions, l *Loaded, res *build.Result) *Manifest {
	m := &Manifest{
		Name:           opt.Config.Name,
		Version:        opt.Version,
		BuildEpoch:     l.Epoch,
		BuiltAt:        opt.Now.UTC().Format(time.RFC3339),
		BuilderVersion: BuilderVersion,
		Fingerprint:    opt.Inputs.Fingerprint,
		Sources:        map[string]SourceInfo{},
		Layers:         l.Layers,
		Outputs:        map[string]build.Output{"full": res.Full, "lite": res.Lite},
		Stats:          res.Stats,
		LiteMaxSizeMB:  opt.Config.Lite.MaxSizeMB,
		Warnings:       append([]string(nil), opt.Inputs.Warnings...),
	}
	for name, meta := range opt.Inputs.Sources {
		m.Sources[name] = sourceInfo(meta, l.Versions[name])
	}
	if lim := int64(opt.Config.Lite.MaxSizeMB) << 20; lim > 0 && res.Lite.Size > lim {
		m.Warnings = append(m.Warnings, fmt.Sprintf("%s is %.1f MB, above the %d MB target (jsDelivr only serves files up to 20 MB)",
			res.Lite.File, float64(res.Lite.Size)/(1<<20), opt.Config.Lite.MaxSizeMB))
	}
	return m
}

func sourceInfo(meta fetch.Meta, version string) SourceInfo {
	return SourceInfo{URL: meta.URL, Version: version, ETag: meta.ETag, LastModified: meta.LastModified,
		SHA256: meta.SHA256, Size: meta.Size, FetchedAt: meta.FetchedAt}
}
