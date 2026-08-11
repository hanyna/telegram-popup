package main

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Message is a single post scraped from the public channel page. The JSON
// tags define the on-disk history format — keep them stable.
type Message struct {
	ID         int    `json:"id"`               // running post number inside the channel
	Channel    string `json:"channel"`          // channel username the post belongs to
	Text       string `json:"text,omitempty"`   // plain text, tags stripped
	Time       string `json:"time,omitempty"`   // ISO timestamp as published by Telegram
	URL        string `json:"url,omitempty"`    // direct t.me link to the post
	Photo      string `json:"photo,omitempty"`  // first attached photo URL, if any
	Video      string `json:"video,omitempty"`  // inline mp4 URL when Telegram embeds the file itself
	VideoThumb string `json:"vthumb,omitempty"` // preview frame for videos
	Duration   string `json:"dur,omitempty"`    // human-readable video length, e.g. "0:31"
}

// HasMedia reports whether the post carries any visual attachment.
func (m Message) HasMedia() bool {
	return m.Photo != "" || m.Video != "" || m.VideoThumb != ""
}

// ChannelInfo is the channel's public identity as shown on its t.me page.
type ChannelInfo struct {
	Name  string // display title, e.g. "אלישע ירד"
	Photo string // profile picture URL
}

var (
	reOgTitle    = regexp.MustCompile(`property="og:title"\s+content="([^"]*)"`)
	reOgImage    = regexp.MustCompile(`property="og:image"\s+content="([^"]*)"`)
	reHeadTitle  = regexp.MustCompile(`tgme_channel_info_header_title[^>]*>(?:<span[^>]*>)?([^<]+)<`)
	reHeadPhoto  = regexp.MustCompile(`tgme_page_photo_image[^>]*>\s*<img[^>]*src="([^"]+)"`)
)

var (
	reOgVideo       = regexp.MustCompile(`property="og:video(?::secure_url)?"\s+content="([^"]+)"`)
	reTwitterStream = regexp.MustCompile(`name="twitter:player:stream"\s+content="([^"]+)"`)
	// Telegram's current embed pages put the file in a plain link — an <a>
	// pointing straight at the CDN mp4 (with an auth token) — not in a
	// <video> tag. Match any CDN mp4 URL wherever it appears.
	reMp4Anywhere = regexp.MustCompile(`https://[A-Za-z0-9.\-]*telesco\.pe/[^"'\s<>]+\.mp4[^"'\s<>]*`)
	reMp4Generic  = regexp.MustCompile(`https://[^"'\s<>]+\.mp4[^"'\s<>]*`)
)

// ExtractVideoSrc pulls the direct mp4 URL out of a post page — either the
// inline <video> tag or the OpenGraph video meta that Telegram publishes on
// plain post pages.
func ExtractVideoSrc(page string) string {
	if m := reVideo.FindStringSubmatch(page); m != nil {
		return html.UnescapeString(m[1])
	}
	if m := reOgVideo.FindStringSubmatch(page); m != nil {
		return html.UnescapeString(m[1])
	}
	if m := reTwitterStream.FindStringSubmatch(page); m != nil {
		return html.UnescapeString(m[1])
	}
	// The link-to-CDN form (today's real markup). Prefer Telegram's own CDN;
	// fall back to any mp4 URL on the page. Unescape because href attributes
	// encode token separators as &amp;.
	if m := reMp4Anywhere.FindString(page); m != "" {
		return html.UnescapeString(m)
	}
	if m := reMp4Generic.FindString(page); m != "" {
		return html.UnescapeString(m)
	}
	return ""
}

