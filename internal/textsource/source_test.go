package textsource

import (
	"context"
	"strings"
	"testing"
)

func options() ParseOptions {
	return ParseOptions{Identity: Identity{Platform: "bilibili", BVID: "BVexample", AID: 1, CID: 12, PartIndex: 2, MediaFingerprint: "media-a"}, TrackKey: "zh-Hans", Language: "zh-Hans", SubtitleKind: "automatic", KindBasis: "provider:ai_status=1"}
}

func parse(t *testing.T, raw string, o ParseOptions) Snapshot {
	t.Helper()
	s, err := ParseSRT(context.Background(), []byte(raw), o)
	if err != nil {
		t.Fatalf("parse: %v; warnings=%v", err, s.Warnings)
	}
	return s
}

const fixture = "1\n00:00:01,000 --> 00:00:03,250\n你好世界\n\n2\n00:00:02,000 --> 00:00:04,000\n另一位说话者\n"

func TestSRTMultilineAndLimitedCleaning(t *testing.T) {
	s := parse(t, "\ufeff1\r\n00:00:01,003 --> 00:00:02,250\r\n<b>鱼 &amp; 鸟</b>\r\n<i>多行</i><br/>代码 &lt;b&gt;\r\n", options())
	if s.CanonicalText != "鱼 & 鸟\n多行\n代码 <b>" {
		t.Fatalf("text=%q", s.CanonicalText)
	}
	if *s.Cues[0].StartMS != 1003 || *s.Cues[0].EndMS != 2250 || s.Cues[0].TimingMethod != TimingSubtitle {
		t.Fatalf("timing=%+v", s.Cues[0])
	}
	if !strings.Contains(s.Cues[0].RawText, "&amp;") || len(s.RawHash) != 64 {
		t.Fatalf("raw text lost: %+v", s)
	}
	if s.Quality != QualityUsable {
		t.Fatalf("quality=%s", s.Quality)
	}
}

func TestDigestIdentityTimingAndRetry(t *testing.T) {
	s := parse(t, fixture, options())
	retry := s
	retry.ID, retry.RawObjectKey, retry.FetchedAt, retry.RawHash = "new-id", "new/object", "later", "new-raw-hash"
	retry.Warnings = []string{"temporary diagnostic"}
	digest, err := Digest(retry)
	if err != nil || digest != s.SourceDigest {
		t.Fatalf("retry changed digest: %s %v", digest, err)
	}
	lf := parse(t, "\ufeff"+strings.ReplaceAll(fixture, "\n", "\r\n"), options())
	if lf.SourceDigest != s.SourceDigest || lf.RawHash == s.RawHash {
		t.Fatal("transport line endings changed source digest or raw hash failed to identify bytes")
	}
	mutations := []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{"part", func(x *Snapshot) { x.Identity.PartIndex++ }},
		{"cid", func(x *Snapshot) { x.Identity.CID++ }},
		{"fingerprint", func(x *Snapshot) { x.Identity.MediaFingerprint = "media-b" }},
		{"language", func(x *Snapshot) { x.Language = "en" }},
		{"track", func(x *Snapshot) { x.TrackKey = "other-track" }},
		{"kind_evidence", func(x *Snapshot) { x.KindBasis = "changed evidence" }},
		{"parser", func(x *Snapshot) { x.ParserVersion = "srt-v2" }},
		{"time", func(x *Snapshot) { x.Cues[0].StartMS = intTime(1100); x.Cues[0].RawRefs[0].StartMS = intTime(1100) }},
		{"raw_mapping", func(x *Snapshot) { x.Cues[0].RawRefs[0].RawText = "<b>你好世界</b>" }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			x, err := Canonicalize(s, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&x)
			d, err := Digest(x)
			if err != nil {
				t.Fatal(err)
			}
			if d == s.SourceDigest {
				t.Fatal("changed source produced same digest")
			}
			if x.CanonicalHash != s.CanonicalHash {
				t.Fatal("metadata changed text hash")
			}
		})
	}
}

func intTime(n int64) *int64 { return &n }

