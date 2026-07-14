package service

import "testing"

func TestResolveChatSessionTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                  string
		explicit, video, file string
		want                  string
	}{
		{"explicit wins", "自定义", "视频标题", "a.mp4", "自定义"},
		{"video title over filename", "", "LLM视频名", "raw.mp4", "LLM视频名"},
		{"filename fallback", "", "", "lecture.mp4", "lecture.mp4"},
		{"empty → 新对话", "", "", "", "新对话"},
		{"trim quotes", "  \"主题\" ", "", "", "主题"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveChatSessionTitle(tc.explicit, tc.video, tc.file)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestIsPlaceholderChatSessionTitle(t *testing.T) {
	t.Parallel()
	if !IsPlaceholderChatSessionTitle("", "v", "f.mp4") {
		t.Fatal("empty should be placeholder")
	}
	if !IsPlaceholderChatSessionTitle("v", "v", "f.mp4") {
		t.Fatal("matching video title is placeholder")
	}
	if !IsPlaceholderChatSessionTitle("f.mp4", "v", "f.mp4") {
		t.Fatal("matching filename is placeholder")
	}
	if !IsPlaceholderChatSessionTitle("f", "v", "f.mp4") {
		t.Fatal("basename without ext is placeholder")
	}
	if IsPlaceholderChatSessionTitle("讲了什么", "v", "f.mp4") {
		t.Fatal("custom title should not be placeholder")
	}
}

func TestAutoTitleChatSessionFromQuestion(t *testing.T) {
	t.Parallel()

	// "请问这个视频讲了什么？后面还有补充"
	// → 去「请问」→「这个视频讲了什么？…」
	// → 去「这个视频」→「讲了什么？…」
	// → 截断问号 →「讲了什么」
	title, ok := AutoTitleChatSessionFromQuestion(
		"视频标题", "视频标题", "a.mp4",
		"请问这个视频讲了什么？后面还有补充",
	)
	if !ok {
		t.Fatal("expected auto title")
	}
	if title != "讲了什么" {
		t.Fatalf("got %q want 讲了什么", title)
	}

	_, ok = AutoTitleChatSessionFromQuestion("已定标题", "视频", "a.mp4", "随便问一句")
	if ok {
		t.Fatal("should not retitle custom session")
	}

	_, ok = AutoTitleChatSessionFromQuestion("视频标题", "视频标题", "a.mp4", "   ")
	if ok {
		t.Fatal("empty question should not title")
	}
}

func TestSanitizeChatSessionTitleMaxLen(t *testing.T) {
	t.Parallel()
	long := ""
	for i := 0; i < 50; i++ {
		long += "题"
	}
	got := sanitizeChatSessionTitle(long)
	if len([]rune(got)) != maxChatSessionTitleRunes {
		t.Fatalf("len=%d want %d", len([]rune(got)), maxChatSessionTitleRunes)
	}
}
