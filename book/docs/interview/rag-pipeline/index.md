# RAG Pipeline 面试题

本文档基于 VidLens RAG Pipeline 源码设计，涵盖检索融合、流式输出、缓存策略等核心知识点。

---

## 题目 1: RRF 融合算法原理

### 问题

请解释 Reciprocal Rank Fusion (RRF) 算法的工作原理，并分析 `k` 参数对融合结果的影响。

### 源码参考

**文件**: `internal/service/retrieval_fusion.go:17-101`

```go
// 1. 记录 vector 排名
for i, chunk := range vectorChunks {
    state := getState(chunk)
    state.chunk.VectorRank = i + 1  // 排名从1开始
}

// 2. 记录 keyword 排名
for i, chunk := range keywordChunks {
    state := getState(chunk)
    state.chunk.KeywordRank = i + 1
}

// 3. RRF 分数计算
for _, key := range order {
    chunk := states[key].chunk
    score := 0.0
    if chunk.VectorRank > 0 {
        score += 1.0 / (k + float64(chunk.VectorRank))  // 向量检索贡献
    }
    if chunk.KeywordRank > 0 {
        score += 1.0 / (k + float64(chunk.KeywordRank))  // 关键词检索贡献
    }
    chunk.RRFScore = score
}

// 4. 稳定排序
sort.SliceStable(fused, func(i, j int) bool {
    return fused[i].RRFScore > fused[j].RRFScore  // RRF 分数降序
})
```

### 追问链

1. **基础**: RRF 为什么使用 `1/(k+rank)` 而不是直接使用排名倒数？
   - 提示: 考虑 `k` 的平滑作用，避免排名靠前的文档权重过大

2. **进阶**: 当一个文档只出现在一个检索结果中时，RRF 如何处理？
   - 观察: `VectorRank > 0` 和 `KeywordRank > 0` 的条件判断

3. **实战**: 如果 `k=60`，排名第1和排名第10的文档，RRF 分数差异是多少？
   - 计算: `1/(60+1) ≈ 0.0164` vs `1/(60+10) ≈ 0.0143`，差异仅 12.7%

4. **设计**: 为什么使用 `sort.SliceStable` 而非 `sort.Slice`？
   - 答案: 保持相同 RRF 分数的文档在各自原始列表中的相对顺序

---

## 题目 2: CJK 分词策略

### 问题

VidLens 如何处理中文查询词的分词？为什么采用 n-gram 而非词典分词？

### 源码参考

**文件**: `internal/service/retrieval_fusion.go:116-161`

```go
func ExtractQueryTerms(query string) []string {
    // ASCII: 连续字母/数字 >= 2 个字符算一个 term
    // CJK: 生成 2-gram, 3-gram, 4-gram
}

func addCJKTerms(runes []rune, add func(string)) {
    // 短文本直接返回
    if len(runes) <= 4 {
        add(string(runes))
    }
    // 生成多种长度的 n-gram
    for n := 2; n <= 4; n++ {
        for i := 0; i+n <= len(runes); i++ {
            add(string(runes[i : i+n]))  // 滑动窗口提取
        }
    }
}
```

### 追问链

1. **基础**: 对于查询 "机器学习算法"，会生成哪些 terms？
   - 2-gram: "机器", "器学", "学习", "习算", "算法"
   - 3-gram: "机器学", "器学习", "学习算", "习算法"
   - 4-gram: "机器学习", "器学习算", "学习算法"

2. **进阶**: 为什么 ASCII 要求至少 2 个字符？
   - 过滤单字母噪音，避免 "a", "I" 等无意义 term

3. **设计**: 为什么最大 n-gram 长度选择 4 而不是更大的值？
   - 权衡: 覆盖常见中文词汇 (2-4字) vs 索引膨胀
   - 性能: n 越大，生成的 term 数量呈 O(n²) 增长