// ParseChannelInfo pulls the channel title and photo out of a t.me/s page.
// Prefers OpenGraph tags (stable), falls back to the visible header markup.
func ParseChannelInfo(page string) ChannelInfo {
	var info ChannelInfo
	if m := reOgTitle.FindStringSubmatch(page); m != nil {
		info.Name = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if info.Name == "" {
		if m := reHeadTitle.FindStringSubmatch(page); m != nil {
			info.Name = strings.TrimSpace(html.UnescapeString(m[1]))
		}
	}
	if m := reOgImage.FindStringSubmatch(page); m != nil {
		info.Photo = html.UnescapeString(m[1])
	}
	if info.Photo == "" {
		if m := reHeadPhoto.FindStringSubmatch(page); m != nil {
			info.Photo = html.UnescapeString(m[1])
		}
	}
	return info
}

var (
	reDataPost = regexp.MustCompile(`data-post="([^"/]+)/(\d+)"`)
	reTime     = regexp.MustCompile(`<time[^>]*datetime="([^"]+)"`)
	rePhoto    = regexp.MustCompile(`tgme_widget_message_photo_wrap[^>]*?url\('([^']+)'\)`)
	reVideo    = regexp.MustCompile(`<video[^>]*?src="([^"]+)"`)
	reVideoTh  = regexp.MustCompile(`tgme_widget_message_video_thumb[^>]*?url\('([^']+)'\)`)
	reDuration = regexp.MustCompile(`video_duration[^>]*>([^<]+)<`)
	reBr       = regexp.MustCompile(`(?i)<br\s*/?>`)
	reTag      = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces   = regexp.MustCompile(`[ \t]+`)
	reNewlines = regexp.MustCompile(`\n{3,}`)
)

// ParseMessages extracts every post bubble from a t.me/s/<channel> page.
// Telegram renders posts oldest-first, and the returned slice keeps that order.
func ParseMessages(page string) []Message {
	locs := reDataPost.FindAllStringSubmatchIndex(page, -1)
	out := make([]Message, 0, len(locs))

	for i, loc := range locs {
		channel := page[loc[2]:loc[3]]
		id, err := strconv.Atoi(page[loc[4]:loc[5]])
		if err != nil {
			continue
		}

		// The bubble runs until the next bubble starts.
		end := len(page)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		block := page[loc[0]:end]

		msg := Message{
			ID:      id,
			Channel: channel,
			Text:    extractText(block),
			URL:     "https://t.me/" + channel + "/" + strconv.Itoa(id),
		}
		if m := reTime.FindStringSubmatch(block); m != nil {
			msg.Time = m[1]
		}
		if m := rePhoto.FindStringSubmatch(block); m != nil {
			msg.Photo = html.UnescapeString(m[1])
		}
		if m := reVideo.FindStringSubmatch(block); m != nil {
			msg.Video = html.UnescapeString(m[1])
		}
		if m := reVideoTh.FindStringSubmatch(block); m != nil {
			msg.VideoThumb = html.UnescapeString(m[1])
		}
		if m := reDuration.FindStringSubmatch(block); m != nil {
			msg.Duration = strings.TrimSpace(m[1])
		}
		out = append(out, msg)
	}
	return out
}

// extractText pulls the message body out of a bubble. Telegram nests arbitrary
// markup (links, bold, emoji spans) inside the text div, so we locate the
// opening tag and then walk forward counting div depth to find its real end.
func extractText(block string) string {
	marker := `class="tgme_widget_message_text`
	start := strings.Index(block, marker)
	if start < 0 {
		return ""
	}
	// Move past the end of the opening <div ...> tag.
	open := strings.Index(block[start:], ">")
	if open < 0 {
		return ""
	}
	cursor := start + open + 1

	depth := 1
	body := cursor
	for cursor < len(block) && depth > 0 {
		nextOpen := strings.Index(block[cursor:], "<div")
		nextClose := strings.Index(block[cursor:], "</div")
		if nextClose < 0 {
			break
		}
		if nextOpen >= 0 && nextOpen < nextClose {
			depth++
			cursor += nextOpen + 4
			continue
		}
		depth--
		if depth == 0 {
			return cleanText(block[body : cursor+nextClose])
		}
		cursor += nextClose + 5
	}
	return ""
}

// lineBreak is a placeholder that survives whitespace flattening, so only real
// <br> tags become newlines and HTML source indentation does not.
const lineBreak = "\x00BR\x00"

func cleanText(s string) string {
	s = reBr.ReplaceAllString(s, lineBreak)
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)

	// Collapse every kind of source whitespace into single spaces first.
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = reSpaces.ReplaceAllString(s, " ")

	s = strings.ReplaceAll(s, lineBreak, "\n")
	s = reNewlines.ReplaceAllString(s, "\n\n")

	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
