package main

import (
	"os"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFeedServesPageAndMessages(t *testing.T) {
	f := NewFeed()
	url, err := f.Start(0) // port 0 → any free port
	if err != nil {
		t.Fatalf("feed did not start: %v", err)
	}

	f.Add([]Message{
		{ID: 1, Channel: "mychan", Text: "ראשונה", Time: "2026-08-06T10:00:00+00:00", URL: "https://t.me/mychan/1"},
		{ID: 2, Channel: "mychan", Text: "שנייה עם <script>bad()</script>", Time: "2026-08-06T11:00:00+00:00", URL: "https://t.me/mychan/2"},
	})

	// Page renders with channel identity.
	resp, err := http.Get(url + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(page), "ערוץ חי") {
		t.Fatal("page missing app title")
	}

	// API returns both items, newest first, sanitized.
	resp, err = http.Get(url + "/api/messages")
	if err != nil {
		t.Fatalf("GET /api/messages: %v", err)
	}
	var data struct{ Items []FeedItem }
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	resp.Body.Close()

	if len(data.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(data.Items))
	}
	if data.Items[0].ID != 2 {
		t.Fatalf("newest first expected, got id %d", data.Items[0].ID)
	}
	if strings.Contains(data.Items[0].HTML, "<script>bad") {
		t.Fatal("hostile text leaked as live HTML into the feed")
	}
	if !strings.Contains(data.Items[0].HTML, "&lt;script&gt;") {
		t.Fatal("expected escaped script tag")
	}
}

func TestFeedDeduplicatesAndStreams(t *testing.T) {
	f := NewFeed()
	url, err := f.Start(0)
	if err != nil {
		t.Fatalf("feed did not start: %v", err)
	}

	f.Add([]Message{{ID: 5, Channel: "mychan", Text: "פעם", Time: "2026-08-06T12:00:00+00:00", URL: "u"}})
	f.Add([]Message{{ID: 5, Channel: "mychan", Text: "פעם", Time: "2026-08-06T12:00:00+00:00", URL: "u"}}) // duplicate must be ignored

	// Open the SSE stream, then push a fresh message and expect it there.
	req, _ := http.NewRequest("GET", url+"/api/stream", nil)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer resp.Body.Close()

	go func() {
		time.Sleep(150 * time.Millisecond)
		f.Add([]Message{{ID: 6, Channel: "mychan", Text: "חדשה בשידור", Time: "2026-08-06T13:00:00+00:00", URL: "u6"}})
	}()

	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	var got strings.Builder
	for time.Now().Before(deadline) {
		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
			if strings.Contains(got.String(), "חדשה בשידור") {
				break
			}
		}
		if rErr != nil {
			break
		}
	}
	if !strings.Contains(got.String(), "חדשה בשידור") {
		t.Fatalf("SSE stream never delivered the new message; got: %q", got.String())
	}

	// Store still holds exactly two unique items.
	r2, _ := http.Get(url + "/api/messages")
	var data struct{ Items []FeedItem }
	_ = json.NewDecoder(r2.Body).Decode(&data)
	r2.Body.Close()
	if len(data.Items) != 2 {
		t.Fatalf("dedupe failed: %d items", len(data.Items))
	}
}