4. **优化**: 如何减少 n-gram 带来的索引膨胀？
   - 方案: 可加入停用词过滤、TF-IDF 权重、或使用 jieba 分词替代

---

## 题目 3: 流式输出架构

### 问题

`AskStream` 方法如何实现流式输出？为什么要区分真正的 streaming 和模拟 streaming？

### 源码参考

**文件**: `internal/service/chat.go:285-330`

```go
func (s *ChatService) AskStream(ctx context.Context, ..., emit func(ChatStreamEvent) error) (*AskResult, error) {
    // 先发 citations 事件
    emit(ChatStreamEvent{Type: "citations", Data: prepared.Citations})

    // 双路径: 优先用真正 streaming, 降级到模拟
    if streaming, ok := chat.(ai.StreamingChatClient); ok {
        // 真正的流式输出 - 逐 token 返回
        err = streaming.StreamChat(ctx, prepared.Messages, func(delta string) error {
            answer += delta
            return emit(ChatStreamEvent{Type: "answer", Data: delta})
        })
    } else {
        // 模拟流式 - 先获取完整答案，再分片发送
        answer, err = chat.Chat(ctx, prepared.Messages)
        for _, chunk := range splitAnswerForStream(answer, 80) {
            emit(ChatStreamEvent{Type: "answer", Data: chunk})
        }
    }

    // 保存并发 done 事件
    emit(ChatStreamEvent{Type: "done", Data: map[string]interface{}{...}})
}
```

### 追问链

1. **基础**: `ChatStreamEvent` 的三种类型分别在什么时机发送？
   - `citations`: 检索完成后立即发送，让用户看到引用来源
   - `answer`: 逐块发送答案内容
   - `done`: 答案生成完毕，携带元数据

2. **进阶**: 如何判断 chat client 是否支持真正的 streaming？
   - 类型断言: `chat.(ai.StreamingChatClient)`
   - Go 接口设计: 编译时确定，运行时检查

3. **设计**: 为什么 `splitAnswerForStream` 使用 80 字符作为分片大小？
   - 平衡: 流式体验 vs HTTP 请求次数
   - 参考: 80 字符约等于两行文本，用户感知流畅

4. **实战**: 如果 streaming 过程中客户端断开连接，如何处理？
   - 观察: `emit` 函数返回 error，可触发提前退出
   - ctx 取消: 传播到 LLM 调用，及时释放资源

---

## 题目 4: 两级缓存设计

### 问题

`ChatMemoryStore` 采用 Redis + DB 两级缓存架构，这种设计有什么优劣？

### 源码参考

**文件**: `internal/service/chat.go:44-47`

```go
type ChatMemoryStore interface {
    GetRecentMessages(ctx context.Context, sessionID int64, limit int) ([]model.ChatMessage, error)
    SaveRecentMessages(ctx context.Context, sessionID int64, messages []model.ChatMessage, limit int) error
}

// 实现特点:
// - Redis 优先，DB 兜底
// - 7 天 TTL 自动过期
// - 写入时双写，读取时 Redis miss 才查 DB
```

### 追问链

1. **基础**: 为什么需要两级缓存而非只用 Redis？
   - 持久化: Redis 重启数据可能丢失，DB 作为可靠存储
   - 成本: 热数据放 Redis，冷数据放 DB

2. **进阶**: 写入时采用什么策略保证一致性？
   - 双写: 先写 Redis，再写 DB
   - 风险: DB 写入失败时 Redis 已更新，如何补偿？

3. **设计**: 7 天 TTL 的选择依据是什么？
   - 业务: 对话历史通常 7 天内有上下文价值
   - 资源: 控制 Redis 内存占用

4. **优化**: 如何处理 Redis 和 DB 数据不一致的场景？
   - 方案1: 读取时用 DB 数据修复 Redis (read-repair)
   - 方案2: 定时任务对账
   - 方案3: 接受最终一致性，业务层容忍

---

## 题目 5: RAG 检索流程

### 问题

