# VidLens Resume Quantification Results

## RAG Retrieval A/B Evaluation

- Date: 2026-06-28
- Environment: local
- Code commit: 04c9f96
- Task IDs: 2, 5, 6
- Case count: 50
- Embedding model: text-embedding-3-small
- TopK: 5
- CandidateK: 30
- Latency note: retrieval latency excludes the shared query embedding API call.

| Mode | Recall@5 | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context | Rerank Changed Rank Count | Citation Context Hit Rate | Expanded Context Hit Rate |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vector only | 96.0% | 0.837 | 0.0% | 1.99 ms | 0.0% | 0.0 chars | 0 | 96.0% | 0.0% |
| Vector + BM25 + RRF | 98.0% | 0.878 | 0.0% | 3.48 ms | 0.0% | 0.0 chars | 0 | 98.0% | 0.0% |
| Rewrite + MultiQuery + RRF | 96.0% | 0.896 | 0.0% | 1881.86 ms | 0.0% | 3587.4 chars | 0 | 96.0% | 0.0% |
| Rewrite + MultiQuery + RRF + Window + Rerank | 98.0% | 0.828 | 0.0% | 1867.43 ms | 0.0% | 10160.3 chars | 41 | 100.0% | 28.0% |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | 96.0% | 0.648 | 0.0% | 2987.20 ms | 0.0% | 10042.4 chars | 49 | 100.0% | 56.0% |

### Per-Category Metrics

| Mode | Category | Cases | Recall@5 | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Citation Context Hit Rate | Expanded Context Hit Rate | Rerank Changed Rank Count |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vector only | context_window_needed | 5 | 100.0% | 0.900 | 0.0% | 1.85 ms | 0.0% | 100.0% | 0.0% | 0 |
| Vector only | direct_fact | 16 | 93.8% | 0.891 | 0.0% | 2.34 ms | 0.0% | 93.8% | 0.0% | 0 |
| Vector only | keyword_exact | 18 | 100.0% | 0.756 | 0.0% | 1.69 ms | 0.0% | 100.0% | 0.0% | 0 |
| Vector only | rerank_needed | 5 | 100.0% | 1.000 | 0.0% | 1.81 ms | 0.0% | 100.0% | 0.0% | 0 |
| Vector only | rewrite_needed | 6 | 83.3% | 0.750 | 0.0% | 2.19 ms | 0.0% | 83.3% | 0.0% | 0 |
| Vector + BM25 + RRF | context_window_needed | 5 | 100.0% | 0.867 | 0.0% | 3.53 ms | 0.0% | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | direct_fact | 16 | 100.0% | 0.908 | 0.0% | 3.39 ms | 0.0% | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | keyword_exact | 18 | 100.0% | 0.835 | 0.0% | 3.50 ms | 0.0% | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | rerank_needed | 5 | 100.0% | 1.000 | 0.0% | 3.48 ms | 0.0% | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | rewrite_needed | 6 | 83.3% | 0.833 | 0.0% | 3.65 ms | 0.0% | 83.3% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | context_window_needed | 5 | 100.0% | 0.850 | 0.0% | 2004.11 ms | 0.0% | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | direct_fact | 16 | 93.8% | 0.896 | 0.0% | 1809.59 ms | 0.0% | 93.8% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | keyword_exact | 18 | 100.0% | 0.900 | 0.0% | 1904.00 ms | 0.0% | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | rerank_needed | 5 | 100.0% | 1.000 | 0.0% | 1900.54 ms | 0.0% | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | rewrite_needed | 6 | 83.3% | 0.833 | 0.0% | 1890.71 ms | 0.0% | 83.3% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF + Window + Rerank | context_window_needed | 5 | 100.0% | 0.767 | 0.0% | 2261.30 ms | 0.0% | 100.0% | 20.0% | 4 |
| Rewrite + MultiQuery + RRF + Window + Rerank | direct_fact | 16 | 100.0% | 0.875 | 0.0% | 1749.43 ms | 0.0% | 100.0% | 25.0% | 16 |
| Rewrite + MultiQuery + RRF + Window + Rerank | keyword_exact | 18 | 100.0% | 0.819 | 0.0% | 1846.60 ms | 0.0% | 100.0% | 33.3% | 11 |
| Rewrite + MultiQuery + RRF + Window + Rerank | rerank_needed | 5 | 100.0% | 0.900 | 0.0% | 1943.03 ms | 0.0% | 100.0% | 20.0% | 4 |
| Rewrite + MultiQuery + RRF + Window + Rerank | rewrite_needed | 6 | 83.3% | 0.722 | 0.0% | 1853.36 ms | 0.0% | 100.0% | 33.3% | 6 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | context_window_needed | 5 | 100.0% | 0.633 | 0.0% | 2860.74 ms | 0.0% | 100.0% | 40.0% | 5 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | direct_fact | 16 | 100.0% | 0.688 | 0.0% | 2948.05 ms | 0.0% | 100.0% | 50.0% | 16 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | keyword_exact | 18 | 94.4% | 0.718 | 0.0% | 3046.59 ms | 0.0% | 100.0% | 44.4% | 17 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | rerank_needed | 5 | 100.0% | 0.467 | 0.0% | 3116.69 ms | 0.0% | 100.0% | 100.0% | 5 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | rewrite_needed | 6 | 83.3% | 0.500 | 0.0% | 2910.93 ms | 0.0% | 100.0% | 83.3% | 6 |

