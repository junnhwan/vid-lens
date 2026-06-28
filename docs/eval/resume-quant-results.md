# VidLens Resume Quantification Results

## RAG Retrieval A/B Evaluation

- Date: 2026-06-28
- Environment: local
- Code commit: d77d130
- Task IDs: 2, 5, 6
- Case count: 50
- Embedding model: text-embedding-3-small
- TopK: 5
- CandidateK: 30
- Latency note: retrieval latency excludes the shared query embedding API call.

| Mode | Recall@5 | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context | Rerank Changed Rank Count | Citation Context Hit Rate | Expanded Context Hit Rate |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vector only | 96.0% | 0.837 | 0.0% | 1.88 ms | 0.0% | 0.0 chars | 0 | 96.0% | 0.0% |
| Vector + BM25 + RRF | 98.0% | 0.878 | 0.0% | 3.39 ms | 0.0% | 0.0 chars | 0 | 98.0% | 0.0% |
| Rewrite + MultiQuery + RRF | 98.0% | 0.904 | 0.0% | 1831.57 ms | 0.0% | 3561.9 chars | 0 | 98.0% | 0.0% |
| Rewrite + MultiQuery + RRF + Window + Rerank | 98.0% | 0.825 | 0.0% | 1993.62 ms | 0.0% | 10131.6 chars | 41 | 100.0% | 26.0% |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | 96.0% | 0.638 | 0.0% | 3016.48 ms | 0.0% | 10042.4 chars | 48 | 100.0% | 58.0% |

### Per-Category Metrics

| Mode | Category | Cases | Recall@5 | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context | Citation Context Hit Rate | Expanded Context Hit Rate | Rerank Changed Rank Count |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vector only | context_window_needed | 5 | 100.0% | 0.900 | 0.0% | 1.60 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector only | direct_fact | 16 | 93.8% | 0.891 | 0.0% | 2.23 ms | 0.0% | 0.0 chars | 93.8% | 0.0% | 0 |
| Vector only | keyword_exact | 18 | 100.0% | 0.756 | 0.0% | 1.78 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector only | rerank_needed | 5 | 100.0% | 1.000 | 0.0% | 1.70 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector only | rewrite_needed | 6 | 83.3% | 0.750 | 0.0% | 1.62 ms | 0.0% | 0.0 chars | 83.3% | 0.0% | 0 |
| Vector + BM25 + RRF | context_window_needed | 5 | 100.0% | 0.867 | 0.0% | 2.96 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | direct_fact | 16 | 100.0% | 0.908 | 0.0% | 3.35 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | keyword_exact | 18 | 100.0% | 0.835 | 0.0% | 3.45 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | rerank_needed | 5 | 100.0% | 1.000 | 0.0% | 3.35 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | rewrite_needed | 6 | 83.3% | 0.833 | 0.0% | 3.76 ms | 0.0% | 0.0 chars | 83.3% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | context_window_needed | 5 | 100.0% | 0.867 | 0.0% | 2096.98 ms | 0.0% | 3619.8 chars | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | direct_fact | 16 | 100.0% | 0.917 | 0.0% | 1730.27 ms | 0.0% | 3878.1 chars | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | keyword_exact | 18 | 100.0% | 0.900 | 0.0% | 1823.91 ms | 0.0% | 3209.6 chars | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | rerank_needed | 5 | 100.0% | 1.000 | 0.0% | 1910.59 ms | 0.0% | 3619.8 chars | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | rewrite_needed | 6 | 83.3% | 0.833 | 0.0% | 1837.64 ms | 0.0% | 3678.7 chars | 83.3% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF + Window + Rerank | context_window_needed | 5 | 100.0% | 0.767 | 0.0% | 2173.47 ms | 0.0% | 10032.2 chars | 100.0% | 20.0% | 4 |
| Rewrite + MultiQuery + RRF + Window + Rerank | direct_fact | 16 | 100.0% | 0.875 | 0.0% | 1784.75 ms | 0.0% | 11067.0 chars | 100.0% | 25.0% | 16 |
| Rewrite + MultiQuery + RRF + Window + Rerank | keyword_exact | 18 | 100.0% | 0.847 | 0.0% | 2009.63 ms | 0.0% | 8916.7 chars | 100.0% | 22.2% | 11 |
| Rewrite + MultiQuery + RRF + Window + Rerank | rerank_needed | 5 | 100.0% | 0.767 | 0.0% | 2437.30 ms | 0.0% | 10481.2 chars | 100.0% | 40.0% | 4 |
| Rewrite + MultiQuery + RRF + Window + Rerank | rewrite_needed | 6 | 83.3% | 0.722 | 0.0% | 1982.97 ms | 0.0% | 11073.3 chars | 100.0% | 33.3% | 6 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | context_window_needed | 5 | 100.0% | 0.633 | 0.0% | 2903.71 ms | 0.0% | 9872.0 chars | 100.0% | 40.0% | 5 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | direct_fact | 16 | 100.0% | 0.688 | 0.0% | 3134.75 ms | 0.0% | 11066.3 chars | 100.0% | 50.0% | 16 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | keyword_exact | 18 | 94.4% | 0.690 | 0.0% | 2913.67 ms | 0.0% | 8913.3 chars | 100.0% | 50.0% | 16 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | rerank_needed | 5 | 100.0% | 0.467 | 0.0% | 3206.53 ms | 0.0% | 10373.4 chars | 100.0% | 100.0% | 5 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | rewrite_needed | 6 | 83.3% | 0.500 | 0.0% | 2945.16 ms | 0.0% | 10565.5 chars | 100.0% | 83.3% | 6 |