请描述 `prepareRAGChat` 方法的完整执行流程，以及各步骤的错误处理策略。

### 源码参考

**文件**: `internal/service/chat.go:144-206`

```go
func (s *ChatService) prepareRAGChat(ctx context.Context, userID, sessionID int64, question string,
    topK int, embedding ai.EmbeddingClient, profile ai.Profile) (*preparedRAGChat, error) {

    // 1. 问题长度校验 (L148-151)
    if len([]rune(question)) > 1000 {
        return nil, fmt.Errorf("问题过长")
    }

    // 2. 权限校验 (L153-159)
    session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)

    // 3. 问题向量化 (L160-163)
    queryVector, err := embedding.Embed(ctx, question)

    // 4. 向量检索 (L174-181)
    citations, err := s.retriever.Search(ctx, queryVector, RetrievalRequest{...})

    // 5. 关键词检索 + RRF 融合 (L184-186)
    citations, err = s.mergeKeywordChunks(
        session.TaskID, userID, profile.EmbeddingModel,
        question, citations, candidateK, topK,
    )

    // 6. 无结果直接报错 (L188-190)
    if len(citations) == 0 {
        return nil, fmt.Errorf("未检索到足够相关的视频片段")
    }

    // 7. 加载最近对话历史 (L192-196)
    recent, err := s.loadRecentMessages(ctx, userID, sessionID, recentLimit)

    // 8. 构建 LLM 消息序列 (L197)
    messages := buildRAGMessages(citations, recent, question)

    return &preparedRAGChat{...}, nil
}
```

### 追问链

1. **基础**: 为什么要在步骤 1 校验问题长度？
   - 防护: 避免超长文本导致 embedding API 超时或计费过高
   - 阈值: 1000 字符 ≈ 500 中文字，足够表达复杂问题

2. **进阶**: 步骤 4 和步骤 5 的检索策略有什么区别？
   - 步骤 4: 向量检索，捕捉语义相似性
   - 步骤 5: 关键词检索，捕捉精确匹配
   - RRF 融合两者优势

3. **设计**: 为什么在步骤 6 对空结果直接报错而非返回空答案？
   - 用户体验: 明确告知"无相关内容"，避免 LLM 编造答案
   - 成本控制: 避免无意义的 LLM 调用

4. **实战**: 如果 embedding API 调用失败，应该如何处理？
   - 当前: 直接返回 error，前端显示错误
   - 优化: 可降级为纯关键词检索，不依赖向量

---

## 题目 6: 候选池与最终返回数量

### 问题

`ChatConfig` 中 `TopK=5` 和 `CandidateK=30` 的设计意图是什么？

### 源码参考

**文件**: `internal/service/chat.go:14-19`

```go
type ChatConfig struct {
    TopK        int     // 最终返回数量 (5)
    CandidateK  int     // 候选池大小 (30)
    MinScore    float32 // 最低相似度 (0.35)
    RecentTurns int     // 最近对话轮数
}
```

### 追问链

1. **基础**: `CandidateK` 和 `TopK` 的关系是什么？
   - 流程: 先从 Milvus 召回 30 个候选，经 RRF 融合后取 Top 5
   - 原因: 粗排 + 精排两阶段设计

2. **进阶**: `MinScore=0.35` 在哪里生效？
   - 观察: 向量检索时作为过滤条件，低分文档直接丢弃
   - 平衡: 过高会漏召回，过低会引入噪音

3. **设计**: 为什么最终只返回 5 个结果给 LLM？
   - Context Window: 控制 prompt 长度，避免超出 LLM 限制
   - 质量: 少而精 > 多而杂，减少噪音干扰

4. **优化**: 如何动态调整这些参数？
   - 方案: 根据问题复杂度、文档集大小自适应
   - 实现: 可通过 A/B 测试确定最优值

---

## 题目 7: 向量检索与关键词检索的互补性

### 问题

