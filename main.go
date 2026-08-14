package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const version = "6.4"

// out is where all runtime chatter goes. The Windows build has no console at
// all, so everything lands in app.log next to the executable; on other
// systems it mirrors to stdout too.
var out io.Writer = os.Stdout

func logf(format string, args ...any) {
	fmt.Fprintf(out, format+"\n", args...)
}

// State remembers, per channel, the newest post already handled — so popups
// never repeat across restarts. (LastID is the pre-2.0 single-channel field,
// migrated on load.)
type State struct {
	LastID  int            `json:"last_id,omitempty"`
	LastIDs map[string]int `json:"last_ids"`
	// Channel display identities (name + avatar), remembered across restarts
	// so the sidebar shows real faces IMMEDIATELY on startup instead of
	// waiting for the (deliberately slow, throttle-safe) first scan.
	Infos map[string]ChannelInfo `json:"infos,omitempty"`
}

// runtimeSettings are the toggles the page can flip while the app runs; the
// scanner goroutine reads them and the HTTP goroutines write them.
type runtimeSettings struct {
	mu      sync.Mutex
	popups  bool
	sound   bool
	theme   string
	include []string // popup keyword filters, editable from the page
	exclude []string
	skipEmpty bool
}

func (r *runtimeSettings) getKeywords() (inc, exc []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.include...), append([]string{}, r.exclude...)
}

func (r *runtimeSettings) setKeywords(inc, exc []string) {
	r.mu.Lock()
	r.include = append([]string{}, inc...)
	r.exclude = append([]string{}, exc...)
	r.mu.Unlock()
}

// wanted applies the keyword filters to a message — same semantics as
// Config.Wanted, but reading the LIVE keyword lists the page can edit.
func (r *runtimeSettings) wanted(text string, hasMedia bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.skipEmpty && strings.TrimSpace(text) == "" && !hasMedia {
		return false
	}
	lower := strings.ToLower(text)
	for _, kw := range r.exclude {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return false
		}
	}
	active := false
	for _, kw := range r.include {
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

func (r *runtimeSettings) getTheme() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.theme
}

func (r *runtimeSettings) setTheme(t string) {
	r.mu.Lock()
	r.theme = t
	r.mu.Unlock()
}

func (r *runtimeSettings) get() (bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.popups, r.sound
}

func (r *runtimeSettings) set(popups, sound *bool) (bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if popups != nil {
		r.popups = *popups
	}
	if sound != nil {
		r.sound = *sound
	}
	return r.popups, r.sound
}

// channelSet is the runtime list of watched channels; the HTTP handlers
// mutate it from other goroutines, hence the lock.
type channelSet struct {
	mu    sync.Mutex
	names []string
}

func (s *channelSet) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.names))
	copy(out, s.names)
	return out
}

func (s *channelSet) add(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.names {
		if n == name {
			return false
		}
	}
	s.names = append(s.names, name)
	return true
}

func (s *channelSet) has(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, n := range s.names {
		if n == name {
			return true
		}
	}
	return false
}

func (s *channelSet) remove(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, n := range s.names {
		if n == name {
			s.names = append(s.names[:i], s.names[i+1:]...)
			return true
		}
	}
	return false
}

