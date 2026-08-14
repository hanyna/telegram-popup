package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Markup shaped like the real t.me/s pages, one post per media kind.
const richFixture = `<html><body>
<div class="tgme_widget_message" data-post="rich/101">
  <div class="tgme_widget_message_photo_wrap" style="background-image:url('https://cdn.example/a1.jpg')"></div>
  <a class="tgme_widget_message_photo_wrap" style="width:250px;background-image:url('https://cdn.example/a2.jpg')"></a>
  <a class="tgme_widget_message_photo_wrap" style="background-image:url('https://cdn.example/a3.jpg')"></a>
  <div class="tgme_widget_message_text js-message_text">אלבום משלוש תמונות</div>
  <time datetime="2026-08-11T10:00:00+00:00"></time>
</div>
<div class="tgme_widget_message" data-post="rich/102">
  <audio class="tgme_widget_message_voice js-message_voice" src="https://cdn.example/voice.ogg" data-waveform="1,2,3"></audio>
  <span class="tgme_widget_message_voice_duration">0:42</span>
  <time datetime="2026-08-11T10:01:00+00:00"></time>
</div>
<div class="tgme_widget_message" data-post="rich/103">
  <video class="tgme_widget_message_roundvideo js-message_roundvideo" src="https://cdn.example/round.mp4"></video>
  <time datetime="2026-08-11T10:02:00+00:00"></time>
</div>
<div class="tgme_widget_message" data-post="rich/104">
  <i class="tgme_widget_message_sticker" style="background-image:url('https://cdn.example/sticker.webp')"></i>
  <time datetime="2026-08-11T10:03:00+00:00"></time>
</div>
<div class="tgme_widget_message" data-post="rich/105">
  <div class="tgme_widget_message_poll_question">מי ינצח בבחירות?</div>
  <div class="tgme_widget_message_poll_option">
    <div class="tgme_widget_message_poll_option_percent">67%</div>
    <div class="tgme_widget_message_poll_option_text">כן</div>
  </div>
  <div class="tgme_widget_message_poll_option">
    <div class="tgme_widget_message_poll_option_percent">33%</div>
    <div class="tgme_widget_message_poll_option_text">לא</div>
  </div>
  <time datetime="2026-08-11T10:04:00+00:00"></time>
</div>
<div class="tgme_widget_message" data-post="rich/106">
  <div class="tgme_widget_message_text js-message_text">תראו כתבה</div>
  <a class="tgme_widget_message_link_preview" href="https://news.example/article">
    <i class="link_preview_right_image" style="background-image:url('https://cdn.example/preview.jpg')"></i>
    <div class="link_preview_title">כותרת הכתבה</div>
    <div class="link_preview_description">תיאור קצר של הכתבה<br>בשתי שורות</div>
  </a>
  <time datetime="2026-08-11T10:05:00+00:00"></time>
</div>
<div class="tgme_widget_message" data-post="rich/107">
  <div class="tgme_widget_message_document_title">דוח_חודשי.pdf</div>
  <div class="tgme_widget_message_document_extra">2.4 MB</div>
  <time datetime="2026-08-11T10:06:00+00:00"></time>
</div>
<div class="tgme_widget_message" data-post="rich/108">
  <div class="tgme_widget_message_video_player">
    <i class="tgme_widget_message_video_thumb" style="background-image:url('https://cdn.example/th.jpg')"></i>
    <video class="tgme_widget_message_video js-message_video" src="https://cdn.example/clip.mp4"></video>
    <time class="message_video_duration js-message_video_duration">0:15</time>
  </div>
  <time datetime="2026-08-11T10:07:00+00:00"></time>
</div>
</body></html>`

func parseRich(t *testing.T) map[int]Message {
	t.Helper()
	msgs := ParseMessages(richFixture)
	if len(msgs) != 8 {
		t.Fatalf("expected 8 posts, got %d", len(msgs))
	}
	byID := map[int]Message{}
	for _, m := range msgs {
		byID[m.ID] = m
	}
	return byID
}

func TestParseAlbum(t *testing.T) {
	m := parseRich(t)[101]
	if len(m.Photos) != 3 {
		t.Fatalf("album should carry 3 photos, got %d (%v)", len(m.Photos), m.Photos)
	}
	if m.Photo != "https://cdn.example/a1.jpg" {
		t.Errorf("Photo must stay the first album photo for archive compatibility, got %q", m.Photo)
	}
	if m.Photos[2] != "https://cdn.example/a3.jpg" {
		t.Errorf("photo order broken: %v", m.Photos)
	}
	if !m.HasMedia() {
		t.Error("album must count as media")
	}
}