为什么 VidLens 同时使用向量检索和关键词检索？各自有什么优劣？

### 源码参考

**文件**: `internal/service/chat.go:174-186`

```go
// 4. 向量检索 (L174-181)
citations, err := s.retriever.Search(ctx, queryVector, RetrievalRequest{
    TaskID:       session.TaskID,
    UserID:       userID,
    EmbeddingModel: profile.EmbeddingModel,
    TopK:         candidateK,
    MinScore:     s.cfg.MinScore,
})

// 5. 关键词检索 + RRF 融合 (L184-186)
citations, err = s.mergeKeywordChunks(
    session.TaskID, userID, profile.EmbeddingModel,
    question, citations, candidateK, topK,
)
```

### 追问链

1. **基础**: 向量检索和关键词检索分别擅长什么场景？
   - 向量: 语义相似，"如何学习编程" 匹配 "编程入门教程"
   - 关键词: 精确匹配，"Python" 只匹配包含 "Python" 的文档

2. **进阶**: 什么情况下两种检索结果差异最大？
   - 同义词: "汽车" vs "轿车"，向量能匹配，关键词不能
   - 专有名词: "GPT-4"，关键词精确匹配，向量可能泛化

3. **设计**: RRF 融合如何平衡两种检索的优势？
   - 双高分: 同时出现在两种结果前列的文档，RRF 分数最高
   - 单高分: 只在一种检索中排名靠前，RRF 分数中等
   - 双低分: 两种检索都排名靠后，RRF 分数最低

4. **实战**: 如何判断当前查询应该偏向哪种检索？
   - 分析: 查询中是否包含专有名词、数字、代码等精确匹配需求
   - 动态权重: 可根据查询特征调整 RRF 中向量/关键词的权重

---

## 题目 8: 文本分块策略

### 问题

`RAGIndexService.BuildTaskIndex` 中的文本分块策略是什么？为什么需要先删旧向量再写新向量？

### 源码参考

**文件**: `internal/service/rag_index.go:69-228`

```go
// 流程:
// 1. 文本分块 → 嵌入生成 → Milvus 写入
// 2. 双重维度校验
// 3. 先删旧向量 → 写 DB → 写向量库

// 关键步骤:
// - 删除: DELETE FROM chunks WHERE task_id = ?
// - 写 DB: INSERT INTO chunks (task_id, index, content, ...)
// - 写向量: Milvus INSERT (id, vector, metadata)
```

### 追问链

1. **基础**: 为什么要先删旧向量再写新向量？
   - 一致性: 避免新旧向量共存导致检索结果混乱
   - 幂等性: 重复执行索引构建，结果一致

2. **进阶**: 如果写 DB 成功但写 Milvus 失败，会导致什么问题？
   - 问题: DB 有记录但无对应向量，检索时找不到
   - 解决: 需要事务或补偿机制

3. **设计**: 双重维度校验指什么？
   - 维度1: embedding 向量维度与模型配置一致
   - 维度2: 向量数量与分块数量一致
   - 目的: 防止数据损坏

4. **优化**: 大文档索引时如何避免长时间锁表？
   - 方案: 分批写入，每批 100-500 条
   - 权衡: 批次太大占用内存，批次太小增加网络开销

---

## 题目 9: RAG 评估指标

### 问题

`rag_eval.go` 中定义的 Recall@K、MRR、NoResultRate 分别衡量什么？如何使用这些指标优化系统？

### 源码参考

**文件**: `internal/service/rag_eval.go`

```go
// 评估指标:
// - Recall@K: 在 Top-K 结果中命中正确答案的比例
// - MRR (Mean Reciprocal Rank): 正确答案排名的倒数的平均值
// - NoResultRate: 无结果返回的查询比例

// 使用方式:
// 1. 准备测试集: (question, expected_chunk_id) 对
// 2. 执行检索，记录返回结果
// 3. 计算各指标
```

### 追问链

