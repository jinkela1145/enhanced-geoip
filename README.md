# EnhancedGeo

[English](README.en.md)

一个开源、可以放心再分发的 IP 地理位置合并库，GitHub Actions 每天自动构建。它把 DB-IP 的免费城市库、iptoasn 的 ASN、CDN 和云厂商公布的网段合并成标准 MMDB，并用各省运营商自己的 ASN 修正中国大陆的省份。

> **状态：开发中，还没有正式发布。** `EnhancedGeo` 是暂定的文件名前缀，统一写在 [`config.json`](config.json) 里。

## 下载

| 文件 | 内容 |
|---|---|
| `EnhancedGeo-City.mmdb` | 完整版：兼容 GeoLite2-City 的结构，外加 ASN 和网络类型标记 |
| `EnhancedGeo-City-Lite.mmdb` | 地图精简版：国家、取整后的坐标、精度半径、网络类型标记（目标 ≤ 20 MB） |

固定下载地址（第一次发布之后可用）：

- `https://github.com/jinkela1145/enhanced-geoip/releases/latest/download/EnhancedGeo-City.mmdb`
- `https://github.com/jinkela1145/enhanced-geoip/releases/latest/download/EnhancedGeo-City-Lite.mmdb`
- jsDelivr 加速（只有精简版）：`https://cdn.jsdelivr.net/gh/jinkela1145/enhanced-geoip@release/EnhancedGeo-City-Lite.mmdb`

每个数据库都附带 `.sha256`。`manifest.json` 记录每个上游的版本、哈希和各层的覆盖情况，`ACCURACY.md` 记录中国 IPv4 / IPv6 的覆盖率。

## 数据从哪来

按优先级从低到高叠加，高的覆盖低的：

1. **底库**：DB-IP IP to City Lite（全球城市和坐标，CC BY 4.0）。
2. **中国大陆省份修正**：运营商的省公司很多有自己的 ASN，并且用它在全球路由表里宣告网段。如果某个网段的起源 ASN 属于某个省公司（[`data/cn_asn_province.csv`](data/cn_asn_province.csv)），而 DB-IP 给出的是别的省或没有省份，就改成这个省（省会坐标，精度半径按省份大小 250–500 km，`source` 记为 `bgp-asn`）。DB-IP 的省份一致时保留 DB-IP 的城市级结果。网段归属哪个 ASN 来自 iptoasn（PDDL）。
3. **网络类型标记**：Cloudflare、Fastly 公布的网段，AWS / Google Cloud / Azure / Oracle 公布的云网段，确认是任播的公共 DNS 网段（[`data/anycast_prefixes.csv`](data/anycast_prefixes.csv)），以及按 ASN 标记的 CDN（[`data/anycast_asns.csv`](data/anycast_asns.csv)）。
4. **人工修正**：[`data/overrides.csv`](data/overrides.csv)，优先级最高。

每个数据源的授权和核对日期见 [SOURCES.md](SOURCES.md)。保留地址和私有地址不收录；香港 HK、澳门 MO、台湾 TW 始终保持自己的国家代码，不会并进 CN。

## 字段

### 完整版 `EnhancedGeo-City.mmdb`

结构和 GeoLite2-City 一致，现成的读库可以直接读（示例记录）：

```json
{
  "continent": {"code": "AS", "geoname_id": 6255147, "names": {"en": "Asia", "zh-CN": "亚洲"}},
  "country": {"iso_code": "CN", "geoname_id": 1814991, "names": {"en": "China", "zh-CN": "中国"}},
  "subdivisions": [{"iso_code": "JS", "geoname_id": 1806260, "names": {"en": "Jiangsu", "zh-CN": "江苏省"}}],
  "location": {"latitude": 32.06167, "longitude": 118.77778, "accuracy_radius": 250},
  "autonomous_system_number": 56046,
  "autonomous_system_organization": "CMNET-JIANGSU-AP China Mobile communications corporation",
  "source": "bgp-asn"
}
```