func TestChannelsAPI(t *testing.T) {
	f := NewFeed()
	list := []string{"aaa"}
	f.ListChannels = func() []string { return list }
	f.AddChannel = func(name string) (string, error) {
		n := normalizeChannel(name)
		if n == "bad" {
			return "", errAddRejected
		}
		list = append(list, n)
		return n, nil
	}
	f.RemoveChannel = func(name string) error {
		for i, c := range list {
			if c == name {
				list = append(list[:i], list[i+1:]...)
				return nil
			}
		}
		return errAddRejected
	}

	url, err := f.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// GET lists entries with identity fields.
	f.SetChannelInfo("aaa", ChannelInfo{Name: "ערוץ אאא", Photo: "https://cdn/p.jpg"})
	resp, _ := http.Get(url + "/api/channels")
	var got struct {
		Channels []struct {
			Name  string `json:"name"`
			Title string `json:"title"`
			Photo string `json:"photo"`
			Muted bool   `json:"muted"`
		}
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if len(got.Channels) != 1 || got.Channels[0].Name != "aaa" {
		t.Fatalf("bad list: %v", got.Channels)
	}
	// Since 6.3.1 the photo is the app's own stable proxy URL, never the
	// rotting CDN address.
	if got.Channels[0].Title != "ערוץ אאא" || got.Channels[0].Photo != "/api/avatar?channel=aaa" {
		t.Fatalf("identity not served: %+v", got.Channels[0])
	}

	// POST adds (accepts a full t.me link).
	resp, _ = http.Post(url+"/api/channels", "application/json",
		strings.NewReader(`{"channel":"https://t.me/bbb"}`))
	var addResp map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&addResp)
	resp.Body.Close()
	if addResp["ok"] != true || addResp["channel"] != "bbb" {
		t.Fatalf("add failed: %v", addResp)
	}

	// POST rejection propagates the error message.
	resp, _ = http.Post(url+"/api/channels", "application/json",
		strings.NewReader(`{"channel":"bad"}`))
	_ = json.NewDecoder(resp.Body).Decode(&addResp)
	resp.Body.Close()
	if _, hasErr := addResp["error"]; !hasErr {
		t.Fatal("expected error for rejected channel")
	}

	// DELETE removes and drops the channel's items from the timeline.
	f.Add([]Message{{ID: 9, Channel: "bbb", Text: "x", Time: "2026-08-06T10:00:00+00:00", URL: "u"}})
	req, _ := http.NewRequest("DELETE", url+"/api/channels?name=bbb", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()

	resp, _ = http.Get(url + "/api/messages")
	var msgs struct{ Items []FeedItem }
	_ = json.NewDecoder(resp.Body).Decode(&msgs)
	resp.Body.Close()
	for _, it := range msgs.Items {
		if it.Channel == "bbb" {
			t.Fatal("removed channel's items still in timeline")
		}
	}
}

func TestCrossChannelOrdering(t *testing.T) {
	f := NewFeed()
	f.Add([]Message{
		{ID: 50, Channel: "aaa", Text: "ישן", Time: "2026-08-06T09:00:00+00:00", URL: "u"},
		{ID: 3, Channel: "bbb", Text: "חדש", Time: "2026-08-06T12:00:00+00:00", URL: "u"},
		{ID: 51, Channel: "aaa", Text: "אמצע", Time: "2026-08-06T10:30:00+00:00", URL: "u"},
	})
	url, _ := f.Start(0)
	resp, _ := http.Get(url + "/api/messages")
	var data struct{ Items []FeedItem }
	_ = json.NewDecoder(resp.Body).Decode(&data)
	resp.Body.Close()

	// Newest first regardless of channel or raw ID.
	if data.Items[0].Channel != "bbb" || data.Items[2].ID != 50 {
		order := []string{}
		for _, it := range data.Items {
			order = append(order, it.Key)
		}
		t.Fatalf("bad cross-channel order: %v", order)
	}
}

var errAddRejected = errors.New("נדחה")

func TestSettingsAPI(t *testing.T) {
	f := NewFeed()
	popups, sound := true, true
	f.GetSettings = func() (bool, bool) { return popups, sound }
	f.SetSettings = func(p, s *bool) (bool, bool) {
		if p != nil {
			popups = *p
		}
		if s != nil {
			sound = *s
		}
		return popups, sound
	}
	url, err := f.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// GET reflects current state.
	resp, _ := http.Get(url + "/api/settings")
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got["popups"] != true || got["sound"] != true {
		t.Fatalf("bad initial: %v", got)
	}

	// POST flips only the provided field.
	resp, _ = http.Post(url+"/api/settings", "application/json",
		strings.NewReader(`{"popups":false}`))
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got["popups"] != false || got["sound"] != true {
		t.Fatalf("partial update broken: %v", got)
	}
	if popups != false {
		t.Fatal("backend state not updated")
	}
}

// Seeding must NOT broadcast — open tabs would light up old posts as new.
func TestSeedDoesNotBroadcast(t *testing.T) {
	f := NewFeed()
	url, err := f.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	req, _ := http.NewRequest("GET", url+"/api/stream", nil)
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer resp.Body.Close()

	go func() {
		time.Sleep(120 * time.Millisecond)
		f.Seed([]Message{{ID: 1, Channel: "c", Text: "ישן מהסיד", Time: "2026-08-06T10:00:00+00:00", URL: "u"}})
		time.Sleep(120 * time.Millisecond)
		f.Add([]Message{{ID: 2, Channel: "c", Text: "חדש אמיתי", Time: "2026-08-06T11:00:00+00:00", URL: "u"}})
	}()

	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	var got strings.Builder
	for time.Now().Before(deadline) {
		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
			if strings.Contains(got.String(), "חדש אמיתי") {
				break
			}
		}
		if rErr != nil {
			break
		}
	}
	if strings.Contains(got.String(), "ישן מהסיד") {
		t.Fatal("seeded message leaked into the live stream")
	}
	if !strings.Contains(got.String(), "חדש אמיתי") {
		t.Fatal("live message did not arrive on the stream")
	}
	// Both are still stored and served over the snapshot API.
	r2, _ := http.Get(url + "/api/messages")
	var data struct{ Items []FeedItem }
	_ = json.NewDecoder(r2.Body).Decode(&data)
	r2.Body.Close()
	if len(data.Items) != 2 {
		t.Fatalf("store should hold both, has %d", len(data.Items))
	}
}

