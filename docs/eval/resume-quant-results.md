# VidLens Resume Quantification Results

## RAG Retrieval A/B Evaluation

- Date: 2026-06-28
- Environment: local
- Code commit: e962ebd
- Task IDs: 2, 5, 6
- Case count: 50
- Embedding model: text-embedding-3-small
- TopK: 5
- CandidateK: 30
- Latency note: retrieval latency excludes the shared query embedding API call.

| Mode | Recall@5 | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context | Rerank Changed Rank Count | Citation Context Hit Rate | Expanded Context Hit Rate |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vector only | 96.0% | 0.837 | 0.0% | 2.27 ms | 0.0% | 0.0 chars | 0 | 96.0% | 0.0% |
| Vector + BM25 + RRF | 98.0% | 0.878 | 0.0% | 4.76 ms | 0.0% | 0.0 chars | 0 | 98.0% | 0.0% |
| Rewrite + MultiQuery + RRF | 98.0% | 0.876 | 0.0% | 1851.27 ms | 0.0% | 3587.4 chars | 0 | 98.0% | 0.0% |
| Rewrite + MultiQuery + RRF + Window + Rerank | 98.0% | 0.823 | 0.0% | 1932.09 ms | 0.0% | 10173.2 chars | 41 | 100.0% | 28.0% |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | 96.0% | 0.638 | 0.0% | 3218.22 ms | 0.0% | 10042.4 chars | 48 | 100.0% | 58.0% |

### Per-Category Metrics

| Mode | Category | Cases | Recall@5 | MRR | No Result Rate | Avg Retrieval Latency | Rewrite Fallback Rate | Avg Expanded Context | Citation Context Hit Rate | Expanded Context Hit Rate | Rerank Changed Rank Count |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Vector only | context_window_needed | 5 | 100.0% | 0.900 | 0.0% | 2.50 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector only | direct_fact | 16 | 93.8% | 0.891 | 0.0% | 2.61 ms | 0.0% | 0.0 chars | 93.8% | 0.0% | 0 |
| Vector only | keyword_exact | 18 | 100.0% | 0.756 | 0.0% | 2.16 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector only | rerank_needed | 5 | 100.0% | 1.000 | 0.0% | 1.79 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector only | rewrite_needed | 6 | 83.3% | 0.750 | 0.0% | 1.93 ms | 0.0% | 0.0 chars | 83.3% | 0.0% | 0 |
| Vector + BM25 + RRF | context_window_needed | 5 | 100.0% | 0.867 | 0.0% | 4.72 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | direct_fact | 16 | 100.0% | 0.908 | 0.0% | 4.24 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | keyword_exact | 18 | 100.0% | 0.835 | 0.0% | 5.04 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | rerank_needed | 5 | 100.0% | 1.000 | 0.0% | 5.60 ms | 0.0% | 0.0 chars | 100.0% | 0.0% | 0 |
| Vector + BM25 + RRF | rewrite_needed | 6 | 83.3% | 0.833 | 0.0% | 4.63 ms | 0.0% | 0.0 chars | 83.3% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | context_window_needed | 5 | 100.0% | 0.850 | 0.0% | 1695.19 ms | 0.0% | 3619.6 chars | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | direct_fact | 16 | 100.0% | 0.906 | 0.0% | 1787.65 ms | 0.0% | 3957.9 chars | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | keyword_exact | 18 | 100.0% | 0.835 | 0.0% | 1926.65 ms | 0.0% | 3209.7 chars | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | rerank_needed | 5 | 100.0% | 1.000 | 0.0% | 1956.40 ms | 0.0% | 3619.8 chars | 100.0% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF | rewrite_needed | 6 | 83.3% | 0.833 | 0.0% | 1837.21 ms | 0.0% | 3678.7 chars | 83.3% | 0.0% | 0 |
| Rewrite + MultiQuery + RRF + Window + Rerank | context_window_needed | 5 | 100.0% | 0.767 | 0.0% | 2054.23 ms | 0.0% | 10192.4 chars | 100.0% | 20.0% | 4 |
| Rewrite + MultiQuery + RRF + Window + Rerank | direct_fact | 16 | 100.0% | 0.875 | 0.0% | 1891.55 ms | 0.0% | 11117.1 chars | 100.0% | 25.0% | 16 |
| Rewrite + MultiQuery + RRF + Window + Rerank | keyword_exact | 18 | 100.0% | 0.806 | 0.0% | 1954.13 ms | 0.0% | 8952.2 chars | 100.0% | 33.3% | 11 |
| Rewrite + MultiQuery + RRF + Window + Rerank | rerank_needed | 5 | 100.0% | 0.900 | 0.0% | 1784.52 ms | 0.0% | 10448.8 chars | 100.0% | 20.0% | 4 |
| Rewrite + MultiQuery + RRF + Window + Rerank | rewrite_needed | 6 | 83.3% | 0.722 | 0.0% | 1995.28 ms | 0.0% | 11073.3 chars | 100.0% | 33.3% | 6 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | context_window_needed | 5 | 100.0% | 0.633 | 0.0% | 3608.64 ms | 0.0% | 9871.8 chars | 100.0% | 40.0% | 5 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | direct_fact | 16 | 100.0% | 0.688 | 0.0% | 3082.79 ms | 0.0% | 11066.4 chars | 100.0% | 50.0% | 16 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | keyword_exact | 18 | 94.4% | 0.690 | 0.0% | 3084.26 ms | 0.0% | 8913.3 chars | 100.0% | 50.0% | 16 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | rerank_needed | 5 | 100.0% | 0.467 | 0.0% | 3308.56 ms | 0.0% | 10373.4 chars | 100.0% | 100.0% | 5 |
| Rewrite + MultiQuery + RRF + Window + Model Rerank | rewrite_needed | 6 | 83.3% | 0.500 | 0.0% | 3580.62 ms | 0.0% | 10565.5 chars | 100.0% | 83.3% | 6 |

