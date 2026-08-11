package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Fixture mirrors the real markup Telegram serves on t.me/s/<channel>:
// nested divs inside the text body, inline <a>/<b>/<tg-emoji> tags, <br/>
// line breaks, HTML entities, and a trailing reply-markup div that must not
// leak into the extracted text.
const fixture = `
<div class="tgme_widget_message_wrap js-widget_message_wrap">
<div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="mychan/1041">
  <div class="tgme_widget_message_user"><a href="https://t.me/mychan"><i class="tgme_widget_message_user_photo"></i></a></div>
  <div class="tgme_widget_message_bubble">
    <div class="tgme_widget_message_text js-message_text" dir="auto">
      <b>עדכון ראשון</b><br/>שורה שנייה עם קישור <a href="https://example.com" target="_blank">כאן</a>
      <div class="tgme_widget_message_inline_wrap"><span>טקסט מקונן</span></div>
      סיום &amp; תו מיוחד &lt;3
    </div>
    <div class="tgme_widget_message_footer compact js-message_footer">
      <a class="tgme_widget_message_date" href="https://t.me/mychan/1041"><time datetime="2026-08-06T09:15:00+00:00">09:15</time></a>
    </div>
  </div>
</div>
</div>
<div class="tgme_widget_message_wrap js-widget_message_wrap">
<div class="tgme_widget_message" data-post="mychan/1042">
  <div class="tgme_widget_message_bubble">
    <div class="tgme_widget_message_text js-message_text" dir="auto">הודעה שנייה קצרה</div>
    <div class="tgme_widget_message_footer compact js-message_footer">
      <a class="tgme_widget_message_date" href="https://t.me/mychan/1042"><time datetime="2026-08-06T10:00:00+00:00">10:00</time></a>
    </div>
  </div>
</div>
</div>
<div class="tgme_widget_message_wrap js-widget_message_wrap">
<div class="tgme_widget_message" data-post="mychan/1043">
  <div class="tgme_widget_message_bubble">
    <a class="tgme_widget_message_photo_wrap" style="background-image:url('x.jpg')"></a>
    <div class="tgme_widget_message_footer compact js-message_footer">
      <a class="tgme_widget_message_date" href="https://t.me/mychan/1043"><time datetime="2026-08-06T10:30:00+00:00">10:30</time></a>
    </div>
  </div>
</div>
</div>
`

func TestParseMessagesCount(t *testing.T) {
	msgs := ParseMessages(fixture)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].ID != 1041 || msgs[2].ID != 1043 {
		t.Fatalf("ids parsed wrong: %d .. %d", msgs[0].ID, msgs[2].ID)
	}
	if msgs[0].URL != "https://t.me/mychan/1041" {
		t.Fatalf("bad url: %q", msgs[0].URL)
	}
	if msgs[1].Time != "2026-08-06T10:00:00+00:00" {
		t.Fatalf("bad time: %q", msgs[1].Time)
	}
}

