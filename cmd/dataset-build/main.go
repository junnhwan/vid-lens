// Command dataset-build constructs the VidLens strict RAG evaluation dataset
// (manifest + train/dev/test split files) from real annotated cases.
//
// Spec 01 (docs/specs/01-eval-foundation.md): "标注直接在 YAML 文件里做，
// 复用 schema 校验"——本工具把人工标注的 case 内容（question/answer_points/
// evidence_ranges.context_ids 均基于真实 video_chunks.vector_id 与转写原文）
// 序列化成 dataset-schema.yaml 要求的四个物理分离文件。
//
// case 内容的诚信约束（spec 01 + annotation-guide）：
//   - evidence_ranges.context_ids 必须是真实 video_chunks.vector_id（旧仓库
//     PG 的稳定 chunk identity，C-简化方案：直接用旧仓库 PG 当 SoT）。
//   - 不用字符位置估算时间戳（ASR 时间戳全 0，不可靠），evidence_ranges 全用
//     context_ids，不填 start_ms/end_ms。
//   - category 字段复用为 intent 标签（video_overview/direct_qa/topic_compare/
//     series_locate/timeline_locate/small_talk）。
//   - test split 由 SealSplit 自动 seal + token sha256。
//   - train/dev 的 content_sha256 由 ComputeSplitContentSHA256 手动算后填入 manifest。
//
// 运行：go run ./cmd/dataset-build -out docs/eval/dataset -token $SEALED_TEST_TOKEN
// token 明文不进仓库（仅本地生成 test split 时用）。
package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"vid-lens/internal/eval"
)

// taskFileMD5: 旧仓库 PG video_tasks.file_md5（eval strict 校验要求
// case.video_id == task.FileMD5）。task 11/12 与 33/34 同 md5（同源视频）。
var taskFileMD5 = map[string]string{
	"2":  "2cdbc4ef70592e6be97dab3704268fa7",
	"8":  "f74d4d5fb7722c3c85e83ededa28a06d",
	"9":  "b6c7ac15b0d03989dd9b604ca3998063",
	"10": "6e523b58fa96340f701cc9e38611854d",
	"11": "add9fde4b2ff06d25f02ee944eec0f50",
	"12": "add9fde4b2ff06d25f02ee944eec0f50",
	"13": "1d39e171ca34cd927cc76c5c35038554",
	"14": "3d7cb392e9e133d0c4e91ef90d0c3ada",
	"15": "003b413828444582cd381e918d6a6841",
	"16": "fd6405e92437e85542415657fab2fd35",
	"24": "108ff31e36cf173a239e7adb68e066e1",
	"25": "cc87b1fd646f1bbb1010530a136a11f0",
	"26": "70e1edf456c2ca896b93f15311e92ac3",
	"30": "013d02fcc36587fdaaa7c6a4d8d651e2",
	"33": "9e5d016c98443dbfe22bcaa0efa164db",
	"34": "9e5d016c98443dbfe22bcaa0efa164db",
}

func vid(taskID string) string { return taskFileMD5[taskID] }

func vids(ids ...string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, taskFileMD5[id])
	}
	return out
}