Conclusion:
BM25+RRF improved Recall@5 from 96.0% to 98.0% and improved MRR from 0.837 to 0.878. This supports a cautious claim about hybrid retrieval improving retrieval ranking on this self-built case set, not a broad claim about answer accuracy or production RAG quality.
Model Rerank did not improve ranking in this run; 不要写 model rerank 提升检索排名的简历 claim，建议默认关闭或仅作为后续可选优化继续评估。

Resume sentence:
设计并实现 VidLens 视频 RAG 检索评测框架，支持 vector-only、BM25+RRF、query rewrite、多查询召回、相邻片段回填和 rerank 多模式对比；在自建 50 条视频 QA case 上，BM25+RRF 将 Recall@5 从 96.0% 提升至 98.0%，MRR 从 0.837 提升至 0.878，但 model rerank 本轮未证明排序收益，因此不作为简历提升 claim。

### Vector only Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 8.43 ms |
| 2 | true | 1 | 5 | 2.20 ms |
| 3 | true | 1 | 5 | 2.14 ms |
| 4 | true | 3 | 5 | 1.66 ms |
| 5 | false | - | 5 | 2.18 ms |
| 6 | true | 1 | 5 | 1.65 ms |
| 7 | true | 1 | 5 | 1.10 ms |
| 8 | true | 1 | 5 | 1.64 ms |
| 9 | true | 1 | 5 | 2.35 ms |
| 10 | true | 1 | 5 | 2.16 ms |
| 11 | true | 1 | 5 | 2.21 ms |
| 12 | true | 1 | 5 | 1.64 ms |
| 13 | true | 1 | 5 | 1.67 ms |
| 14 | true | 2 | 5 | 1.70 ms |
| 15 | true | 1 | 3 | 2.18 ms |
| 16 | true | 1 | 3 | 1.89 ms |
| 17 | true | 1 | 3 | 2.26 ms |
| 18 | true | 1 | 3 | 2.13 ms |
| 19 | true | 1 | 3 | 2.15 ms |
| 20 | true | 1 | 5 | 1.70 ms |
| 21 | true | 1 | 5 | 1.66 ms |
| 22 | true | 1 | 5 | 1.61 ms |
| 23 | true | 1 | 5 | 1.67 ms |
| 24 | true | 1 | 5 | 1.63 ms |
| 25 | true | 4 | 5 | 1.89 ms |
| 26 | false | - | 5 | 1.56 ms |
| 27 | true | 1 | 5 | 1.44 ms |
| 28 | true | 1 | 5 | 1.78 ms |
| 29 | true | 1 | 5 | 1.59 ms |
| 30 | true | 1 | 5 | 1.59 ms |
| 31 | true | 1 | 5 | 1.61 ms |
| 32 | true | 1 | 5 | 1.62 ms |
| 33 | true | 1 | 3 | 1.16 ms |
| 34 | true | 1 | 3 | 1.63 ms |
| 35 | true | 1 | 3 | 1.68 ms |
| 36 | true | 1 | 5 | 1.64 ms |
| 37 | true | 1 | 5 | 1.61 ms |
| 38 | true | 2 | 5 | 1.41 ms |
| 39 | true | 4 | 5 | 1.69 ms |
| 40 | true | 3 | 5 | 1.66 ms |
| 41 | true | 4 | 5 | 1.64 ms |
| 42 | true | 4 | 5 | 1.60 ms |
| 43 | true | 1 | 5 | 1.66 ms |
| 44 | true | 5 | 5 | 1.74 ms |
| 45 | true | 1 | 5 | 1.63 ms |
| 46 | true | 1 | 5 | 1.64 ms |
| 47 | true | 1 | 5 | 1.06 ms |
| 48 | true | 1 | 3 | 1.90 ms |
| 49 | true | 1 | 5 | 2.16 ms |
| 50 | true | 1 | 5 | 1.73 ms |

