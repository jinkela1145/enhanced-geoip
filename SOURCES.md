# 数据源与授权 / Sources and licenses

> **核对日期 / Checked on: 2026-10-02.** 每一条都是当天读的官网或仓库原文，引文保留原语言。新增数据源或上游改版时，要重新核对并更新日期。
> Every entry was checked against the source's own terms on that date; quotes are verbatim.
>
> 状态 / Status：✅ 用于公开数据 / used in the published data　🔍 只用于统计或评估，不进产物 / statistics or evaluation only　⚠️ 待定 / pending　❌ 不使用 / not used
>
> 决策记录见 [docs/DECISIONS.md](docs/DECISIONS.md)。

## 总览 / Overview

| 数据源 / Source | 用途 / Use | 协议 / License or terms | 状态 |
|---|---|---|---|
| DB-IP IP to City Lite | 底库 / base layer | CC BY 4.0 | ✅ |
| DB-IP IP to ASN Lite | ASN 备用 / ASN fallback | CC BY 4.0 | ✅ |
| iptoasn.com | ASN、中国省级修正 / ASN, China province layer | PDDL 1.0 | ✅ |
| Cloudflare / Fastly 公布的网段 | 任播、CDN 标记 / anycast, CDN flags | 未写授权 / no license stated | ✅（仓库主人 2026-10-02 决定接受） |
| AWS / Google Cloud / Azure / Oracle 公布的网段 | 云厂商标记 / cloud flags | 未写授权 / no license stated | ✅（同上） |
| GeoNames | 省份 / 城市表（`data/cn_admin.csv`、`data/cn_cities.csv`） | CC BY 4.0 | ✅ |
| 本仓库维护的表 / curated tables in `data/` | 人工修正、映射 | CC BY 4.0（本项目） | ✅ |
| APNIC delegated 统计 | 中国覆盖率的分母 / coverage denominator | 可自由下载使用 | 🔍 |
| RIPE Atlas 探针 | 基准测试（第 3 阶段） | RIPE Atlas T&C v3.5 | 🔍 |
| geofeed（RFC 8805）与 RIPE 数据库 | geofeed 层（第 4 阶段） | 多数未写授权；RIPE DB 限制再利用 | ⚠️ |
| APNIC whois（批量文件 / 查询 / RDAP） | — | **明确禁止用于 IP 定位** | ❌ |
| ip2region | — | 代码 Apache-2.0 OR MIT；数据来源存疑 | ❌（仓库主人 2026-10-02 决定不用） |
| MaxMind GeoLite2（含 MetaCubeX 等转发副本） | — | EULA：提供给第三方前须经 MaxMind 书面同意 | ❌ |
| IP2Location LITE | — | 现行条款写明不得再分发 | ❌ |
| IPinfo Lite | — | CC BY-SA 4.0；只有国家和 ASN | ❌ |
| Sypex Geo | — | 许可协议禁止分发 | ❌ |
| 纯真（CZ88 / QQWry / CZDB）、IPIP.net 免费版 | — | 按项目规则不用 | ❌ |

---

## ✅ 用于公开数据 / Used in the published data

### DB-IP IP to City Lite

- **内容**：IPv4 / IPv6 → 大洲、国家、一级行政区、城市、经纬度。2026-10 版 7,786,864 条。
- **下载**：`https://download.db-ip.com/free/dbip-city-lite-{YYYY}-{MM}.mmdb.gz`（每月 1 日发布；当月文件还没出来时退回上个月）。
- **协议**："The free DB-IP Lite database by DB-IP is licensed under a Creative Commons Attribution 4.0 International License."
- **署名**："You are free to use this database in your application, provided you give attribution to DB-IP.com for the data." / "In the case of a web application, you must include a link back to DB-IP.com on pages that display or use results from the database." 指定写法：`<a href='https://db-ip.com'>IP Geolocation by DB-IP</a>`
- **字段**（官方格式文档）：`continent`、`country` 带 `geoname_id` 和 10 种语言名称（含 `zh-CN`）；`city.names` 只有 `en`；`location` 只有经纬度，没有 `accuracy_radius` 和 `time_zone`。
- **出处**：<https://db-ip.com/db/download/ip-to-city-lite>、<https://db-ip.com/db/lite.php>、<https://db-ip.com/db/format/ip-to-city-lite/mmdb.html>

### DB-IP IP to ASN Lite（备用 / fallback）

- **下载**：`https://download.db-ip.com/free/dbip-asn-lite-{YYYY}-{MM}.mmdb.gz`，每月 1 日。协议和署名同上。
- **用法**：只在 iptoasn 下载失败时使用，并在构建报告和 issue 里给出警告。
- **出处**：<https://db-ip.com/db/download/ip-to-asn-lite>