Conclusion:
BM25+RRF improved Recall@5 from 96.0% to 98.0% and improved MRR from 0.837 to 0.878. This supports a cautious claim about hybrid retrieval improving retrieval ranking on this self-built case set, not a broad claim about answer accuracy or production RAG quality.
Model Rerank did not improve ranking in this run; 不要写 model rerank 提升检索排名的简历 claim，建议默认关闭或仅作为后续可选优化继续评估。

Resume sentence:
设计并实现 VidLens 视频 RAG 检索评测框架，支持 vector-only、BM25+RRF、query rewrite、多查询召回、相邻片段回填和 rerank 多模式对比；在自建 50 条视频 QA case 上，BM25+RRF 将 Recall@5 从 96.0% 提升至 98.0%，MRR 从 0.837 提升至 0.878，但 model rerank 本轮未证明排序收益，因此不作为简历提升 claim。

### Vector only Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 6.87 ms |
| 2 | true | 1 | 5 | 1.59 ms |
| 3 | true | 1 | 5 | 2.11 ms |
| 4 | true | 3 | 5 | 1.65 ms |
| 5 | false | - | 5 | 3.35 ms |
| 6 | true | 1 | 5 | 1.60 ms |
| 7 | true | 1 | 5 | 2.25 ms |
| 8 | true | 1 | 5 | 1.77 ms |
| 9 | true | 1 | 5 | 1.08 ms |
| 10 | true | 1 | 5 | 1.60 ms |
| 11 | true | 1 | 5 | 2.14 ms |
| 12 | true | 1 | 5 | 1.26 ms |
| 13 | true | 1 | 5 | 1.58 ms |
| 14 | true | 2 | 5 | 1.76 ms |
| 15 | true | 1 | 3 | 1.57 ms |
| 16 | true | 1 | 3 | 1.60 ms |
| 17 | true | 1 | 3 | 1.04 ms |
| 18 | true | 1 | 3 | 1.56 ms |
| 19 | true | 1 | 3 | 1.55 ms |
| 20 | true | 1 | 5 | 1.56 ms |
| 21 | true | 1 | 5 | 1.04 ms |
| 22 | true | 1 | 5 | 1.56 ms |
| 23 | true | 1 | 5 | 1.62 ms |
| 24 | true | 1 | 5 | 4.70 ms |
| 25 | true | 4 | 5 | 4.82 ms |
| 26 | false | - | 5 | 1.67 ms |
| 27 | true | 1 | 5 | 1.56 ms |
| 28 | true | 1 | 5 | 2.14 ms |
| 29 | true | 1 | 5 | 2.19 ms |
| 30 | true | 1 | 5 | 1.55 ms |
| 31 | true | 1 | 5 | 2.19 ms |
| 32 | true | 1 | 5 | 2.08 ms |
| 33 | true | 1 | 3 | 1.56 ms |
| 34 | true | 1 | 3 | 1.59 ms |
| 35 | true | 1 | 3 | 1.55 ms |
| 36 | true | 1 | 5 | 1.55 ms |
| 37 | true | 1 | 5 | 1.65 ms |
| 38 | true | 2 | 5 | 2.38 ms |
| 39 | true | 4 | 5 | 2.23 ms |
| 40 | true | 3 | 5 | 1.94 ms |
| 41 | true | 4 | 5 | 2.17 ms |
| 42 | true | 4 | 5 | 1.68 ms |
| 43 | true | 1 | 5 | 2.12 ms |
| 44 | true | 5 | 5 | 1.61 ms |
| 45 | true | 1 | 5 | 1.62 ms |
| 46 | true | 1 | 5 | 1.55 ms |
| 47 | true | 1 | 5 | 1.58 ms |
| 48 | true | 1 | 3 | 1.72 ms |
| 49 | true | 1 | 5 | 2.07 ms |
| 50 | true | 1 | 5 | 2.13 ms |

