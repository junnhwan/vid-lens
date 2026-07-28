# VidLens Intent Classifier Eval Results

## Intent Classifier Eval (spec 05 分类层)

与 RAG 检索评测口径不同：本节测 intent 分类层（规则层短路 + LLM 兜底），
非检索质量。固化 case 集 = spec 01 train+dev split（16 条），test-sealed 不进
（sealed 框架不进分类评测）。

### 规则层 + LLM 兜底（oracle 上界）

- Date: 2026-07-28
- Code commit: 086c9f6（spec 05 落地）
- Dataset: real-v1 / train+dev split / 16 cases
- LLM 兜底: oracle chat client（返回黄金 intent + 0.9 置信度），上界非真实 LLM

| 指标 | 值 |
| --- | ---: |
| 规则层短路率 | 18.8% (3/16) |
| 规则层单层准确率 | 87.5% (14/16) |
| 总体准确率(规则短路+LLM兜底) | 93.8% (15/16) |
| LLM 兜底命中率(未短路且规则错→LLM命中) | 6.2% (1/13 未短路) |
| 省 LLM 调用(短路派生) | 18.8% |

### 诚信约束（写简历/对外必须带）

- LLM 兜底用 oracle 上界（返回黄金 intent），非真实 LLM 数字；真实 LLM 效果
  需线上对比测，不由本节数字支撑。
- 短路率 18.8% 偏低因 spec 01 dataset 以 direct_qa 精确问答为主（13/16），
  直接问答弱命中不短路是保守设计（避免误压其他 intent 的 LLM 兜底）。
- 固化 case 集规模 16 条（train+dev），统计意义弱于 rerank 消融（6 dev）——
  本节数字诚实标注为"分类层验收"，不夸大为线上分类效果。

### 运行命令

```
go test ./internal/service/ -run TestIntentClassifierEvalOnDataset -v
```