func TestMuteAPI(t *testing.T) {
	f := NewFeed()
	mutedSet := map[string]bool{}
	f.SetMuted = func(name string, m bool) error { mutedSet[name] = m; return nil }
	f.IsMuted = func(name string) bool { return mutedSet[name] }
	f.ListChannels = func() []string { return []string{"ccc"} }

	url, err := f.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	resp, _ := http.Post(url+"/api/mute", "application/json",
		strings.NewReader(`{"channel":"@ccc","muted":true}`))
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got["ok"] != true || got["channel"] != "ccc" || got["muted"] != true {
		t.Fatalf("mute failed: %v", got)
	}
	if !mutedSet["ccc"] {
		t.Fatal("backend not muted")
	}

	// The channels list reflects it.
	resp, _ = http.Get(url + "/api/channels")
	var list struct {
		Channels []struct {
			Name  string `json:"name"`
			Muted bool   `json:"muted"`
		}
	}
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if !list.Channels[0].Muted {
		t.Fatal("muted flag not surfaced in list")
	}
}

func TestParseChannelInfo(t *testing.T) {
	page := `<head>
	<meta property="og:title" content="אלישע ירד — הערוץ">
	<meta property="og:image" content="https://cdn4.telesco.pe/file/profile.jpg">
	</head><body>...</body>`
	info := ParseChannelInfo(page)
	if info.Name != "אלישע ירד — הערוץ" {
		t.Fatalf("bad name: %q", info.Name)
	}
	if info.Photo != "https://cdn4.telesco.pe/file/profile.jpg" {
		t.Fatalf("bad photo: %q", info.Photo)
	}

	// Fallback path: header markup without og tags.
	page2 := `<div class="tgme_channel_info_header_title" dir="auto"><span dir="auto">ערוץ בדיקה</span></div>
	<i class="tgme_page_photo_image"><img src="https://cdn/x.jpg"></i>`
	info2 := ParseChannelInfo(page2)
	if info2.Name != "ערוץ בדיקה" || info2.Photo != "https://cdn/x.jpg" {
		t.Fatalf("fallback broken: %+v", info2)
	}
}