func TestOverlapsRepeatsAndRollingMapping(t *testing.T) {
	o := options()
	o.Limits.MergeRollingCaptions = true
	// These overlap, but differ in onset: they are retained as independent
	// observations rather than de-duplicated by matching text.
	s := parse(t, "1\n00:00:01,000 --> 00:00:03,000\n再说一次\n\n2\n00:00:02,000 --> 00:00:04,000\n再说一次\n\n3\n00:00:06,000 --> 00:00:07,000\n再说一次\n", o)
	if len(s.Cues) != 3 || strings.Count(s.CanonicalText, "再说一次") != 3 {
		t.Fatalf("legitimate repeated speech lost: %+v", s)
	}
	rolling := "1\n00:00:01,000 --> 00:00:02,000\n滚动\n\n2\n00:00:01,000 --> 00:00:03,000\n滚动字幕\n\n3\n00:00:01,000 --> 00:00:04,000\n滚动字幕增长\n"
	r := parse(t, rolling, o)
	if len(r.Cues) != 1 || len(r.Cues[0].RawRefs) != 3 || r.CanonicalText != "滚动字幕增长" || *r.Cues[0].EndMS != 4000 {
		t.Fatalf("rolling mapping=%+v", r)
	}
	if *r.Cues[0].RawRefs[0].EndMS != 2000 || r.Cues[0].RawRefs[1].RawText != "滚动字幕" {
		t.Fatal("original mapping changed")
	}
	o.Limits.MergeRollingCaptions = false
	if len(parse(t, rolling, o).Cues) != 3 {
		t.Fatal("default unexpectedly merges captions")
	}
}

func TestParserRejectsDamagedAndNonSubtitleInput(t *testing.T) {
	bad := []string{
		"", "<i><d p=\"1.0,1,25\">弹幕不是字幕</d></i>",
		"1\n-00:00:01,000 --> 00:00:02,000\n负时间\n",
		"1\n00:61:01,000 --> 00:00:02,000\n坏分\n",
		"1\n00:00:02,000 --> 00:00:02,000\n零长度\n",
		"1\n00:00:03,000 --> 00:00:02,000\n反向\n",
		"1\n00:00:01,000 --> 00:00:02,000\n<b></b>\n",
		fixture + "\n3\n损坏时间\n不能静默删除坏行\n",
		"1\n00:00:01,000 --> 00:00:02,000\n没有分隔\n2\n00:00:02,000 --> 00:00:03,000\n下一条\n",
		string([]byte{0xff, 0xfe}), fixture + "\x00",
	}
	for _, raw := range bad {
		s, err := ParseSRT(context.Background(), []byte(raw), options())
		if err == nil || s.Quality != QualityUnusable || len(s.Warnings) == 0 {
			t.Fatalf("accepted damaged source %q: %+v %v", raw, s, err)
		}
	}
}