Source counts: vector=232

### Vector + BM25 + RRF Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 3.87 ms |
| 2 | true | 1 | 5 | 3.12 ms |
| 3 | true | 1 | 5 | 3.14 ms |
| 4 | true | 2 | 5 | 3.08 ms |
| 5 | true | 5 | 5 | 3.68 ms |
| 6 | true | 1 | 5 | 3.19 ms |
| 7 | true | 1 | 5 | 3.05 ms |
| 8 | true | 1 | 5 | 3.03 ms |
| 9 | true | 1 | 5 | 4.23 ms |
| 10 | true | 1 | 5 | 4.63 ms |
| 11 | true | 1 | 5 | 3.31 ms |
| 12 | true | 1 | 5 | 3.03 ms |
| 13 | true | 1 | 5 | 3.17 ms |
| 14 | true | 1 | 5 | 3.84 ms |
| 15 | true | 1 | 3 | 3.12 ms |
| 16 | true | 1 | 3 | 3.78 ms |
| 17 | true | 1 | 3 | 2.65 ms |
| 18 | true | 1 | 3 | 3.74 ms |
| 19 | true | 1 | 3 | 3.38 ms |
| 20 | true | 1 | 5 | 3.74 ms |
| 21 | true | 1 | 5 | 2.63 ms |
| 22 | true | 1 | 5 | 3.17 ms |
| 23 | true | 1 | 5 | 3.23 ms |
| 24 | true | 1 | 5 | 3.87 ms |
| 25 | true | 3 | 5 | 3.15 ms |
| 26 | false | - | 5 | 2.94 ms |
| 27 | true | 1 | 5 | 2.94 ms |
| 28 | true | 1 | 5 | 3.16 ms |
| 29 | true | 1 | 5 | 3.54 ms |
| 30 | true | 1 | 5 | 5.04 ms |
| 31 | true | 1 | 5 | 3.20 ms |
| 32 | true | 1 | 5 | 3.19 ms |
| 33 | true | 1 | 3 | 3.75 ms |
| 34 | true | 1 | 3 | 4.01 ms |
| 35 | true | 1 | 3 | 3.16 ms |
| 36 | true | 1 | 5 | 3.00 ms |
| 37 | true | 1 | 5 | 2.96 ms |
| 38 | true | 3 | 5 | 4.00 ms |
| 39 | true | 5 | 5 | 4.67 ms |
| 40 | true | 1 | 5 | 3.76 ms |
| 41 | true | 3 | 5 | 3.69 ms |
| 42 | true | 2 | 5 | 3.48 ms |
| 43 | true | 1 | 5 | 4.55 ms |
| 44 | true | 1 | 5 | 3.72 ms |
| 45 | true | 2 | 5 | 3.16 ms |
| 46 | true | 1 | 5 | 2.62 ms |
| 47 | true | 1 | 5 | 3.62 ms |
| 48 | true | 1 | 3 | 4.24 ms |
| 49 | true | 1 | 5 | 3.65 ms |
| 50 | true | 1 | 5 | 3.27 ms |

Source counts: hybrid=213, vector=19

