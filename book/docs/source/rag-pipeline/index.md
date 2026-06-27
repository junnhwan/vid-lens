# RAG Pipeline 源码走读

本文档详细分析 VidLens RAG Pipeline 的源码实现，包括核心结构体、关键函数、调用链和设计决策。

---

## 1. 文件表

| 文件路径 | 职责 | 核心类型/函数 |
|---------|------|--------------|
| `internal/service/chat.go` | 聊天服务主入口 | `ChatService`, `ChatConfig`, `RetrievedChunk` |
| `internal/service/retrieval_fusion.go` | 检索结果融合 | `FuseRetrievedChunks`, `ExtractQueryTerms` |
| `internal/service/chat_memory.go` | 对话历史存储 | `ChatMemoryStore` 接口 |
| `internal/service/rag_index.go` | RAG 索引构建 | `RAGIndexService`, `BuildTaskIndex` |
| `internal/service/rag_eval.go` | RAG 评估工具 | Recall@K, MRR, NoResultRate |
| `internal/repository/` | 数据访问层 | `Repositories` |
| `internal/ai/` | AI 客户端接口 | `EmbeddingClient`, `StreamingChatClient` |

---

## 2. 核心结构体

### 2.1 ChatService

**文件**: `internal/service/chat.go:49-55`

```go
type ChatService struct {
    repos     *repository.Repositories  // 数据访问层，提供 DB 操作
    retriever RAGRetriever              // 向量检索器，连接 Milvus
    memory    ChatMemoryStore           // 对话记忆，Redis + DB 两级缓存
    recorder  ai.CallRecorder           // LLM 调用记录，用于审计和调试
    cfg       ChatConfig                // 配置参数
}
```

**设计意图**:
- 依赖注入: 所有依赖通过构造函数注入，便于测试和替换
- 接口抽象: `RAGRetriever`, `ChatMemoryStore`, `CallRecorder` 都是接口
- 职责单一: `ChatService` 只负责编排，不直接操作存储

---

### 2.2 ChatConfig

**文件**: `internal/service/chat.go:14-19`

```go
type ChatConfig struct {
    TopK        int     // 最终返回数量 (默认 5)
    CandidateK  int     // 候选池大小 (默认 30)
    MinScore    float32 // 最低相似度阈值 (默认 0.35)
    RecentTurns int     // 加载最近对话轮数
}
```

**参数说明**:

| 参数 | 默认值 | 作用 |
|------|--------|------|
| `TopK` | 5 | 最终返回给 LLM 的文档数量 |
| `CandidateK` | 30 | 粗排阶段召回的候选数量 |
| `MinScore` | 0.35 | 向量检索的最低相似度过滤 |
| `RecentTurns` | - | 加载最近 N 轮对话作为上下文 |

**设计决策**:
- 两阶段设计: `CandidateK` (粗排) > `TopK` (精排)，平衡召回率和精度
- `MinScore` 阈值: 过滤低质量结果，减少噪音
- 可配置: 通过配置文件或环境变量调整，无需改代码

---

### 2.3 RetrievedChunk

**文件**: `internal/service/chat.go:29-38`

```go
type RetrievedChunk struct {
    ChunkID     int64   // 数据库主键
    ChunkIndex  int     // 分块在文档中的位置
    Score       float32 // 原始相似度分数
    Content     string  // 文本内容
    Source      string  // 来源: "vector" / "keyword" / "hybrid"
    VectorRank  int     // 在向量检索结果中的排名
    KeywordRank int     // 在关键词检索结果中的排名
    RRFScore    float64 // RRF 融合后的分数
}
```

**字段职责**:
- `ChunkID`: 唯一标识，用于关联数据库记录
- `Source`: 标记结果来源，便于调试和分析
- `VectorRank`/`KeywordRank`: 记录原始排名，用于 RRF 计算
- `RRFScore`: 最终排序依据

---

## 3. 关键函数分析

### 3.1 FuseRetrievedChunks - RRF 融合算法

**文件**: `internal/service/retrieval_fusion.go:17-101`