func main() {
	testMode := flag.Bool("test", false, "הצג התראה לדוגמה ובדוק חיבור לערוץ, ואז צא")
	loginMode := flag.Bool("login", false, "התחברות חד-פעמית לטלגרם (להורדת סרטונים ארוכים)")
	flag.Parse()

	dir := exeDir()
	cfgPath := filepath.Join(dir, "config.json")
	statePath := filepath.Join(dir, "state.json")

	// No console window exists on Windows — everything goes to app.log.
	if logFile, lErr := os.Create(filepath.Join(dir, "app.log")); lErr == nil {
		_, _ = logFile.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM for Notepad
		out = io.MultiWriter(os.Stdout, logFile)
		defer logFile.Close()
	}

	banner()
	cleanupLegacy(dir)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		// In the cloud there is no config file and no one to edit one — the
		// defaults plus TGPOPUP_CHANNELS are the configuration.
		if inCloud() {
			cfg = DefaultConfig()
			logf("אין config.json — מצב ענן ממשיך עם ברירות מחדל ומשתני סביבה")
		} else if os.IsNotExist(err) {
			alert("Telegram Popup",
				"נוצר קובץ config.json חדש בתיקייה.\nפתח אותו, מלא ערוץ בשדה \"channels\", והפעל שוב.")
			return
		} else {
			alert("Telegram Popup", "שגיאה בקריאת config.json:\n"+err.Error())
			return
		}
	}
	// Interactive Telegram login runs in its own console mode and exits.
	if *loginMode {
		runLogin(dir, cfg)
		return
	}

	// Cloud hosting (Render/Fly/Docker): headless, world-reachable, keyed.
	// On a desktop this is a no-op.
	cloudBind, cloudKey := applyCloudEnv(&cfg)
	if cloudBind != "" {
		logf("מצב ענן: מאזין על %s:%d · מפתח גישה: %v", cloudBind, cfg.FeedPort, cloudKey != "")
		if cloudKey == "" {
			logf("אזהרה: TGPOPUP_KEY לא הוגדר — הדף פתוח לכל מי שמנחש את הכתובת")
		}
	}

	if len(cfg.Channels) == 0 {
		alert("Telegram Popup",
			"רשימת הערוצים בקובץ config.json ריקה.\nמלא לפחות ערוץ ציבורי אחד, לדוגמה: \"channels\": [\"durov\"]")
		return
	}

	scriptPath, err := WritePopupScript(dir)
	if err != nil {
		alert("Telegram Popup", "לא הצלחתי לכתוב את popup.ps1:\n"+err.Error())
		return
	}
	tmpDir := filepath.Join(dir, "tmp")
	_ = os.MkdirAll(tmpDir, 0o755)

	channels := &channelSet{names: append([]string{}, cfg.Channels...)}
	muted := &channelSet{names: append([]string{}, cfg.MutedChannels...)}
	pinned := &channelSet{names: append([]string{}, cfg.PinnedChannels...)}
	settings := &runtimeSettings{
		popups: cfg.Popups, sound: cfg.Sound, theme: cfg.Theme,
		include: append([]string{}, cfg.IncludeKeywords...),
		exclude: append([]string{}, cfg.ExcludeKeywords...),
		skipEmpty: cfg.SkipEmptyText,
	}

	// Optional Telegram account client — enables large "too big" videos.
	tg := NewTGClient(cfg.TelegramAppID, cfg.TelegramAppHash,
		filepath.Join(dir, "telegram.session"), filepath.Join(dir, "cache"))
	if tg != nil && tg.HasSession() {
		logf("חשבון טלגרם מחובר — סרטונים ארוכים יורדו דרך ה-API")
	}

	// The permanent archive: every message ever seen, kept on disk forever.
	store, storeErr := NewStore(filepath.Join(dir, "history"))
	if storeErr != nil {
		logLine("ארכיון ההיסטוריה לא נפתח (%v) — ממשיכים בלעדיו", storeErr)
		store = nil
	} else {
		logf("ארכיון: %d הודעות שמורות", store.Count())
	}

	logf("ערוצים: %s", strings.Join(cfg.Channels, ", "))

	// The effective polling period is derived from the channel count, not taken
	// blindly from the config: eleven channels at the configured 15s meant
	// ~2,600 requests an hour and Telegram simply stopped answering.
	// Recomputed on every cycle, so adding channels from the page adjusts the
	// pace immediately — no restart needed.
	currentInterval := func() time.Duration {
		return pollInterval(cfg.PollSeconds, len(channels.list()))
	}
	logf("%s", describeRate(len(cfg.Channels), currentInterval()))
	if currentInterval() > time.Duration(cfg.PollSeconds)*time.Second {
		logf("(הקצב הותאם אוטומטית למספר הערוצים כדי שטלגרם לא תחסום)")
	}
	logf("חלונית נשארת %d שניות", cfg.PopupSeconds)
	if len(cfg.IncludeKeywords) > 0 {
		logf("סינון: רק הודעות שמכילות %s", strings.Join(cfg.IncludeKeywords, ", "))
	}

	// kick lets the HTTP layer trigger an immediate scan (e.g. right after a
	// channel is added from the page).
	kick := make(chan struct{}, 1)
	poke := func() {
		select {
		case kick <- struct{}{}:
		default:
		}
	}

	var persistMu sync.Mutex
	persist := func() {
		persistMu.Lock()
		defer persistMu.Unlock()
		cfg.Channels = channels.list()
		cfg.MutedChannels = muted.list()
		cfg.PinnedChannels = pinned.list()
		cfg.Popups, cfg.Sound = settings.get()
		cfg.Theme = settings.getTheme()
		cfg.IncludeKeywords, cfg.ExcludeKeywords = settings.getKeywords()
		if data, mErr := json.MarshalIndent(cfg, "", "  "); mErr == nil {
			_ = os.WriteFile(cfgPath, data, 0o644)
		}
	}

	// The live feed: a chat-style page in the browser, updating in real time.
	var feed *Feed
	if cfg.Feed {
		feed = NewFeed()
		feed.FetchOlder = func(channel string, beforeID int) ([]Message, error) {
			msgs, err := fetchChannelBefore(channel, beforeID)
			if err == nil && store != nil {
				store.Append(msgs) // history scrolled into view joins the archive
			}
			return msgs, err
		}
		feed.ListChannels = channels.list
		feed.AddChannel = func(raw string) (string, error) {
			name := normalizeChannel(raw)
			if name == "" {
				return "", errors.New("שם ערוץ ריק")
			}
			msgs, info, fErr := fetchChannel(name)
			if fErr != nil {
				return "", errors.New("לא הצלחתי להגיע לערוץ — בדוק את השם")
			}
			if len(msgs) == 0 {
				return "", errors.New("הערוץ לא ציבורי או שאין בו הודעות גלויות")
			}
			if !channels.add(name) {
				return "", errors.New("הערוץ כבר ברשימה")
			}
			feed.SetChannelInfo(name, info)
			persist()
			logLine("ערוץ נוסף מהדף: @%s", name)
			poke()
			return name, nil
		}
		feed.RemoveChannel = func(name string) error {
			if !channels.remove(name) {
				return errors.New("הערוץ לא נמצא ברשימה")
			}
			if store != nil {
				store.RemoveChannel(name)
			}
			persist()
			logLine("ערוץ הוסר מהדף: @%s", name)
			return nil
		}
		if store != nil {
			feed.Search = store.Search
			feed.ArchiveCount = store.Count
		}
		feed.Refresh = poke
		feed.ResolveVideo = fetchEmbedVideo
		if tg != nil {
			feed.HasTGAccount = tg.HasSession
			feed.BigVideo = func(channel string, id int) (string, error) {
				path, err := tg.DownloadVideo(channel, id)
				if err != nil {
					logLine("הורדת סרטון ארוך @%s/%d נכשלה: %v", channel, id, err)
				}
				return path, err
			}
		}
		feed.IsMuted = muted.has
		feed.SetMuted = func(name string, m bool) error {
			if m {
				muted.add(name)
			} else {
				muted.remove(name)
			}
			persist()
			logLine("השתקת @%s: %v", name, m)
			return nil
		}
		feed.IsPinned = pinned.has
		feed.SetPinned = func(name string, p bool) error {
			if p {
				pinned.add(name)
			} else {
				pinned.remove(name)
			}
			persist()
			logLine("נעיצת @%s: %v", name, p)
			return nil
		}
		feed.GetKeywords = settings.getKeywords
		feed.SetKeywords = func(inc, exc []string) {
			settings.setKeywords(inc, exc)
			persist()
			logLine("מילות מפתח עודכנו מהדף: %d לכלול, %d להחריג", len(inc), len(exc))
		}
		feed.GetSettings = settings.get
		feed.GetTheme = settings.getTheme
		feed.SetTheme = func(t string) { settings.setTheme(t); persist() }
		feed.GetAutostart = AutostartEnabled
		feed.SetAutostart = func(on bool) error {
			if err := SetAutostart(on); err != nil {
				return errors.New("שינוי ההפעלה האוטומטית נכשל")
			}
			logLine("הפעלה עם ווינדוס: %v", on)
			return nil
		}
		feed.SetSettings = func(popups, sound *bool) (bool, bool) {
			p, s := settings.set(popups, sound)
			persist()
			logLine("הגדרות עודכנו מהדף: פושים=%v צליל=%v", p, s)
			return p, s
		}

		feed.BindAddr = cloudBind
		feed.AccessKey = cloudKey
		if feedURL, fErr := feed.Start(cfg.FeedPort); fErr != nil {
			logLine("דף הערוץ החי לא עלה: %v", fErr)
			if strings.Contains(fErr.Error(), "in use") || strings.Contains(fErr.Error(), "Only one usage") {
				// The app is already running — so this launch simply becomes
				// "reopen the page": open the existing instance and exit.
				logLine("התוכנה כבר רצה — פותח את הדף הקיים ויוצא.")
				OpenBrowser("http://127.0.0.1:"+strconv.Itoa(cfg.FeedPort), cfg.AppWindow)
				return
			}
			feed = nil
		} else {
			logf("דף הערוץ החי: %s", feedURL)
			// A click on any popup opens OUR page — never Telegram.
			PopupClickURL = feedURL
			if !*testMode {
				StartTray(dir, TrayCallbacks{
					OpenPage: func() { OpenBrowser(feedURL, cfg.AppWindow) },
					PopupsOn: func() bool { p, _ := settings.get(); return p },
					TogglePops: func() {
						p, _ := settings.get()
						np := !p
						settings.set(&np, nil)
						persist()
					},
					Quit: func() { os.Exit(0) },
				})
			}
			if cfg.OpenFeedOnStart && !*testMode {
				OpenBrowser(feedURL, cfg.AppWindow)
			}
		}
	}

	if *testMode {
		runTest(cfg, scriptPath, tmpDir)
		return
	}

	state := loadState(statePath)
	seeded := map[string]bool{} // per channel, per startup: feed seeding done

	// Seed the page instantly from the on-disk archive — the network scan
	// then only tops it up. This is why history survives restarts.
	if feed != nil && store != nil {
		for _, ch := range channels.list() {
			if recent := store.LoadRecent(ch, cfg.HistoryMessages); len(recent) > 0 {
				feed.Seed(recent)
			}
		}
	}
	// Remembered identities: names and avatars appear the moment the page
	// opens, not 20 slow-paced seconds later.
	if feed != nil {
		for ch, info := range state.Infos {
			feed.SetChannelInfo(ch, info)
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// One background worker fills in history, slowly, without competing with
	// live scanning for Telegram's patience.
	startBackfillWorker(feed, store)

	// scanMu guards the state shared across concurrent channel scans:
	// per-channel last-id map, the "seeded" set, the saved state file, and the
	// popup slot counter.
	var scanMu sync.Mutex
	shownThisSweep := 0

	// scanChannel handles one channel inside one sweep. It is panic-proof:
	// whatever a malformed page manages to trigger, the app keeps running and
	// moves on to the next channel.
	scanChannel := func(ch string) {
		defer func() {
			if r := recover(); r != nil {
				logLine("@%s: תקלה פנימית נתפסה והמעקב ממשיך: %v", ch, r)
			}
		}()

		// Respect this channel's backoff — a channel Telegram is refusing must
		// not be retried on every tick, or the block never lifts.
		if ok, left := tgLimiter.ready(ch); !ok {
			logLine("@%s: בהמתנה אחרי חסימה — ניסיון הבא בעוד %.0f שניות", ch, left.Seconds())
			return
		}

		msgs, info, err := fetchChannel(ch)
		if err != nil {
			wait := tgLimiter.failure(ch, err, throttled(err))
			logLine("@%s: %v · ממתין %.0f שניות לפני ניסיון נוסף", ch, err, wait.Seconds())
			return
		}
		if feed != nil && info.Name != "" {
			feed.SetChannelInfo(ch, info) // avatar + display name appear now
		}
		if len(msgs) == 0 {
			// An empty page from a 200 response usually means Telegram served
			// the "please confirm you're human" shell, which is a throttle in
			// disguise — back off rather than pounding it.
			wait := tgLimiter.failure(ch, errEmptyPage, true)
			logLine("@%s: הדף נטען אך ריק (ככל הנראה הגבלת קצב) · ממתין %.0f שניות", ch, wait.Seconds())
			return
		}
		tgLimiter.success(ch, len(msgs))
		if store != nil {
			store.Append(msgs)
		}

		scanMu.Lock()
		last := state.LastIDs[ch]
		var fresh []Message
		for _, m := range msgs {
			if m.ID > last {
				fresh = append(fresh, m)
			}
		}
		sort.Slice(fresh, func(i, j int) bool { return fresh[i].ID < fresh[j].ID })
		// Persist identity only on meaningful change (name, or photo presence)
		// — Telegram rotates photo URLs every fetch, and rewriting state.json
		// twelve times a sweep for cosmetic URL churn helps no one. The
		// freshest URL still lands in memory for the avatar cache to use.
		prev := state.Infos[ch]
		infoChanged := info.Name != "" &&
			(prev.Name != info.Name || (prev.Photo == "") != (info.Photo == ""))
		if info.Name != "" {
			state.Infos[ch] = info
		}
		if len(fresh) > 0 {
			state.LastIDs[ch] = fresh[len(fresh)-1].ID
		}
		if len(fresh) > 0 || infoChanged {
			saveState(statePath, state)
		}
		firstSeed := !seeded[ch]
		seeded[ch] = true
		scanMu.Unlock()

		// Seed the feed on every startup (and for every newly added channel).
		// feed.Add broadcasts; a tab that already shows these dedupes by key,
		// so nothing re-lights on restart, while a just-opened empty tab fills.
		if feed != nil {
			if firstSeed {
				feed.Add(msgs)
				if cfg.HistoryMessages > len(msgs) {
					oldest := msgs[0].ID
					for _, m := range msgs {
						if m.ID < oldest {
							oldest = m.ID
						}
					}
					// Queued, not launched. Eleven channels each spawning their
					// own history crawler on startup was a request storm on top
					// of a request storm; one worker drains this slowly in the
					// background while live scanning keeps priority.
					backfillQueue <- backfillJob{
						channel: ch,
						target:  cfg.HistoryMessages - len(msgs),
						oldest:  oldest,
					}
				}
			} else {
				feed.Add(fresh)
			}
		}

		// First sight of this channel: sync silently so the user is not
		// flooded with its whole history as popups.
		if last == 0 && !cfg.NotifyOnFirstRun {
			logLine("@%s: סנכרון ראשוני הושלם.", ch)
			return
		}

		popupsOn, soundOn := settings.get()
		if !popupsOn || muted.has(ch) {
			return
		}
		for _, m := range fresh {
			// Live keyword filters (editable from the page), not the boot-time
			// config snapshot.
			if !settings.wanted(m.Text, m.HasMedia()) {
				continue
			}
			scanMu.Lock()
			if shownThisSweep >= cfg.MaxPopupsPerScan {
				scanMu.Unlock()
				break
			}
			slot := shownThisSweep
			shownThisSweep++
			scanMu.Unlock()

			popupTitle := "@" + m.Channel
			if feed != nil {
				if info, ok := feed.GetChannelInfo(m.Channel); ok && info.Name != "" {
					popupTitle = info.Name
				}
			}
			if err := ShowPopup(scriptPath, tmpDir, popupTitle, m, cfg.PopupSeconds, slot, soundOn); err != nil {
				logLine("שגיאה בהצגת חלונית: %v", err)
				continue
			}
			logLine("@%s: הודעה חדשה #%d: %s", m.Channel, m.ID, preview(m.Text))
			time.Sleep(400 * time.Millisecond)
		}
	}

	// scan sweeps the channels one at a time, spaced out by the global pacer.
	//
	// An earlier version fired five fetches at once to stop a slow channel
	// blocking the rest. That fixed the wrong problem: bursting is exactly
	// what makes Telegram start refusing, and a refused channel returns
	// instantly — so the "slow channel" case largely vanishes once the pace is
	// civil. Sequential and paced beats parallel and blocked.
	//
	// Ordering rotates each sweep so the same channel is not always first in
	// line for the popup budget.
	sweep := 0
	scan := func() {
		scanMu.Lock()
		shownThisSweep = 0
		scanMu.Unlock()

		list := channels.list()
		if len(list) == 0 {
			return
		}
		off := sweep % len(list)
		sweep++
		for i := range list {
			scanChannel(list[(i+off)%len(list)])
		}
	}

	// Background retries for videos that were not yet available — Telegram
	// usually finishes processing fresh uploads within minutes.
	if feed != nil {
		go func() {
			retryTicker := time.NewTicker(3 * time.Minute)
			defer retryTicker.Stop()
			for range retryTicker.C {
				feed.RetryPending()
			}
		}()
	}

	scan()
	for {
		// time.After (not a fixed Ticker) so the interval tracks the live
		// channel count — add a channel from the page, the pace adapts now.
		select {
		case <-time.After(currentInterval()):
			scan()
		case <-kick:
			scan()
		case <-stop:
			logf("נעצר. להתראות.")
			return
		}
	}
}

func runTest(cfg Config, scriptPath, tmpDir string) {
	ch := cfg.Channels[0]
	logf("מצב בדיקה — מושך את @%s פעם אחת ומציג את ההודעה האחרונה.", ch)
	msgs, _, err := fetchChannel(ch)
	if err != nil {
		alert("Telegram Popup — בדיקה", "משיכת הערוץ נכשלה:\n"+err.Error())
		return
	}
	if len(msgs) == 0 {
		alert("Telegram Popup — בדיקה",
			"הדף נטען אבל לא נמצאו הודעות.\nסיבה נפוצה: הערוץ פרטי, או שתצוגה מקדימה מושבתת בהגדרותיו.")
		return
	}
	logf("נמצאו %d הודעות. האחרונה היא #%d.", len(msgs), msgs[len(msgs)-1].ID)
	last := msgs[len(msgs)-1]
	if err := ShowPopup(scriptPath, tmpDir, "@"+ch, last, cfg.PopupSeconds, 0, cfg.Sound); err != nil {
		alert("Telegram Popup — בדיקה", "שגיאה בהצגת החלונית:\n"+err.Error())
		return
	}
	time.Sleep(time.Duration(cfg.PopupSeconds+2) * time.Second)
	alert("Telegram Popup — בדיקה",
		fmt.Sprintf("החיבור לערוץ @%s תקין — נמצאו %d הודעות (אחרונה #%d).\nאם ראית את החלונית בפינת המסך, הכל מוכן.",
			ch, len(msgs), msgs[len(msgs)-1].ID))
}

func fetchChannel(channel string) ([]Message, ChannelInfo, error) {
	// Escape hatch used for local testing; unset in normal operation.
	if base := os.Getenv("TGPOPUP_BASE_URL"); base != "" {
		return fetchURL(strings.TrimRight(base, "/") + "/" + channel)
	}
	return fetchURL("https://t.me/s/" + channel)
}

// fetchChannelBefore pages backwards: Telegram serves the ~20 posts that
// precede the given ID when asked with ?before=.
func fetchChannelBefore(channel string, beforeID int) ([]Message, error) {
	var msgs []Message
	var err error
	if base := os.Getenv("TGPOPUP_BASE_URL"); base != "" {
		msgs, _, err = fetchURL(strings.TrimRight(base, "/") + "/" + channel + "?before=" + strconv.Itoa(beforeID))
	} else {
		msgs, _, err = fetchURL("https://t.me/s/" + channel + "?before=" + strconv.Itoa(beforeID))
	}
	return msgs, err
}

// backfillJob is one channel's history-filling request, drained by a single
// background worker so history crawling never competes with live scanning.
type backfillJob struct {
	channel string
	target  int
	oldest  int
}

// Buffered generously: enqueueing must never block a scan.
var backfillQueue = make(chan backfillJob, 64)

// startBackfillWorker drains the queue with exactly one job in flight. Between
// jobs it pauses, so filling history stays a background courtesy rather than a
// second source of load.
func startBackfillWorker(feed *Feed, store *Store) {
	go func() {
		for job := range backfillQueue {
			if feed == nil {
				continue
			}
			backfillHistory(feed, store, job.channel, job.target, job.oldest)
			time.Sleep(5 * time.Second)
		}
	}()
}

// backfillHistory tops the feed up with older posts, page by page, until the
// target count is reached or the channel has nothing further back. Everything
// it fetches also lands in the permanent archive.
func backfillHistory(feed *Feed, store *Store, channel string, target, oldest int) {
	have := 0
	for have < target && oldest > 1 {
		// A channel currently being refused gets no history crawling either.
		if ok, _ := tgLimiter.ready(channel); !ok {
			return
		}
		page, err := fetchChannelBefore(channel, oldest)
		if err != nil || len(page) == 0 {
			return
		}
		if store != nil {
			store.Append(page)
		}
		added, minID := feed.AddOlder(page)
		if added == 0 {
			return // nothing new — reached the start of the channel
		}
		have += added
		oldest = minID
		time.Sleep(3 * time.Second) // be polite to Telegram
	}
}

const desktopUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
const mobileUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"

// fetchRaw downloads one page as text with browser-like headers.
func fetchRaw(url string) (string, error) { return fetchRawUA(url, desktopUA) }

func fetchRawUA(url, ua string) (string, error) {
	// Every outbound request to Telegram passes the global pacer first. This
	// is the single choke point that keeps the app from looking like a crawler.
	tgLimiter.wait()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept-Language", "he,en;q=0.8")
	// Browser-shaped headers: a bare Go request is trivially fingerprintable.
	// NOTE: Accept-Encoding is deliberately NOT set. Go's transport adds gzip
	// and transparently decompresses it only while the caller leaves the
	// header alone; setting it by hand hands back raw compressed bytes.
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", &httpStatusError{Code: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func fetchURL(url string) ([]Message, ChannelInfo, error) {
	page, err := fetchRaw(url)
	if err != nil {
		return nil, ChannelInfo{}, err
	}
	return ParseMessages(page), ParseChannelInfo(page), nil
}

// fetchEmbedVideo resolves the direct mp4 URL of a post whose video the /s
// listing exposes only as a thumbnail. Two independent attempts: the
// single-post embed page, and the listing page focused on that post — one of
// them carries the <video> tag for almost every channel video.
func fetchEmbedVideo(channel string, id int) (string, error) {
	base := os.Getenv("TGPOPUP_BASE_URL")

	embedURL := "https://t.me/" + channel + "/" + strconv.Itoa(id) + "?embed=1&mode=tme"
	if base != "" {
		embedURL = strings.TrimRight(base, "/") + "/" + channel + "/" + strconv.Itoa(id) + "?embed=1&mode=tme"
	}
	if page, err := fetchRaw(embedURL); err == nil {
		if src := ExtractVideoSrc(page); src != "" {
			return src, nil
		}
	}

	// Second angle: the listing page centered on this post sometimes inlines
	// the very video the default listing withheld.
	listURL := "https://t.me/s/" + channel + "?before=" + strconv.Itoa(id+1)
	if base != "" {
		listURL = strings.TrimRight(base, "/") + "/" + channel + "?before=" + strconv.Itoa(id+1)
	}
	if page, err := fetchRaw(listURL); err == nil {
		for _, m := range ParseMessages(page) {
			if m.ID == id && m.Video != "" {
				return m.Video, nil
			}
		}
	}

	// Third angle: grouped-media posts sometimes expose the file only when
	// asked for the single item.
	if page, err := fetchRaw(embedURL + "&single=1"); err == nil {
		if src := ExtractVideoSrc(page); src != "" {
			return src, nil
		}
	}

	// Fourth angle: the plain post page publishes the video in OpenGraph /
	// twitter:player meta.
	postURL := "https://t.me/" + channel + "/" + strconv.Itoa(id)
	if base != "" {
		postURL = strings.TrimRight(base, "/") + "/" + channel + "/" + strconv.Itoa(id) + "?plain=1"
	}
	if page, err := fetchRaw(postURL); err == nil {
		if src := ExtractVideoSrc(page); src != "" {
			return src, nil
		}
	}

	// Fifth angle: same post page, but as a mobile browser — Telegram serves
	// different markup to phones and occasionally includes the file there.
	if page, err := fetchRawUA(postURL, mobileUA); err == nil {
		if src := ExtractVideoSrc(page); src != "" {
			return src, nil
		}
	}
	return "", errors.New("הסרטון לא זמין לצפייה ישירה")
}

func loadState(path string) State {
	var s State
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &s)
	}
	if s.LastIDs == nil {
		s.LastIDs = map[string]int{}
	}
	if s.Infos == nil {
		s.Infos = map[string]ChannelInfo{}
	}
	// Pre-2.0 state had a single last_id with no channel attached; it is
	// dropped rather than guessed — worst case is one silent re-sync.
	s.LastID = 0
	return s
}

