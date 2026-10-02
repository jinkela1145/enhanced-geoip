package pipeline

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/jinkela1145/enhanced-geoip/internal/build"
	"github.com/jinkela1145/enhanced-geoip/internal/config"
)

func pct(m map[string]float64, k string) string {
	v, ok := m[k]
	if !ok {
		return "—"
	}
	return strconv.FormatFloat(v, 'f', 2, 64) + "%"
}

func coverageTable(b *strings.Builder, title string, c build.CoverageReport) {
	fmt.Fprintf(b, "## %s\n\n", title)
	if c.Delegated == 0 {
		b.WriteString("（本次构建没有 APNIC delegated 数据 / no APNIC delegated data in this build）\n\n")
		return
	}
	fmt.Fprintf(b, "分母 / Denominator: APNIC 分配给 CN 的地址空间 / space delegated to CN = %s %s\n\n", human(c.Delegated), c.Unit)
	b.WriteString("| 指标 / Metric | 占比 / Share |\n|---|---|\n")
	rows := []struct{ label, key string }{
		{"数据库里有记录 / has a record", "in_database"},
		{"国家为 CN / country is CN", "country_cn"},
		{"CN：有城市 / with city", "cn_city"},
		{"CN：只有省 / subdivision only", "cn_subdivision_only"},
		{"CN：只有国家 / country only", "cn_country_only"},
		{"CN：位置来自 DB-IP / located by DB-IP", "cn_source_dbip"},
		{"CN：位置来自省公司 ASN 层 / located by provincial ASN layer", "cn_source_bgp_asn"},
		{"CN：位置来自人工修正 / located by overrides", "cn_source_override"},
		{"ASN 层与 DB-IP 省份一致（保留 DB-IP）/ ASN layer agrees with DB-IP", "cn_asn_agree"},
		{"ASN 层纠正了 DB-IP 的省份 / ASN layer corrected the province", "cn_asn_corrected"},
		{"ASN 层补上了缺失的省份 / ASN layer filled a missing province", "cn_asn_filled"},
	}
	for _, r := range rows {
		fmt.Fprintf(b, "| %s | %s |\n", r.label, pct(c.Percent, r.key))
	}
	if len(c.TopCountry) > 0 {
		keys := make([]string, 0, len(c.TopCountry))
		for k := range c.TopCountry {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return c.TopCountry[keys[i]] > c.TopCountry[keys[j]] })
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %.2f%%", k, c.TopCountry[k]))
		}
		fmt.Fprintf(b, "\n这部分地址在库里的国家分布 / Countries of this space in the database: %s\n", strings.Join(parts, ", "))
	}
	b.WriteString("\n")
}