```go
func FuseRetrievedChunks(
    vectorChunks []RetrievedChunk,   // 向量检索结果
    keywordChunks []RetrievedChunk,  // 关键词检索结果
    topK int,                        // 返回数量
    k float64,                       // RRF 参数 (默认 60.0)
) []RetrievedChunk {

    if k <= 0 {
        k = defaultRRFK  // 60.0
    }

    // 1. 记录 vector 排名
    for i, chunk := range vectorChunks {
        state := getState(chunk)
        state.chunk.VectorRank = i + 1  // 排名从 1 开始
    }

    // 2. 记录 keyword 排名
    for i, chunk := range keywordChunks {
        state := getState(chunk)
        state.chunk.KeywordRank = i + 1
    }

    // 3. 计算 RRF 分数
    for _, key := range order {
        chunk := states[key].chunk
        score := 0.0
        if chunk.VectorRank > 0 {
            score += 1.0 / (k + float64(chunk.VectorRank))
        }
        if chunk.KeywordRank > 0 {
            score += 1.0 / (k + float64(chunk.KeywordRank))
        }
        chunk.RRFScore = score
    }

    // 4. 稳定排序
    sort.SliceStable(fused, func(i, j int) bool {
        return fused[i].RRFScore > fused[j].RRFScore
    })

    // 5. 截取 topK
    if len(fused) > topK {
        fused = fused[:topK]
    }

    return fused
}
```

**算法流程**:
1. 遍历向量检索结果，记录每个 chunk 的排名
2. 遍历关键词检索结果，记录每个 chunk 的排名
3. 计算 RRF 分数: `score = 1/(k+rank_vector) + 1/(k+rank_keyword)`
4. 按 RRF 分数降序排序 (稳定排序)
5. 截取前 topK 个结果

**设计亮点**:
- 使用 `map` 去重: 同一 chunk 只保留最高分数
- 稳定排序: 相同分数保持原始顺序
- `k=60`: 经验值，平衡排名差异

---

### 3.2 ExtractQueryTerms - CJK 分词

**文件**: `internal/service/retrieval_fusion.go:116-161`

```go
func ExtractQueryTerms(query string) []string {
    terms := []string{}
    runes := []rune(query)
    currentASCII := []rune{}

    addASCIITerm := func() {
        if len(currentASCII) >= 2 {
            terms = append(terms, string(currentASCII))
        }
        currentASCII = []rune{}
    }

    for _, r := range runes {
        if isASCII(r) {
            currentASCII = append(currentASCII, r)
        } else {
            addASCIITerm()
            if isCJK(r) {
                // CJK 字符，提取 n-gram
                addCJKTerms(...)
            }
        }
    }
    addASCIITerm()

    return terms
}

func addCJKTerms(runes []rune, add func(string)) {
    // 短文本直接返回
    if len(runes) <= 4 {
        add(string(runes))
    }
    // 生成 2-gram, 3-gram, 4-gram
    for n := 2; n <= 4; n++ {
        for i := 0; i+n <= len(runes); i++ {
            add(string(runes[i : i+n]))
        }
    }
}
```

**处理逻辑**:
- ASCII: 连续字母/数字 >= 2 个字符算一个 term
- CJK: 生成 2-gram, 3-gram, 4-gram

**示例**:
```
输入: "学习Python编程"
输出: ["学习", "Python", "编程", "学习Py", "ython", "学习Pyt", "ython编", ...]
```

**设计权衡**:
- n-gram vs 词典分词: 简单高效，无需加载词典
- 2-4 gram: 覆盖常见中文词汇，避免索引膨胀
- ASCII 过滤: 避免 "a", "I" 等无意义 term

---

### 3.3 prepareRAGChat - RAG 检索主流程

**文件**: `internal/service/chat.go:144-206`

```go
func (s *ChatService) prepareRAGChat(
    ctx context.Context,
    userID, sessionID int64,
    question string,
    topK int,
    embedding ai.EmbeddingClient,
    profile ai.Profile,
) (*preparedRAGChat, error) {

    // 1. 问题长度校验 (L148-151)
    if len([]rune(question)) > 1000 {
        return nil, fmt.Errorf("问题过长")
    }

    // 2. 权限校验 (L153-159)
    session, err := s.repos.Chat.FindSessionForUser(userID, sessionID)
    if err != nil {
        return nil, err
    }

    // 3. 问题向量化 (L160-163)
    queryVector, err := embedding.Embed(ctx, question)
    if err != nil {
        return nil, err
    }

    // 4. 向量检索 (L174-181)
    citations, err := s.retriever.Search(ctx, queryVector, RetrievalRequest{
        TaskID:         session.TaskID,
        UserID:         userID,
        EmbeddingModel: profile.EmbeddingModel,
        TopK:           candidateK,
        MinScore:       s.cfg.MinScore,
    })
    if err != nil {
        return nil, err
    }

    // 5. 关键词检索 + RRF 融合 (L184-186)
    citations, err = s.mergeKeywordChunks(
        session.TaskID, userID, profile.EmbeddingModel,
        question, citations, candidateK, topK,
    )
    if err != nil {
        return nil, err
    }

    // 6. 无结果直接报错 (L188-190)
    if len(citations) == 0 {
        return nil, fmt.Errorf("未检索到足够相关的视频片段")
    }

    // 7. 加载最近对话历史 (L192-196)
    recent, err := s.loadRecentMessages(ctx, userID, sessionID, recentLimit)
    if err != nil {
        return nil, err
    }

    // 8. 构建 LLM 消息序列 (L197)
    messages := buildRAGMessages(citations, recent, question)

    return &preparedRAGChat{
        Session:   session,
        Citations: citations,
        Messages:  messages,
    }, nil
}
```