## Agent Answer Evaluation

| Mode | Answer Point Coverage | Citation Hit Rate | No Answer Rate | Avg Tool Steps | Fallback/Error Rate | Avg Latency |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Ordinary RAG answer | 28.0% | 100.0% | 6.0% | 0.0 | 2.0% | 7248.09 ms |
| Agentic answer | 22.0% | 98.0% | 2.0% | 2.0 | 2.0% | 8496.85 ms |

Agentic answer eval did not prove a safer answer-point coverage improvement over ordinary RAG in this run. Do not claim answer accuracy improvement from Agentic QA without stronger eval evidence.

Agentic QA resume positioning:
This run does not support claiming that Agentic QA improved answer accuracy, deterministic answer-point coverage, or citation hit rate. Ordinary RAG answer scored 28.0% answer-point coverage and 100.0% citation hit rate, while Agentic answer scored 22.0% answer-point coverage and 98.0% citation hit rate. Agentic answer did reduce no-answer rate from 6.0% to 2.0%, but that is not enough to claim overall answer quality improvement because coverage regressed and latency increased.

Defensible Agentic QA wording:
在 VidLens 视频 RAG 基础上实现模板式 Agentic Video QA，将复杂视频问题拆成检索、相邻转写窗口加载、片段总结/对比/风险分析和带引用回答生成等受限步骤；接口返回 tool trace、citations、template 和 model，提升回答链路的可解释性。本轮 50 条真实 QA answer eval 未证明 Agentic 回答覆盖率或引用命中率优于 ordinary RAG，因此不写“回答准确率提升”类 claim。

Conclusion:
BM25+RRF improved Recall@5 from 96.0% to 98.0% and improved MRR from 0.837 to 0.878. This supports a cautious claim about hybrid retrieval improving retrieval ranking on this self-built case set, not a broad claim about answer accuracy or production RAG quality.
Model Rerank did not improve ranking in this run; 不要写 model rerank 提升检索排名的简历 claim，建议默认关闭或仅作为后续可选优化继续评估。

