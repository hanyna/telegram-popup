package main

import (
	"strings"
	"testing"
)

func TestBuildPopupHTMLEscapesHostileText(t *testing.T) {
	msg := Message{
		ID:   7,
		Text: "<script>alert('x')</script> & סתם טקסט",
		URL:  "https://t.me/mychan/7",
	}
	page := BuildPopupHTML("@mychan", msg, "12:30", 15)

	if strings.Contains(page, "<script>alert") {
		t.Fatal("message text was injected as live HTML")
	}
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Fatal("expected escaped script tag in output")
	}
	if !strings.Contains(page, "@mychan") {
		t.Fatal("title missing")
	}
}

func TestBuildPopupHTMLLinkifiesURLs(t *testing.T) {
	msg := Message{
		ID:   8,
		Text: "פרטים בקישור https://example.com/page?a=1&b=2 ותודה",
		URL:  "https://t.me/mychan/8",
	}
	page := BuildPopupHTML("@mychan", msg, "12:31", 15)

	if !strings.Contains(page, `onclick="return openUrl(`) {
		t.Fatal("URL was not linkified")
	}
	// The raw ampersand must not survive unescaped inside the anchor attrs.
	if strings.Contains(page, `openUrl('https://example.com/page?a=1&b=2')`) {
		t.Fatal("URL embedded without escaping")
	}
	if !strings.Contains(page, "ותודה") {
		t.Fatal("text after the URL was lost")
	}
}

func TestBuildPopupHTMLNewlinesAndPhoto(t *testing.T) {
	msg := Message{
		ID:    9,
		Text:  "שורה ראשונה\nשורה שנייה",
		Photo: "https://cdn4.telesco.pe/file/pic.jpg",
		URL:   "https://t.me/mychan/9",
	}
	page := BuildPopupHTML("@mychan", msg, "12:32", 15)

	if !strings.Contains(page, "שורה ראשונה<br>שורה שנייה") {
		t.Fatal("newline was not converted to <br>")
	}
	if !strings.Contains(page, `<img src="https://cdn4.telesco.pe/file/pic.jpg"`) {
		t.Fatal("photo tag missing")
	}
}

func TestBuildPopupHTMLPhotoOnly(t *testing.T) {
	msg := Message{ID: 10, Photo: "https://cdn4.telesco.pe/p.jpg", URL: "https://t.me/mychan/10"}
	page := BuildPopupHTML("@mychan", msg, "12:33", 15)

	if strings.Contains(page, `class="body"`) {
		t.Fatal("empty body div should be omitted for photo-only posts")
	}
	if !strings.Contains(page, `class="photo"`) {
		t.Fatal("photo section missing")
	}
}

func TestBuildPopupHTMLInlineVideo(t *testing.T) {
	// Since 6.1.1 a video post in a popup is a STILL preview — never a
	// playing element (the IE engine leaked content audio; see mute_test.go).
	msg := Message{
		ID:         11,
		Text:       "צפו!",
		Video:      "https://cdn4.telesco.pe/file/clip.mp4",
		VideoThumb: "https://cdn4.telesco.pe/file/thumb.jpg",
		Duration:   "0:31",
		URL:        "https://t.me/mychan/11",
	}
	page := BuildPopupHTML("@mychan", msg, "13:00", 15)

	if strings.Contains(page, "<video") {
		t.Fatal("popup must NOT embed a playing video — this is the speaking-popups bug")
	}
	if !strings.Contains(page, `img src="https://cdn4.telesco.pe/file/thumb.jpg"`) {
		t.Fatal("preview frame missing")
	}
	if !strings.Contains(page, "playbtn") {
		t.Fatal("play badge missing — the card should still read as a video")
	}
	if !strings.Contains(page, `class="durbadge">0:31<`) {
		t.Fatal("duration badge missing")
	}
}

func TestBuildPopupHTMLThumbOnlyVideo(t *testing.T) {
	msg := Message{
		ID:         12,
		VideoThumb: "https://cdn4.telesco.pe/file/big.jpg",
		Duration:   "12:05",
		URL:        "https://t.me/mychan/12",
	}
	page := BuildPopupHTML("@mychan", msg, "13:05", 15)

	if strings.Contains(page, "<video") {
		t.Fatal("thumb-only post must not embed a video tag")
	}
	if !strings.Contains(page, `class="playbtn"`) {
		t.Fatal("play button overlay missing")
	}
	if !strings.Contains(page, `<img src="https://cdn4.telesco.pe/file/big.jpg"`) {
		t.Fatal("thumb image missing")
	}
}

func TestPopupScriptHasNoGoBacktickCollisions(t *testing.T) {
	// The PowerShell source lives inside a Go raw string, so a stray backtick
	// would silently truncate it at compile time of a future edit.
	if strings.Contains(popupScript, "`") {
		t.Fatal("popupScript must not contain backticks")
	}
	if strings.Contains(htmlTemplate, "`") {
		t.Fatal("htmlTemplate must not contain backticks")
	}
}