**流程图**:

```
输入: question, userID, sessionID
         │
         ▼
    ┌─────────────┐
    │ 1. 长度校验  │ ──> 超过1000字符 → 返回错误
    └─────────────┘
         │
         ▼
    ┌─────────────┐
    │ 2. 权限校验  │ ──> session 不存在或无权限 → 返回错误
    └─────────────┘
         │
         ▼
    ┌─────────────┐
    │ 3. 问题向量化 │ ──> embedding.Embed() → queryVector
    └─────────────┘
         │
         ▼
    ┌─────────────┐
    │ 4. 向量检索  │ ──> retriever.Search() → vectorChunks
    └─────────────┘
         │
         ▼
    ┌───────────────────┐
    │ 5. 关键词检索+RRF  │ ──> mergeKeywordChunks() → citations
    └───────────────────┘
         │
         ▼
    ┌─────────────┐
    │ 6. 空结果检查 │ ──> len(citations)==0 → 返回错误
    └─────────────┘
         │
         ▼
    ┌─────────────┐
    │ 7. 加载历史  │ ──> loadRecentMessages() → recent
    └─────────────┘
         │
         ▼
    ┌─────────────┐
    │ 8. 构建消息  │ ──> buildRAGMessages() → messages
    └─────────────┘
         │
         ▼
输出: preparedRAGChat
```

---

### 3.4 AskStream - 流式问答

**文件**: `internal/service/chat.go:285-330`

```go
func (s *ChatService) AskStream(
    ctx context.Context,
    req AskRequest,
    emit func(ChatStreamEvent) error,
) (*AskResult, error) {

    // 1. 准备 RAG 数据
    prepared, err := s.prepareRAGChat(ctx, req.UserID, req.SessionID,
        req.Question, topK, embedding, profile)
    if err != nil {
        return nil, err
    }

    // 2. 先发 citations 事件
    emit(ChatStreamEvent{Type: "citations", Data: prepared.Citations})

    // 3. 调用 LLM (双路径)
    var answer string
    if streaming, ok := chat.(ai.StreamingChatClient); ok {
        // 真正的流式输出
        err = streaming.StreamChat(ctx, prepared.Messages, func(delta string) error {
            answer += delta
            return emit(ChatStreamEvent{Type: "answer", Data: delta})
        })
    } else {
        // 模拟流式输出
        answer, err = chat.Chat(ctx, prepared.Messages)
        if err == nil {
            for _, chunk := range splitAnswerForStream(answer, 80) {
                emit(ChatStreamEvent{Type: "answer", Data: chunk})
            }
        }
    }

    // 4. 保存对话记录
    s.memory.Append(ctx, req.UserID, req.SessionID, ChatMessage{
        Role:    "user",
        Content: req.Question,
    })
    s.memory.Append(ctx, req.UserID, req.SessionID, ChatMessage{
        Role:    "assistant",
        Content: answer,
    })

    // 5. 发送 done 事件
    emit(ChatStreamEvent{
        Type: "done",
        Data: map[string]interface{}{
            "answer":      answer,
            "citations":   prepared.Citations,
            "token_usage": tokenUsage,
        },
    })

    return &AskResult{Answer: answer}, nil
}
```

**流式事件类型**:

| 事件类型 | 时机 | 数据内容 |
|---------|------|---------|
| `citations` | 检索完成后 | 引用的文档片段列表 |
| `answer` | 生成过程中 | 答案的增量文本 |
| `done` | 生成完成后 | 完整答案 + 元数据 |

**双路径设计**:
- 优先使用 `StreamingChatClient` 接口，实现真正的逐 token 流式
- 降级方案: 先获取完整答案，再按 80 字符分片模拟流式
- 目的: 兼容不支持流式的 LLM provider