func TestNestedDivsAndEntities(t *testing.T) {
	msgs := ParseMessages(fixture)
	got := msgs[0].Text

	want := "עדכון ראשון\nשורה שנייה עם קישור כאן טקסט מקונן סיום & תו מיוחד <3"
	if got != want {
		t.Fatalf("text mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// A media-only post has no text div; it must not swallow the footer markup.
func TestMediaOnlyPostHasEmptyText(t *testing.T) {
	msgs := ParseMessages(fixture)
	if msgs[2].Text != "" {
		t.Fatalf("expected empty text for media post, got %q", msgs[2].Text)
	}
}

func TestSecondMessageText(t *testing.T) {
	msgs := ParseMessages(fixture)
	if msgs[1].Text != "הודעה שנייה קצרה" {
		t.Fatalf("got %q", msgs[1].Text)
	}
}

func TestNormalizeChannel(t *testing.T) {
	cases := map[string]string{
		"durov":                     "durov",
		"@durov":                    "durov",
		"https://t.me/durov":        "durov",
		"https://t.me/s/durov":      "durov",
		"t.me/durov/123":            "durov",
		"  https://t.me/s/durov/  ": "durov",
		"telegram.me/durov?x=1":     "durov",
	}
	for in, want := range cases {
		if got := normalizeChannel(in); got != want {
			t.Errorf("normalizeChannel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKeywordFilters(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SkipEmptyText = true

	if cfg.Wanted("", false) {
		t.Error("empty message should be filtered when SkipEmptyText is on")
	}
	if !cfg.Wanted("", true) {
		t.Error("a media-only post is real content and must pass")
	}
	if !cfg.Wanted("כל הודעה עוברת כשאין מילות מפתח", false) {
		t.Error("no filters means everything passes")
	}

	cfg.IncludeKeywords = []string{"דחוף"}
	if cfg.Wanted("הודעה רגילה", false) {
		t.Error("include filter should reject non-matching text")
	}
	if !cfg.Wanted("שימו לב, דחוף!", false) {
		t.Error("include filter should accept matching text")
	}

	cfg.IncludeKeywords = nil
	cfg.ExcludeKeywords = []string{"פרסומת"}
	if cfg.Wanted("זו פרסומת ממומנת", false) {
		t.Error("exclude filter should reject")
	}
	if !cfg.Wanted("תוכן אמיתי", false) {
		t.Error("exclude filter should let other text through")
	}
}

func TestVideoExtraction(t *testing.T) {
	block := `<div class="tgme_widget_message" data-post="mychan/3001">
	  <a class="tgme_widget_message_video_player js-message_video_player" href="https://t.me/mychan/3001">
	    <i class="tgme_widget_message_video_thumb" style="background-image:url('https://cdn4.telesco.pe/file/thumb99.jpg')"></i>
	    <div class="tgme_widget_message_video_wrap" style="width:640px;">
	      <video src="https://cdn4.telesco.pe/file/clip99.mp4" class="tgme_widget_message_video js-message_video"></video>
	    </div>
	    <time class="message_video_duration js-message_video_duration">0:31</time>
	  </a>
	  <div class="tgme_widget_message_text js-message_text">צפו בסרטון</div>
	</div>`
	msgs := ParseMessages(block)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.Video != "https://cdn4.telesco.pe/file/clip99.mp4" {
		t.Fatalf("video not extracted: %q", m.Video)
	}
	if m.VideoThumb != "https://cdn4.telesco.pe/file/thumb99.jpg" {
		t.Fatalf("thumb not extracted: %q", m.VideoThumb)
	}
	if m.Duration != "0:31" {
		t.Fatalf("duration not extracted: %q", m.Duration)
	}
	if !m.HasMedia() {
		t.Fatal("video post must count as media")
	}
}

// Large videos are served as a preview frame only — no <video> tag.
func TestVideoThumbOnlyExtraction(t *testing.T) {
	block := `<div class="tgme_widget_message" data-post="mychan/3002">
	  <a class="tgme_widget_message_video_player" href="https://t.me/mychan/3002">
	    <i class="tgme_widget_message_video_thumb" style="background-image:url('https://cdn4.telesco.pe/file/big-thumb.jpg')"></i>
	    <time class="message_video_duration">12:05</time>
	  </a>
	</div>`
	msgs := ParseMessages(block)
	m := msgs[0]
	if m.Video != "" {
		t.Fatalf("no inline video expected, got %q", m.Video)
	}
	if m.VideoThumb != "https://cdn4.telesco.pe/file/big-thumb.jpg" {
		t.Fatalf("thumb not extracted: %q", m.VideoThumb)
	}
	if m.Duration != "12:05" {
		t.Fatalf("duration: %q", m.Duration)
	}
}

func TestPhotoExtraction(t *testing.T) {
	block := `<div class="tgme_widget_message" data-post="mychan/2001">
	  <a class="tgme_widget_message_photo_wrap x" href="https://t.me/mychan/2001"
	     style="width:800px;background-image:url('https://cdn4.telesco.pe/file/abc123.jpg')"></a>
	  <div class="tgme_widget_message_text js-message_text">עם תמונה</div>
	</div>`
	msgs := ParseMessages(block)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Photo != "https://cdn4.telesco.pe/file/abc123.jpg" {
		t.Fatalf("photo not extracted: %q", msgs[0].Photo)
	}
	if msgs[0].Text != "עם תמונה" {
		t.Fatalf("text broken: %q", msgs[0].Text)
	}
}

func TestCleanupLegacy(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Must be removed: old Hebrew launchers, mojibake launcher, stale backup.
	gone := []string{
		mk("AddChannels.bat"),
		mk("הפעל.bat"),
		mk("בדיקה.bat"),
		mk("הפעל-ברקע.vbs"),
		mk("שפ¸±ש£.bat"), // garbled extraction leftover
		mk("TelegramPopup.old"),
	}
	// Must survive: settings, state, current launchers, pending update.
	keep := []string{
		mk("config.json"),
		mk("state.json"),
		mk("Start.bat"),
		mk("Stop.bat"),
		mk("Update.bat"),
		mk("TelegramPopup.new"),
		mk("README.md"),
	}

	cleanupLegacy(dir)

	for _, p := range gone {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("legacy file survived: %s", filepath.Base(p))
		}
	}
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("wanted file deleted: %s", filepath.Base(p))
		}
	}
}

func TestSameFileContent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	c := filepath.Join(dir, "c.bin")
	_ = os.WriteFile(a, []byte("identical-content"), 0o644)
	_ = os.WriteFile(b, []byte("identical-content"), 0o644)
	_ = os.WriteFile(c, []byte("different-content"), 0o644)

	if !sameFileContent(a, b) {
		t.Fatal("identical files reported different")
	}
	if sameFileContent(a, c) {
		t.Fatal("different files reported identical")
	}
	if sameFileContent(a, filepath.Join(dir, "missing.bin")) {
		t.Fatal("missing file reported identical")
	}
}