Source counts: vector=232

### Vector + BM25 + RRF Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 3.84 ms |
| 2 | true | 1 | 5 | 3.27 ms |
| 3 | true | 1 | 5 | 3.43 ms |
| 4 | true | 2 | 5 | 3.60 ms |
| 5 | true | 5 | 5 | 3.85 ms |
| 6 | true | 1 | 5 | 2.72 ms |
| 7 | true | 1 | 5 | 3.39 ms |
| 8 | true | 1 | 5 | 3.35 ms |
| 9 | true | 1 | 5 | 3.26 ms |
| 10 | true | 1 | 5 | 3.21 ms |
| 11 | true | 1 | 5 | 2.69 ms |
| 12 | true | 1 | 5 | 3.26 ms |
| 13 | true | 1 | 5 | 3.34 ms |
| 14 | true | 1 | 5 | 5.53 ms |
| 15 | true | 1 | 3 | 3.21 ms |
| 16 | true | 1 | 3 | 3.78 ms |
| 17 | true | 1 | 3 | 3.34 ms |
| 18 | true | 1 | 3 | 2.74 ms |
| 19 | true | 1 | 3 | 3.87 ms |
| 20 | true | 1 | 5 | 2.68 ms |
| 21 | true | 1 | 5 | 3.27 ms |
| 22 | true | 1 | 5 | 2.98 ms |
| 23 | true | 1 | 5 | 3.25 ms |
| 24 | true | 1 | 5 | 3.80 ms |
| 25 | true | 3 | 5 | 3.83 ms |
| 26 | false | - | 5 | 3.88 ms |
| 27 | true | 1 | 5 | 3.21 ms |
| 28 | true | 1 | 5 | 3.07 ms |
| 29 | true | 1 | 5 | 3.96 ms |
| 30 | true | 1 | 5 | 2.97 ms |
| 31 | true | 1 | 5 | 3.12 ms |
| 32 | true | 1 | 5 | 2.88 ms |
| 33 | true | 1 | 3 | 2.74 ms |
| 34 | true | 1 | 3 | 3.08 ms |
| 35 | true | 1 | 3 | 2.72 ms |
| 36 | true | 1 | 5 | 2.83 ms |
| 37 | true | 1 | 5 | 3.18 ms |
| 38 | true | 3 | 5 | 2.69 ms |
| 39 | true | 5 | 5 | 4.33 ms |
| 40 | true | 1 | 5 | 3.80 ms |
| 41 | true | 3 | 5 | 3.96 ms |
| 42 | true | 2 | 5 | 3.95 ms |
| 43 | true | 1 | 5 | 3.40 ms |
| 44 | true | 1 | 5 | 4.13 ms |
| 45 | true | 2 | 5 | 3.60 ms |
| 46 | true | 1 | 5 | 2.84 ms |
| 47 | true | 1 | 5 | 3.17 ms |
| 48 | true | 1 | 3 | 2.83 ms |
| 49 | true | 1 | 5 | 4.68 ms |
| 50 | true | 1 | 5 | 3.21 ms |

Source counts: hybrid=213, vector=19