func saveState(path string, s State) {
	if data, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

// cleanupLegacy removes leftovers that older versions scattered in the app
// folder: the Hebrew-named launcher files from before the ASCII rename (both
// their proper names and the garbled forms broken ZIP extraction produced),
// interrupted-update backups, and stale popup temp files. Settings and state
// are never touched.
func cleanupLegacy(dir string) {
	// Exact names older versions shipped.
	// (TelegramPopup.new is handled separately below — it may be a pending
	// update the user extracted but has not run yet.)
	for _, n := range []string{"הפעל.bat", "בדיקה.bat", "עצור.bat", "הפעל-ברקע.vbs", "הוראות.md", "TelegramPopup.old", "AddChannels.bat"} {
		_ = os.Remove(filepath.Join(dir, n))
	}

	// A TelegramPopup.new that is byte-identical to the running program is a
	// leftover of an update that was already applied (or extracted twice) —
	// safe to remove. A DIFFERENT .new is a pending update and is kept.
	if exe, err := os.Executable(); err == nil {
		newPath := filepath.Join(dir, "TelegramPopup.new")
		if sameFileContent(exe, newPath) {
			_ = os.Remove(newPath)
		}
	}
	// Launcher files whose names came out as mojibake on old extractions:
	// any .bat/.vbs with non-ASCII characters in the name is one of ours.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			name := e.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if ext != ".bat" && ext != ".vbs" {
				continue
			}
			for _, r := range name {
				if r > 127 {
					_ = os.Remove(filepath.Join(dir, name))
					break
				}
			}
		}
	}
	// Popup temp files that somehow outlived their popup.
	tmp := filepath.Join(dir, "tmp")
	if entries, err := os.ReadDir(tmp); err == nil {
		for _, e := range entries {
			if info, iErr := e.Info(); iErr == nil && time.Since(info.ModTime()) > 24*time.Hour {
				_ = os.Remove(filepath.Join(tmp, e.Name()))
			}
		}
	}
}

// sameFileContent reports whether two files hold identical bytes. Cheap size
// check first; full hash only when sizes match.
func sameFileContent(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil || ia.Size() != ib.Size() {
		return false
	}
	ha, err := hashFile(a)
	if err != nil {
		return false
	}
	hb, err := hashFile(b)
	if err != nil {
		return false
	}
	return ha == hb
}

func hashFile(path string) (string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	return filepath.Dir(exe)
}

func banner() {
	logf("=====================================")
	logf("  Telegram Popup  v%s", version)
	logf("  התראות מערוצי טלגרם ציבוריים")
	logf("=====================================")
}

func logLine(format string, args ...any) {
	logf("[%s] %s", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

func preview(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

func prettyTime(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	return t.Local().Format("15:04")
}