func TestFeedMediaHTMLVariants(t *testing.T) {
	// Real video (has duration): full controls, sound available, no loop.
	real := Message{ID: 1, Channel: "c", Video: "https://cdn/v.mp4", VideoThumb: "https://cdn/t.jpg", Duration: "3:15"}
	h := feedMediaHTML(real)
	if !strings.Contains(h, "controls") {
		t.Fatal("real video must have controls")
	}
	if strings.Contains(h, "muted") || strings.Contains(h, "loop") || strings.Contains(h, "autoplay") {
		t.Fatalf("real video must not autoplay muted in a loop: %s", h)
	}

	// GIF-like clip (no duration): silent looping preview, no controls.
	gif := Message{ID: 2, Channel: "c", Video: "https://cdn/g.mp4"}
	h = feedMediaHTML(gif)
	if !strings.Contains(h, "gifvid") || !strings.Contains(h, "muted loop") || strings.Contains(h, "controls") || strings.Contains(h, "autoplay") {
		t.Fatalf("gif clip should be a visibility-played muted loop: %s", h)
	}

	// Long video (thumb only): click-to-embed player wiring.
	long := Message{ID: 345, Channel: "mychan", VideoThumb: "https://cdn/big.jpg", Duration: "12:05"}
	h = feedMediaHTML(long)
	if !strings.Contains(h, `data-embed="mychan/345"`) {
		t.Fatalf("embed target missing: %s", h)
	}
	if !strings.Contains(h, "playbtn") {
		t.Fatal("play overlay missing")
	}
}

func TestVideoResolveAPIWithCache(t *testing.T) {
	f := NewFeed()
	calls := 0
	f.ResolveVideo = func(channel string, id int) (string, error) {
		calls++
		if channel == "gone" {
			return "", errAddRejected
		}
		return "https://cdn4.telesco.pe/file/full.mp4", nil
	}
	url, err := f.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	get := func(q string) map[string]any {
		resp, _ := http.Get(url + "/api/video?" + q)
		var d map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&d)
		resp.Body.Close()
		return d
	}

	d := get("channel=mychan&id=345")
	if d["ok"] != true || d["url"] != "https://cdn4.telesco.pe/file/full.mp4" {
		t.Fatalf("resolve failed: %v", d)
	}
	// Second request must come from the cache.
	_ = get("channel=mychan&id=345")
	if calls != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls)
	}
	// Failure path returns the error and is NOT cached.
	d = get("channel=gone&id=7")
	if _, hasErr := d["error"]; !hasErr {
		t.Fatalf("expected error: %v", d)
	}
	// Garbage input rejected without calling upstream.
	before := calls
	_ = get("channel=&id=abc")
	if calls != before {
		t.Fatal("bad input reached the resolver")
	}
}

func TestExtractVideoSrc(t *testing.T) {
	page := `<div class="tgme_widget_message_video_wrap">
	  <video src="https://cdn4.telesco.pe/file/big-video.mp4?token=x" class="tgme_widget_message_video"></video></div>`
	if got := ExtractVideoSrc(page); got != "https://cdn4.telesco.pe/file/big-video.mp4?token=x" {
		t.Fatalf("bad src: %q", got)
	}
	if ExtractVideoSrc("<div>no video here</div>") != "" {
		t.Fatal("expected empty for page without video")
	}
}

// The streaming proxy must pass Range through so the player can seek.
func TestMediaProxyStreamsWithRange(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("proxy request without browser UA")
		}
		if rng := r.Header.Get("Range"); rng == "bytes=100-199" {
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Range", "bytes 100-199/1000")
			w.Header().Set("Accept-Ranges", "bytes")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(make([]byte, 100))
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(make([]byte, 1000))
	}))
	defer upstream.Close()

	f := NewFeed()
	f.ResolveVideo = func(channel string, id int) (string, error) { return upstream.URL + "/v.mp4", nil }
	url, err := f.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Full request.
	resp, err := http.Get(url + "/api/media?channel=mychan&id=42")
	if err != nil {
		t.Fatalf("media: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(body) != 1000 || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("full stream broken: status=%d len=%d", resp.StatusCode, len(body))
	}

	// Ranged request → 206 with headers passed through.
	req, _ := http.NewRequest("GET", url+"/api/media?channel=mychan&id=42", nil)
	req.Header.Set("Range", "bytes=100-199")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || len(body) != 100 {
		t.Fatalf("range not honored: status=%d len=%d", resp.StatusCode, len(body))
	}
	if resp.Header.Get("Content-Range") != "bytes 100-199/1000" {
		t.Fatalf("content-range lost: %q", resp.Header.Get("Content-Range"))
	}
}