### Rewrite + MultiQuery + RRF Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 1757.19 ms |
| 2 | true | 1 | 5 | 2051.48 ms |
| 3 | true | 1 | 5 | 1726.52 ms |
| 4 | true | 1 | 5 | 2000.51 ms |
| 5 | false | - | 5 | 2191.09 ms |
| 6 | true | 1 | 5 | 1853.39 ms |
| 7 | true | 1 | 5 | 1547.35 ms |
| 8 | true | 1 | 5 | 2153.83 ms |
| 9 | true | 1 | 5 | 2022.88 ms |
| 10 | true | 1 | 5 | 2010.15 ms |
| 11 | true | 1 | 5 | 1807.70 ms |
| 12 | true | 1 | 5 | 1314.78 ms |
| 13 | true | 1 | 5 | 1174.90 ms |
| 14 | true | 1 | 5 | 1809.08 ms |
| 15 | true | 1 | 3 | 2058.28 ms |
| 16 | true | 1 | 3 | 1713.03 ms |
| 17 | true | 1 | 3 | 1991.58 ms |
| 18 | true | 1 | 3 | 1973.93 ms |
| 19 | true | 1 | 3 | 1853.49 ms |
| 20 | true | 1 | 5 | 2222.47 ms |
| 21 | true | 1 | 5 | 1966.30 ms |
| 22 | true | 1 | 5 | 1124.27 ms |
| 23 | true | 1 | 5 | 1705.83 ms |
| 24 | true | 1 | 5 | 2290.78 ms |
| 25 | true | 3 | 5 | 1916.51 ms |
| 26 | false | - | 5 | 2087.85 ms |
| 27 | true | 1 | 5 | 1889.92 ms |
| 28 | true | 1 | 5 | 2038.97 ms |
| 29 | true | 1 | 5 | 1864.61 ms |
| 30 | true | 1 | 5 | 1825.56 ms |
| 31 | true | 1 | 5 | 2016.25 ms |
| 32 | true | 1 | 5 | 2002.05 ms |
| 33 | true | 1 | 3 | 2240.24 ms |
| 34 | true | 2 | 3 | 1887.87 ms |
| 35 | true | 1 | 3 | 1920.59 ms |
| 36 | true | 1 | 5 | 1962.94 ms |
| 37 | true | 1 | 5 | 2226.98 ms |
| 38 | true | 4 | 5 | 2116.93 ms |
| 39 | true | 5 | 5 | 1792.48 ms |
| 40 | true | 1 | 5 | 2132.27 ms |
| 41 | true | 1 | 5 | 1956.28 ms |
| 42 | true | 2 | 5 | 1022.60 ms |
| 43 | true | 1 | 5 | 1793.12 ms |
| 44 | true | 1 | 5 | 1724.08 ms |
| 45 | true | 1 | 5 | 1851.37 ms |
| 46 | true | 1 | 5 | 1918.32 ms |
| 47 | true | 1 | 5 | 1787.33 ms |
| 48 | true | 1 | 3 | 1992.44 ms |
| 49 | true | 1 | 5 | 1959.30 ms |
| 50 | true | 1 | 5 | 1845.29 ms |

Source counts: hybrid=208, vector=24

### Rewrite + MultiQuery + RRF + Window + Rerank Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 1926.25 ms |
| 2 | true | 1 | 5 | 1813.23 ms |
| 3 | true | 1 | 5 | 1766.24 ms |
| 4 | true | 1 | 5 | 1947.59 ms |
| 5 | true | 2 | 5 | 2003.54 ms |
| 6 | true | 2 | 5 | 1879.54 ms |
| 7 | true | 1 | 5 | 1198.06 ms |
| 8 | true | 1 | 5 | 1717.38 ms |
| 9 | true | 1 | 5 | 1956.00 ms |
| 10 | true | 1 | 5 | 1936.25 ms |
| 11 | true | 2 | 5 | 2391.01 ms |
| 12 | true | 1 | 5 | 1314.78 ms |
| 13 | true | 1 | 5 | 1185.16 ms |
| 14 | true | 1 | 5 | 1868.77 ms |
| 15 | true | 1 | 3 | 2027.38 ms |
| 16 | true | 1 | 3 | 1807.44 ms |
| 17 | true | 1 | 3 | 1834.71 ms |
| 18 | true | 1 | 3 | 1696.31 ms |
| 19 | true | 1 | 3 | 1567.42 ms |
| 20 | true | 1 | 5 | 1995.31 ms |
| 21 | true | 1 | 5 | 1669.61 ms |
| 22 | true | 1 | 5 | 1241.98 ms |
| 23 | true | 1 | 5 | 1876.01 ms |
| 24 | true | 3 | 5 | 1816.83 ms |
| 25 | true | 2 | 5 | 1724.41 ms |
| 26 | false | - | 5 | 2016.59 ms |
| 27 | true | 2 | 5 | 1868.33 ms |
| 28 | true | 1 | 5 | 1871.52 ms |
| 29 | true | 2 | 5 | 1907.84 ms |
| 30 | true | 1 | 5 | 2294.13 ms |
| 31 | true | 1 | 5 | 1809.08 ms |
| 32 | true | 1 | 5 | 2035.29 ms |
| 33 | true | 1 | 3 | 1667.90 ms |
| 34 | true | 2 | 3 | 1651.98 ms |
| 35 | true | 1 | 3 | 2631.21 ms |
| 36 | true | 1 | 5 | 1780.80 ms |
| 37 | true | 2 | 5 | 2062.87 ms |
| 38 | true | 3 | 5 | 2485.46 ms |
| 39 | true | 4 | 5 | 2087.54 ms |
| 40 | true | 1 | 5 | 1996.81 ms |
| 41 | true | 2 | 5 | 2066.19 ms |
| 42 | true | 2 | 5 | 1307.79 ms |
| 43 | true | 1 | 5 | 2346.17 ms |
| 44 | true | 1 | 5 | 1912.55 ms |
| 45 | true | 1 | 5 | 1695.21 ms |
| 46 | true | 1 | 5 | 1834.17 ms |
| 47 | true | 2 | 5 | 1841.85 ms |
| 48 | true | 1 | 3 | 2225.57 ms |
| 49 | true | 1 | 5 | 1930.48 ms |
| 50 | true | 1 | 5 | 1883.09 ms |