### Rewrite + MultiQuery + RRF Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 1715.24 ms |
| 2 | true | 1 | 5 | 1931.33 ms |
| 3 | true | 1 | 5 | 1165.23 ms |
| 4 | true | 1 | 5 | 2312.28 ms |
| 5 | true | 3 | 5 | 1827.31 ms |
| 6 | true | 1 | 5 | 1912.69 ms |
| 7 | true | 1 | 5 | 1111.46 ms |
| 8 | true | 1 | 5 | 1709.01 ms |
| 9 | true | 1 | 5 | 1787.99 ms |
| 10 | true | 1 | 5 | 1766.81 ms |
| 11 | true | 1 | 5 | 1944.97 ms |
| 12 | true | 1 | 5 | 1733.46 ms |
| 13 | true | 1 | 5 | 1821.33 ms |
| 14 | true | 1 | 5 | 1726.35 ms |
| 15 | true | 1 | 3 | 1654.36 ms |
| 16 | true | 1 | 3 | 1783.44 ms |
| 17 | true | 1 | 3 | 1674.03 ms |
| 18 | true | 1 | 3 | 1711.66 ms |
| 19 | true | 1 | 3 | 1905.94 ms |
| 20 | true | 1 | 5 | 1987.36 ms |
| 21 | true | 1 | 5 | 1184.55 ms |
| 22 | true | 1 | 5 | 1814.92 ms |
| 23 | true | 1 | 5 | 1986.18 ms |
| 24 | true | 1 | 5 | 1832.71 ms |
| 25 | true | 3 | 5 | 1984.13 ms |
| 26 | false | - | 5 | 1904.81 ms |
| 27 | true | 1 | 5 | 2126.52 ms |
| 28 | true | 1 | 5 | 1924.06 ms |
| 29 | true | 1 | 5 | 2003.11 ms |
| 30 | true | 1 | 5 | 1808.85 ms |
| 31 | true | 1 | 5 | 2019.65 ms |
| 32 | true | 1 | 5 | 1748.04 ms |
| 33 | true | 1 | 3 | 1654.57 ms |
| 34 | true | 1 | 3 | 2324.30 ms |
| 35 | true | 1 | 3 | 2059.13 ms |
| 36 | true | 1 | 5 | 2242.37 ms |
| 37 | true | 1 | 5 | 1887.14 ms |
| 38 | true | 3 | 5 | 2491.01 ms |
| 39 | true | 5 | 5 | 2112.27 ms |
| 40 | true | 1 | 5 | 1869.37 ms |
| 41 | true | 2 | 5 | 1870.38 ms |
| 42 | true | 2 | 5 | 1249.53 ms |
| 43 | true | 1 | 5 | 1805.24 ms |
| 44 | true | 1 | 5 | 1746.20 ms |
| 45 | true | 1 | 5 | 1194.16 ms |
| 46 | true | 1 | 5 | 2109.05 ms |
| 47 | true | 1 | 5 | 1843.43 ms |
| 48 | true | 1 | 3 | 1929.73 ms |
| 49 | true | 1 | 5 | 1837.73 ms |
| 50 | true | 1 | 5 | 1833.03 ms |

Source counts: hybrid=200, vector=32

### Rewrite + MultiQuery + RRF + Window + Rerank Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 1914.58 ms |
| 2 | true | 1 | 5 | 1941.67 ms |
| 3 | true | 1 | 5 | 1161.63 ms |
| 4 | true | 1 | 5 | 1712.28 ms |
| 5 | true | 2 | 5 | 2118.63 ms |
| 6 | true | 2 | 5 | 1999.41 ms |
| 7 | true | 1 | 5 | 1112.47 ms |
| 8 | true | 1 | 5 | 1839.60 ms |
| 9 | true | 1 | 5 | 1781.83 ms |
| 10 | true | 1 | 5 | 1763.38 ms |
| 11 | true | 2 | 5 | 2107.06 ms |
| 12 | true | 1 | 5 | 1901.45 ms |
| 13 | true | 1 | 5 | 1704.92 ms |
| 14 | true | 1 | 5 | 1865.18 ms |
| 15 | true | 1 | 3 | 1785.22 ms |
| 16 | true | 1 | 3 | 2334.54 ms |
| 17 | true | 1 | 3 | 1974.54 ms |
| 18 | true | 1 | 3 | 2168.99 ms |
| 19 | true | 1 | 3 | 1986.10 ms |
| 20 | true | 1 | 5 | 1775.83 ms |
| 21 | true | 1 | 5 | 1156.08 ms |
| 22 | true | 1 | 5 | 1875.17 ms |
| 23 | true | 1 | 5 | 1878.49 ms |
| 24 | true | 3 | 5 | 1897.29 ms |
| 25 | true | 2 | 5 | 2130.23 ms |
| 26 | false | - | 5 | 1852.91 ms |
| 27 | true | 2 | 5 | 1688.27 ms |
| 28 | true | 1 | 5 | 2195.96 ms |
| 29 | true | 2 | 5 | 1750.91 ms |
| 30 | true | 1 | 5 | 2145.63 ms |
| 31 | true | 1 | 5 | 2235.35 ms |
| 32 | true | 1 | 5 | 2070.71 ms |
| 33 | true | 1 | 3 | 1868.24 ms |
| 34 | true | 1 | 3 | 2299.08 ms |
| 35 | true | 1 | 3 | 1984.84 ms |
| 36 | true | 1 | 5 | 1980.74 ms |
| 37 | true | 2 | 5 | 2258.82 ms |
| 38 | true | 3 | 5 | 2383.40 ms |
| 39 | true | 4 | 5 | 2431.14 ms |
| 40 | true | 1 | 5 | 2292.70 ms |
| 41 | true | 2 | 5 | 2414.86 ms |
| 42 | true | 2 | 5 | 1627.53 ms |
| 43 | true | 1 | 5 | 2259.55 ms |
| 44 | true | 1 | 5 | 2217.72 ms |
| 45 | true | 1 | 5 | 1649.61 ms |
| 46 | true | 3 | 5 | 2414.47 ms |
| 47 | true | 2 | 5 | 2262.89 ms |
| 48 | true | 1 | 3 | 2760.58 ms |
| 49 | true | 1 | 5 | 2407.89 ms |
| 50 | true | 1 | 5 | 2340.68 ms |