---

### 3.5 BuildTaskIndex - 索引构建

**文件**: `internal/service/rag_index.go:69-228`

```go
func (s *RAGIndexService) BuildTaskIndex(
    ctx context.Context,
    userID, taskID int64,
    embeddingModel string,
) error {

    // 1. 获取文档内容
    docs, err := s.repos.Document.FindByTaskID(taskID)

    // 2. 文本分块
    chunks := splitDocuments(docs, chunkSize, overlap)

    // 3. 生成 embeddings
    vectors, err := s.embedding.BatchEmbed(ctx, chunkContents)

    // 4. 双重维度校验
    if len(vectors) != len(chunks) {
        return fmt.Errorf("向量数量与分块数量不匹配")
    }
    for _, v := range vectors {
        if len(v) != expectedDim {
            return fmt.Errorf("向量维度不匹配")
        }
    }

    // 5. 先删旧向量
    s.repos.Chunk.DeleteByTaskID(taskID)
    s.milvus.DeleteByTaskID(taskID)

    // 6. 写 DB
    chunkIDs, err := s.repos.Chunk.BatchInsert(chunks)

    // 7. 写向量库
    err = s.milvus.BatchInsert(chunkIDs, vectors, metadata)

    return nil
}
```

**执行流程**:
1. 从数据库读取任务关联的所有文档
2. 将文档切分为固定大小的 chunk (带 overlap)
3. 调用 embedding API 生成向量
4. 校验向量数量和维度
5. 删除 Milvus 中的旧向量
6. 写入数据库 (chunk 表)
7. 写入 Milvus (向量库)

**关键设计**:
- 先删后写: 保证幂等性，可重复执行
- 双重校验: 防止数据损坏
- 分离存储: DB 存元数据，Milvus 存向量

---

## 4. 调用链分析

### 4.1 用户提问 → 获取答案

```
用户请求
    │
    ▼
AskStream()                    [chat.go:285]
    │
    ├─► prepareRAGChat()       [chat.go:144]
    │       │
    │       ├─► FindSessionForUser()     [repository]
    │       ├─► embedding.Embed()        [ai/embedding.go]
    │       ├─► retriever.Search()       [retrieval.go]
    │       ├─► mergeKeywordChunks()     [chat.go:xxx]
    │       │       │
    │       │       ├─► ExtractQueryTerms()  [retrieval_fusion.go:116]
    │       │       ├─► keywordSearch()      [retrieval.go]
    │       │       └─► FuseRetrievedChunks()[retrieval_fusion.go:17]
    │       │
    │       ├─► loadRecentMessages()     [chat_memory.go]
    │       └─► buildRAGMessages()       [chat.go:xxx]
    │
    ├─► emit("citations")
    ├─► streaming.StreamChat()  [ai/chat.go]
    │       │
    │       └─► emit("answer") × N
    │
    ├─► memory.Append() × 2    [chat_memory.go]
    └─► emit("done")
```

### 4.2 文档索引构建

```
BuildTaskIndex()               [rag_index.go:69]
    │
    ├─► FindByTaskID()         [repository/document.go]
    ├─► splitDocuments()       [rag_index.go:xxx]
    ├─► embedding.BatchEmbed() [ai/embedding.go]
    ├─► DeleteByTaskID()       [repository/chunk.go]
    ├─► DeleteByTaskID()       [milvus.go]
    ├─► BatchInsert()          [repository/chunk.go]
    └─► BatchInsert()          [milvus.go]
```

### 4.3 对话历史管理

```
ChatMemoryStore.Append()       [chat_memory.go]
    │
    ├─► Redis SET (7天TTL)
    └─► DB INSERT

ChatMemoryStore.Recent()       [chat_memory.go]
    │
    ├─► Redis LRANGE
    │       │
    │       └─► miss → DB SELECT
    │
    └─► 返回最近 N 条消息
```

---

## 5. 设计决策分析

### 5.1 为什么使用 RRF 而非加权求和？

**问题**: 如何融合向量检索和关键词检索的结果？

**方案对比**:

| 方案 | 优点 | 缺点 |
|------|------|------|
| 加权求和 | 简单直观 | 需要归一化分数，权重难调 |
| RRF | 无需归一化，参数少 | 只用排名，丢弃分数信息 |
| 学习排序 | 效果最好 | 需要标注数据，复杂度高 |

**VidLens 选择 RRF**:
- 简单: 只需一个参数 `k=60`
- 鲁棒: 不依赖分数归一化
- 有效: 在多个 benchmark 上表现优异

