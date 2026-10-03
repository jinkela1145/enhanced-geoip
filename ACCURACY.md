# ACCURACY — EnhancedGeo 2026.10.03

> 自动生成 / Generated automatically. 基于 RIPE Atlas 的误差基准测试会在第 3 阶段加入，这里先给覆盖率。
> The RIPE Atlas error benchmark arrives in phase 3; this file currently reports coverage only.

「有城市」只表示记录里写了城市，不代表一定准确。
"With city" only means the record names a city; it says nothing about correctness.

## 中国 IPv6 覆盖率 / China IPv6 coverage

分母 / Denominator: APNIC 分配给 CN 的地址空间 / space delegated to CN = 4.49G /48

| 指标 / Metric | 占比 / Share |
|---|---|
| 数据库里有记录 / has a record | 100.00% |
| 国家为 CN / country is CN | 99.97% |
| CN：有城市 / with city | 99.97% |
| CN：只有省 / subdivision only | 0.00% |
| CN：只有国家 / country only | 0.00% |
| CN：位置来自 DB-IP / located by DB-IP | 99.97% |
| CN：位置来自省公司 ASN 层 / located by provincial ASN layer | 0.00% |
| CN：位置来自人工修正 / located by overrides | 0.00% |
| ASN 层与 DB-IP 省份一致（保留 DB-IP）/ ASN layer agrees with DB-IP | 0.00% |
| ASN 层纠正了 DB-IP 的省份 / ASN layer corrected the province | 0.00% |
| ASN 层补上了缺失的省份 / ASN layer filled a missing province | 0.00% |

这部分地址在库里的国家分布 / Countries of this space in the database: CN 99.97%, HK 0.02%, US 0.00%, BE 0.00%, JP 0.00%

## 中国 IPv4 覆盖率 / China IPv4 coverage

分母 / Denominator: APNIC 分配给 CN 的地址空间 / space delegated to CN = 342.94M addresses

| 指标 / Metric | 占比 / Share |
|---|---|
| 数据库里有记录 / has a record | 100.00% |
| 国家为 CN / country is CN | 99.30% |
| CN：有城市 / with city | 99.30% |
| CN：只有省 / subdivision only | 0.00% |
| CN：只有国家 / country only | 0.00% |
| CN：位置来自 DB-IP / located by DB-IP | 99.30% |
| CN：位置来自省公司 ASN 层 / located by provincial ASN layer | 0.00% |
| CN：位置来自人工修正 / located by overrides | 0.00% |
| ASN 层与 DB-IP 省份一致（保留 DB-IP）/ ASN layer agrees with DB-IP | 0.00% |
| ASN 层纠正了 DB-IP 的省份 / ASN layer corrected the province | 0.00% |
| ASN 层补上了缺失的省份 / ASN layer filled a missing province | 0.00% |

这部分地址在库里的国家分布 / Countries of this space in the database: CN 99.30%, HK 0.22%, DE 0.14%, US 0.14%, FR 0.04%

## 全库各层占比 / Layer shares of the whole database

### IPv4

总量 / Total: 3.70G addresses, 3699971 ranges

| 指标 / Metric | 占比 / Share |
|---|---|
| source = dbip | 100.00% |
| 有 ASN / with ASN | 84.64% |
| 任播 / anycast | 0.05% |
| CDN | 0.40% |
| cloud = aws | 2.75% |
| cloud = azure | 1.47% |
| cloud = gcp | 0.52% |
| cloud = oracle | 0.12% |

### IPv6

总量 / Total: 35180.04G /48, 4214130 ranges

| 指标 / Metric | 占比 / Share |
|---|---|
| source = dbip | 100.00% |
| 有 ASN / with ASN | 0.03% |
| 任播 / anycast | 0.00% |
| CDN | 0.00% |
| cloud = aws | 0.00% |
| cloud = azure | 0.00% |
| cloud = gcp | 0.00% |

## Lite 版的合并 / Lite aggregation

Lite 版把被切碎的小块合并成一个位置：取覆盖地址最多的那个，精度半径放大到能盖住这个块三分之二的地址。国家或网络类型不同的块、有空洞的块都原样保留。Full 版不做这种合并。
The Lite edition gives each split block the location that covers most of it and widens the accuracy radius until it covers two thirds of the block. Blocks whose parts differ in country or network flags, or that have gaps, are left as they are. The full edition is not aggregated.

| 地址族 / Family | 块大小 / Block | 合并的块 / Merged blocks | 原样保留的碎块 / Split blocks kept | 位置变了的地址 / Addresses moved |
|---|---|---|---|---|
| IPv4 | /24 | 226125 | 15267 | 0.30% |
| IPv6 | /40 | 271949 | 15953 | 0.00% |

## 基准测试 / Benchmark

第 3 阶段加入 / Coming in phase 3.
