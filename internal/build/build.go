// Package build merges the parsed layers and writes the MMDB files.
package build

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"

	"github.com/jinkela1145/enhanced-geoip/internal/config"
	"github.com/jinkela1145/enhanced-geoip/internal/fetch"
	"github.com/jinkela1145/enhanced-geoip/internal/iprange"
	"github.com/jinkela1145/enhanced-geoip/internal/sources"
)

// Inputs are the parsed layers, lowest priority first.
type Inputs struct {
	Base      *sources.BaseData
	ASN       *sources.ASNData // may be nil
	Flags     []sources.FlagEntry
	ASNFlags  map[uint32]sources.NetFlags
	Provinces []sources.CNProvince
	Cities    []sources.CNCity
	CNASN     []sources.CNASN
	Overrides []sources.Override
	// Address space delegated to China, only used for coverage statistics.
	CNDelegatedV4, CNDelegatedV6 []iprange.Range
}

// Options control a build.
type Options struct {
	Config     *config.Config
	OutDir     string
	BuildEpoch int64
	Logf       func(format string, args ...any)
}

// Output describes one written database.
type Output struct {
	File         string `json:"file"`
	DatabaseType string `json:"database_type"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	RecordSize   int    `json:"record_size"`
	Ranges       int    `json:"ranges"`
}

// Result of a build.
type Result struct {
	Full  Output `json:"full"`
	Lite  Output `json:"lite"`
	Stats Stats  `json:"stats"`
}

type family struct {
	is4   bool
	base  iprange.SegSpans[int32]
	asn   iprange.SegSpans[int32]
	flags iprange.SegSpans[int32]
	over  iprange.SegSpans[int32]
	deleg iprange.RangeSpans
}

func idx(segs []iprange.Seg[int32], li int) int32 {
	if li < 0 {
		return -1
	}
	return segs[li].Val
}

// Build writes the full and Lite databases into opt.OutDir.
func Build(in *Inputs, opt Options) (*Result, error) {
	if in.Base == nil {
		return nil, errors.New("base layer is missing")
	}
	if opt.Logf == nil {
		opt.Logf = func(string, ...any) {}
	}
	cfg := opt.Config
	r, err := newResolver(in, cfg)
	if err != nil {
		return nil, err
	}
	// Reserved, private and aliased networks are never written (mmdbwriter
	// rejects them); the base parser already removes them, this is a guard.
	fams := []*family{
		{is4: true, base: iprange.Subtract(in.Base.V4, sources.Excluded(true)), deleg: in.CNDelegatedV4},
		{is4: false, base: iprange.Subtract(in.Base.V6, sources.Excluded(false)), deleg: in.CNDelegatedV6},
	}
	for _, f := range fams {
		if in.ASN != nil {
			if f.is4 {
				f.asn = in.ASN.V4
			} else {
				f.asn = in.ASN.V6
			}
		}
		f.flags = r.flattenFlags(in.Flags, f.is4)
		f.over = r.flattenOverrides(f.is4)
	}
	if err := os.MkdirAll(opt.OutDir, 0o755); err != nil {
		return nil, err
	}
	res := &Result{}
	langs := languages(in.Base.Languages)

	// Pass 1: full database and statistics.
	opt.Logf("building %s", cfg.FullFile())
	tree, err := newTree(cfg.FullType(), langs, cfg.Description, opt.BuildEpoch, 32)
	if err != nil {
		return nil, err
	}
	cache := newValueCache()
	fullVals := map[key]mmdbtype.Map{}
	accs := map[bool]*familyAcc{
		true:  newFamilyAcc(true, in.CNDelegatedV4),
		false: newFamilyAcc(false, in.CNDelegatedV6),
	}
	for _, f := range fams {
		acc := accs[f.is4]
		var pendStart, pendEnd netip.Addr
		var pendKey key
		pending := false
		var insertErr error
		flush := func() {
			if !pending || insertErr != nil {
				return
			}
			v, ok := fullVals[pendKey]
			if !ok {
				rv := r.resolve(pendKey)
				v = cache.full(&rv)
				fullVals[pendKey] = v
			}
			if err := insertRange(tree, pendStart, pendEnd, v); err != nil {
				insertErr = fmt.Errorf("inserting %s-%s: %w", pendStart, pendEnd, err)
			}
			acc.ranges++
			pending = false
		}
		iprange.Refine(f.base, []iprange.Spans{f.asn, f.flags, f.over, f.deleg},
			func(s, e netip.Addr, bi int, li []int) {
				k, outcome := r.keyFor(f.base[bi].Val, idx(f.asn, li[0]), idx(f.flags, li[1]), idx(f.over, li[2]))
				acc.add(r, k, outcome, iprange.Range{Start: s, End: e}.Size(), li[3] >= 0)
				if pending && k == pendKey && pendEnd.Next() == s {
					pendEnd = e
					return
				}
				flush()
				pendStart, pendEnd, pendKey, pending = s, e, k, true
			})
		flush()
		if insertErr != nil {
			return nil, insertErr
		}
	}
	res.Full, err = writeTree(tree, filepath.Join(opt.OutDir, cfg.FullFile()))
	if err != nil {
		return nil, err
	}
	res.Full.DatabaseType = cfg.FullType()
	res.Full.RecordSize = 32
	res.Full.Ranges = accs[true].ranges + accs[false].ranges
	tree, cache, fullVals = nil, nil, nil
	runtime.GC()
	res.Stats = Stats{
		IPv4: accs[true].report(), IPv6: accs[false].report(),
		CNCoverageIPv4: accs[true].coverage(), CNCoverageIPv6: accs[false].coverage(),
	}
	if len(r.unmatchedCN) > 0 {
		res.Stats.UnmatchedCNSubdivisions = r.unmatchedCN
	}

	// Pass 2: Lite database.
	opt.Logf("building %s", cfg.LiteFile())
	type liteRun struct {
		start, end netip.Addr
		k          liteKey
	}
	var runs []liteRun
	step, tiers := cfg.Lite.CoordStep, cfg.Lite.RadiusTiers
	lc := newValueCache()
	for _, f := range fams {
		iprange.Refine(f.base, []iprange.Spans{f.asn, f.flags, f.over},
			func(s, e netip.Addr, bi int, li []int) {
				k, _ := r.keyFor(f.base[bi].Val, idx(f.asn, li[0]), idx(f.flags, li[1]), idx(f.over, li[2]))
				rv := r.resolve(k)
				lk := lc.liteKeyOf(&rv, step, tiers)
				if n := len(runs); n > 0 && runs[n-1].k == lk && runs[n-1].end.Next() == s {
					runs[n-1].end = e
					return
				}
				runs = append(runs, liteRun{s, e, lk})
			})
	}
	liteDesc := map[string]string{}
	for k, v := range cfg.Description {
		liteDesc[k] = v
	}
	liteDesc["en"] += fmt.Sprintf(" Lite edition: country, coordinates rounded to %g°, accuracy radius tiers and network flags only.", step)
	if _, ok := liteDesc["zh-CN"]; ok {
		liteDesc["zh-CN"] += fmt.Sprintf(" 精简版：只含国家、取整到 %g° 的坐标、分档的精度半径和网络类型标记。", step)
	}
	var lastErr error
	for _, rs := range []int{24, 28, 32} {
		lt, err := newTree(cfg.LiteType(), langs, liteDesc, opt.BuildEpoch, rs)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if err := insertRange(lt, run.start, run.end, lc.liteValue(run.k, step)); err != nil {
				return nil, fmt.Errorf("lite: inserting %s-%s: %w", run.start, run.end, err)
			}
		}
		out, err := writeTree(lt, filepath.Join(opt.OutDir, cfg.LiteFile()))
		if err != nil && strings.Contains(err.Error(), "exceeded record capacity") {
			lastErr = err
			continue
		}
		if err != nil {
			return nil, err
		}
		out.DatabaseType, out.RecordSize, out.Ranges = cfg.LiteType(), rs, len(runs)
		res.Lite = out
		lastErr = nil
		break
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return res, nil
}

func (r *resolver) flattenFlags(all []sources.FlagEntry, is4 bool) []iprange.Seg[int32] {
	var entries []iprange.Entry[sources.NetFlags]
	for _, e := range all {
		if e.R.Is4() == is4 {
			entries = append(entries, iprange.Entry[sources.NetFlags]{R: e.R, Val: e.Flags, Rank: e.Bits})
		}
	}
	segs := iprange.Flatten(entries, func(vs []sources.NetFlags) (sources.NetFlags, bool) {
		var f sources.NetFlags
		for _, v := range vs {
			f = f.Merge(v)
		}
		return f, !f.IsZero()
	})
	out := make([]iprange.Seg[int32], 0, len(segs))
	for _, s := range segs {
		out = append(out, iprange.Seg[int32]{Start: s.Start, End: s.End, Val: r.internFlags(s.Val)})
	}
	return out
}

func (r *resolver) flattenOverrides(is4 bool) []iprange.Seg[int32] {
	var entries []iprange.Entry[int32]
	for i, o := range r.overrides {
		if o.src.R.Is4() == is4 {
			entries = append(entries, iprange.Entry[int32]{R: o.src.R, Val: int32(i), Rank: i})
		}
	}
	return iprange.Flatten(entries, func(vs []int32) (int32, bool) { return vs[len(vs)-1], true })
}

func languages(base []string) []string {
	set := map[string]bool{"en": true, "zh-CN": true}
	for _, l := range base {
		set[l] = true
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	slices.Sort(out)
	return out
}

func newTree(dbType string, langs []string, desc map[string]string, epoch int64, recordSize int) (*mmdbwriter.Tree, error) {
	return mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: dbType,
		Languages:    langs,
		Description:  desc,
		BuildEpoch:   epoch,
		IPVersion:    6,
		RecordSize:   recordSize,
		KeyGenerator: newMemoKeyGen(),
	})
}

func insertRange(t *mmdbwriter.Tree, s, e netip.Addr, v mmdbtype.DataType) error {
	return t.InsertRange(net.IP(s.AsSlice()), net.IP(e.AsSlice()), v)
}

func writeTree(t *mmdbwriter.Tree, path string) (Output, error) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return Output{}, err
	}
	if _, err := t.WriteTo(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return Output{}, err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return Output{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return Output{}, err
	}
	sum, size, err := fetch.FileSHA256(path)
	if err != nil {
		return Output{}, err
	}
	return Output{File: filepath.Base(path), Size: size, SHA256: sum}, nil
}