Source counts: hybrid=205, vector=27

### Rewrite + MultiQuery + RRF + Window + Model Rerank Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 3143.06 ms |
| 2 | true | 3 | 5 | 3202.49 ms |
| 3 | true | 2 | 5 | 2965.83 ms |
| 4 | true | 2 | 5 | 3515.39 ms |
| 5 | true | 2 | 5 | 2929.98 ms |
| 6 | true | 2 | 5 | 3041.46 ms |
| 7 | true | 1 | 5 | 2287.20 ms |
| 8 | true | 1 | 5 | 3580.44 ms |
| 9 | true | 2 | 5 | 2800.31 ms |
| 10 | true | 2 | 5 | 2792.17 ms |
| 11 | true | 1 | 5 | 2935.34 ms |
| 12 | true | 2 | 5 | 2423.53 ms |
| 13 | true | 1 | 5 | 2816.27 ms |
| 14 | true | 2 | 5 | 2904.12 ms |
| 15 | true | 1 | 3 | 2474.11 ms |
| 16 | true | 1 | 3 | 2623.00 ms |
| 17 | true | 3 | 3 | 2570.54 ms |
| 18 | true | 1 | 3 | 2375.47 ms |
| 19 | true | 1 | 3 | 2471.00 ms |
| 20 | true | 1 | 5 | 2803.25 ms |
| 21 | true | 1 | 5 | 3083.96 ms |
| 22 | true | 3 | 5 | 2405.28 ms |
| 23 | true | 3 | 5 | 3606.00 ms |
| 24 | false | - | 5 | 3021.14 ms |
| 25 | true | 1 | 5 | 2899.27 ms |
| 26 | true | 1 | 5 | 2828.34 ms |
| 27 | true | 2 | 5 | 3376.08 ms |
| 28 | true | 1 | 5 | 3124.11 ms |
| 29 | true | 1 | 5 | 3074.30 ms |
| 30 | true | 2 | 5 | 3322.34 ms |
| 31 | true | 2 | 5 | 2966.13 ms |
| 32 | true | 2 | 5 | 2997.10 ms |
| 33 | true | 1 | 3 | 2703.49 ms |
| 34 | true | 1 | 3 | 2781.38 ms |
| 35 | true | 3 | 3 | 2262.48 ms |
| 36 | true | 2 | 5 | 2946.40 ms |
| 37 | true | 1 | 5 | 2847.30 ms |
| 38 | true | 1 | 5 | 2909.49 ms |
| 39 | true | 3 | 5 | 3347.60 ms |
| 40 | true | 4 | 5 | 4123.95 ms |
| 41 | false | - | 5 | 3283.77 ms |
| 42 | true | 2 | 5 | 2850.93 ms |
| 43 | true | 3 | 5 | 3338.02 ms |
| 44 | true | 1 | 5 | 3444.26 ms |
| 45 | true | 2 | 5 | 3578.67 ms |
| 46 | true | 2 | 5 | 2950.03 ms |
| 47 | true | 2 | 5 | 3028.16 ms |
| 48 | true | 3 | 3 | 2632.10 ms |
| 49 | true | 2 | 5 | 3510.61 ms |
| 50 | true | 2 | 5 | 3462.56 ms |

Source counts: hybrid=198, vector=34