### iptoasn.com

- **内容**：IP 段 → 起源 ASN、国家代码、AS 名称（来自 BGP 路由表）。
- **下载**：`https://iptoasn.com/data/ip2asn-combined.tsv.gz`，每小时更新。TSV 列：`range_start`、`range_end`、`AS_number`、`country_code`、`AS_description`。
- **协议**：PDDL 1.0（公有领域，不强制署名），维护者 Frank Denis。公共查询 API 已于 2020-12-31 停用；构建每天最多下载一次并使用缓存。
- **用途**：每条记录的 ASN；中国省级修正用到的「网段 → 起源 ASN」。`data/cn_asn_province.csv` 里的「ASN → 省份」由人工审核，依据之一是 iptoasn 自带的 AS 名称（PDDL）；iptoasn 没有说明这些名称最初的来源。
- **出处**：<https://iptoasn.com/>

### Cloudflare / Fastly 公布的网段

| 列表 | 地址 | 标记 | 条款 |
|---|---|---|---|
| Cloudflare | <https://www.cloudflare.com/ips-v4/>、<https://www.cloudflare.com/ips-v6/> | anycast + cdn | 页面没写授权。任播依据："Cloudflare responds with an anycast IP address"（<https://developers.cloudflare.com/fundamentals/concepts/how-cloudflare-works/>） |
| Fastly | <https://api.fastly.com/public-ip-list> | cdn | 文档没写授权 |

### 云厂商公布的网段 / Cloud provider ranges

| 列表 | 地址 | 标记 | 条款 |
|---|---|---|---|
| AWS `ip-ranges.json` | <https://ip-ranges.amazonaws.com/ip-ranges.json> | `cloud=aws` + 地域；`CLOUDFRONT` → cdn；`GLOBALACCELERATOR` → anycast | 文档页没写授权 |
| Google `cloud.json` | <https://www.gstatic.com/ipranges/cloud.json> | `cloud=gcp` + 地域 | 说明页文字为 CC BY 4.0，JSON 本身没写 |
| Azure Service Tags | 微软下载中心 id=56519（`ServiceTags_Public_YYYYMMDD.json`，每周换地址，构建时从下载页解析） | `AzureCloud.*` → `cloud=azure` + 地域；`AzureFrontDoor.Frontend` → cdn | 下载页没写授权 |
| Oracle `public_ip_ranges.json` | <https://docs.oracle.com/iaas/tools/public_ip_ranges.json> | `cloud=oracle` + 地域 | 只有 "(C) Copyright 2026"，没写授权 |

以上列表都是厂商公开给大家写防火墙规则用的事实性数据，没有明确的再分发授权；仓库主人 2026-10-02 决定接受。产物里只出现「某网段属于某厂商 / 是 CDN / 是任播」这样的标记。

### GeoNames

- **用途**：`data/cn_admin.csv`、`data/cn_cities.csv` 的中英文名称、geoname_id 和坐标（用 `egeo gen-cn-admin` 生成，人工审核后提交）。
- **下载**：`https://download.geonames.org/export/dump/` 下的 `admin1CodesASCII.txt`、`CN.zip`、`alternatenames/CN.zip`。
- **协议**：readme.txt："This work is licensed under a Creative Commons Attribution 4.0 License"；官网："commercial usage is allowed"。
- **署名**："You should give credit to GeoNames when using data or web services with a link or another reference to GeoNames." → <https://www.geonames.org/>

### 本仓库维护的表 / Curated tables

`data/overrides.csv`、`data/cn_admin.csv`、`data/cn_cities.csv`、`data/cn_asn_province.csv`、`data/anycast_prefixes.csv`、`data/anycast_asns.csv`，随数据按 CC BY 4.0 发布：

- 每一行都要写依据；
- 依据不能来自标 ❌ 的数据源，也不能是 APNIC whois 的查询结果；
- 优先使用当事方自己公布的信息。

---

## 🔍 只用于统计或评估 / Statistics or evaluation only

### APNIC delegated 统计文件

- `https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest`，每天更新。
- 条款（README.TXT）："The files are freely available for download and use on the condition that APNIC will not be held responsible for any loss or damage arising from the use of the information contained in these reports."
- 只用中国的分配总量当覆盖率的分母，不当定位数据，也不参与「上游是否变化」的判断。

### RIPE Atlas 探针（第 3 阶段）

