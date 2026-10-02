// Package testutil builds small synthetic upstream files for tests.
package testutil

import (
	"compress/gzip"
	"net"
	"os"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// FakeNet describes one network of the fake DB-IP database.
type FakeNet struct {
	CIDR             string
	Country          string // ISO code
	Subdivision      string // English name, empty for none
	SubdivisionISO   string
	City             string // English name, empty for none
	Lat, Lon         float64
	NoLocation       bool
	ContinentCode    string
	CountryNameEN    string
	CountryNameZH    string
	ContinentNameEN  string
	ContinentNameZH  string
	CountryGeonameID uint32
}

// DefaultFakeNets mimics the structure (not the content) of DB-IP City Lite.
var DefaultFakeNets = []FakeNet{
	{CIDR: "1.0.0.0/24", Country: "AU", Subdivision: "Queensland", City: "Brisbane", Lat: -27.47, Lon: 153.02},
	{CIDR: "1.0.1.0/24", Country: "CN", Subdivision: "Fujian", City: "Fuzhou", Lat: 26.06, Lon: 119.30},
	{CIDR: "1.0.8.0/21", Country: "CN", Subdivision: "Guangdong", City: "Guangzhou", Lat: 23.13, Lon: 113.26},
	{CIDR: "1.1.1.0/24", Country: "AU", Subdivision: "New South Wales", City: "Sydney", Lat: -33.87, Lon: 151.21},
	{CIDR: "8.8.8.0/24", Country: "US", Subdivision: "California", City: "Mountain View", Lat: 37.42, Lon: -122.08},
	{CIDR: "36.0.0.0/16", Country: "CN", Subdivision: "Beijing", City: "Beijing", Lat: 39.90, Lon: 116.41},
	{CIDR: "36.1.0.0/16", Country: "CN", Lat: 35.0, Lon: 105.0},
	{CIDR: "39.0.0.0/16", Country: "CN", Subdivision: "Jiangsu", City: "Nanjing", Lat: 32.06, Lon: 118.80},
	{CIDR: "3.112.0.0/14", Country: "JP", Subdivision: "Tokyo", City: "Tokyo", Lat: 35.69, Lon: 139.69},
	{CIDR: "104.16.0.0/13", Country: "US", Subdivision: "California", City: "San Francisco", Lat: 37.77, Lon: -122.42},
	{CIDR: "10.0.0.0/8", Country: "US", City: "Private"},
	{CIDR: "223.0.0.0/16", Country: "HK", Subdivision: "Central and Western", City: "Hong Kong", Lat: 22.28, Lon: 114.16},
	{CIDR: "223.1.0.0/16", Country: "TW", Subdivision: "Taipei City", City: "Taipei", Lat: 25.05, Lon: 121.53},
	{CIDR: "223.2.0.0/16", Country: "MO", City: "Macau", Lat: 22.20, Lon: 113.55},
	{CIDR: "2001:250::/32", Country: "CN", Subdivision: "Beijing", City: "Beijing", Lat: 39.90, Lon: 116.41},
	{CIDR: "240e::/20", Country: "CN", Subdivision: "Beijing", City: "Beijing", Lat: 39.90, Lon: 116.41},
	{CIDR: "2409:8000::/20", Country: "CN", Subdivision: "Guangdong", City: "Guangzhou", Lat: 23.13, Lon: 113.26},
	{CIDR: "2400:cb00::/32", Country: "US", Subdivision: "California", City: "San Francisco", Lat: 37.77, Lon: -122.42},
	{CIDR: "2a00:1450::/32", Country: "IE", Subdivision: "Leinster", City: "Dublin", Lat: 53.35, Lon: -6.26},
	{CIDR: "2001:1::/32", Country: "US", City: "Reserved"},
}

var countryInfo = map[string][4]string{
	// iso: continent code, continent en, country en, country zh
	"AU": {"OC", "Oceania", "Australia", "澳大利亚"},
	"CN": {"AS", "Asia", "China", "中国"},
	"US": {"NA", "North America", "United States", "美国"},
	"JP": {"AS", "Asia", "Japan", "日本"},
	"HK": {"AS", "Asia", "Hong Kong", "香港"},
	"TW": {"AS", "Asia", "Taiwan", "台湾"},
	"MO": {"AS", "Asia", "Macao", "澳门"},
	"IE": {"EU", "Europe", "Ireland", "爱尔兰"},
}

// WriteFakeDBIP writes a gzip-compressed MMDB that looks like DB-IP City Lite.
func WriteFakeDBIP(path string, nets []FakeNet) error {
	tree, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            "DBIP-City-Lite",
		Languages:               []string{"en", "zh-CN"},
		IncludeReservedNetworks: true,
		DisableIPv4Aliasing:     true,
		RecordSize:              28,
		BuildEpoch:              1790000000,
	})
	if err != nil {
		return err
	}
	for _, n := range nets {
		info := countryInfo[n.Country]
		rec := mmdbtype.Map{
			"continent": mmdbtype.Map{
				"code":       mmdbtype.String(info[0]),
				"geoname_id": mmdbtype.Uint32(6255140 + uint32(len(info[0]))),
				"names":      mmdbtype.Map{"en": mmdbtype.String(info[1]), "zh-CN": mmdbtype.String(info[1] + "洲")},
			},
			"country": mmdbtype.Map{
				"geoname_id":           mmdbtype.Uint32(1000 + uint32(n.Country[0])*100 + uint32(n.Country[1])),
				"iso_code":             mmdbtype.String(n.Country),
				"is_in_european_union": mmdbtype.Bool(n.Country == "IE"),
				"names":                mmdbtype.Map{"en": mmdbtype.String(info[2]), "zh-CN": mmdbtype.String(info[3])},
			},
		}
		if n.Subdivision != "" {
			sd := mmdbtype.Map{"names": mmdbtype.Map{"en": mmdbtype.String(n.Subdivision)}}
			if n.SubdivisionISO != "" {
				sd["iso_code"] = mmdbtype.String(n.SubdivisionISO)
			}
			rec["subdivisions"] = mmdbtype.Slice{sd}
		}
		if n.City != "" {
			rec["city"] = mmdbtype.Map{"names": mmdbtype.Map{"en": mmdbtype.String(n.City)}}
		}
		if !n.NoLocation {
			rec["location"] = mmdbtype.Map{"latitude": mmdbtype.Float64(n.Lat), "longitude": mmdbtype.Float64(n.Lon)}
		}
		_, ipnet, err := net.ParseCIDR(n.CIDR)
		if err != nil {
			return err
		}
		if err := tree.Insert(ipnet, rec); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	if _, err := tree.WriteTo(zw); err != nil {
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// WriteFile writes content to path, creating parent directories.
func WriteFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// IPtoASNSample is a small ip2asn-combined.tsv.
const IPtoASNSample = "1.0.0.0\t1.0.0.255\t13335\tUS\tCLOUDFLARENET\n" +
	"1.0.1.0\t1.0.3.255\t4134\tCN\tCHINANET-BACKBONE No.31,Jin-rong Street\n" +
	"1.0.4.0\t1.0.7.255\t0\tNone\tNot routed\n" +
	"1.0.8.0\t1.0.15.255\t4134\tCN\tCHINANET-BACKBONE No.31,Jin-rong Street\n" +
	"1.1.1.0\t1.1.1.255\t13335\tUS\tCLOUDFLARENET\n" +
	"8.8.8.0\t8.8.8.255\t15169\tUS\tGOOGLE\n" +
	"36.0.0.0\t36.0.255.255\t56046\tCN\tCMNET-JIANGSU-AP China Mobile communications corporation\n" +
	"36.1.0.0\t36.1.255.255\t56046\tCN\tCMNET-JIANGSU-AP China Mobile communications corporation\n" +
	"39.0.0.0\t39.0.255.255\t56046\tCN\tCMNET-JIANGSU-AP China Mobile communications corporation\n" +
	"3.112.0.0\t3.115.255.255\t16509\tUS\tAMAZON-02\n" +
	"104.16.0.0\t104.23.255.255\t13335\tUS\tCLOUDFLARENET\n" +
	"223.0.0.0\t223.0.255.255\t4760\tHK\tHKTIMS-AP HKT Limited\n" +
	"2001:250::\t2001:250:ffff:ffff:ffff:ffff:ffff:ffff\t23910\tCN\tCERNET2-AS\n" +
	"240e::\t240e:fff:ffff:ffff:ffff:ffff:ffff:ffff\t4134\tCN\tCHINANET-BACKBONE No.31,Jin-rong Street\n" +
	"2409:8000::\t2409:8000:ffff:ffff:ffff:ffff:ffff:ffff\t9808\tCN\tCMNET-GD Guangdong Mobile Communication Co.Ltd.\n" +
	"2409:8020::\t2409:8020:ffff:ffff:ffff:ffff:ffff:ffff\t56046\tCN\tCMNET-JIANGSU-AP China Mobile communications corporation\n" +
	"2400:cb00::\t2400:cb00:ffff:ffff:ffff:ffff:ffff:ffff\t13335\tUS\tCLOUDFLARENET\n" +
	"2a00:1450::\t2a00:1450:ffff:ffff:ffff:ffff:ffff:ffff\t15169\tUS\tGOOGLE\n"

// AWSSample is a small ip-ranges.json.
const AWSSample = `{
  "syncToken": "1790000000",
  "createDate": "2026-10-01-00-00-00",
  "prefixes": [
    {"ip_prefix": "3.112.0.0/14", "region": "ap-northeast-1", "service": "AMAZON", "network_border_group": "ap-northeast-1"},
    {"ip_prefix": "3.112.0.0/14", "region": "ap-northeast-1", "service": "EC2", "network_border_group": "ap-northeast-1"},
    {"ip_prefix": "3.113.0.0/16", "region": "GLOBAL", "service": "CLOUDFRONT", "network_border_group": "GLOBAL"},
    {"ip_prefix": "3.114.0.0/24", "region": "GLOBAL", "service": "GLOBALACCELERATOR", "network_border_group": "GLOBAL"}
  ],
  "ipv6_prefixes": [
    {"ipv6_prefix": "2406:da14::/32", "region": "ap-northeast-1", "service": "AMAZON", "network_border_group": "ap-northeast-1"}
  ]
}`

// FastlySample is a small public-ip-list response.
const FastlySample = `{"addresses":["151.101.0.0/16"],"ipv6_addresses":["2a04:4e40::/32"]}`

// GCPSample is a small cloud.json.
const GCPSample = `{"syncToken":"1","creationTime":"2026-10-01T00:00:00","prefixes":[
 {"ipv4Prefix":"34.1.208.0/20","service":"Google Cloud","scope":"africa-south1"},
 {"ipv6Prefix":"2600:1900:8000::/44","service":"Google Cloud","scope":"us-central1"}]}`

// OracleSample is a small public_ip_ranges.json.
const OracleSample = `{"last_updated_timestamp":"2026-10-01T00:00:00","regions":[
 {"region":"ap-tokyo-1","cidrs":[{"cidr":"140.238.32.0/20","tags":["OCI"]}]}]}`

// AzureSample is a small ServiceTags_Public json.
const AzureSample = `{"changeNumber":350,"cloud":"Public","values":[
 {"name":"AzureCloud","id":"AzureCloud","properties":{"region":"","addressPrefixes":["13.64.0.0/11"]}},
 {"name":"AzureCloud.japaneast","id":"AzureCloud.japaneast","properties":{"region":"japaneast","addressPrefixes":["13.71.144.0/20","2603:1040:400::/46"]}},
 {"name":"AzureFrontDoor.Frontend","id":"AzureFrontDoor.Frontend","properties":{"region":"","addressPrefixes":["13.107.246.0/24"]}},
 {"name":"Storage","id":"Storage","properties":{"region":"","addressPrefixes":["13.65.0.0/16"]}}]}`

// DelegatedSample is a small delegated-apnic-extended file.
const DelegatedSample = `2|apnic|20261002|5|19830613|20261001|+1000
apnic|*|asn|*|1|summary
apnic|*|ipv4|*|3|summary
apnic|*|ipv6|*|2|summary
apnic|CN|asn|3460|1|20020801|allocated|A92E1062
apnic|CN|ipv4|1.0.1.0|768|20110414|allocated|A92E1062
apnic|CN|ipv4|36.0.0.0|131072|20100910|allocated|A92E1062
apnic|HK|ipv4|223.0.0.0|65536|20100910|allocated|A9123
apnic|CN|ipv6|240e::|20|20131016|allocated|A92E1062
apnic|CN|ipv6|2409:8000::|20|20130318|allocated|A92E1062
apnic||ipv6|2409:9000::|20||available|
`