Source counts: hybrid=200, vector=32

### Rewrite + MultiQuery + RRF + Window + Model Rerank Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 3260.32 ms |
| 2 | true | 3 | 5 | 3374.70 ms |
| 3 | true | 2 | 5 | 2465.43 ms |
| 4 | true | 2 | 5 | 2891.72 ms |
| 5 | true | 2 | 5 | 3276.40 ms |
| 6 | true | 2 | 5 | 3260.88 ms |
| 7 | true | 1 | 5 | 2971.15 ms |
| 8 | true | 1 | 5 | 2961.16 ms |
| 9 | true | 2 | 5 | 2866.58 ms |
| 10 | true | 2 | 5 | 3028.57 ms |
| 11 | true | 1 | 5 | 3148.49 ms |
| 12 | true | 2 | 5 | 3088.64 ms |
| 13 | true | 1 | 5 | 3492.62 ms |
| 14 | true | 2 | 5 | 3073.16 ms |
| 15 | true | 1 | 3 | 2557.70 ms |
| 16 | true | 1 | 3 | 2515.94 ms |
| 17 | true | 3 | 3 | 2551.45 ms |
| 18 | true | 1 | 3 | 2448.51 ms |
| 19 | true | 1 | 3 | 2565.30 ms |
| 20 | true | 2 | 5 | 2754.42 ms |
| 21 | true | 1 | 5 | 2626.25 ms |
| 22 | true | 3 | 5 | 2941.49 ms |
| 23 | true | 3 | 5 | 2806.43 ms |
| 24 | false | - | 5 | 2961.15 ms |
| 25 | true | 1 | 5 | 4969.74 ms |
| 26 | true | 1 | 5 | 2695.55 ms |
| 27 | true | 2 | 5 | 3029.03 ms |
| 28 | true | 1 | 5 | 2982.87 ms |
| 29 | true | 1 | 5 | 2911.85 ms |
| 30 | true | 2 | 5 | 2780.00 ms |
| 31 | true | 2 | 5 | 3072.45 ms |
| 32 | true | 2 | 5 | 2920.66 ms |
| 33 | true | 1 | 3 | 2692.24 ms |
| 34 | true | 1 | 3 | 2607.98 ms |
| 35 | true | 3 | 3 | 2278.42 ms |
| 36 | true | 2 | 5 | 2776.55 ms |
| 37 | true | 1 | 5 | 3146.99 ms |
| 38 | true | 1 | 5 | 2981.79 ms |
| 39 | true | 3 | 5 | 3567.79 ms |
| 40 | true | 4 | 5 | 3713.05 ms |
| 41 | false | - | 5 | 3565.20 ms |
| 42 | true | 2 | 5 | 2762.89 ms |
| 43 | true | 3 | 5 | 3334.80 ms |
| 44 | true | 1 | 5 | 3397.37 ms |
| 45 | true | 2 | 5 | 2715.79 ms |
| 46 | true | 2 | 5 | 2893.20 ms |
| 47 | true | 2 | 5 | 3155.56 ms |
| 48 | true | 3 | 3 | 2731.43 ms |
| 49 | true | 2 | 5 | 3427.79 ms |
| 50 | true | 2 | 5 | 3824.66 ms |

Source counts: hybrid=193, vector=39