func TestParseVoice(t *testing.T) {
	m := parseRich(t)[102]
	if m.Voice != "https://cdn.example/voice.ogg" {
		t.Fatalf("voice URL not extracted: %q", m.Voice)
	}
	if m.VoiceDur != "0:42" {
		t.Errorf("voice duration: got %q", m.VoiceDur)
	}
	if !m.HasMedia() {
		t.Error("a voice message must count as media (or the popup filter drops it)")
	}
}

func TestParseRoundVideo(t *testing.T) {
	m := parseRich(t)[103]
	if m.Round != "https://cdn.example/round.mp4" {
		t.Fatalf("round video not extracted: %q", m.Round)
	}
	if m.Video != "" {
		t.Error("a round note must NOT also register as a regular video")
	}
}

func TestParseSticker(t *testing.T) {
	m := parseRich(t)[104]
	if m.Sticker != "https://cdn.example/sticker.webp" {
		t.Fatalf("sticker not extracted: %q", m.Sticker)
	}
}

func TestParsePoll(t *testing.T) {
	m := parseRich(t)[105]
	if m.Poll != "מי ינצח בבחירות?" {
		t.Fatalf("poll question: %q", m.Poll)
	}
	if len(m.PollOpts) != 2 {
		t.Fatalf("poll options: got %d", len(m.PollOpts))
	}
	if m.PollOpts[0].Text != "כן" || m.PollOpts[0].Pct != "67%" {
		t.Errorf("first option wrong: %+v", m.PollOpts[0])
	}
}

func TestParseLinkPreview(t *testing.T) {
	m := parseRich(t)[106]
	if m.LinkTitle != "כותרת הכתבה" {
		t.Fatalf("link title: %q", m.LinkTitle)
	}
	if m.LinkHref != "https://news.example/article" {
		t.Errorf("link href: %q", m.LinkHref)
	}
	if !strings.Contains(m.LinkDesc, "תיאור קצר") {
		t.Errorf("link description: %q", m.LinkDesc)
	}
	if m.LinkImage != "https://cdn.example/preview.jpg" {
		t.Errorf("link image: %q", m.LinkImage)
	}
}

func TestParseDocument(t *testing.T) {
	m := parseRich(t)[107]
	if m.Doc != "דוח_חודשי.pdf" || m.DocSize != "2.4 MB" {
		t.Fatalf("document: %q / %q", m.Doc, m.DocSize)
	}
}

func TestRegularVideoStillWorks(t *testing.T) {
	m := parseRich(t)[108]
	if m.Video != "https://cdn.example/clip.mp4" {
		t.Fatalf("regular video regressed: %q", m.Video)
	}
	if m.Round != "" {
		t.Error("regular video must not be classified as round")
	}
}

// Renderers: every media kind must produce visible, correctly-classed markup.
func TestFeedMediaHTMLRichKinds(t *testing.T) {
	byID := parseRich(t)
	cases := []struct {
		id       int
		mustHave []string
	}{
		{101, []string{`class="album"`, "a1.jpg", "a2.jpg", "a3.jpg"}},
		{102, []string{"voicebox", "voice.ogg", "0:42", "<audio"}},
		// Round notes (and all user-playable videos) stream via the proxy so
		// expired archive URLs can never fail-and-autoplay.
		{103, []string{"roundvid", "/api/media", `preload="none"`}},
		{104, []string{"stickerbox", "sticker.webp"}},
		{105, []string{"pollbox", "מי ינצח בבחירות?", "67%", "width:67%"}},
		{106, []string{"linkcard", "כותרת הכתבה", "news.example/article", `rel="noopener"`}},
		{107, []string{"docbox", "דוח_חודשי.pdf", "2.4 MB"}},
	}
	for _, c := range cases {
		htmlOut := feedMediaHTML(byID[c.id])
		for _, want := range c.mustHave {
			if !strings.Contains(htmlOut, want) {
				t.Errorf("post %d: rendered HTML missing %q\n%s", c.id, want, htmlOut)
			}
		}
	}
}