func human(v float64) string {
	switch {
	case v >= 1e9:
		return strconv.FormatFloat(v/1e9, 'f', 2, 64) + "G"
	case v >= 1e6:
		return strconv.FormatFloat(v/1e6, 'f', 2, 64) + "M"
	case v >= 1e3:
		return strconv.FormatFloat(v/1e3, 'f', 2, 64) + "K"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func familyTable(b *strings.Builder, title string, f build.FamilyReport) {
	fmt.Fprintf(b, "### %s\n\n", title)
	if f.Total == 0 {
		b.WriteString("（空 / empty）\n\n")
		return
	}
	share := func(v float64) string { return fmt.Sprintf("%.2f%%", 100*v/f.Total) }
	fmt.Fprintf(b, "总量 / Total: %s %s, %d ranges\n\n| 指标 / Metric | 占比 / Share |\n|---|---|\n", human(f.Total), f.Unit, f.Ranges)
	srcs := make([]string, 0, len(f.BySource))
	for k := range f.BySource {
		srcs = append(srcs, k)
	}
	slices.Sort(srcs)
	for _, k := range srcs {
		fmt.Fprintf(b, "| source = %s | %s |\n", k, share(f.BySource[k]))
	}
	fmt.Fprintf(b, "| 有 ASN / with ASN | %s |\n", share(f.WithASN))
	fmt.Fprintf(b, "| 任播 / anycast | %s |\n", share(f.Anycast))
	fmt.Fprintf(b, "| CDN | %s |\n", share(f.CDN))
	clouds := make([]string, 0, len(f.Cloud))
	for k := range f.Cloud {
		clouds = append(clouds, k)
	}
	slices.Sort(clouds)
	for _, k := range clouds {
		fmt.Fprintf(b, "| cloud = %s | %s |\n", k, share(f.Cloud[k]))
	}
	b.WriteString("\n")
}

func accuracyMarkdown(m *Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# ACCURACY — %s %s\n\n", m.Name, m.Version)
	b.WriteString("> 自动生成 / Generated automatically. 基于 RIPE Atlas 的误差基准测试会在第 3 阶段加入，这里先给覆盖率。" +
		"\n> The RIPE Atlas error benchmark arrives in phase 3; this file currently reports coverage only.\n\n")
	b.WriteString("「有城市」只表示记录里写了城市，不代表一定准确。\n\"With city\" only means the record names a city; it says nothing about correctness.\n\n")
	coverageTable(&b, "中国 IPv6 覆盖率 / China IPv6 coverage", m.Stats.CNCoverageIPv6)
	coverageTable(&b, "中国 IPv4 覆盖率 / China IPv4 coverage", m.Stats.CNCoverageIPv4)
	b.WriteString("## 全库各层占比 / Layer shares of the whole database\n\n")
	familyTable(&b, "IPv4", m.Stats.IPv4)
	familyTable(&b, "IPv6", m.Stats.IPv6)
	b.WriteString("## 基准测试 / Benchmark\n\n第 3 阶段加入 / Coming in phase 3.\n")
	return b.String()
}

func mb(size int64) string { return fmt.Sprintf("%.1f MB", float64(size)/(1<<20)) }

func releaseNotes(cfg *config.Config, m *Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n\n", m.Name, m.Version)
	full, lite := m.Outputs["full"], m.Outputs["lite"]
	fmt.Fprintf(&b, "- `%s` (%s): GeoLite2-City compatible structure + ASN + network flags\n", full.File, mb(full.Size))
	fmt.Fprintf(&b, "- `%s` (%s): map edition (country, rounded coordinates, accuracy radius, network flags)\n\n", lite.File, mb(lite.Size))
	b.WriteString("Upstream versions:\n\n")
	names := make([]string, 0, len(m.Sources))
	for n := range m.Sources {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		s := m.Sources[n]
		v := s.Version
		if v == "" {
			v = "fetched " + s.FetchedAt
		}
		fmt.Fprintf(&b, "- %s: %s (sha256 %s…)\n", n, v, s.SHA256[:min(12, len(s.SHA256))])
	}
	cov := m.Stats.CNCoverageIPv6
	if cov.Delegated > 0 {
		fmt.Fprintf(&b, "\nChina IPv6: %s of the APNIC-delegated space located inside CN, %s with city, %s by the provincial ASN layer.\n",
			pct(cov.Percent, "country_cn"), pct(cov.Percent, "cn_city"), pct(cov.Percent, "cn_source_bgp_asn"))
	}
	if len(m.Warnings) > 0 {
		b.WriteString("\nWarnings:\n\n")
		for _, w := range m.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}
	fmt.Fprintf(&b, "\nIP Geolocation by DB-IP (https://db-ip.com). Place data © GeoNames (https://www.geonames.org), CC BY 4.0. "+
		"ASN data from iptoasn.com (PDDL 1.0). Data license: CC BY 4.0. See https://github.com/%s/blob/main/SOURCES.md\n", cfg.Repo)
	return b.String()
}

// provinceKeywords maps English place names found in AS descriptions to
// province ISO codes. It is only used to suggest candidates for review.
var provinceKeywords = []struct{ word, iso string }{
	{"BEIJING", "BJ"}, {"TIANJIN", "TJ"}, {"HEBEI", "HE"}, {"SHIJIAZHUANG", "HE"}, {"SHANXI", "SX"}, {"TAIYUAN", "SX"},
	{"NEIMENGGU", "NM"}, {"INNER MONGOLIA", "NM"}, {"INNERMONGOLIA", "NM"}, {"NEI MONGOL", "NM"}, {"HOHHOT", "NM"},
	{"LIAONING", "LN"}, {"SHENYANG", "LN"}, {"DALIAN", "LN"}, {"JILIN", "JL"}, {"CHANGCHUN", "JL"},
	{"HEILONGJIANG", "HL"}, {"HARBIN", "HL"}, {"SHANGHAI", "SH"}, {"JIANGSU", "JS"}, {"NANJING", "JS"}, {"WUXI", "JS"},
	{"ZHEJIANG", "ZJ"}, {"HANGZHOU", "ZJ"}, {"NINGBO", "ZJ"}, {"WENZHOU", "ZJ"}, {"ANHUI", "AH"}, {"HEFEI", "AH"},
	{"FUJIAN", "FJ"}, {"XIAMEN", "FJ"}, {"JIANGXI", "JX"}, {"NANCHANG", "JX"}, {"SHANDONG", "SD"}, {"QINGDAO", "SD"},
	{"JINAN", "SD"}, {"HENAN", "HA"}, {"ZHENGZHOU", "HA"}, {"HUBEI", "HB"}, {"WUHAN", "HB"}, {"HUNAN", "HN"},
	{"CHANGSHA", "HN"}, {"GUANGDONG", "GD"}, {"GUANGZHOU", "GD"}, {"SHENZHEN", "GD"}, {"DONGGUAN", "GD"},
	{"FOSHAN", "GD"}, {"GUANGXI", "GX"}, {"NANNING", "GX"}, {"HAINAN", "HI"}, {"HAIKOU", "HI"}, {"CHONGQING", "CQ"},
	{"SICHUAN", "SC"}, {"CHENGDU", "SC"}, {"GUIZHOU", "GZ"}, {"GUIYANG", "GZ"}, {"YUNNAN", "YN"}, {"KUNMING", "YN"},
	{"XIZANG", "XZ"}, {"TIBET", "XZ"}, {"LHASA", "XZ"}, {"SHAANXI", "SN"}, {"XIAN", "SN"}, {"GANSU", "GS"},
	{"LANZHOU", "GS"}, {"QINGHAI", "QH"}, {"XINING", "QH"}, {"NINGXIA", "NX"}, {"YINCHUAN", "NX"},
	{"XINJIANG", "XJ"}, {"URUMQI", "XJ"},
}

func guessProvince(desc string) string {
	u := strings.ToUpper(desc)
	var found []string
	for _, k := range provinceKeywords {
		if strings.Contains(u, k.word) && !slices.Contains(found, k.iso) {
			found = append(found, k.iso)
		}
	}
	return strings.Join(found, ";")
}

// writeReports writes review material for maintainers (not released).
func writeReports(dir string, l *Loaded, res *build.Result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if l.In.ASN != nil {
		type agg struct {
			v4, v6 float64
		}
		sums := map[int32]*agg{}
		for _, is4 := range []bool{true, false} {
			list := l.In.ASN.V6
			if is4 {
				list = l.In.ASN.V4
			}
			for _, s := range list {
				info := l.In.ASN.Infos[s.Val]
				if info.Country != "CN" {
					continue
				}
				a := sums[s.Val]
				if a == nil {
					a = &agg{}
					sums[s.Val] = a
				}
				size := s.Range().Size().Float64()
				if is4 {
					a.v4 += size
				} else {
					a.v6 += size / math.Ldexp(1, 80)
				}
			}
		}
		mapped := map[uint32]string{}
		for _, row := range l.In.CNASN {
			mapped[row.ASN] = row.ProvinceISO
		}
		ids := make([]int32, 0, len(sums))
		for id := range sums {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			a, b := sums[ids[i]], sums[ids[j]]
			if a.v6 != b.v6 {
				return a.v6 > b.v6
			}
			if a.v4 != b.v4 {
				return a.v4 > b.v4
			}
			return l.In.ASN.Infos[ids[i]].ASN < l.In.ASN.Infos[ids[j]].ASN
		})
		f, err := os.Create(filepath.Join(dir, "cn_asn_candidates.csv"))
		if err != nil {
			return err
		}
		w := csv.NewWriter(f)
		_ = w.Write([]string{"asn", "as_description", "ipv4_addresses", "ipv6_48s", "keyword_province", "mapped_province"})
		for _, id := range ids {
			info := l.In.ASN.Infos[id]
			a := sums[id]
			_ = w.Write([]string{strconv.FormatUint(uint64(info.ASN), 10), info.Org,
				strconv.FormatFloat(a.v4, 'f', 0, 64), strconv.FormatFloat(a.v6, 'f', 2, 64),
				guessProvince(info.Org), mapped[info.ASN]})
		}
		w.Flush()
		if err := w.Error(); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	if len(res.Stats.UnmatchedCNSubdivisions) > 0 {
		f, err := os.Create(filepath.Join(dir, "unmatched_cn_subdivisions.csv"))
		if err != nil {
			return err
		}
		w := csv.NewWriter(f)
		_ = w.Write([]string{"dbip_subdivision_name", "records"})
		names := make([]string, 0, len(res.Stats.UnmatchedCNSubdivisions))
		for n := range res.Stats.UnmatchedCNSubdivisions {
			names = append(names, n)
		}
		slices.Sort(names)
		for _, n := range names {
			_ = w.Write([]string{n, strconv.Itoa(res.Stats.UnmatchedCNSubdivisions[n])})
		}
		w.Flush()
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}
