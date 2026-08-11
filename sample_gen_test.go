package main

import (
	"os"
	"testing"
)

// Not a real test — emits sample HTML for visual review.
func TestGenerateSamples(t *testing.T) {
	if os.Getenv("GEN_SAMPLES") == "" {
		t.Skip("set GEN_SAMPLES=1 to emit samples")
	}
	thumb := "data:image/svg+xml;charset=utf-8," +
		"%3Csvg xmlns='http://www.w3.org/2000/svg' width='800' height='450'%3E" +
		"%3Cdefs%3E%3ClinearGradient id='g' x1='0' y1='0' x2='1' y2='1'%3E" +
		"%3Cstop offset='0' stop-color='%233a2a5c'/%3E%3Cstop offset='1' stop-color='%23172033'/%3E" +
		"%3C/linearGradient%3E%3C/defs%3E%3Crect width='800' height='450' fill='url(%23g)'/%3E" +
		"%3Ctext x='400' y='235' text-anchor='middle' fill='%23aa96ff' font-size='38' font-family='Segoe UI'%3E%D7%A4%D7%A8%D7%99%D7%99%D7%9D %D7%9E%D7%94%D7%A1%D7%A8%D7%98%D7%95%D7%9F%3C/text%3E%3C/svg%3E"

	vid := Message{
		ID:         21,
		Text:       "סרטון מהשיעור של אמש — שווה צפייה!",
		VideoThumb: thumb,
		Duration:   "12:05",
		Time:       "2026-08-08T21:00:00+00:00",
		URL:        "https://t.me/mychan/21",
	}
	_ = os.WriteFile("/tmp/render/video.html", []byte(BuildPopupHTML("@ערוץ_הקהילה", vid, "00:00", 15)), 0o644)
}