- `continent`、`country`、`subdivisions`、`city`、`location`：同 GeoLite2-City。名称至少有 `en`；中国省级修正的记录同时有 `zh-CN`；其他地方上游有中文名就带上（DB-IP 只给大洲和国家提供中文名）。`location.time_zone` 只在上游提供时才写。
- `autonomous_system_number`、`autonomous_system_organization`：和 GeoLite2-ASN 同名。
- `network`：`anycast`（任播）、`cdn`、`cloud`（云厂商代号）、`cloud_region`（云厂商的地域代码）。值为假或为空的键直接省略。
- `source`：位置最终来自哪一层：`dbip`、`bgp-asn`、`override`。
- `accuracy_radius`：DB-IP Lite 不提供精度半径，这里按规则填写：有城市 50 km，只有省 250 km，只有国家 1000 km，任播 1000 km，省级修正按省份 250–500 km。

### 精简版 `EnhancedGeo-City-Lite.mmdb`

给世界地图用，**字段不会随意改动**：

```json
{
  "country":  {"iso_code": "JP"},
  "location": {"latitude": 35.5, "longitude": 139.5, "accuracy_radius": 50},
  "network":  {"anycast": true, "cdn": true, "cloud": "aws"}
}
```

- 坐标四舍五入到 0.5°（可在 `config.json` 里改）。
- `accuracy_radius` 分档：10 / 25 / 50 / 100 / 250 / 500 / 1000 km（向上取档）。
- `network` 里值为假或为空的键省略，全部为空时整个 `network` 省略；精简版没有 `cloud_region`。
- 不含名称和 ASN，这样相邻的相同记录能合并，文件更小。

`cloud` 代号：

| 代号 | 厂商 | 来源 |
|---|---|---|
| `aws` | Amazon Web Services | `ip-ranges.json` |
| `gcp` | Google Cloud | `cloud.json` |
| `azure` | Microsoft Azure | Service Tags（`AzureCloud.*`） |
| `oracle` | Oracle Cloud Infrastructure | `public_ip_ranges.json` |

## 读取示例

### Go