func TestExtractVideoSrcFromOgMeta(t *testing.T) {
	page := `<head><meta property="og:video" content="https://cdn4.telesco.pe/file/og-video.mp4"></head>`
	if got := ExtractVideoSrc(page); got != "https://cdn4.telesco.pe/file/og-video.mp4" {
		t.Fatalf("og:video not extracted: %q", got)
	}
}

// A failed video goes to the retry queue; when a later retry succeeds, open
// tabs get a "videoready" event and the cache serves instantly.
func TestVideoRetryPendingLifecycle(t *testing.T) {
	f := NewFeed()
	available := false
	f.ResolveVideo = func(channel string, id int) (string, error) {
		if !available {
			return "", errAddRejected
		}
		return "https://cdn/late-video.mp4", nil
	}
	url, err := f.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// First click fails → queued as pending.
	resp, _ := http.Get(url + "/api/video?channel=mychan&id=777")
	var d map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&d)
	resp.Body.Close()
	if d["pending"] != true {
		t.Fatalf("failure not marked pending: %v", d)
	}
	if f.PendingCount() != 1 {
		t.Fatalf("pending queue: %d", f.PendingCount())
	}

	// Open an SSE tab, then let the background retry succeed.
	req, _ := http.NewRequest("GET", url+"/api/stream", nil)
	client := &http.Client{Timeout: 5 * time.Second}
	sresp, err := client.Do(req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer sresp.Body.Close()

	available = true
	go func() { time.Sleep(150 * time.Millisecond); f.RetryPending() }()

	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	var got strings.Builder
	for time.Now().Before(deadline) {
		n, rErr := sresp.Body.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
			if strings.Contains(got.String(), "videoready") {
				break
			}
		}
		if rErr != nil {
			break
		}
	}
	if !strings.Contains(got.String(), `"videoready"`) || !strings.Contains(got.String(), `"id":777`) {
		t.Fatalf("videoready event missing: %q", got.String())
	}
	if f.PendingCount() != 0 {
		t.Fatal("resolved video still pending")
	}

	// Now the video serves straight from cache.
	resp, _ = http.Get(url + "/api/video?channel=mychan&id=777")
	_ = json.NewDecoder(resp.Body).Decode(&d)
	resp.Body.Close()
	if d["ok"] != true || d["url"] != "https://cdn/late-video.mp4" {
		t.Fatalf("cache after retry broken: %v", d)
	}
}

// Regression for the REAL Telegram markup (Aug 2026): the embed page has no
// <video> tag — the file sits in an <a href="...mp4?token=..."> link.
func TestExtractVideoSrcFromRealEmbedMarkup(t *testing.T) {
	page := `<div class="tgme_widget_message_video_player not_supported">
	  <i class="tgme_widget_message_video_thumb" style="background-image:url('https://cdn4.telesco.pe/file/thumb.jpg')"></i>
	  <div class="message_media_not_supported_label">This media is not supported in your browser</div>
	  <a class="message_media_view_in_telegram" href="https://cdn4.telesco.pe/file/5e25d247a1.mp4?token=FAKE-TEST-TOKEN-NOT-A-SECRET&amp;size=big">VIEW IN TELEGRAM</a>
	</div>`
	got := ExtractVideoSrc(page)
	want := "https://cdn4.telesco.pe/file/5e25d247a1.mp4?token=FAKE-TEST-TOKEN-NOT-A-SECRET&size=big"
	if got != want {
		t.Fatalf("real markup not extracted:\n got: %q\nwant: %q", got, want)
	}
}