// Sidebar previews: media-only posts must describe themselves.
func TestPreviewLabelsForRichMedia(t *testing.T) {
	byID := parseRich(t)
	expects := map[int]string{
		101: "אלבום",
		102: "🎤",
		103: "🎬",
		104: "סטיקר",
		105: "📊",
		107: "📎",
	}
	for id, want := range expects {
		item := toItem(byID[id])
		if !strings.Contains(item.Preview, want) {
			t.Errorf("post %d preview %q should contain %q", id, item.Preview, want)
		}
	}
}

// Pin API: full cycle against a live feed server.
func TestPinAPI(t *testing.T) {
	f := NewFeed()
	pins := map[string]bool{}
	f.ListChannels = func() []string { return []string{"a", "b"} }
	f.IsPinned = func(n string) bool { return pins[n] }
	f.SetPinned = func(n string, p bool) error { pins[n] = p; return nil }

	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url+"/api/pin", "application/json", strings.NewReader(`{"channel":"@b","pinned":true}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !pins["b"] {
		t.Fatal("pin did not stick (and @-prefix must be normalized)")
	}

	r2, _ := http.Get(url + "/api/channels")
	var got struct {
		Channels []struct {
			Name   string `json:"name"`
			Pinned bool   `json:"pinned"`
		} `json:"channels"`
	}
	_ = json.NewDecoder(r2.Body).Decode(&got)
	r2.Body.Close()
	found := false
	for _, c := range got.Channels {
		if c.Name == "b" && c.Pinned {
			found = true
		}
	}
	if !found {
		t.Error("channels API must report pinned=true")
	}
}

// Keywords API: round-trip with cleaning and deduping.
func TestKeywordsAPI(t *testing.T) {
	f := NewFeed()
	var inc, exc []string
	f.GetKeywords = func() ([]string, []string) { return inc, exc }
	f.SetKeywords = func(i, e []string) { inc, exc = i, e }

	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"include":[" פיגוע ","","חדשות","פיגוע"],"exclude":["פרסומת"]}`
	resp, err := http.Post(url+"/api/keywords", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(inc) != 2 || inc[0] != "פיגוע" || inc[1] != "חדשות" {
		t.Fatalf("include not cleaned/deduped: %v", inc)
	}
	if len(exc) != 1 || exc[0] != "פרסומת" {
		t.Fatalf("exclude wrong: %v", exc)
	}

	r2, _ := http.Get(url + "/api/keywords")
	var got struct {
		Include []string `json:"include"`
		Exclude []string `json:"exclude"`
	}
	_ = json.NewDecoder(r2.Body).Decode(&got)
	r2.Body.Close()
	if len(got.Include) != 2 || len(got.Exclude) != 1 {
		t.Errorf("GET round-trip mismatch: %+v", got)
	}
}

// Live keyword semantics used by the scanner.
func TestRuntimeKeywordFiltering(t *testing.T) {
	s := &runtimeSettings{skipEmpty: true}
	if !s.wanted("סתם הודעה", false) {
		t.Error("no keywords set → everything wanted")
	}
	s.setKeywords([]string{"פיגוע"}, nil)
	if s.wanted("חדשות רגילות", false) {
		t.Error("include list active → non-matching text must be filtered")
	}
	if !s.wanted("אירע פיגוע בצומת", false) {
		t.Error("matching text must pass")
	}
	s.setKeywords(nil, []string{"פרסומת"})
	if s.wanted("פרסומת: קנו עכשיו", false) {
		t.Error("excluded text must be filtered")
	}
	if !s.wanted("", true) {
		t.Error("media-only post must pass the empty-text guard")
	}
	if s.wanted("", false) {
		t.Error("truly empty post must be dropped")
	}
}

// The rich fixture must also flow end-to-end: server → parse → feed items.
func TestRichMediaEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(richFixture))
	}))
	defer srv.Close()
	t.Setenv("TGPOPUP_BASE_URL", srv.URL)

	msgs, _, err := fetchChannel("rich")
	if err != nil {
		t.Fatal(err)
	}
	feed := NewFeed()
	feed.Add(msgs)

	feed.mu.Lock()
	defer feed.mu.Unlock()
	if len(feed.items) != 8 {
		t.Fatalf("all 8 rich posts should land in the feed, got %d", len(feed.items))
	}
	joined := ""
	for _, it := range feed.items {
		joined += it.HTML
	}
	for _, want := range []string{"album", "voicebox", "roundvid", "stickerbox", "pollbox", "linkcard", "docbox"} {
		if !strings.Contains(joined, want) {
			t.Errorf("feed HTML missing %q", want)
		}
	}
}