1. **基础**: Recall@5 = 0.8 意味着什么？
   - 含义: 80% 的查询，正确答案出现在 Top-5 结果中
   - 反面: 20% 的查询需要看第 6 个及之后的结果

2. **进阶**: MRR 和 Recall@K 有什么区别？
   - Recall: 只关心"有没有命中"，不关心排名位置
   - MRR: 关心"命中的位置有多靠前"
   - 示例: 正确答案在第 1 名 vs 第 5 名，Recall 相同但 MRR 不同

3. **设计**: NoResultRate 过高说明什么问题？
   - 可能原因: MinScore 阈值过高、文档覆盖不足、embedding 模型不匹配
   - 优化: 调整阈值、补充文档、更换模型

4. **实战**: 如何建立自动化评估流水线？
   - 步骤: 收集真实查询 → 人工标注 → 自动评估 → 回归测试
   - 工具: 可集成到 CI/CD，每次索引更新后自动评估

---

## 题目 10: 系统整体架构设计

### 问题

请画出 VidLens RAG Pipeline 的整体架构图，并解释各组件的职责。

### 源码参考

**文件**: `internal/service/chat.go`

```go
type ChatService struct {
    repos     *repository.Repositories  // 数据访问层
    retriever RAGRetriever              // 向量检索器
    memory    ChatMemoryStore           // 对话记忆存储
    recorder  ai.CallRecorder           // LLM 调用记录
    cfg       ChatConfig                // 配置参数
}

// 核心流程:
// 1. ChatService.prepareRAGChat() - 检索准备
// 2. ChatService.AskStream() - 流式问答
// 3. RAGRetriever.Search() - 向量检索
// 4. mergeKeywordChunks() - 关键词检索 + RRF 融合
// 5. ChatMemoryStore - 对话历史管理
// 6. ai.StreamingChatClient - LLM 调用
```

### 追问链

1. **基础**: 各组件之间如何解耦？
   - 接口: `RAGRetriever`, `ChatMemoryStore` 都是接口
   - 依赖注入: `ChatService` 通过构造函数注入依赖
   - 好处: 便于测试、替换实现

2. **进阶**: 如果要支持多模态 RAG (图片、音频)，需要修改哪些组件？
   - `RAGRetriever`: 支持多模态向量检索
   - `RetrievedChunk`: 增加媒体类型字段
   - `buildRAGMessages`: 构造多模态 prompt

3. **设计**: 为什么将 `recorder` 设计为独立组件？
   - 职责分离: 记录逻辑与业务逻辑解耦
   - 灵活性: 可开关、可替换实现 (stdout/DB/第三方)
   - 审计: 便于追溯 LLM 调用历史

4. **实战**: 如何横向扩展这个系统？
   - 无状态: `ChatService` 本身无状态，可水平扩展
   - 有状态: `ChatMemoryStore` 依赖 Redis，需保证 Redis 可用
   - 瓶颈: Milvus 检索性能，可通过分片优化

---

## 总结

以上 10 道面试题覆盖了 VidLens RAG Pipeline 的核心知识点:

| 题号 | 主题 | 难度 |
|------|------|------|
| 1 | RRF 融合算法 | ⭐⭐ |
| 2 | CJK 分词策略 | ⭐⭐ |
| 3 | 流式输出架构 | ⭐⭐⭐ |
| 4 | 两级缓存设计 | ⭐⭐⭐ |
| 5 | RAG 检索流程 | ⭐⭐ |
| 6 | 候选池设计 | ⭐⭐ |
| 7 | 混合检索策略 | ⭐⭐⭐ |
| 8 | 文本分块策略 | ⭐⭐⭐ |
| 9 | RAG 评估指标 | ⭐⭐ |
| 10 | 系统架构设计 | ⭐⭐⭐⭐ |

每道题都包含:
- 完整代码片段与行号引用
- 4 层追问链 (基础 → 进阶 → 设计 → 实战)
- 明确的答案方向与提示