Resume sentence:
设计并实现 VidLens 视频 RAG 检索评测框架，支持 vector-only、BM25+RRF、query rewrite、多查询召回、相邻片段回填和 rerank 多模式对比；在自建 50 条视频 QA case 上，BM25+RRF 将 Recall@5 从 96.0% 提升至 98.0%，MRR 从 0.837 提升至 0.878，但 model rerank 本轮未证明排序收益，因此不作为简历提升 claim。

### Vector only Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 10.98 ms |
| 2 | true | 1 | 5 | 2.20 ms |
| 3 | true | 1 | 5 | 2.24 ms |
| 4 | true | 3 | 5 | 1.71 ms |
| 5 | false | - | 5 | 2.21 ms |
| 6 | true | 1 | 5 | 2.69 ms |
| 7 | true | 1 | 5 | 3.39 ms |
| 8 | true | 1 | 5 | 2.77 ms |
| 9 | true | 1 | 5 | 2.18 ms |
| 10 | true | 1 | 5 | 1.63 ms |
| 11 | true | 1 | 5 | 1.73 ms |
| 12 | true | 1 | 5 | 2.41 ms |
| 13 | true | 1 | 5 | 2.09 ms |
| 14 | true | 2 | 5 | 2.13 ms |
| 15 | true | 1 | 3 | 2.16 ms |
| 16 | true | 1 | 3 | 2.26 ms |
| 17 | true | 1 | 3 | 2.98 ms |
| 18 | true | 1 | 3 | 2.24 ms |
| 19 | true | 1 | 3 | 2.25 ms |
| 20 | true | 1 | 5 | 2.22 ms |
| 21 | true | 1 | 5 | 1.62 ms |
| 22 | true | 1 | 5 | 1.65 ms |
| 23 | true | 1 | 5 | 2.22 ms |
| 24 | true | 1 | 5 | 1.64 ms |
| 25 | true | 4 | 5 | 1.64 ms |
| 26 | false | - | 5 | 1.65 ms |
| 27 | true | 1 | 5 | 1.73 ms |
| 28 | true | 1 | 5 | 1.58 ms |
| 29 | true | 1 | 5 | 1.78 ms |
| 30 | true | 1 | 5 | 2.13 ms |
| 31 | true | 1 | 5 | 1.61 ms |
| 32 | true | 1 | 5 | 1.63 ms |
| 33 | true | 1 | 3 | 1.67 ms |
| 34 | true | 1 | 3 | 2.25 ms |
| 35 | true | 1 | 3 | 2.29 ms |
| 36 | true | 1 | 5 | 2.41 ms |
| 37 | true | 1 | 5 | 2.33 ms |
| 38 | true | 2 | 5 | 2.93 ms |
| 39 | true | 4 | 5 | 2.47 ms |
| 40 | true | 3 | 5 | 2.04 ms |
| 41 | true | 4 | 5 | 2.14 ms |
| 42 | true | 4 | 5 | 2.20 ms |
| 43 | true | 1 | 5 | 2.52 ms |
| 44 | true | 5 | 5 | 2.34 ms |
| 45 | true | 1 | 5 | 1.66 ms |
| 46 | true | 1 | 5 | 1.59 ms |
| 47 | true | 1 | 5 | 1.73 ms |
| 48 | true | 1 | 3 | 2.17 ms |
| 49 | true | 1 | 5 | 1.72 ms |
| 50 | true | 1 | 5 | 1.73 ms |

Source counts: vector=232