用 [`maxminddb-golang`](https://github.com/oschwald/maxminddb-golang)。注意：`geoip2-golang` 只认 MaxMind 和 DB-IP 的几个固定库名，打不开本库。

```go
import (
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
)

db, err := maxminddb.Open("EnhancedGeo-City-Lite.mmdb")
if err != nil {
	return err
}
defer db.Close()

var rec struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	Location struct {
		Latitude       float64 `maxminddb:"latitude"`
		Longitude      float64 `maxminddb:"longitude"`
		AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
	} `maxminddb:"location"`
	Network struct {
		Anycast bool   `maxminddb:"anycast"`
		CDN     bool   `maxminddb:"cdn"`
		Cloud   string `maxminddb:"cloud"`
	} `maxminddb:"network"`
}
err = db.Lookup(netip.MustParseAddr("1.1.1.1")).Decode(&rec)
```

### Python

完整版的 `database_type` 含 `City`，官方的 `geoip2` 可以直接用 `city()`；额外字段用 `maxminddb` 读：

```python
import geoip2.database
import maxminddb

with geoip2.database.Reader("EnhancedGeo-City.mmdb") as reader:
    r = reader.city("8.8.8.8")
    print(r.country.iso_code, r.location.latitude, r.location.longitude)

with maxminddb.open_database("EnhancedGeo-City.mmdb") as reader:
    rec = reader.get("8.8.8.8")
    print(rec.get("network"), rec.get("autonomous_system_number"), rec.get("source"))
```

## 更新频率

每天 UTC 02:17 检查所有上游，输入有变化才发新版（tag 用日期，例如 `2026.10.02`），只保留最近 30 个 Release。iptoasn 每小时更新，所以通常每天都会有新版；DB-IP 每月 1 日更新。

## 局限

- **任播地址**的坐标没有地理意义（同一个地址在全世界很多地方同时存在），所以半径设为 1000 km 并标 `network.anycast`。
- **移动网络**通常只能定位到省，有时只到国家。
- **DB-IP Lite** 是免费版，精度不如商业版。准确度基准测试会在后续版本加入 `ACCURACY.md`。
- **中国 IPv6**：省级修正只覆盖用省公司 ASN 宣告的网段。电信的大部分网段挂在全国骨干 AS4134 下面，这一层帮不上。
- 不做比城市更细的定位，也不做代理 / VPN 检测。

## 署名（必须）

数据按 [CC BY 4.0](DATA_LICENSE.md) 发布，代码按 [Apache-2.0](LICENSE) 发布。使用本库的数据时：

- 署名本库，并注明数据包含 DB-IP 和 GeoNames 的数据；
- **网页应用**还必须在显示或使用查询结果的页面上放上：`<a href='https://db-ip.com'>IP Geolocation by DB-IP</a>`（DB-IP 的要求）。

详见 [NOTICE](NOTICE) 和 [SOURCES.md](SOURCES.md)。

## 提交修正

修正写在 [`data/overrides.csv`](data/overrides.csv)，通过 Pull Request 提交：

```
network,country_iso,province,city,latitude,longitude,accuracy_km,evidence
1.2.3.0/24,CN,广东,深圳,22.54,114.06,50,https://example.com/依据
```

- `network`：CIDR，或者 `起始IP-结束IP`。
- `country_iso`：ISO 3166-1 两位代码。
- `province` / `city`：中国大陆用中文名，必须在 [`data/cn_admin.csv`](data/cn_admin.csv) / [`data/cn_cities.csv`](data/cn_cities.csv) 里；其他地方写英文名。
- `latitude` / `longitude`：中国大陆的省市在表里时可以不写；其他情况必须写。`accuracy_km` 可以不写。
- `evidence`：**必填**，写链接或说明。最好是当事方自己公布的信息（例如高校网络中心公布的本校网段、云厂商公布的地域网段）。**不能**抄自 GeoLite2、纯真、IPIP.net 等不允许再分发的数据库，也不能来自 APNIC whois 查询结果（APNIC 的条款禁止用 whois 数据做 IP 定位）。

## 自己构建

需要 Go 1.24+，并且能访问各个上游网站：

```
go run ./cmd/egeo all     # 下载 + 构建 + 校验，输出在 dist/
go test ./...
```

`egeo` 的子命令：`fetch`（下载）、`changed`（和上一个版本比较）、`build`（构建）、`verify`（校验）、`gen-cn-admin`（从 GeoNames 生成省份 / 城市表的候选，供人工审核）。

## 维护者：GitHub 设置

1. 推送代码后，Actions 会自动试跑（不发布），报告写到 `ci-reports` 分支。
2. 确认没问题后，打开定时发布：仓库 **Settings → Secrets and variables → Actions → Variables → New repository variable**，名字填 `PUBLISH_ENABLED`，值填 `true`。
3. 也可以手动发布一次：**Actions → build → Run workflow**，勾选 `publish`。
4. 构建失败或有警告时会自动开 issue（同一个问题不会重复开）。

不需要配置任何 Secret。

## 依赖

| 依赖 | 协议 |
|---|---|
| [github.com/maxmind/mmdbwriter](https://github.com/maxmind/mmdbwriter) v1.2.0 | Apache-2.0 OR MIT |
| [github.com/oschwald/maxminddb-golang/v2](https://github.com/oschwald/maxminddb-golang) v2.1.1 | ISC |
| [go4.org/netipx](https://github.com/go4org/netipx)（间接依赖） | BSD-3-Clause |
| [golang.org/x/sys](https://github.com/golang/sys)（间接依赖） | BSD-3-Clause |

GitHub Actions 使用 `actions/checkout`、`actions/setup-go`、`actions/cache`、`actions/upload-artifact`、`actions/download-artifact`（均为 MIT），全部固定到 commit SHA。没有任何 GPL / LGPL / AGPL 代码。
