# 决策记录 / Decision log

| 日期 | 决定 | 原因 |
|---|---|---|
| 2026-10-02 | 只做一个公开版本；只用明确允许再分发的数据源；涉及授权拿不准的先问仓库主人 | 项目开源，仓库主人在意授权 |
| 2026-10-02 | 不用 APNIC whois（批量文件、查询、RDAP）推断位置 | APNIC 条款："you cannot use the database to map IP address to geographic location" |
| 2026-10-02 | 不用 ip2region | 早年 README 写明数据聚合自淘宝、GeoIP、纯真；现在的数据是其延续；IPv6 大块基本是默认值 |
| 2026-10-02 | 接受 Cloudflare / Fastly / AWS / Google Cloud / Azure / Oracle 公布的网段列表 | 厂商公开给大家用的事实性数据，没写授权；产物里只出现标记 |
| 2026-10-02 | 中国大陆省份修正用「省公司 ASN」：iptoasn（PDDL）给出网段的起源 ASN，`data/cn_asn_province.csv` 人工维护 ASN → 省份 | 授权干净；省公司在全球路由表里用自己的 ASN 宣告网段；全国骨干 ASN 不收录 |
| 2026-10-02 | 省级修正只在 DB-IP 给出别的省或没有省份时生效，一致时保留 DB-IP 的城市 | 只纠错，不把更细的结果改粗 |
| 2026-10-02 | 原计划的 YAML 表（`anycast_asns.yaml`、`known_ips.yaml` 等）改用 CSV，字段不变 | 不为读 YAML 增加依赖 |
| 2026-10-02 | 文件名前缀暂用 `EnhancedGeo`，集中在 `config.json` | GeoIP 是 MaxMind 的注册商标，文件名避免使用 |
| 2026-10-02 | 第一次正式发布前只做试跑；定时发布由仓库变量 `PUBLISH_ENABLED` 打开 | 创建 Release 前要先问仓库主人 |
| 2026-10-02 | 任播标记只给确认是任播的网段（Cloudflare 公布的列表、公共 DNS、AWS Global Accelerator）；Akamai 等只标 cdn | Akamai 等 CDN 的边缘节点多数是单播，整段标任播会把准确的位置半径拉到 1000 km |
| 2026-10-02 | 精简版位置按 IPv4 /24、IPv6 /40 合并（取覆盖地址最多的位置，精度半径放大到盖住块内三分之二的地址；国家、网络标记不同或有空洞的块不合并），另发 `.mmdb.gz` 给 jsDelivr | 第一次真实构建精简版 63.9 MB，远超 20 MB。用真实 DB-IP 数据测过：坐标改 2° 也只到 55.6 MB，体积大头是 DB-IP 的 IPv6 按 /48 切得很碎（335 万段）。按上面的规则合并后约 29 MB，gzip 约 16 MB；仓库主人选了这个方案 |