func main() {
	var (
		outDir   = flag.String("out", "docs/eval/dataset", "output directory for the four dataset files")
		token    = flag.String("token", "", "sealed test access token (plaintext; do NOT commit; leave empty to auto-generate a random one and print it once)")
	)
	flag.Parse()

	if *token == "" {
		*token = randomToken()
		log.Printf("No -token provided; generated a random sealed test token (print once, do not commit): %s", *token)
	}

	// source_group 划分（按内容来源，以 source_group 为最小单位划分 train/dev/test，
	// 同 source_group/video_id 不得跨 split——annotation guide §2）。
	// video_id 必须填 task.file_md5（strict 校验：case.video_id == task.FileMD5，
	// 见 cmd/rag-eval/artifact_snapshot.go line 81-84）。下方 taskFileMD5 从旧仓库
	// PG（localhost:5434）video_tasks.file_md5 查得，eval 时 case.video_id 与之对齐。
	const (
		sgSoftwareEng    = "software_eng_lecture" // task 2
		sgReactDynamic   = "react_dynamic"         // task 10
		sgCodingPractice = "coding_agent_practice" // task 14 (英文，中英混合 ASR 噪声)
		sgWSLAgent       = "wsl_agent_setup"       // task 24
		sgMemorySystems  = "memory_systems"        // task 8,9（跨视频同主题）
		sgAIPitfalls     = "ai_pitfalls"           // task 15
		sgAgentSeries    = "agent_series"           // task 11,12,13,16,25（跨视频 Agent 主题）
		sgCodingTools    = "ai_coding_tools"       // task 30,33,34（跨视频工具对比）
		sgStudentJourney = "student_journey"        // task 26（无答案 case 原料）
	)

	// taskFileMD5 / vid / vids 见包级定义。

	dataset := eval.Dataset{
		SchemaVersion: "1",
		DatasetVersion: "real-v1",
		Manifest: eval.SplitManifest{Splits: map[eval.Split]eval.SplitDefinition{
			eval.SplitTrain: {Sources: []eval.SourceGroup{
				{ID: sgSoftwareEng, VideoIDs: vids("2")},
				{ID: sgReactDynamic, VideoIDs: vids("10")},
				{ID: sgCodingPractice, VideoIDs: vids("14")},
				{ID: sgWSLAgent, VideoIDs: vids("24")},
			}},
			eval.SplitDev: {Sources: []eval.SourceGroup{
				{ID: sgMemorySystems, VideoIDs: vids("8", "9")},
				{ID: sgAIPitfalls, VideoIDs: vids("15")},
			}},
			eval.SplitTest: {Sources: []eval.SourceGroup{
				// task 12 与 11 同 md5、34 与 33 同 md5（同源物理视频），
				// strict manifest video_id 按 file_md5 去重，只保留 11/33。
				{ID: sgAgentSeries, VideoIDs: vids("11", "13", "16", "25")},
				{ID: sgCodingTools, VideoIDs: vids("30", "33")},
				{ID: sgStudentJourney, VideoIDs: vids("26")},
			}},
		}},
		Cases: buildCases(sgSoftwareEng, sgReactDynamic, sgCodingPractice, sgWSLAgent,
			sgMemorySystems, sgAIPitfalls, sgAgentSeries, sgCodingTools, sgStudentJourney),
	}

	// 先 seal test（自动算 test 的 content_sha256 + access_token_sha256）。
	if err := eval.SealSplit(&dataset, eval.SplitTest, *token); err != nil {
		log.Fatalf("seal test split: %v", err)
	}

	// 手动算 train/dev 的 content_sha256 填入 manifest（test 已由 SealSplit 填）。
	for _, split := range []eval.Split{eval.SplitTrain, eval.SplitDev} {
		hash, err := eval.ComputeSplitContentSHA256(dataset, split)
		if err != nil {
			log.Fatalf("compute %s content sha: %v", split, err)
		}
		def := dataset.Manifest.Splits[split]
		def.ContentSHA256 = hash
		dataset.Manifest.Splits[split] = def
	}

	// 最后算 manifest.sha256（覆盖所有 split 归属，不含 case 内容）。
	manifestHash, err := eval.ComputeManifestSHA256(dataset.DatasetVersion, dataset.Manifest.Splits)
	if err != nil {
		log.Fatalf("compute manifest sha: %v", err)
	}
	dataset.Manifest.SHA256 = manifestHash

	// 校验：dataset 自洽（case_id 唯一、evidence 唯一、source_group/video_id 归属一致）。
	if err := eval.ValidateDataset(dataset, eval.ValidationOptions{ExpectedVersion: "real-v1"}); err != nil {
		log.Fatalf("validate dataset: %v", err)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("mkdir out: %v", err)
	}

	// 写四个文件：manifest + train + dev + sealed test。
	manifestRaw, err := eval.MarshalDatasetManifestYAML(dataset)
	if err != nil {
		log.Fatalf("marshal manifest: %v", err)
	}
	if err := writeOnce(filepath.Join(*outDir, "manifest.yaml"), manifestRaw); err != nil {
		log.Fatalf("write manifest: %v", err)
	}

	for _, split := range []eval.Split{eval.SplitTrain, eval.SplitDev, eval.SplitTest} {
		raw, err := eval.MarshalSplitDatasetYAML(dataset, split)
		if err != nil {
			log.Fatalf("marshal %s split: %v", split, err)
		}
		name := string(split) + ".yaml"
		if split == eval.SplitTest {
			name = "test-sealed.yaml"
		}
		if err := writeOnce(filepath.Join(*outDir, name), raw); err != nil {
			log.Fatalf("write %s: %v", name, err)
		}
	}

	fmt.Printf("dataset built in %s:\n", *outDir)
	fmt.Printf("  manifest.yaml (sha256=%s)\n", dataset.Manifest.SHA256[:12])
	fmt.Printf("  train.yaml (%d cases)\n", countCases(dataset, eval.SplitTrain))
	fmt.Printf("  dev.yaml   (%d cases)\n", countCases(dataset, eval.SplitDev))
	fmt.Printf("  test-sealed.yaml (%d cases, sealed, token sha256=%s)\n",
		countCases(dataset, eval.SplitTest), dataset.Manifest.Splits[eval.SplitTest].AccessTokenSHA256[:12])
}