### Vector + BM25 + RRF Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 4.12 ms |
| 2 | true | 1 | 5 | 3.68 ms |
| 3 | true | 1 | 5 | 3.18 ms |
| 4 | true | 2 | 5 | 3.51 ms |
| 5 | true | 5 | 5 | 4.29 ms |
| 6 | true | 1 | 5 | 3.82 ms |
| 7 | true | 1 | 5 | 3.80 ms |
| 8 | true | 1 | 5 | 4.30 ms |
| 9 | true | 1 | 5 | 4.05 ms |
| 10 | true | 1 | 5 | 3.88 ms |
| 11 | true | 1 | 5 | 3.40 ms |
| 12 | true | 1 | 5 | 3.34 ms |
| 13 | true | 1 | 5 | 5.05 ms |
| 14 | true | 1 | 5 | 3.87 ms |
| 15 | true | 1 | 3 | 4.31 ms |
| 16 | true | 1 | 3 | 6.27 ms |
| 17 | true | 1 | 3 | 5.81 ms |
| 18 | true | 1 | 3 | 4.85 ms |
| 19 | true | 1 | 3 | 4.38 ms |
| 20 | true | 1 | 5 | 3.79 ms |
| 21 | true | 1 | 5 | 4.42 ms |
| 22 | true | 1 | 5 | 4.96 ms |
| 23 | true | 1 | 5 | 5.18 ms |
| 24 | true | 1 | 5 | 4.76 ms |
| 25 | true | 3 | 5 | 4.72 ms |
| 26 | false | - | 5 | 5.38 ms |
| 27 | true | 1 | 5 | 5.77 ms |
| 28 | true | 1 | 5 | 3.95 ms |
| 29 | true | 1 | 5 | 3.88 ms |
| 30 | true | 1 | 5 | 5.66 ms |
| 31 | true | 1 | 5 | 4.78 ms |
| 32 | true | 1 | 5 | 4.89 ms |
| 33 | true | 1 | 3 | 5.54 ms |
| 34 | true | 1 | 3 | 5.46 ms |
| 35 | true | 1 | 3 | 4.28 ms |
| 36 | true | 1 | 5 | 4.99 ms |
| 37 | true | 1 | 5 | 4.77 ms |
| 38 | true | 3 | 5 | 5.32 ms |
| 39 | true | 5 | 5 | 6.58 ms |
| 40 | true | 1 | 5 | 4.80 ms |
| 41 | true | 3 | 5 | 5.50 ms |
| 42 | true | 2 | 5 | 5.53 ms |
| 43 | true | 1 | 5 | 4.24 ms |
| 44 | true | 1 | 5 | 4.75 ms |
| 45 | true | 2 | 5 | 6.12 ms |
| 46 | true | 1 | 5 | 5.47 ms |
| 47 | true | 1 | 5 | 4.36 ms |
| 48 | true | 1 | 3 | 7.14 ms |
| 49 | true | 1 | 5 | 5.93 ms |
| 50 | true | 1 | 5 | 5.07 ms |

Source counts: hybrid=213, vector=19

### Rewrite + MultiQuery + RRF Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 1514.69 ms |
| 2 | true | 1 | 5 | 1548.13 ms |
| 3 | true | 1 | 5 | 1269.91 ms |
| 4 | true | 2 | 5 | 1863.67 ms |
| 5 | true | 4 | 5 | 2083.06 ms |
| 6 | true | 1 | 5 | 1782.52 ms |
| 7 | true | 1 | 5 | 1812.80 ms |
| 8 | true | 1 | 5 | 1797.99 ms |
| 9 | true | 1 | 5 | 1623.52 ms |
| 10 | true | 1 | 5 | 2638.07 ms |
| 11 | true | 1 | 5 | 2056.28 ms |
| 12 | true | 1 | 5 | 1719.19 ms |
| 13 | true | 1 | 5 | 1874.99 ms |
| 14 | true | 1 | 5 | 1649.87 ms |
| 15 | true | 1 | 3 | 1777.75 ms |
| 16 | true | 1 | 3 | 1960.10 ms |
| 17 | true | 1 | 3 | 1648.77 ms |
| 18 | true | 1 | 3 | 1769.56 ms |
| 19 | true | 1 | 3 | 1698.87 ms |
| 20 | true | 1 | 5 | 1810.66 ms |
| 21 | true | 1 | 5 | 1985.42 ms |
| 22 | true | 1 | 5 | 1617.84 ms |
| 23 | true | 1 | 5 | 1886.40 ms |
| 24 | true | 1 | 5 | 1861.79 ms |
| 25 | true | 4 | 5 | 1716.41 ms |
| 26 | false | - | 5 | 2062.90 ms |
| 27 | true | 1 | 5 | 2400.71 ms |
| 28 | true | 1 | 5 | 1827.27 ms |
| 29 | true | 1 | 5 | 1677.28 ms |
| 30 | true | 1 | 5 | 1936.99 ms |
| 31 | true | 1 | 5 | 1792.54 ms |
| 32 | true | 1 | 5 | 1744.03 ms |
| 33 | true | 1 | 3 | 1766.19 ms |
| 34 | true | 1 | 3 | 2235.32 ms |
| 35 | true | 1 | 3 | 1987.40 ms |
| 36 | true | 1 | 5 | 1815.11 ms |
| 37 | true | 1 | 5 | 1293.05 ms |
| 38 | true | 4 | 5 | 1680.85 ms |
| 39 | true | 5 | 5 | 1819.52 ms |
| 40 | true | 1 | 5 | 1872.67 ms |
| 41 | true | 3 | 5 | 2476.33 ms |
| 42 | true | 2 | 5 | 1908.76 ms |
| 43 | true | 1 | 5 | 1699.54 ms |
| 44 | true | 1 | 5 | 1856.96 ms |
| 45 | true | 2 | 5 | 1959.58 ms |
| 46 | true | 1 | 5 | 1854.89 ms |
| 47 | true | 1 | 5 | 1938.31 ms |
| 48 | true | 1 | 3 | 1841.24 ms |
| 49 | true | 1 | 5 | 1853.02 ms |
| 50 | true | 1 | 5 | 2294.53 ms |