func TestDurationLanguageTruncationAndResourceLimits(t *testing.T) {
	tests := []struct {
		name   string
		change func(*ParseOptions)
	}{
		{"bytes", func(o *ParseOptions) { o.Limits.MaxBytes = 10 }},
		{"cues", func(o *ParseOptions) { o.Limits.MaxCues = 1 }},
		{"cue_text", func(o *ParseOptions) { o.Limits.MaxCueRunes = 2 }},
		{"all_text", func(o *ParseOptions) { o.Limits.MaxTextRunes = 5 }},
		{"out_of_range", func(o *ParseOptions) { o.DurationMS = intTime(1000); o.Limits.DurationToleranceMS = 500 }},
		{"language_mismatch", func(o *ParseOptions) { o.ExpectedLanguage = "en" }},
		{"truncated", func(o *ParseOptions) { no := false; o.TrackComplete = &no }},
		{"known_kind_without_evidence", func(o *ParseOptions) { o.KindBasis = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := options()
			test.change(&o)
			if _, err := ParseSRT(context.Background(), []byte(fixture), o); err == nil {
				t.Fatal("bad source accepted")
			}
		})
	}
	o := options()
	o.DurationMS = intTime(3500)
	o.ExpectedLanguage = "zh-CN"
	s := parse(t, fixture, o)
	if len(s.Warnings) != 1 || *s.Cues[1].EndMS != 4000 {
		t.Fatalf("slight platform error was not retained: %+v", s)
	}
	o = options()
	o.Language = ""
	o.ExpectedLanguage = "en"
	if len(parse(t, fixture, o).Warnings) == 0 {
		t.Fatal("unknown language lacks diagnostic")
	}
	// A late first subtitle with long silence is still usable.
	o = options()
	o.DurationMS = intTime(3600000)
	parse(t, "1\n00:50:00,000 --> 00:50:02,000\n安静之后的一句话\n", o)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ParseSRT(ctx, []byte(fixture), options()); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestASRUnknownTimesRemainNullAndSnapshotIndependent(t *testing.T) {
	s, err := Canonicalize(Snapshot{Kind: KindLegacy, Identity: Identity{Platform: "local"}, ParserVersion: "legacy-v1", Cues: []Cue{{RawText: "旧全文", Text: "旧全文"}}}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Cues[0].StartMS != nil || s.Cues[0].EndMS != nil || s.Cues[0].TimingMethod != TimingUnknown {
		t.Fatalf("fabricated time: %+v", s.Cues[0])
	}
	broken := s
	broken.Cues = append([]Cue(nil), s.Cues...)
	broken.Cues[0].TimingMethod = TimingSubtitle
	if Validate(broken, Limits{}) == nil {
		t.Fatal("untimed source falsely claims subtitle timing")
	}
	original := parse(t, fixture, options())
	copy, err := Canonicalize(original, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	*copy.Cues[0].StartMS = 1234
	*copy.Cues[0].RawRefs[0].StartMS = 1234
	if *original.Cues[0].StartMS != 1000 || *original.Cues[0].RawRefs[0].StartMS != 1000 {
		t.Fatal("snapshot aliases original timings")
	}
}

func TestASRExactObservationMappingJoinAndNativeProvenance(t *testing.T) {
	first, second, space := 0, 3, " "
	end := 5
	original := Snapshot{Kind: KindASR, Identity: Identity{Platform: "local"}, ParserVersion: "test", Cues: []Cue{
		{ID: "a", RawText: "甲乙", Text: "甲乙", StartMS: intTime(1000), EndMS: intTime(2000), TimingMethod: "asr_native", RawRefs: []RawCueRef{{ID: "a", Order: 1, ObservationID: "window", ObservationOrder: 1, RawText: "甲乙 丙丁", StartMS: intTime(0), EndMS: intTime(10000), TimingMethod: "asr_window", TextStart: &first, TextEnd: &second, NativeTimings: []NativeTiming{{SegmentIndex: 0, TextStart: 0, TextEnd: 2, StartMS: 1000, EndMS: 2000, Method: "asr_native"}}}}},
		{ID: "b", RawText: "丙丁", Text: "丙丁", StartMS: intTime(3000), EndMS: intTime(4000), TimingMethod: "asr_native", JoinBefore: &space, RawRefs: []RawCueRef{{ID: "b", Order: 2, ObservationID: "window", ObservationOrder: 1, StartMS: intTime(0), EndMS: intTime(10000), TimingMethod: "asr_window", TextStart: &second, TextEnd: &end}}},
	}}
	s, err := Canonicalize(original, Limits{})
	if err != nil || s.CanonicalText != "甲乙 丙丁" {
		t.Fatalf("join mapping lost: %+v %v", s, err)
	}
	*original.Cues[1].JoinBefore = ""
	original.Cues[0].RawRefs[0].NativeTimings[0].StartMS = 1200
	original.Cues[0].StartMS = intTime(1200)
	if *s.Cues[1].JoinBefore != " " || s.Cues[0].RawRefs[0].NativeTimings[0].StartMS != 1000 {
		t.Fatal("canonical snapshot aliases input")
	}
	changed, err := Canonicalize(original, Limits{})
	if err != nil || changed.SourceDigest == s.SourceDigest {
		t.Fatal("joins/native provenance omitted from digest")
	}
	bad := s
	bad.Cues = append([]Cue(nil), s.Cues...)
	bad.Cues[1].RawRefs = append([]RawCueRef(nil), s.Cues[1].RawRefs...)
	v := 1
	bad.Cues[1].RawRefs[0].TextStart = &v
	if Validate(bad, Limits{}) == nil {
		t.Fatal("false original offsets accepted")
	}
	if _, err := Canonicalize(s, Limits{MaxCues: 1}); err == nil {
		t.Fatal("observation limits ignored")
	}
}

func TestSubtitleProviderLanguageAliasesValidateWithoutRewritingProvenance(t *testing.T) {
	for _, test := range []struct {
		actual, wanted string
		valid          bool
	}{
		{"ai-zh", "zh-CN", true}, {"auto-zh-Hans", "zh-CN", true}, {"ai-en", "en-US", true},
		{"ai-en", "zh-CN", false}, {"ai-unknown", "zh-CN", false}, {"auto-und", "zh-CN", false}, {"ai-zh", "", true},
	} {
		o := options()
		o.Language = test.actual
		o.TrackKey = test.actual + ":1"
		o.ExpectedLanguage = test.wanted
		o.SubtitleKind = "unknown"
		o.KindBasis = "provider has no confirmed type"
		s, err := ParseSRT(context.Background(), []byte("1\n00:00:01,000 --> 00:00:02,000\n真实字幕内容。\n"), o)
		if (err == nil) != test.valid {
			t.Fatalf("%s against %s: %v", test.actual, test.wanted, err)
		}
		if test.valid && (s.Language != test.actual || s.TrackKey != o.TrackKey || s.SubtitleKind != "unknown" || s.Quality != QualityUsable) {
			t.Fatal("language matching rewrote declared provenance")
		}
	}
}
