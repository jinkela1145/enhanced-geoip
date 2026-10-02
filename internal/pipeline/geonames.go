package pipeline

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jinkela1145/enhanced-geoip/internal/config"
	"github.com/jinkela1145/enhanced-geoip/internal/fetch"
	"github.com/jinkela1145/enhanced-geoip/internal/sources"
)

// provinceISO maps the start of GeoNames' English admin1 names to ISO
// 3166-2:CN codes. Longer keys are checked first ("shaanxi" before "shanxi").
var provinceISO = []struct{ prefix, iso string }{
	{"inner mongolia", "NM"}, {"heilongjiang", "HL"}, {"shaanxi", "SN"}, {"shanxi", "SX"}, {"guangdong", "GD"},
	{"guangxi", "GX"}, {"jiangsu", "JS"}, {"jiangxi", "JX"}, {"xinjiang", "XJ"}, {"zhejiang", "ZJ"},
	{"shandong", "SD"}, {"shanghai", "SH"}, {"liaoning", "LN"}, {"chongqing", "CQ"}, {"sichuan", "SC"},
	{"guizhou", "GZ"}, {"qinghai", "QH"}, {"ningxia", "NX"}, {"tianjin", "TJ"}, {"beijing", "BJ"},
	{"fujian", "FJ"}, {"yunnan", "YN"}, {"hainan", "HI"}, {"hebei", "HE"}, {"henan", "HA"}, {"hubei", "HB"},
	{"hunan", "HN"}, {"anhui", "AH"}, {"gansu", "GS"}, {"jilin", "JL"}, {"tibet", "XZ"}, {"xizang", "XZ"},
}

// provinceRadius gives larger accuracy radii to very large provinces.
var provinceRadius = map[string]uint16{"XJ": 500, "XZ": 500, "NM": 500, "QH": 500, "GS": 400, "HL": 300, "SC": 300, "YN": 300}

func isoForAdmin1(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, p := range provinceISO {
		if strings.HasPrefix(n, p.prefix) {
			return p.iso
		}
	}
	return ""
}

type gnFeature struct {
	id       uint32
	name     string
	lat, lon string
	fcode    string
	admin1   string
	pop      int64
}

type zhName struct {
	name  string
	score int
}

// GenCNAdmin downloads GeoNames dumps and writes cn_admin.candidate.csv and
// cn_cities.candidate.csv into outDir for human review.
func GenCNAdmin(ctx context.Context, cfg *config.Config, cacheDir, outDir string, logf func(string, ...any)) error {
	f := fetch.New(cacheDir, cfg.UserAgent())
	f.Logf = logf
	paths := map[string]string{}
	for _, name := range []string{config.SrcGeoNamesAdmin1, config.SrcGeoNamesCN, config.SrcGeoNamesCNAlt} {
		p, _, err := f.Fetch(ctx, name, cfg.Sources[name])
		if err != nil {
			return err
		}
		paths[name] = p
	}
	return genCNAdminFromFiles(paths[config.SrcGeoNamesAdmin1], paths[config.SrcGeoNamesCN], paths[config.SrcGeoNamesCNAlt], outDir)
}

func openZipMember(path, member string) (io.ReadCloser, func(), error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	for _, f := range zr.File {
		if f.Name == member {
			rc, err := f.Open()
			if err != nil {
				zr.Close()
				return nil, nil, err
			}
			return rc, func() { rc.Close(); zr.Close() }, nil
		}
	}
	zr.Close()
	return nil, nil, fmt.Errorf("%s: member %s not found", path, member)
}