Source counts: hybrid=202, vector=30

### Rewrite + MultiQuery + RRF + Window + Rerank Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 1729.43 ms |
| 2 | true | 1 | 5 | 1794.53 ms |
| 3 | true | 1 | 5 | 1491.14 ms |
| 4 | true | 2 | 5 | 1760.88 ms |
| 5 | true | 2 | 5 | 1706.55 ms |
| 6 | true | 2 | 5 | 1786.08 ms |
| 7 | true | 1 | 5 | 1819.53 ms |
| 8 | true | 1 | 5 | 1834.73 ms |
| 9 | true | 1 | 5 | 1928.68 ms |
| 10 | true | 1 | 5 | 2042.33 ms |
| 11 | true | 2 | 5 | 1869.37 ms |
| 12 | true | 1 | 5 | 1960.69 ms |
| 13 | true | 1 | 5 | 1901.45 ms |
| 14 | true | 1 | 5 | 1836.32 ms |
| 15 | true | 1 | 3 | 1186.69 ms |
| 16 | true | 1 | 3 | 2092.59 ms |
| 17 | true | 1 | 3 | 1734.91 ms |
| 18 | true | 1 | 3 | 1818.90 ms |
| 19 | true | 1 | 3 | 2040.07 ms |
| 20 | true | 1 | 5 | 1907.48 ms |
| 21 | true | 1 | 5 | 2259.80 ms |
| 22 | true | 1 | 5 | 1864.87 ms |
| 23 | true | 1 | 5 | 1799.93 ms |
| 24 | true | 3 | 5 | 1767.12 ms |
| 25 | true | 2 | 5 | 2014.81 ms |
| 26 | false | - | 5 | 1958.44 ms |
| 27 | true | 1 | 5 | 2016.86 ms |
| 28 | true | 1 | 5 | 1862.35 ms |
| 29 | true | 2 | 5 | 2290.15 ms |
| 30 | true | 1 | 5 | 2186.41 ms |
| 31 | true | 1 | 5 | 2262.72 ms |
| 32 | true | 1 | 5 | 1973.12 ms |
| 33 | true | 1 | 3 | 1964.26 ms |
| 34 | true | 1 | 3 | 1932.08 ms |
| 35 | true | 1 | 3 | 2204.47 ms |
| 36 | true | 1 | 5 | 1917.29 ms |
| 37 | true | 2 | 5 | 1307.97 ms |
| 38 | true | 3 | 5 | 2533.14 ms |
| 39 | true | 4 | 5 | 2049.18 ms |
| 40 | true | 1 | 5 | 2355.22 ms |
| 41 | true | 4 | 5 | 2123.74 ms |
| 42 | true | 2 | 5 | 2355.26 ms |
| 43 | true | 1 | 5 | 2308.27 ms |
| 44 | true | 1 | 5 | 2044.48 ms |
| 45 | true | 2 | 5 | 2087.66 ms |
| 46 | true | 1 | 5 | 1997.48 ms |
| 47 | true | 2 | 5 | 1691.92 ms |
| 48 | true | 1 | 3 | 1755.61 ms |
| 49 | true | 1 | 5 | 1745.99 ms |
| 50 | true | 1 | 5 | 1731.61 ms |

