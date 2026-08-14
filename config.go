package main

import (
	"encoding/json"
	"os"
	"strings"
)

type Config struct {
	Channels         []string `json:"channels"`
	Channel          string   `json:"channel,omitempty"` // legacy single-channel field
	PollSeconds      int      `json:"poll_seconds"`
	Popups           bool     `json:"popups"`
	PopupSeconds     int      `json:"popup_seconds"`
	MaxPopupsPerScan int      `json:"max_popups_per_scan"`
	Sound            bool     `json:"sound"`
	Feed             bool     `json:"feed"`
	FeedPort         int      `json:"feed_port"`
	OpenFeedOnStart  bool     `json:"open_feed_on_start"`
	AppWindow        bool     `json:"app_window"`
	HistoryMessages  int      `json:"history_messages"`
	NotifyOnFirstRun bool     `json:"notify_on_first_run"`
	MutedChannels    []string `json:"muted_channels"`
	PinnedChannels   []string `json:"pinned_channels"`
	IncludeKeywords  []string `json:"include_keywords"`
	ExcludeKeywords  []string `json:"exclude_keywords"`
	SkipEmptyText    bool     `json:"skip_empty_text"`
	Theme            string   `json:"theme"`
	TelegramAppID    int      `json:"telegram_app_id"`
	TelegramAppHash  string   `json:"telegram_app_hash"`
	// UpdateURL is the raw-content base of the GitHub repo the desktop app
	// checks for new versions (version.json + downloads/TelegramPopup-Update.zip).
	UpdateURL string `json:"update_url"`
	// AllowChannelEdit unlocks adding/removing channels from the page. The
	// user asked for a LOCKED list, so the default (a missing field) is
	// locked; set to true in config.json to bring the +/✕ buttons back.
	AllowChannelEdit bool `json:"allow_channel_edit"`
}

func DefaultConfig() Config {
	return Config{
		Channels:         []string{},
		PollSeconds:      20,
		Popups:           true,
		PopupSeconds:     15,
		MaxPopupsPerScan: 3,
		Sound:            true,
		Feed:             true,
		FeedPort:         8420,
		OpenFeedOnStart:  true,
		AppWindow:        true,
		HistoryMessages:  60,
		MutedChannels:    []string{},
		PinnedChannels:   []string{},
		NotifyOnFirstRun: false,
		IncludeKeywords:  []string{},
		ExcludeKeywords:  []string{},
		SkipEmptyText:    true,
		Theme:            "dark",
		UpdateURL:        defaultUpdateURL,
	}
}

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Drop a starter file next to the executable so the user has
			// something to edit instead of a cryptic error.
			if data, mErr := json.MarshalIndent(cfg, "", "  "); mErr == nil {
				_ = os.WriteFile(path, data, 0o644)
			}
		}
		return cfg, err
	}

	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}

	// Legacy configs carried a single "channel" string — fold it in.
	if cfg.Channel != "" {
		cfg.Channels = append(cfg.Channels, cfg.Channel)
		cfg.Channel = ""
	}
	seen := map[string]bool{}
	var clean []string
	for _, ch := range cfg.Channels {
		ch = normalizeChannel(ch)
		if ch != "" && !seen[ch] {
			seen[ch] = true
			clean = append(clean, ch)
		}
	}
	cfg.Channels = clean

	if cfg.PollSeconds < 5 {
		cfg.PollSeconds = 5
	}
	if cfg.PopupSeconds < 3 {
		cfg.PopupSeconds = 3
	}
	if cfg.MaxPopupsPerScan < 1 {
		cfg.MaxPopupsPerScan = 1
	}
	if cfg.FeedPort < 1 || cfg.FeedPort > 65535 {
		cfg.FeedPort = 8420
	}
	if cfg.HistoryMessages < 0 {
		cfg.HistoryMessages = 0
	}
	if cfg.HistoryMessages > 500 {
		cfg.HistoryMessages = 500
	}
	if cfg.Theme != "light" {
		cfg.Theme = "dark"
	}
	if strings.TrimSpace(cfg.UpdateURL) == "" {
		cfg.UpdateURL = defaultUpdateURL
	}
	return cfg, nil
}

// defaultUpdateURL points at the user's public repo (raw content).
const defaultUpdateURL = "https://raw.githubusercontent.com/hanyna/telegram-popup/main"

// normalizeChannel accepts anything the user is likely to paste: a bare
// username, @username, t.me/username, or the full https://t.me/s/username URL.
func normalizeChannel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "www.")
	s = strings.TrimPrefix(s, "t.me/")
	s = strings.TrimPrefix(s, "telegram.me/")
	s = strings.TrimPrefix(s, "s/")
	s = strings.TrimPrefix(s, "@")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

// Wanted applies the keyword filters to a message. A media-only post (photo
// or video) counts as content, so it is not dropped by the empty-text guard.
func (c Config) Wanted(text string, hasMedia bool) bool {
	if c.SkipEmptyText && strings.TrimSpace(text) == "" && !hasMedia {
		return false
	}
	lower := strings.ToLower(text)

	for _, kw := range c.ExcludeKeywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return false
		}
	}

	active := false
	for _, kw := range c.IncludeKeywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
		active = true
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return !active
}

// saveConfigTelegram merges api_id/api_hash into the on-disk config without
// disturbing other fields — used by the login console.
func saveConfigTelegram(path string, appID int, appHash string) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return
	}
	cfg.TelegramAppID = appID
	cfg.TelegramAppHash = appHash
	if data, mErr := json.MarshalIndent(cfg, "", "  "); mErr == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}