func genCNAdminFromFiles(admin1Path, cnZip, altZip, outDir string) error {
	// admin1: CN.01 <tab> name <tab> asciiname <tab> geonameid
	type admin1 struct {
		code, name, iso string
		id              uint32
	}
	var provs []admin1
	af, err := sources.Open(admin1Path)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(af)
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < 4 || !strings.HasPrefix(cols[0], "CN.") {
			continue
		}
		id, _ := strconv.ParseUint(cols[3], 10, 32)
		iso := isoForAdmin1(cols[2])
		if iso == "" {
			af.Close()
			return fmt.Errorf("admin1 %s %q has no ISO mapping", cols[0], cols[2])
		}
		provs = append(provs, admin1{code: strings.TrimPrefix(cols[0], "CN."), name: cols[2], iso: iso, id: uint32(id)})
	}
	af.Close()
	if len(provs) < 31 {
		return fmt.Errorf("only %d Chinese admin1 entries found", len(provs))
	}

	// CN.txt features: capitals (PPLA / PPLC) and prefecture seats (PPLA2).
	rc, closeFn, err := openZipMember(cnZip, "CN.txt")
	if err != nil {
		return err
	}
	capitals := map[string]gnFeature{}
	var seats []gnFeature
	sc = bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		c := strings.Split(sc.Text(), "\t")
		if len(c) < 19 || c[6] != "P" {
			continue
		}
		code := c[7]
		if code != "PPLA" && code != "PPLC" && code != "PPLA2" {
			continue
		}
		id, _ := strconv.ParseUint(c[0], 10, 32)
		pop, _ := strconv.ParseInt(c[14], 10, 64)
		ft := gnFeature{id: uint32(id), name: c[2], lat: c[4], lon: c[5], fcode: code, admin1: c[10], pop: pop}
		if code == "PPLA" || code == "PPLC" {
			if old, ok := capitals[ft.admin1]; !ok || (old.fcode != "PPLA" && code == "PPLA") || (old.fcode == code && pop > old.pop) {
				capitals[ft.admin1] = ft
			}
		}
		seats = append(seats, ft)
	}
	closeFn()
	if err := sc.Err(); err != nil {
		return err
	}

	want := map[uint32]bool{}
	for _, p := range provs {
		want[p.id] = true
	}
	for _, s := range seats {
		want[s.id] = true
	}
	// Alternate names: alternateNameId, geonameid, isolanguage, name, isPreferredName, isShortName, isColloquial, isHistoric
	rc, closeFn, err = openZipMember(altZip, "CN.txt")
	if err != nil {
		return err
	}
	zh := map[uint32]zhName{}
	sc = bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		c := strings.Split(sc.Text(), "\t")
		if len(c) < 8 {
			continue
		}
		id64, _ := strconv.ParseUint(c[1], 10, 32)
		id := uint32(id64)
		if !want[id] || c[6] == "1" || c[7] == "1" {
			continue
		}
		score := 0
		switch c[2] {
		case "zh-CN":
			score = 40
		case "zh-Hans":
			score = 30
		case "zh":
			score = 20
		default:
			continue
		}
		if c[4] == "1" {
			score += 5
		}
		if score > zh[id].score {
			zh[id] = zhName{c[3], score}
		}
	}
	closeFn()
	if err := sc.Err(); err != nil {
		return err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	isoOf := map[string]string{}
	sort.Slice(provs, func(i, j int) bool { return provs[i].iso < provs[j].iso })
	var adminRows [][]string
	for _, p := range provs {
		isoOf[p.code] = p.iso
		capital, ok := capitals[p.code]
		if !ok {
			return fmt.Errorf("no capital (PPLA/PPLC) found for %s", p.name)
		}
		radius := provinceRadius[p.iso]
		if radius == 0 {
			radius = 250
		}
		adminRows = append(adminRows, []string{p.iso, zh[p.id].name, p.name, p.name, strconv.FormatUint(uint64(p.id), 10),
			zh[capital.id].name, capital.name, strconv.FormatUint(uint64(capital.id), 10), capital.lat, capital.lon,
			strconv.Itoa(int(radius))})
	}
	if err := writeCSV(filepath.Join(outDir, "cn_admin.candidate.csv"), sources.HeaderCNAdmin, adminRows); err != nil {
		return err
	}
	sort.Slice(seats, func(i, j int) bool {
		if seats[i].admin1 != seats[j].admin1 {
			return seats[i].admin1 < seats[j].admin1
		}
		return seats[i].name < seats[j].name
	})
	var cityRows [][]string
	for _, s := range seats {
		iso := isoOf[s.admin1]
		if iso == "" || zh[s.id].name == "" {
			continue
		}
		cityRows = append(cityRows, []string{iso, zh[s.id].name, s.name, strconv.FormatUint(uint64(s.id), 10), s.lat, s.lon})
	}
	return writeCSV(filepath.Join(outDir, "cn_cities.candidate.csv"), sources.HeaderCNCities, cityRows)
}

func writeCSV(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write(header)
	_ = w.WriteAll(rows)
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