Source counts: hybrid=203, vector=29

### Rewrite + MultiQuery + RRF + Window + Model Rerank Case Details

| # | Hit | First Hit Rank | Result Count | Latency |
| ---: | --- | ---: | ---: | ---: |
| 1 | true | 1 | 5 | 2890.71 ms |
| 2 | true | 3 | 5 | 3046.96 ms |
| 3 | true | 2 | 5 | 2285.21 ms |
| 4 | true | 2 | 5 | 2875.05 ms |
| 5 | true | 2 | 5 | 3596.61 ms |
| 6 | true | 2 | 5 | 3446.54 ms |
| 7 | true | 1 | 5 | 3086.68 ms |
| 8 | true | 1 | 5 | 3250.56 ms |
| 9 | true | 2 | 5 | 2891.76 ms |
| 10 | true | 2 | 5 | 2881.12 ms |
| 11 | true | 1 | 5 | 3235.47 ms |
| 12 | true | 2 | 5 | 2873.77 ms |
| 13 | true | 1 | 5 | 2809.08 ms |
| 14 | true | 2 | 5 | 2916.10 ms |
| 15 | true | 1 | 3 | 2103.29 ms |
| 16 | true | 1 | 3 | 2667.66 ms |
| 17 | true | 3 | 3 | 2637.66 ms |
| 18 | true | 1 | 3 | 2517.33 ms |
| 19 | true | 1 | 3 | 2551.52 ms |
| 20 | true | 2 | 5 | 3110.78 ms |
| 21 | true | 1 | 5 | 3712.33 ms |
| 22 | true | 3 | 5 | 3071.65 ms |
| 23 | true | 3 | 5 | 2978.74 ms |
| 24 | false | - | 5 | 3193.43 ms |
| 25 | true | 1 | 5 | 3131.20 ms |
| 26 | true | 1 | 5 | 6160.38 ms |
| 27 | true | 2 | 5 | 2867.39 ms |
| 28 | true | 1 | 5 | 3259.79 ms |
| 29 | true | 1 | 5 | 3108.35 ms |
| 30 | true | 2 | 5 | 3166.23 ms |
| 31 | true | 2 | 5 | 3173.83 ms |
| 32 | true | 2 | 5 | 3127.84 ms |
| 33 | true | 1 | 3 | 2578.25 ms |
| 34 | true | 1 | 3 | 2776.00 ms |
| 35 | true | 3 | 3 | 2641.78 ms |
| 36 | true | 2 | 5 | 3525.62 ms |
| 37 | true | 1 | 5 | 2567.46 ms |
| 38 | true | 1 | 5 | 5851.85 ms |
| 39 | true | 3 | 5 | 3495.31 ms |
| 40 | true | 4 | 5 | 3567.30 ms |
| 41 | false | - | 5 | 3361.56 ms |
| 42 | true | 2 | 5 | 4226.13 ms |
| 43 | true | 3 | 5 | 3456.49 ms |
| 44 | true | 1 | 5 | 3779.47 ms |
| 45 | true | 2 | 5 | 3915.98 ms |
| 46 | true | 2 | 5 | 2781.45 ms |
| 47 | true | 2 | 5 | 2881.19 ms |
| 48 | true | 3 | 3 | 2741.73 ms |
| 49 | true | 2 | 5 | 4565.57 ms |
| 50 | true | 2 | 5 | 3572.86 ms |

Source counts: hybrid=196, vector=36