- API `https://atlas.ripe.net/api/v2/probes/`；每日存档 `https://ftp.ripe.net/ripe/atlas/probes/archive/`。
- RIPE Atlas Service Terms and Conditions v3.5（2025-09-26 更新，2025-11-04 生效）第 4.5 条："Users agree to make responsible use of the RIPE Atlas Data in line with the purposes of the RIPE Atlas Service"；"Any commercial use of the RIPE Atlas Data is subject to prior permission by the RIPE NCC"。
- 探针坐标："Locations of all probes, whether they are marked as public or not, are irreversibly obfuscated up to one kilometre away."
- 只用来计算误差，`ACCURACY.md` 只发布按国家汇总的统计。

---

## ⚠️ 待定 / Pending

### geofeed（RFC 8805）与 RIPE 数据库（第 4 阶段）

- geofeed 是各运营商自己发布的 CSV，绝大多数没写授权。
- 找 geofeed 地址要读 RIR 的 whois。RIPE Database Terms and Conditions 第 4.5 条："A User may not re-package, download, compile, re-distribute or re-use any or all of the RIPE Database or the data contained therein unless they do so only with an insubstantial part of the RIPE Database or the data contained therein or when permission to do so is granted by the RIPE NCC."；第 3.1 条又把 "publishing geolocation information about the usage of Internet number resources" 列为数据库用途之一。动手前要向 RIPE NCC 确认。

---

## ❌ 不使用 / Not used

### APNIC whois（批量文件、whois 查询、RDAP）

APNIC Whois Database Acceptable Use Agreement（APNIC whois 和 RDAP 结果里的条款链接 <http://www.apnic.net/db/dbcopyright.html> 会跳转到这里）：

> "Similarly, you cannot use the database to map IP address to geographic location."
>
> "The APNIC whois data may not be passed on in bulk to any other person or organization unless approved by APNIC."

所以不从 `inet6num` 的 `netname` / `descr` 推省份，也不用 whois 里的高校名称给教育网定位。出处：<https://www.apnic.net/manage-ip/using-whois/bulk-access/copyright/>

### ip2region

- 代码协议 `Apache-2.0 OR MIT`。
- `data/ipv4_source.txt` 是 2015 年数据文件一路改名延续下来的；v1 时期的 README（tag `v1.11.0`）原文（略去链接）："ip2region的数据聚合自以下服务商的开放API或者数据(升级程序每秒请求次数2到4次): 01, >80%, 淘宝IP地址库 / 02, ≈10%, GeoIP / 03, ≈2%, 纯真IP库"。现在的 README 不再说明来源，2025-09 加入的 IPv6 数据也没说来源。
- 数据本身：港澳台写成 `中国|香港特别行政区|…|CN`；IPv6 里电信 `240e::/20`、联通 `2408:8000::/20` 99% 以上标为北京，移动 `2409:8000::/20` 91.8% 标为广东。
- 仓库主人 2026-10-02 决定不使用。

### MaxMind GeoLite2（包括任何转发的副本）

- GeoLite EULA（2026-02-12 更新）："you will not disclose the Services to any third party without notifying MaxMind of the anticipated disclosure and obtaining MaxMind's prior written consent"。<https://www.maxmind.com/en/geolite2/eula>
- MetaCubeX 发布的 `GeoLite2-ASN.mmdb` 来自 `xishang0128/geoip`，后者用 license key 从 `download.maxmind.com` 下载后原样发布，同样受 EULA 约束。

### IP2Location LITE

<https://lite.ip2location.com/data-license> 已不再提 CC BY-SA，写的是 "You are not permitted to redistribute or resell this product"，同页又写 "Licensees may copy, distribute, display, and perform the work and create derivative works"。条款矛盾，不使用。

### IPinfo Lite

CC BY-SA 4.0，只有国家、大洲和 ASN（<https://ipinfo.io/lite>）。没有增量，还会让整个数据变成 BY-SA。

### Sypex Geo

许可协议 <https://sypexgeo.net/ru/agreement/> 第 3.2.4 条把 "Распространять Программный продукт или индивидуальные копии файлов, библиотек и другого исходного кода продукта" 列为禁止事项。

### 纯真（CZ88 / QQWry / CZDB）、IPIP.net 免费版

按项目规则不用。

---

## 发布数据的署名 / Attribution of the published data

- 数据协议：CC BY 4.0（见 [DATA_LICENSE.md](DATA_LICENSE.md)）；代码：Apache-2.0。
- IP Geolocation by DB-IP — <https://db-ip.com>（CC BY 4.0，经过合并和修改）
- GeoNames — <https://www.geonames.org/>（CC BY 4.0）
- iptoasn.com, by Frank Denis（PDDL 1.0）
- 网页应用必须在显示或使用查询结果的页面放上 `<a href='https://db-ip.com'>IP Geolocation by DB-IP</a>`。
