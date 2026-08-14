package main

import (
	"strings"
	"testing"
)

// Regression guards for the "popup speaks out loud" bug: the popup renders on
// the IE11 engine, which ignores the muted ATTRIBUTE on autoplaying video —
// muting must therefore also happen via the muted PROPERTY from script.

func TestPopupTemplateForceMutesFromScript(t *testing.T) {
	for _, want := range []string{
		"function muteAll()",
		".muted = true",
		"setInterval(muteAll",
	} {
		if !strings.Contains(htmlTemplate, want) {
			t.Errorf("popup template lost its script-level muting (%q missing) — IE plays autoplay videos WITH SOUND without it", want)
		}
	}
}

// The final word on the speaking-popups bug: a popup contains NO playable
// media element AT ALL. Video posts render as a still frame with a play badge.
func TestPopupContainsNoPlayableMedia(t *testing.T) {
	cases := []Message{
		{ID: 1, Channel: "ch", Video: "https://cdn.example/talk.mp4", VideoThumb: "https://cdn.example/th.jpg", Duration: "3:12"},
		{ID: 2, Channel: "ch", Video: "https://cdn.example/gif.mp4"}, // no duration (GIF-like)
		{ID: 3, Channel: "ch", Voice: "https://cdn.example/v.ogg", VoiceDur: "0:42"},
		{ID: 4, Channel: "ch", Round: "https://cdn.example/note.mp4"},
	}
	for _, m := range cases {
		page := BuildPopupHTML("@ch", m, "12:00", 15)
		if strings.Contains(page, "<video") || strings.Contains(page, "<audio") {
			t.Errorf("popup for msg %d embeds a playable element — this is the bug that spoke out loud:\n%s", m.ID, page)
		}
	}
	// The video popup must still LOOK like a video: frame + play badge.
	withThumb := BuildPopupHTML("@ch", cases[0], "12:00", 15)
	for _, want := range []string{"playbtn", "th.jpg", "3:12"} {
		if !strings.Contains(withThumb, want) {
			t.Errorf("video popup lost its visual preview (%q missing)", want)
		}
	}
	// Voice popup says what arrived.
	voice := BuildPopupHTML("@ch", cases[2], "12:00", 15)
	if !strings.Contains(voice, "הודעה קולית") {
		t.Error("voice popup must label itself")
	}
}

func TestPopupScriptDisablesIENavigationSounds(t *testing.T) {
	if !strings.Contains(popupScript, "FEATURE_DISABLE_NAVIGATION_SOUNDS") {
		t.Error("popup.ps1 must disable the IE navigation click sound")
	}
}

func TestFeedAutoplayPreviewsAreMarkedMuted(t *testing.T) {
	out := feedMediaHTML(Message{ID: 2, Channel: "ch", Video: "https://cdn.example/gifclip.mp4"})
	// Since 6.4 GIF previews carry NO autoplay attribute — the page plays
	// each one only while visible (performance + silence by construction).
	if !strings.Contains(out, "gifvid") || !strings.Contains(out, "muted loop") {
		t.Fatalf("GIF-like preview must be a muted looping gifvid, got: %s", out)
	}
	if strings.Contains(out, "autoplay") {
		t.Fatalf("GIF preview must not autoplay from markup: %s", out)
	}
	// Videos the user starts by hand must NOT autoplay.
	withDur := feedMediaHTML(Message{ID: 3, Channel: "ch", Video: "https://cdn.example/real.mp4", Duration: "1:02"})
	if strings.Contains(withDur, "autoplay") {
		t.Error("a real video with duration must not autoplay")
	}
	round := feedMediaHTML(Message{ID: 4, Channel: "ch", Round: "https://cdn.example/note.mp4"})
	if strings.Contains(round, "autoplay") {
		t.Error("round video notes must not autoplay (they are speech)")
	}
	voice := feedMediaHTML(Message{ID: 5, Channel: "ch", Voice: "https://cdn.example/v.ogg"})
	if strings.Contains(voice, "autoplay") {
		t.Error("voice messages must never autoplay")
	}
}

func TestFeedPageEnforcesMutedProperty(t *testing.T) {
	for _, want := range []string{
		"video.gifvid",
		"v.muted = true",
		"volumechange",
	} {
		if !strings.Contains(feedPage, want) {
			t.Errorf("feed page lost its script-level mute enforcement (%q missing)", want)
		}
	}
}

// The root cause of the "wall of sound": stale archived video URLs erroring
// en masse while the retry handler auto-played them. Pin both fixes.
func TestErrorRetryNeverAutoplaysUserVideos(t *testing.T) {
	// The page's retry handler may call play() ONLY for autoplay previews.
	idx := strings.Index(feedPage, "msgs.addEventListener('error'")
	if idx < 0 {
		t.Fatal("error-retry handler missing")
	}
	handler := feedPage[idx : idx+1400]
	if strings.Contains(handler, "v.play(") {
		t.Fatal("retry handler must NEVER call play() — that was the audio storm")
	}
	if !strings.Contains(handler, "gifvid") {
		t.Fatal("retry handler must hand repaired GIFs back to the visibility observer")
	}
}

func TestUserPlayableVideosStreamViaProxyWithNoPreload(t *testing.T) {
	// Controls video: proxy src + preload none → stale tokens can't error.
	out := feedMediaHTML(Message{ID: 9, Channel: "ch", Video: "https://stale.example/x.mp4?token=old", Duration: "2:10"})
	if !strings.Contains(out, "/api/media?channel=ch") {
		t.Errorf("controls video must stream via the proxy, got: %s", out)
	}
	if strings.Contains(out, "stale.example") {
		t.Error("the stale direct URL must not appear in the markup")
	}
	if !strings.Contains(out, `preload="none"`) {
		t.Error("controls video must not fetch anything before the user clicks")
	}
	// GIF-like previews keep the direct URL (muted autoplay) + proxy retry.
	gif := feedMediaHTML(Message{ID: 10, Channel: "ch", Video: "https://cdn.example/g.mp4"})
	if !strings.Contains(gif, "cdn.example/g.mp4") || !strings.Contains(gif, "data-proxy") {
		t.Errorf("GIF-like preview lost its direct-src + retry setup: %s", gif)
	}
}
