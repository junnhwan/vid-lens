package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"vid-lens/internal/eval"
	"vid-lens/internal/model"
)

// Spec 05 验收 = 仅分类层评测（决策记录 §4）。固化 case 集 = spec 01 共享 query 池
// （category = 黄金 intent），跨规则层/LLM 层跑准确率/短路率/LLM 兜底命中率。
// test-sealed 不进分类评测（spec line 105：sealed 框架不进）。
//
// 运行命令（spec line 110）：
//   go test ./internal/service/ -run TestIntentClassifierEvalOnDataset -v
//
// LLM 兜底用 oracle chat client（返回黄金 intent + 0.9 置信度）代表"LLM 兜底
// 若可用能命中什么"——真实 LLM 效果需线上对比测，本评测诚实标注"oracle 上界"
// 而非真实 LLM 数字（决策记录 §4：不照搬 wali，不夸大 LLM 兜底）。

func TestIntentClassifierEvalOnDataset(t *testing.T) {
	cases := loadClassifierEvalCases(t)
	if len(cases) == 0 {
		t.Skip("no dataset cases found at docs/eval/dataset (run from repo root)")
	}

	router := NewIntentRouter(NewRuleIntentClassifier())

	var ruleCorrect, shortCircuited, llmFallbackHit, llmFallbackAttempts int
	var ruleShortCorrect, nonShortLLMCorrect int
	var perCategory = map[string]struct{ total, correct int }{}

	videoSession := &model.ChatSession{ScopeType: model.ChatScopeVideo, TaskID: 1}
	// KB session 用一个真实 KB id（影响 topic_compare/series_locate 提权）。
	kbSession := &model.ChatSession{ScopeType: model.ChatScopeKnowledgeBase, KnowledgeBaseID: 1}

	for _, c := range cases {
		golden := Intent(c.Category)
		session := videoSession
		// spec 01 dataset 的 topic_compare case（如 dev-mem-compare）应在 KB scope 下
		// 分类（跨视频对比是 KB 主用例）。用 category 推 session：compare/series → KB。
		if golden == IntentTopicCompare || golden == IntentSeriesLocate {
			session = kbSession
		}

		// 规则层判定
		ruleIntent, ruleConf := router.rule.Classify(c.Question, session, ChatModeVideoAssistant, nil)
		ruleOK := ruleIntent == golden
		shorted := ruleConf >= shortCircuitThreshold
		if ruleOK {
			ruleCorrect++
		}
		if shorted {
			shortCircuited++
			if ruleOK {
				ruleShortCorrect++
			}
		}

		// LLM 兜底（oracle：返回黄金 intent + 0.9）——只在规则层未短路时跑。
		var finalIntent Intent
		if shorted {
			finalIntent = ruleIntent
		} else {
			llmFallbackAttempts++
			oracle := &scriptedChatClient{responses: []string{
				fmt.Sprintf(`{"intent":"%s","confidence":0.9}`, golden),
			}}
			finalIntent = router.Classify(context.Background(), c.Question, session, ChatModeVideoAssistant, nil, oracle)
			if finalIntent == golden {
				nonShortLLMCorrect++
			}
			// LLM 兜底命中 = 未短路 + LLM 修正成功（决策记录 §4 验收定义）。
			if !ruleOK && finalIntent == golden {
				llmFallbackHit++
			}
		}

		// 总体准确率（规则短路 + LLM 兜底）
		_ = finalIntent
		cat := c.Category
		e := perCategory[cat]
		e.total++
		if finalIntent == golden {
			e.correct++
		}
		perCategory[cat] = e
	}

	n := len(cases)
	// 总体准确率 = 短路且规则正确 + 未短路且 LLM 正确（互斥，不重复计）。
	overallCorrect := ruleShortCorrect + nonShortLLMCorrect
	ruleAcc := pct(ruleCorrect, n)
	overallAcc := pct(overallCorrect, n)
	shortRate := pct(shortCircuited, n)
	llmFallbackHitRate := pct(llmFallbackHit, n)
	savedLLM := shortRate // 省 LLM 调用 = 短路率派生（决策记录 §4）

	t.Logf("=== Spec 05 分类层评测 (固化 case 集 %d 条, train+dev, sealed 不进) ===", n)
	t.Logf("规则层准确率:          %s (%d/%d)", ruleAcc, ruleCorrect, n)
	t.Logf("规则层短路率:          %s (%d/%d)", shortRate, shortCircuited, n)
	t.Logf("LLM 兜底命中率:        %s (%d/%d 未短路)", llmFallbackHitRate, llmFallbackHit, llmFallbackAttempts)
	t.Logf("总体准确率(规则+LLM):  %s (%d/%d)", overallAcc, overallCorrect, n)
	t.Logf("省 LLM 调用(短路派生): %s", savedLLM)
	t.Logf("固化 case 集规模:      %d 条", n)
	t.Logf("HONEST: LLM 兜底用 oracle 上界(返回黄金 intent), 非真实 LLM 数字; 真实 LLM 效果需线上对比测。")
	for cat, e := range perCategory {
		t.Logf("  [%s] %s (%d/%d)", cat, pct(e.correct, e.total), e.correct, e.total)
	}

	// 不在测试里断言具体准确率阈值——数字回填 spec 占位符（spec line 80），
	// 此处只保证评测能跑出真实数字。断言"评测跑出非空数字"防止退化。
	if n == 0 || overallCorrect == 0 {
		t.Fatalf("eval produced zero correct classifications (n=%d correct=%d)", n, overallCorrect)
	}
}

func pct(part, total int) string {
	if total == 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(part)/float64(total)*100)
}

// loadClassifierEvalCases 加载 spec 01 train+dev split 的 case（category = 黄金
// intent）。test-sealed 不进分类评测（sealed 框架不进，spec line 105）。
func loadClassifierEvalCases(t *testing.T) []eval.Case {
	t.Helper()
	// 从仓库根的 docs/eval/dataset/ 加载。测试可能从 internal/service 或根跑，
	// 往上找 docs/eval/dataset/manifest.yaml 锚点定位根。
	roots := []string{
		".", "..", "../..", "../../..", "../../../..",
	}
	var root string
	for _, r := range roots {
		if _, err := os.Stat(filepath.Join(r, "docs/eval/dataset/manifest.yaml")); err == nil {
			root = r
			break
		}
	}
	if root == "" {
		t.Logf("could not locate docs/eval/dataset/manifest.yaml from working dir; skipping eval")
		return nil
	}
	manifestRaw, err := os.ReadFile(filepath.Join(root, "docs/eval/dataset/manifest.yaml"))
	if err != nil {
		t.Logf("read manifest: %v", err)
		return nil
	}
	var all []eval.Case
	for _, split := range []eval.Split{eval.SplitTrain, eval.SplitDev} {
		splitRaw, err := os.ReadFile(filepath.Join(root, "docs/eval/dataset", string(split)+".yaml"))
		if err != nil {
			t.Logf("skip split %s: %v", split, err)
			continue
		}
		ds, err := eval.LoadSplitDataset(manifestRaw, splitRaw, eval.SplitLoadOptions{
			Split: split, ExpectedVersion: "real-v1",
		})
		if err != nil {
			t.Logf("load split %s: %v", split, err)
			continue
		}
		all = append(all, ds.Cases...)
	}
	return all
}