func countCases(d eval.Dataset, split eval.Split) int {
	n := 0
	for _, c := range d.Cases {
		if c.Split == split {
			n++
		}
	}
	return n
}

// writeOnce refuses to overwrite an existing file (防误覆盖已发布 dataset version）。
func writeOnce(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("file already exists: %s（删除旧文件再重跑，或升级 dataset version）", path)
	}
	return os.WriteFile(path, data, 0o644)
}

func randomToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("generate random token: %v", err)
	}
	return "vdl_sealed_" + hex.EncodeToString(b)
}

// buildCases 返回全部标注 case（基于真实 video_chunks 内容 + vector_id）。
//
// 诚信标注原则：
//   - question 像真实用户自然问句，不露"根据视频"字样。
//   - answer_point.text 是原文能支撑的简短要点。
//   - evidence_ranges.context_ids 指向答案所在的 chunk 的真实 vector_id。
//   - difficulty 按 annotation guide §3：easy=单直接片段、问句接近原文；medium=同义
//     理解/相邻证据；hard=多非相邻证据/易受干扰。
//   - category 复用为 intent 标签（spec 01 拍板）。
//   - 无答案 case（answerable:false）加 negative_confusers。
func buildCases(groups ...string) []eval.Case {
	_ = groups
	cases := make([]eval.Case, 0, 48)

	// ===== TRAIN =====
	// source_group=software_eng_lecture (task 2 软件构造，10 chunk 内容扎实不重复)

	// chunk 0：软件定义 = 现实需求在计算机世界的投影 + 代码是负债。
	cases = append(cases, eval.Case{
		CaseID: "train-sw-def", VideoID: vid("2"), SourceGroup: "software_eng_lecture", Split: eval.SplitTrain,
		TaskID: 2, TaskHint: "软件构造讲座",
		Question: "视频中把软件定义成什么？",
		Category: "direct_qa", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "软件是现实的需求在计算机世界中的投影", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_2_0eac538931db_0"},
		}},
	})

	// chunk 4：好的代码三特征 safe from bug / ready for change / easy to understand。
	cases = append(cases, eval.Case{
		CaseID: "train-good-code", VideoID: vid("2"), SourceGroup: "software_eng_lecture", Split: eval.SplitTrain,
		TaskID: 2, TaskHint: "软件构造讲座",
		Question: "好代码的几个特征是什么？",
		Category: "direct_qa", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "safe from bug（没有 bug）", Required: true},
			{ID: "ap2", Text: "ready for change（易于修改）", Required: true},
			{ID: "ap3", Text: "easy to understand（易于理解）", Required: false},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_2_08b66f954749_4"},
		}},
	})

	// chunk 6-7：错误放大效应 + 完美需求只在小众领域。
	cases = append(cases, eval.Case{
		CaseID: "train-error-amp", VideoID: vid("2"), SourceGroup: "software_eng_lecture", Split: eval.SplitTrain,
		TaskID: 2, TaskHint: "软件构造讲座",
		Question: "需求分析出错会带来什么后果？为什么不能从一开始就设计完美需求？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "错误放大效应：前面错了后面都会错", Required: true},
			{ID: "ap2", Text: "完美需求只在小众领域（如核电站）可能出现", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_2_9f01b0156ab7_6"},
		}, {
			ID: "ev2", GroupID: "g2", Source: eval.EvidenceSourceASR, Relevance: 2,
			ContextIDs: []string{"task_2_24c8c5861ea2_7"},
		}},
	})

	// overview：软件构造视频整体讲了什么（关检索走摘要直拼）。
	cases = append(cases, eval.Case{
		CaseID: "train-sw-overview", VideoID: vid("2"), SourceGroup: "software_eng_lecture", Split: eval.SplitTrain,
		TaskID: 2, TaskHint: "软件构造讲座",
		Question: "这个视频主要讲了什么？",
		Category: "video_overview", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "软件的定义、软件构造、好代码的特征、复杂度管理", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 2,
			ContextIDs: []string{"task_2_0eac538931db_0"},
		}},
	})

	// chunk 1：复杂度来源（业务复杂度 / 代码复杂度 / 人的复杂度）。
	cases = append(cases, eval.Case{
		CaseID: "train-complexity-source", VideoID: vid("2"), SourceGroup: "software_eng_lecture", Split: eval.SplitTrain,
		TaskID: 2, TaskHint: "软件构造讲座",
		Question: "软件的复杂度可以从哪几个角度分析？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "业务本身的复杂度", Required: true},
			{ID: "ap2", Text: "代码本身的复杂度", Required: true},
			{ID: "ap3", Text: "还有一层人的复杂度", Required: false},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_2_a6ddf3ea82cb_1"},
		}},
	})

	// chunk 2：代码复杂度分两种（修改的复杂度 = 读写放大 / 理解的复杂度）。
	cases = append(cases, eval.Case{
		CaseID: "train-code-complexity", VideoID: vid("2"), SourceGroup: "software_eng_lecture", Split: eval.SplitTrain,
		TaskID: 2, TaskHint: "软件构造讲座",
		Question: "代码本身的复杂度分成哪两种？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "修改的复杂度（读写放大，看多少改多少行）", Required: true},
			{ID: "ap2", Text: "理解的复杂度（抽象做得不好要看其他代码）", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_2_22be211ae4a7_2"},
		}},
	})

	// chunk 3：软件构造不只是写代码（设计/原型/测试/debug/code review）。
	cases = append(cases, eval.Case{
		CaseID: "train-construction-scope", VideoID: vid("2"), SourceGroup: "software_eng_lecture", Split: eval.SplitTrain,
		TaskID: 2, TaskHint: "软件构造讲座",
		Question: "软件构造包含哪些环节？是不是只写代码？",
		Category: "direct_qa", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "不只写代码，还含设计/原型/制造/debug/测试/code review", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_2_78676544587d_3"},
		}},
	})

	// source_group=react_dynamic (task 10 动态图 React，3 chunk)
	cases = append(cases, eval.Case{
		CaseID: "train-react-dag", VideoID: vid("10"), SourceGroup: "react_dynamic", Split: eval.SplitTrain,
			TaskID: 10, TaskHint: "动态图 React 讲解",
		Question: "动态图 React 相比串行 React 解决了什么问题？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "串行 React 一个节点慢后面全阻塞", Required: true},
			{ID: "ap2", Text: "动态图用 DAG 依赖图并行调度，提高执行速度", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_10_f01efe2c26d3_0"},
		}},
	})

	// source_group=coding_agent_practice (task 14 英文 Mario，coding agent 历史)
	cases = append(cases, eval.Case{
		CaseID: "train-coding-agent-history", VideoID: vid("14"), SourceGroup: "coding_agent_practice", Split: eval.SplitTrain,
			TaskID: 14, TaskHint: "coding agent 实践（英文）",
		Question: "Claude Code 之前的 coding agent 前身有哪些？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "GitHub Copilot、Aider、AutoGPT 是前身", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_14_418825b065a6_5"},
		}},
	})

	// source_group=wsl_agent_setup (task 24 WSL)
	cases = append(cases, eval.Case{
		CaseID: "train-wsl-why", VideoID: vid("24"), SourceGroup: "wsl_agent_setup", Split: eval.SplitTrain,
			TaskID: 24, TaskHint: "Windows WSL 跑 AI agent",
		Question: "为什么 AI agent 在 WSL 里比在 PowerShell 里表现更好？",
		Category: "direct_qa", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "大模型训练语料多为 Linux 风格命令，在 Linux 环境出错率更低", Required: true},
			{ID: "ap2", Text: "与生产环境（Linux 服务器）更一致", Required: false},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_24_2a3860e602dc_0"},
		}},
	})

	// ===== DEV =====
	// source_group=memory_systems (task 8 项目记忆系统 + task 9 Moon Zero，跨视频同主题)
	cases = append(cases, eval.Case{
		CaseID: "dev-mem-forms", VideoID: vid("8"), SourceGroup: "memory_systems", Split: eval.SplitDev,
			TaskID: 8, TaskHint: "项目记忆系统架构",
		Question: "项目记忆系统有哪几种记忆形态？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "短期对话窗口（滑动窗口）、结构化偏好、图增强层等", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_8_d143caa44ea9_0"},
		}},
	})

	cases = append(cases, eval.Case{
		CaseID: "dev-moonzero-pain", VideoID: vid("9"), SourceGroup: "memory_systems", Split: eval.SplitDev,
			TaskID: 9, TaskHint: "Moon Zero 记忆框架",
		Question: "Moon Zero 记忆框架要解决的核心痛点是什么？",
		Category: "direct_qa", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "上下文窗口长度有限，无法支撑长记忆", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_9_7fa022df038c_0"},
		}},
	})

	// 跨视频：topic_compare（memory_systems 内 task 8 vs task 9）
	cases = append(cases, eval.Case{
		CaseID: "dev-mem-compare", VideoID: vid("9"), SourceGroup: "memory_systems", Split: eval.SplitDev,
			TaskID: 9, TaskHint: "记忆系统对比（Moon Zero 侧）",
		Question: "项目自己的记忆系统和 Moon Zero 框架在落地方式上有什么区别？",
		Category: "topic_compare", Difficulty: "hard", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "项目没有直接接入 Moon Zero 中间件，而是利用它的思想", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_9_7fa022df038c_0"},
		}},
	})

	// chunk 0 续：把豆包当情绪伴侣越聊越焦虑（陷阱之一）。
	cases = append(cases, eval.Case{
		CaseID: "dev-pitfall-companion", VideoID: vid("15"), SourceGroup: "ai_pitfalls", Split: eval.SplitDev,
			TaskID: 15, TaskHint: "AI 使用十大陷阱",
		Question: "把豆包当情绪伴侣会怎么样？",
		Category: "direct_qa", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "越聊越焦虑", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_15_7511363ae2dd_0"},
		}},
	})

	// source_group=ai_pitfalls (task 15)
	cases = append(cases, eval.Case{
		CaseID: "dev-pitfall-iteration", VideoID: vid("15"), SourceGroup: "ai_pitfalls", Split: eval.SplitDev,
			TaskID: 15, TaskHint: "AI 使用十大陷阱",
		Question: "大模型的迭代节奏现在是什么状态？",
		Category: "direct_qa", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "进入月更主线、周更产品、分叉专用模型的阶段", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_15_7511363ae2dd_0"},
		}},
	})

	// overview（ai_pitfalls 整体）
	cases = append(cases, eval.Case{
		CaseID: "dev-pitfall-overview", VideoID: vid("15"), SourceGroup: "ai_pitfalls", Split: eval.SplitDev,
			TaskID: 15, TaskHint: "AI 使用十大陷阱",
		Question: "这期视频想讲什么？",
		Category: "video_overview", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "剖析 AI 使用中的十个大坑及应对策略", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 2,
			ContextIDs: []string{"task_15_7511363ae2dd_0"},
		}},
	})

	// ===== TEST =====
	// source_group=agent_series (task 11/12/13/16/25)
	// 注意 task 11/12 内容重复（同一视频重传），用 task 13（演变策略，内容不重复）。
	cases = append(cases, eval.Case{
		CaseID: "test-agent-loop", VideoID: vid("13"), SourceGroup: "agent_series", Split: eval.SplitTest,
			TaskID: 13, TaskHint: "Agent 开发演变",
		Question: "Loop Engineering 和之前的 Prompt Engineering 是什么关系？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "Prompt 没有死掉，成为 Loop 里面的一个组件", Required: true},
			{ID: "ap2", Text: "本质都是让模型以外的东西（Harness）让 Agent 运行更好", Required: false},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_13_613361c1f8e3_3"},
		}},
	})

	cases = append(cases, eval.Case{
		CaseID: "test-agent-moat", VideoID: vid("13"), SourceGroup: "agent_series", Split: eval.SplitTest,
			TaskID: 13, TaskHint: "Agent 开发演变",
		Question: "这一行（Agent 开发）的护城河是什么？",
		Category: "direct_qa", Difficulty: "hard", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "做 context creation（融入个人经验判断品味）而非 content（知识性内容）", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_13_05098e437ecc_8"},
		}},
	})

	cases = append(cases, eval.Case{
		CaseID: "test-agent-verify", VideoID: vid("16"), SourceGroup: "agent_series", Split: eval.SplitTest,
			TaskID: 16, TaskHint: "AI agent 自动化验证",
		Question: "做 AI agent 开发时，作者花在写代码上的时间占比大概是多少？剩下时间花在哪？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "写代码只占 10%，90% 花在手动测试", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_16_b7b60a47daaf_0"},
		}},
	})

	cases = append(cases, eval.Case{
		CaseID: "test-pyagent-uses", VideoID: vid("25"), SourceGroup: "agent_series", Split: eval.SplitTest,
			TaskID: 25, TaskHint: "智能体本质与 pyagent",
		Question: "pyagent 这个项目有哪几种用法？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "本身是 coding agent 可替代 Claude Code/Codex；可自行扩展功能", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_25_151c300431bc_0"},
		}},
	})

	// source_group=ai_coding_tools (task 30 Codex + task 33/34 Claude Code，跨视频对比)
	cases = append(cases, eval.Case{
		CaseID: "test-codex-what", VideoID: vid("30"), SourceGroup: "ai_coding_tools", Split: eval.SplitTest,
			TaskID: 30, TaskHint: "OpenAI Codex 使用指南",
		Question: "Codex 是什么定位的产品？",
		Category: "direct_qa", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "OpenAI 的核心产品，对标 Anthropic 的 Claude Code", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_30_82aca6cb5a6e_0"},
		}},
	})

	cases = append(cases, eval.Case{
		CaseID: "test-ccloud-src", VideoID: vid("33"), SourceGroup: "ai_coding_tools", Split: eval.SplitTest,
			TaskID: 33, TaskHint: "Claude Code 源码解析",
		Question: "作者在 Claude Code 源码里找到的 cloud.tsx 文件是用来做什么的？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "用文本方式画那个小吉祥物（cloud boot 的 cloud）", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_33_50f0eda78c53_0"},
		}},
	})

	// 跨视频：topic_compare（Codex vs Claude Code，ai_coding_tools 内）
	cases = append(cases, eval.Case{
		CaseID: "test-tools-compare", VideoID: vid("30"), SourceGroup: "ai_coding_tools", Split: eval.SplitTest,
			TaskID: 30, TaskHint: "Codex vs Claude Code 对比",
		Question: "Codex 和 Claude Code 这两个 coding agent 在定位上是什么关系？",
		Category: "topic_compare", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "Codex 是 OpenAI 核心产品，对标 Anthropic 的 Claude Code", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_30_82aca6cb5a6e_0"},
		}, {
			ID: "ev2", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 2,
			ContextIDs: []string{"task_33_50f0eda78c53_0"},
		}},
	})

	// chunk 1：Multi-Agent 在年底被认为可能是伪需求。
	cases = append(cases, eval.Case{
		CaseID: "test-multiagent-pseudo", VideoID: vid("13"), SourceGroup: "agent_series", Split: eval.SplitTest,
			TaskID: 13, TaskHint: "Agent 开发演变",
		Question: "Multi-Agent 在什么时候被认为可能是伪需求？",
		Category: "direct_qa", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "2025 年底开始有人说 Multi-Agent 可能是伪需求", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_13_2baf09fced17_1"},
		}},
	})

	// chunk 0：这期视频面向什么人、讲什么（overview 变体，但走 direct_qa 检索）
	cases = append(cases, eval.Case{
		CaseID: "test-agent-audience", VideoID: vid("13"), SourceGroup: "agent_series", Split: eval.SplitTest,
			TaskID: 13, TaskHint: "Agent 开发演变",
		Question: "这期 Agent 开发视频主要面向什么人？",
		Category: "direct_qa", Difficulty: "easy", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "面向找实习的或已经在干 Agent 开发的人", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_13_70b603685a03_0"},
		}},
	})

	// series_locate：Agent 系列里 OpenCloud 在哪期讲的（跨视频，agent_series 内）
	cases = append(cases, eval.Case{
		CaseID: "test-series-openccloud", VideoID: vid("11"), SourceGroup: "agent_series", Split: eval.SplitTest,
			TaskID: 11, TaskHint: "Agent 全栈知识系列",
		Question: "Agent 系列里 OpenCloud 爆火标志着什么？",
		Category: "series_locate", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "标志 Agent 从工程技术走向实际产品", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_11_dd898dfb5c8b_0"},
		}},
	})

	// source_group=student_journey (task 26) —— 无答案 case（问技术问题，但视频是保研探索，无答案）
	cases = append(cases, eval.Case{
		CaseID: "test-student-noanswer", VideoID: vid("26"), SourceGroup: "student_journey", Split: eval.SplitTest,
			TaskID: 26, TaskHint: "大二学生保研探索",
		Question: "分布式锁释放时为什么要校验 owner？",
		Category: "direct_qa", Difficulty: "hard", Answerable: false,
		NegativeConfusers: []eval.EvidenceRange{{
			ID: "nc1", GroupID: "g-nc", Source: eval.EvidenceSourceASR, Relevance: 1,
			ContextIDs: []string{"task_26_ba8c8de909e3_0"},
		}},
		Notes: "无答案 case：该视频是保研探索个人经历，不含分布式锁技术内容；negative_confusers 是开头片段（语义不相关，检验错误接受）。",
	})

	// timeline_locate case（spec 05 需要，但 ASR 时间戳全 0，用 context_ids 作稳定定位）
	cases = append(cases, eval.Case{
		CaseID: "test-codex-structure", VideoID: vid("30"), SourceGroup: "ai_coding_tools", Split: eval.SplitTest,
			TaskID: 30, TaskHint: "Codex 使用指南",
		Question: "Codex 视频分成哪几个部分来讲？",
		Category: "timeline_locate", Difficulty: "medium", Answerable: true,
		AnswerPoints: []eval.AnswerPoint{
			{ID: "ap1", Text: "基础篇、进阶篇、扩展篇三部分", Required: true},
		},
		EvidenceRanges: []eval.EvidenceRange{{
			ID: "ev1", GroupID: "g1", Source: eval.EvidenceSourceASR, Relevance: 3,
			ContextIDs: []string{"task_30_82aca6cb5a6e_0"},
		}},
		Notes: "timeline_locate 用 context_ids 作稳定定位（ASR 时间戳全 0，按 annotation guide §5 用 context_ids 而非估算时间戳）。",
	})

	// 第二个无答案 case（task 26 问 RAG 技术，视频无此内容）
	cases = append(cases, eval.Case{
		CaseID: "test-student-noanswer2", VideoID: vid("26"), SourceGroup: "student_journey", Split: eval.SplitTest,
			TaskID: 26, TaskHint: "大二学生保研探索",
		Question: "BM25 混合检索和纯向量检索在 Recall@5 上差多少？",
		Category: "direct_qa", Difficulty: "hard", Answerable: false,
		NegativeConfusers: []eval.EvidenceRange{{
			ID: "nc1", GroupID: "g-nc", Source: eval.EvidenceSourceASR, Relevance: 1,
			ContextIDs: []string{"task_26_ba8c8de909e3_0"},
		}},
		Notes: "无答案 case：保研探索视频不含 RAG 检索技术内容。",
	})

	// small_talk case（spec 05 需要，关检索关 LLM）
	cases = append(cases, eval.Case{
		CaseID: "test-smalltalk", VideoID: vid("30"), SourceGroup: "ai_coding_tools", Split: eval.SplitTest,
			TaskID: 30, TaskHint: "闲聊",
		Question: "谢谢你的讲解",
		Category: "small_talk", Difficulty: "easy", Answerable: false,
		Notes: "small_talk：闲聊不检索不生成，answerable:false（无需 evidence）。",
	})

	return cases
}