---

### 5.2 为什么用 n-gram 而非 jieba 分词？

**问题**: 如何对中文查询进行分词用于关键词检索？

**方案对比**:

| 方案 | 优点 | 缺点 |
|------|------|------|
| jieba 分词 | 准确，支持词性标注 | 需加载词典，内存占用大 |
| n-gram | 简单高效，无需词典 | 可能产生无意义片段 |
| 字符级 | 最细粒度 | 索引膨胀严重 |

**VidLens 选择 n-gram**:
- 轻量: 无需加载词典，适合容器化部署
- 快速: O(n) 复杂度
- 覆盖: 2-4 gram 覆盖大部分中文词汇

---

### 5.3 为什么先删旧向量再写新向量？

**问题**: 更新索引时如何保证数据一致性？

**方案对比**:

| 方案 | 优点 | 缺点 |
|------|------|------|
| 先删后写 | 简单，幂等 | 短暂不可用 |
| 双写切换 | 无 downtime | 复杂，需要版本管理 |
| 增量更新 | 高效 | 难以处理删除 |

**VidLens 选择先删后写**:
- 简单: 逻辑清晰，易于理解和维护
- 幂等: 重复执行结果一致
- 可接受: 索引更新是低频操作，短暂不可用影响小

---

### 5.4 为什么采用 Redis + DB 两级缓存？

**问题**: 如何高效存储和读取对话历史？

**方案对比**:

| 方案 | 优点 | 缺点 |
|------|------|------|
| 只用 Redis | 快速 | 持久化依赖 RDB/AOF |
| 只用 DB | 可靠 | 读取慢，高并发压力大 |
| Redis + DB | 快速 + 可靠 | 一致性复杂 |

**VidLens 选择两级缓存**:
- 性能: 热数据在 Redis，毫秒级读取
- 可靠: DB 作为兜底，Redis 重启不丢数据
- 成本: 7 天 TTL 自动清理，控制内存占用

---

### 5.5 为什么区分真正的 streaming 和模拟 streaming？

**问题**: 如何实现流式输出？

**方案对比**:

| 方案 | 优点 | 缺点 |
|------|------|------|
| 真正 streaming | 低延迟，用户体验好 | 依赖 LLM provider 支持 |
| 模拟 streaming | 兼容性好 | 首次响应延迟高 |
| 统一接口 | 代码简洁 | 可能无法充分利用特性 |

**VidLens 选择双路径**:
- 优先: 使用真正 streaming，最佳体验
- 降级: 不支持时自动切换模拟 streaming
- 兼容: 统一接口，调用方无感知

---

## 6. 扩展点

### 6.1 支持多模态 RAG

**需要修改的组件**:

1. `RetrievedChunk`: 增加 `MediaType` 字段
2. `RAGRetriever`: 支持图片/音频向量检索
3. `buildRAGMessages`: 构造多模态 prompt
4. `EmbeddingClient`: 支持图片/音频 embedding

### 6.2 支持动态 RRF 权重

**当前实现**: 向量检索和关键词检索权重相等

**扩展方案**:
```go
type RRFConfig struct {
    K              float64
    VectorWeight   float64  // 新增: 向量检索权重
    KeywordWeight  float64  // 新增: 关键词检索权重
}
```

### 6.3 支持增量索引

**当前实现**: 全量重建索引

**扩展方案**:
- 检测文档变更 (新增/修改/删除)
- 只更新变更部分
- 使用版本号管理索引

---

## 7. 性能优化建议

### 7.1 Embedding 批量化

**当前**: 逐条调用 embedding API

**优化**: 使用 `BatchEmbed` 接口，减少网络开销

### 7.2 向量检索缓存

**优化**: 对热门查询缓存检索结果，TTL 5 分钟

### 7.3 异步写入对话历史

**当前**: 同步写入 Redis + DB

**优化**: 使用消息队列异步写入，减少响应延迟

---

## 8. 总结

VidLens RAG Pipeline 的核心设计特点:

| 特点 | 实现 |
|------|------|
| 混合检索 | 向量 + 关键词，RRF 融合 |
| 流式输出 | 双路径，优先真正 streaming |
| 两级缓存 | Redis + DB，7 天 TTL |
| 幂等索引 | 先删后写，双重校验 |
| 接口抽象 | 依赖注入，便于测试和扩展 |

整体架构清晰，职责分离，具备良好的可维护性和可扩展性。
