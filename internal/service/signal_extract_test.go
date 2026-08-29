package service

import (
	"reflect"
	"testing"
)

// Spec 05：Signal 提取只测提取结果对不对（spec line 118），不测正则实现细节。
// 时间戳/实体/句式标志的外部可观察行为。

func TestExtractSignalsTimestamps(t *testing.T) {
	cases := []struct {
		question string
		want     []TimestampRange
	}{
		{
			question: "第15分钟讲了什么",
			want:     []TimestampRange{{StartMS: 15 * 60 * 1000, EndMS: 15 * 60 * 1000, Raw: "第15分钟"}},
		},
		{
			question: "15:00 那段在讲什么",
			want:     []TimestampRange{{StartMS: 15 * 60 * 1000, EndMS: 15 * 60 * 1000, Raw: "15:00"}},
		},
		{
			question: "15分30秒处提到的方法",
			want:     []TimestampRange{{StartMS: 15*60*1000 + 30*1000, EndMS: 15*60*1000 + 30*1000, Raw: "15分30秒"}},
		},
		{
			question: "第15到20分钟讲了什么",
			want:     []TimestampRange{{StartMS: 15 * 60 * 1000, EndMS: 20 * 60 * 1000, Raw: "第15到20分钟"}},
		},
		{
			question: "15:00-20:00 的内容",
			want:     []TimestampRange{{StartMS: 15 * 60 * 1000, EndMS: 20 * 60 * 1000, Raw: "15:00-20:00"}},
		},
	}
	for _, c := range cases {
		got := ExtractSignals(c.question).Timestamps
		if len(got) != len(c.want) {
			t.Fatalf("ExtractSignals(%q).Timestamps = %+v, want %+v", c.question, got, c.want)
		}
		for i, r := range got {
			if r.StartMS != c.want[i].StartMS || r.EndMS != c.want[i].EndMS {
				t.Fatalf("ExtractSignals(%q)[%d] = %+v, want %+v", c.question, i, r, c.want[i])
			}
		}
	}
}

func TestExtractSignalsNoTimestamp(t *testing.T) {
	got := ExtractSignals("谁要校验 owner")
	if len(got.Timestamps) != 0 {
		t.Fatalf("non-time question should yield no timestamps, got %+v", got.Timestamps)
	}
}

func TestExtractSignalsSentenceFlags(t *testing.T) {
	cases := []struct {
		question    string
		compare     bool
		overview    bool
		smallTalk   bool
	}{
		{"这两个视频对比一下失败恢复", true, false, false},
		{"两个框架有什么区别", true, false, false},
		{"这个视频主要讲了什么", false, true, false},
		{"简单总结一下", false, true, false},
		{"你好，谢谢", false, false, true},
		{"谁要校验 owner", false, false, false},
	}
	for _, c := range cases {
		got := ExtractSignals(c.question)
		if got.HasCompare != c.compare {
			t.Fatalf("ExtractSignals(%q).HasCompare = %v, want %v", c.question, got.HasCompare, c.compare)
		}
		if got.HasOverview != c.overview {
			t.Fatalf("ExtractSignals(%q).HasOverview = %v, want %v", c.question, got.HasOverview, c.overview)
		}
		if got.HasSmallTalk != c.smallTalk {
			t.Fatalf("ExtractSignals(%q).HasSmallTalk = %v, want %v", c.question, got.HasSmallTalk, c.smallTalk)
		}
	}
}

func TestExtractSignalsEntities(t *testing.T) {
	got := ExtractSignals(`"分布式锁" 的 owner 校验和 Redis 的对比`)
	want := []string{"分布式锁", "Redis"}
	if !reflect.DeepEqual(got.Entities, want) {
		t.Fatalf("entities = %+v, want %+v", got.Entities, want)
	}
}

func TestExtractSignalsEmptyQuestion(t *testing.T) {
	got := ExtractSignals("")
	if len(got.Timestamps) != 0 || len(got.Entities) != 0 || got.HasCompare || got.HasOverview || got.HasSmallTalk {
		t.Fatalf("empty question should yield zero signals, got %+v", got)
	}
}

// TestExtractSignalsNoSideEffects 锁定"无副作用"契约（spec 05 line 90）：
// 同一问题多次提取结果一致，不依赖任何外部状态。
func TestExtractSignalsNoSideEffects(t *testing.T) {
	q := "第15分钟讲了 Redis"
	first := ExtractSignals(q)
	for range 5 {
		again := ExtractSignals(q)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("ExtractSignals not idempotent: first=%+v again=%+v", first, again)
		}
	}
}