// A stale cached CDN token must be refreshed transparently by the proxy.
func TestMediaProxyRefreshesExpiredToken(t *testing.T) {
	stale := true
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "token=old") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write(make([]byte, 500))
	}))
	defer upstream.Close()

	f := NewFeed()
	f.ResolveVideo = func(channel string, id int) (string, error) {
		if stale {
			return upstream.URL + "/v.mp4?token=old", nil
		}
		return upstream.URL + "/v.mp4?token=new", nil
	}
	url, err := f.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// Prime the cache with the stale-token URL.
	resp, _ := http.Get(url + "/api/video?channel=mychan&id=9")
	resp.Body.Close()
	stale = false // Telegram would now hand out a fresh token

	// The proxy hits 403 on the cached URL, re-resolves, and streams fine.
	resp, err = http.Get(url + "/api/media?channel=mychan&id=9")
	if err != nil {
		t.Fatalf("media: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(body) != 500 {
		t.Fatalf("stale token not refreshed: status=%d len=%d", resp.StatusCode, len(body))
	}
}

// When anonymous resolution fails but a Telegram account is connected, the
// video API reports big:true and the media proxy streams the downloaded file.
func TestBigVideoAccountFallback(t *testing.T) {
	dir := t.TempDir()
	// A fake "downloaded" mp4 on disk.
	vidPath := dir + "/big.mp4"
	_ = os.WriteFile(vidPath, []byte("BIGVIDEODATA"), 0o644)

	f := NewFeed()
	f.ResolveVideo = func(channel string, id int) (string, error) {
		return "", errAddRejected // anonymous paths exhausted
	}
	f.HasTGAccount = func() bool { return true }
	f.BigVideo = func(channel string, id int) (string, error) {
		return vidPath, nil
	}
	url, err := f.Start(0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// /api/video → big:true (not "pending"/error).
	resp, _ := http.Get(url + "/api/video?channel=mychan&id=500")
	var d map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&d)
	resp.Body.Close()
	if d["ok"] != true || d["big"] != true {
		t.Fatalf("expected big-video response, got %v", d)
	}

	// /api/media streams the local file, with Range support.
	req, _ := http.NewRequest("GET", url+"/api/media?channel=mychan&id=500", nil)
	req.Header.Set("Range", "bytes=0-2")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("media: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || string(body) != "BIG" {
		t.Fatalf("range serve of local file broken: status=%d body=%q", resp.StatusCode, string(body))
	}
}

// The locked channel list: no hooks wired → APIs refuse, page told to hide.
func TestLockedChannelList(t *testing.T) {
	f := NewFeed()
	f.ListChannels = func() []string { return []string{"aaa"} }
	// No AddChannel/RemoveChannel hooks — the locked configuration.
	url, err := f.Start(0)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(url + "/api/channels")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got["locked"] != true {
		t.Fatal("locked list must be reported to the page")
	}

	// POST (add) must be refused.
	resp, _ = http.Post(url+"/api/channels", "application/json", strings.NewReader(`{"channel":"newone"}`))
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("locked add: want 501, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// DELETE must be refused.
	req, _ := http.NewRequest("DELETE", url+"/api/channels?name=aaa", nil)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("locked delete: want 501, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestUnlockedListReportsUnlocked(t *testing.T) {
	f := NewFeed()
	f.ListChannels = func() []string { return []string{"aaa"} }
	f.AddChannel = func(n string) (string, error) { return n, nil }
	f.RemoveChannel = func(n string) error { return nil }
	url, _ := f.Start(0)
	resp, err := http.Get(url + "/api/channels")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if got["locked"] != false {
		t.Fatal("editable list must report locked=false")
	}
}

func TestConfigDefaultsToLockedChannels(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/config.json"
	_ = os.WriteFile(p, []byte(`{"channels":["a"],"poll_seconds":30}`), 0o644)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AllowChannelEdit {
		t.Fatal("a config without the field must default to LOCKED (user request)")
	}
}
