package main

import (
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// HTML notification card
// ---------------------------------------------------------------------------
// The popup body is real HTML rendered by the .NET WebBrowser control in IE11
// mode. The CSS below is deliberately written against the IE11 feature set
// (flexbox, gradients, keyframe animations — but no CSS variables and no
// template-literal JS), so it renders identically without any external
// runtime. The host window supplies borderless chrome, rounded corners and
// always-on-top; everything visual lives here.

const htmlTemplate = `<!DOCTYPE html>
<html lang="he" dir="rtl">
<head>
<meta charset="utf-8">
<meta http-equiv="x-ua-compatible" content="IE=edge">
<title>n</title>
<style>
  * { margin:0; padding:0; box-sizing:border-box; }
  html,body { overflow:hidden; background:#141824; }
  body {
    font-family:'Segoe UI','Segoe UI Emoji',Arial,sans-serif;
    direction:rtl; cursor:pointer;
    -ms-user-select:none; user-select:none;
  }
  #card {
    position:relative;
    border-radius:14px; overflow:hidden;
    background:#1a1f2e;
    background:linear-gradient(160deg,#222941 0%,#181c2b 55%,#141824 100%);
  }
  .accent { height:3px; background:linear-gradient(90deg,#37aee2 0%,#31c48d 100%); }

  .header { display:flex; align-items:center; padding:13px 16px 7px 12px; }
  .avatar {
    width:40px; height:40px; border-radius:50%;
    background:linear-gradient(135deg,#37aee2 0%,#1e88d2 100%);
    color:#fff; font-weight:600; font-size:18px;
    text-align:center; line-height:40px;
    margin-left:11px;
    box-shadow:0 2px 8px rgba(30,136,210,0.35);
  }
  .headtext { flex:1 1 auto; min-width:0; }
  .chname {
    font-size:14px; font-weight:600; color:#f2f4fa;
    white-space:nowrap; overflow:hidden; text-overflow:ellipsis;
  }
  .meta { font-size:11px; color:#8b93a9; margin-top:1px; }
  .close {
    width:26px; height:26px; line-height:24px;
    text-align:center; border-radius:6px;
    color:#8b93a9; font-size:13px; cursor:pointer;
  }
  .close:hover { background:rgba(255,255,255,0.08); color:#fff; }

  .body {
    padding:5px 18px 12px;
    color:#e9ecf5; font-size:14px; line-height:1.62;
    word-wrap:break-word;
    max-height:300px; overflow:hidden;
  }
  .body a { color:#5cb8ff; text-decoration:none; border-bottom:1px solid rgba(92,184,255,0.35); }
  .photo { padding:2px 18px 12px; }
  .photo img, .photo video {
    width:100%; display:block; border-radius:9px;
    border:1px solid rgba(255,255,255,0.07);
  }
  /* Fixed 16:9 box: the card height is known before the media even starts
     downloading, so the window never resizes mid-display. */
  .vidwrap {
    position:relative; height:0; padding-bottom:56.25%; overflow:hidden;
    border-radius:9px; border:1px solid rgba(255,255,255,0.07);
    background:#0d1120;
  }
  .vidwrap video {
    position:absolute; top:0; left:0; width:100%; height:100%;
    border:none; border-radius:0;
  }
  .vidwrap img {
    position:absolute; top:50%; left:0; width:100%; height:auto;
    transform:translateY(-50%); -ms-transform:translateY(-50%);
    border:none; border-radius:0;
  }
  .playbtn {
    position:absolute; top:50%; left:50%;
    width:58px; height:58px; margin:-29px 0 0 -29px;
    border-radius:50%;
    background:rgba(16,20,32,0.72);
    border:2px solid rgba(255,255,255,0.9);
  }
  .playbtn span {
    position:absolute; top:50%; left:50%;
    margin:-11px 0 0 -7px;
    width:0; height:0;
    border-top:11px solid transparent;
    border-bottom:11px solid transparent;
    border-right:none;
    border-left:19px solid #ffffff;
  }
  .durbadge {
    position:absolute; bottom:9px; left:9px;
    direction:ltr;
    background:rgba(10,13,22,0.72);
    color:#fff; font-size:11px;
    padding:2px 8px; border-radius:10px;
  }

  .footer {
    display:flex; align-items:center; justify-content:space-between;
    padding:0 18px 12px; color:#6f778f; font-size:11px;
  }
  .brand { color:#4d5670; }

  .progresswrap { position:absolute; bottom:0; left:0; right:0; height:3px; background:rgba(255,255,255,0.05); }
  #bar { height:3px; width:100%; background:linear-gradient(90deg,#37aee2,#31c48d); }

  @keyframes rise { from { opacity:0; transform:translateY(10px); } to { opacity:1; transform:translateY(0); } }
  #card { animation:rise 0.28s ease-out; }
</style>
</head>
<body onclick="openIt()" oncontextmenu="return false;">
<div id="card">
  <div class="accent"></div>
  <div class="header">
    <div class="avatar">%%INITIAL%%</div>
    <div class="headtext">
      <div class="chname">%%TITLE%%</div>
      <div class="meta">%%STAMP%%</div>
    </div>
    <div class="close" onclick="closeBtn()">&#10005;</div>
  </div>
  %%PHOTO%%
  %%BODY%%
  <div class="footer">
    <span>לחיצה פותחת את דף הערוץ</span>
    <span class="brand">Telegram Popup</span>
  </div>
  <div class="progresswrap"><div id="bar"></div></div>
</div>
<script>
  var TOTAL = %%SECONDS%% * 1000;
  var left = TOTAL, paused = false, done = false;

  function tryExt(fn) { try { fn(); } catch (e) {} }
  function closeWin() { if (done) return; done = true; tryExt(function(){ window.external.CloseWin(); }); }
  function openIt()   { if (done) return; done = true; tryExt(function(){ window.external.OpenLink(); }); }
  function closeBtn() {
    if (window.event) { window.event.cancelBubble = true; }
    closeWin();
  }
  function openUrl(u) {
    if (window.event) { window.event.cancelBubble = true; }
    tryExt(function(){ window.external.OpenUrl(u); });
    return false;
  }

  document.body.onmouseover = function () { paused = true; };
  document.body.onmouseout  = function () { paused = false; };

  window.onload = function () {
    var card = document.getElementById('card');
    // Convert CSS pixels to device pixels so the host window matches the
    // card exactly on scaled displays (125%/150% DPI).
    var ratio = 1;
    try { ratio = screen.deviceXDPI / screen.logicalXDPI; } catch (e) {}
    if (!ratio || ratio < 0.5) { ratio = 1; }
    tryExt(function(){ window.external.SetHeight(Math.ceil(card.offsetHeight * ratio)); });
    var bar = document.getElementById('bar');
    setInterval(function () {
      if (paused || done) { return; }
      left -= 100;
      var pct = (left / TOTAL) * 100;
      if (pct < 0) { pct = 0; }
      bar.style.width = pct + '%';
      if (left <= 0) { closeWin(); }
    }, 100);
  };
</script>
</body>
</html>`

var reURL = regexp.MustCompile(`https?://[^\s<>"']+`)

// BuildPopupHTML renders one message into the notification card.
func BuildPopupHTML(title string, msg Message, stamp string, seconds int) string {
	page := htmlTemplate

	initial := "T"
	for _, r := range strings.TrimPrefix(title, "@") {
		initial = strings.ToUpper(string(r))
		break
	}

	bodyHTML := ""
	if strings.TrimSpace(msg.Text) != "" {
		bodyHTML = `<div class="body">` + richText(msg.Text) + `</div>`
	}

	photoHTML := mediaHTML(msg)

	r := strings.NewReplacer(
		"%%INITIAL%%", html.EscapeString(initial),
		"%%TITLE%%", html.EscapeString(title),
		"%%STAMP%%", html.EscapeString(stamp),
		"%%PHOTO%%", photoHTML,
		"%%BODY%%", bodyHTML,
		"%%SECONDS%%", strconv.Itoa(maxInt(seconds, 3)),
	)
	return r.Replace(page)
}

// mediaHTML renders the visual attachment of a post, by preference order:
// an inline video plays silently in the card itself; a video that Telegram
// serves only as a preview frame gets that frame plus a play button (clicking
// anywhere opens the post, where the full video lives); a photo is embedded
// directly.
func mediaHTML(msg Message) string {
	badge := ""
	if msg.Duration != "" {
		badge = `<span class="durbadge">` + html.EscapeString(msg.Duration) + `</span>`
	}

	switch {
	case msg.Video != "":
		poster := ""
		if msg.VideoThumb != "" {
			poster = ` poster="` + html.EscapeString(msg.VideoThumb) + `"`
		}
		return `<div class="photo"><div class="vidwrap">` +
			`<video src="` + html.EscapeString(msg.Video) + `"` + poster +
			` autoplay muted loop playsinline></video>` + badge + `</div></div>`

	case msg.VideoThumb != "":
		return `<div class="photo"><div class="vidwrap">` +
			`<img src="` + html.EscapeString(msg.VideoThumb) + `" alt="">` +
			`<div class="playbtn"><span></span></div>` + badge + `</div></div>`

	case msg.Photo != "":
		return `<div class="photo"><img src="` + html.EscapeString(msg.Photo) + `" alt=""></div>`
	}
	return ""
}

// richText escapes the message body, then turns bare URLs into clickable
// links and newlines into <br>. Escaping happens per-fragment so a URL is
// never half-escaped.
func richText(text string) string {
	var b strings.Builder
	last := 0
	for _, loc := range reURL.FindAllStringIndex(text, -1) {
		b.WriteString(htmlWithBreaks(text[last:loc[0]]))
		raw := text[loc[0]:loc[1]]
		safe := strings.ReplaceAll(raw, "'", "%27")
		b.WriteString(`<a href="#" onclick="return openUrl('` + html.EscapeString(safe) + `');">` + html.EscapeString(trimDisplayURL(raw)) + `</a>`)
		last = loc[1]
	}
	b.WriteString(htmlWithBreaks(text[last:]))
	return b.String()
}

func htmlWithBreaks(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "\n", "<br>")
}

func trimDisplayURL(u string) string {
	d := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	r := []rune(d)
	if len(r) > 42 {
		return string(r[:42]) + "…"
	}
	return d
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Host window (PowerShell)
// ---------------------------------------------------------------------------
// popup.ps1 opens a borderless, rounded, always-on-top window and renders the
// HTML card inside the .NET WebBrowser control forced into IE11 mode (a
// per-user registry switch — no admin rights). If anything in that path
// fails, it falls back to a plain WinForms rendering of the same message, so
// a notification always appears.

const popupScript = `param(
  [string]$HtmlFile = "",
  [string]$TextFile = "",
  [string]$Title = "Telegram",
  [string]$Url = "",
  [string]$Stamp = "",
  [int]$Seconds = 15,
  [int]$Offset = 0,
  [int]$Sound = 1
)

Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

# --- IE11 mode for the embedded browser (per-user, no admin needed) --------
try {
  $fc = 'HKCU:\Software\Microsoft\Internet Explorer\Main\FeatureControl\FEATURE_BROWSER_EMULATION'
  if (-not (Test-Path $fc)) { New-Item -Path $fc -Force | Out-Null }
  New-ItemProperty -Path $fc -Name 'powershell.exe' -Value 11001 -PropertyType DWord -Force | Out-Null
} catch {}

# --- JS <-> host bridge ----------------------------------------------------
$bridgeSrc = @'
using System;
using System.Runtime.InteropServices;

[ComVisible(true)]
public class PopupBridge
{
    public Action CloseAction;
    public Action OpenAction;
    public Action<string> OpenUrlAction;
    public Action<int> HeightAction;

    public void CloseWin()        { var a = CloseAction;   if (a != null) a(); }
    public void OpenLink()        { var a = OpenAction;    if (a != null) a(); }
    public void OpenUrl(string u) { var a = OpenUrlAction; if (a != null) a(u); }
    public void SetHeight(int h)  { var a = HeightAction;  if (a != null) a(h); }
}
'@
$haveBridge = $true
try { Add-Type -TypeDefinition $bridgeSrc -Language CSharp -ErrorAction Stop } catch {
  if (-not ([System.Management.Automation.PSTypeName]'PopupBridge').Type) { $haveBridge = $false }
}

$W = 424
$R = 14

function Set-RoundRegion([System.Windows.Forms.Form]$f, [int]$rad) {
  $gp = New-Object System.Drawing.Drawing2D.GraphicsPath
  $d = $rad * 2
  $gp.AddArc(0, 0, $d, $d, 180, 90)
  $gp.AddArc($f.Width - $d, 0, $d, $d, 270, 90)
  $gp.AddArc($f.Width - $d, $f.Height - $d, $d, $d, 0, 90)
  $gp.AddArc(0, $f.Height - $d, $d, $d, 90, 90)
  $gp.CloseFigure()
  $f.Region = New-Object System.Drawing.Region($gp)
}

function Place-Window([System.Windows.Forms.Form]$f, [int]$slot) {
  $area = [System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea
  $f.Left = $area.Right - $f.Width - 16
  $f.Top  = $area.Bottom - $f.Height - 16 - ($slot * ($f.Height + 10))
  if ($f.Top -lt $area.Top) { $f.Top = $area.Top + 8 }
}

# ===========================================================================
#  Primary path: HTML card in the embedded browser
# ===========================================================================
function Show-Html {
  $script:closing = $false

  $form                 = New-Object System.Windows.Forms.Form
  $form.FormBorderStyle = 'None'
  $form.BackColor       = [System.Drawing.Color]::FromArgb(20, 24, 36)
  $form.TopMost         = $true
  $form.ShowInTaskbar   = $false
  $form.StartPosition   = 'Manual'
  $form.Width           = $W
  $form.Height          = 200
  $form.Opacity         = 0

  $wb = New-Object System.Windows.Forms.WebBrowser
  $wb.Dock                            = 'Fill'
  $wb.ScriptErrorsSuppressed          = $true
  $wb.ScrollBarsEnabled               = $false
  $wb.AllowWebBrowserDrop             = $false
  $wb.IsWebBrowserContextMenuEnabled  = $false
  $wb.WebBrowserShortcutsEnabled      = $false
  $form.Controls.Add($wb)

  $fadeOut = New-Object System.Windows.Forms.Timer
  $fadeOut.Interval = 25
  $fadeOut.Add_Tick({
    if ($form.Opacity -le 0.08) { $fadeOut.Stop(); $form.Close() }
    else { $form.Opacity = $form.Opacity - 0.12 }
  }.GetNewClosure())

  $dismiss = {
    if ($script:closing) { return }
    $script:closing = $true
    $fadeOut.Start()
  }.GetNewClosure()

  $bridge = New-Object PopupBridge
  $bridge.CloseAction = [Action]$dismiss
  $bridge.OpenAction = [Action]{
    if ($Url -ne '') { try { Start-Process $Url } catch {} }
    & $dismiss
  }.GetNewClosure()
  $bridge.OpenUrlAction = [Action[string]]{
    param($u)
    if ($u -match '^https?://') { try { Start-Process $u } catch {} }
  }
  $bridge.HeightAction = [Action[int]]{
    param($h)
    if ($h -lt 90) { $h = 90 }
    if ($h -gt 560) { $h = 560 }
    $form.Height = $h
    Place-Window $form $Offset
    Set-RoundRegion $form $R
    if ($form.Opacity -lt 0.9) { $form.Opacity = 0.985 }
  }.GetNewClosure()
  $wb.ObjectForScripting = $bridge

  # Failsafe: show even if the page never reports its height.
  $reveal = New-Object System.Windows.Forms.Timer
  $reveal.Interval = 1600
  $reveal.Add_Tick({
    $reveal.Stop()
    if ($form.Opacity -lt 0.9) {
      $form.Height = 210
      Place-Window $form $Offset
      Set-RoundRegion $form $R
      $form.Opacity = 0.985
    }
  }.GetNewClosure())

  # Failsafe: never outlive the intended lifetime by much, even if JS died.
  $life = New-Object System.Windows.Forms.Timer
  $life.Interval = ([Math]::Max($Seconds, 3) + 8) * 1000
  $life.Add_Tick({ $life.Stop(); & $dismiss }.GetNewClosure())

  $form.Add_Shown({
    $reveal.Start()
    $life.Start()
    if ($Sound -eq 1) { [System.Media.SystemSounds]::Asterisk.Play() }
  }.GetNewClosure())

  $wb.Navigate($HtmlFile)
  [void]$form.ShowDialog()
}

# ===========================================================================
#  Fallback path: plain WinForms card (no browser involved)
# ===========================================================================
function Show-Classic {
  $body = ''
  if ($TextFile -ne '' -and (Test-Path $TextFile)) {
    $body = [System.IO.File]::ReadAllText($TextFile, [System.Text.Encoding]::UTF8)
  }
  if ([string]::IsNullOrWhiteSpace($body)) { $body = '(הודעה ללא טקסט)' }
  if ($body.Length -gt 700) { $body = $body.Substring(0, 700) + '…' }

  $bg     = [System.Drawing.Color]::FromArgb(24, 28, 43)
  $accent = [System.Drawing.Color]::FromArgb(55, 174, 226)
  $fg     = [System.Drawing.Color]::FromArgb(240, 240, 245)
  $muted  = [System.Drawing.Color]::FromArgb(139, 147, 169)

  $form                   = New-Object System.Windows.Forms.Form
  $form.FormBorderStyle   = 'None'
  $form.BackColor         = $bg
  $form.TopMost           = $true
  $form.ShowInTaskbar     = $false
  $form.StartPosition     = 'Manual'
  $form.Width             = $W
  $form.Opacity           = 0.97
  $form.RightToLeft       = 'Yes'
  $form.RightToLeftLayout = $true

  $stripe           = New-Object System.Windows.Forms.Panel
  $stripe.Height    = 3
  $stripe.Dock      = 'Top'
  $stripe.BackColor = $accent
  $form.Controls.Add($stripe)

  $lblTitle           = New-Object System.Windows.Forms.Label
  $lblTitle.Text      = $Title
  $lblTitle.Font      = New-Object System.Drawing.Font('Segoe UI Semibold', 11)
  $lblTitle.ForeColor = $accent
  $lblTitle.AutoSize  = $false
  $lblTitle.Location  = New-Object System.Drawing.Point(18, 14)
  $lblTitle.Size      = New-Object System.Drawing.Size(($W - 50), 24)
  $lblTitle.TextAlign = 'MiddleRight'
  $form.Controls.Add($lblTitle)

  $lblBody           = New-Object System.Windows.Forms.Label
  $lblBody.Text      = $body
  $lblBody.Font      = New-Object System.Drawing.Font('Segoe UI', 10.5)
  $lblBody.ForeColor = $fg
  $lblBody.AutoSize  = $false
  $lblBody.Location  = New-Object System.Drawing.Point(18, 44)
  $lblBody.Width     = ($W - 50)
  $lblBody.TextAlign = 'TopRight'

  $bmp      = New-Object System.Drawing.Bitmap 1, 1
  $g        = [System.Drawing.Graphics]::FromImage($bmp)
  $measured = $g.MeasureString($body, $lblBody.Font, ($W - 50))
  $g.Dispose()
  $bmp.Dispose()
  $bodyH = [Math]::Min([Math]::Max([int]$measured.Height + 12, 40), 320)
  $lblBody.Height = $bodyH
  $form.Controls.Add($lblBody)

  $lblFoot           = New-Object System.Windows.Forms.Label
  $lblFoot.Text      = "$Stamp  •  לחיצה פותחת את דף הערוץ"
  $lblFoot.Font      = New-Object System.Drawing.Font('Segoe UI', 8.5)
  $lblFoot.ForeColor = $muted
  $lblFoot.AutoSize  = $false
  $lblFoot.Location  = New-Object System.Drawing.Point(18, (52 + $bodyH))
  $lblFoot.Size      = New-Object System.Drawing.Size(($W - 50), 20)
  $lblFoot.TextAlign = 'MiddleRight'
  $form.Controls.Add($lblFoot)

  $btnClose           = New-Object System.Windows.Forms.Label
  $btnClose.Text      = [string][char]0x2715
  $btnClose.Font      = New-Object System.Drawing.Font('Segoe UI', 10)
  $btnClose.ForeColor = $muted
  $btnClose.AutoSize  = $false
  $btnClose.Size      = New-Object System.Drawing.Size(26, 22)
  $btnClose.Location  = New-Object System.Drawing.Point(8, 12)
  $btnClose.TextAlign = 'MiddleCenter'
  $btnClose.Cursor    = 'Hand'
  $form.Controls.Add($btnClose)
  $btnClose.BringToFront()

  $form.Height = 52 + $bodyH + 28
  Place-Window $form $Offset
  Set-RoundRegion $form $R

  $script:closing2 = $false
  $fadeOut = New-Object System.Windows.Forms.Timer
  $fadeOut.Interval = 30
  $fadeOut.Add_Tick({
    if ($form.Opacity -le 0.05) { $fadeOut.Stop(); $form.Close() }
    else { $form.Opacity = $form.Opacity - 0.08 }
  }.GetNewClosure())

  $dismiss2 = {
    if ($script:closing2) { return }
    $script:closing2 = $true
    $fadeOut.Start()
  }.GetNewClosure()

  $life = New-Object System.Windows.Forms.Timer
  $life.Interval = [Math]::Max($Seconds, 3) * 1000
  $life.Add_Tick({ $life.Stop(); & $dismiss2 }.GetNewClosure())

  $openIt = {
    if ($Url -ne '') { try { Start-Process $Url } catch {} }
    & $dismiss2
  }.GetNewClosure()
  $form.Add_Click($openIt)
  $lblTitle.Add_Click($openIt)
  $lblBody.Add_Click($openIt)
  $lblFoot.Add_Click($openIt)
  $btnClose.Add_Click({ & $dismiss2 }.GetNewClosure())

  $form.Add_MouseEnter({ $life.Stop() }.GetNewClosure())
  $form.Add_MouseLeave({ if (-not $script:closing2) { $life.Start() } }.GetNewClosure())

  $form.Add_Shown({
    $life.Start()
    if ($Sound -eq 1) { [System.Media.SystemSounds]::Asterisk.Play() }
  }.GetNewClosure())

  [void]$form.ShowDialog()
}

# --- choose a path ---------------------------------------------------------
if ($haveBridge -and $HtmlFile -ne '' -and (Test-Path $HtmlFile)) {
  try { Show-Html } catch { Show-Classic }
} else {
  Show-Classic
}
`

// WritePopupScript materialises popup.ps1 next to the executable, with a UTF-8
// BOM so Windows PowerShell 5.1 reads the Hebrew literals correctly.
func WritePopupScript(dir string) (string, error) {
	path := filepath.Join(dir, "popup.ps1")
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(popupScript)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// PopupClickURL is where a click on a popup takes the user. Main points it
// at the local live page — never at Telegram. Empty = click just dismisses.
var PopupClickURL string

// ShowPopup renders one notification. On non-Windows hosts it degrades to a
// console line, which keeps the program testable outside Windows.
func ShowPopup(scriptPath, tmpDir, title string, msg Message, seconds, offset int, sound bool) error {
	if runtime.GOOS != "windows" {
		fmt.Printf("\n[popup] %s | %s\n%s\n%s\n", title, msg.Time, msg.Text, msg.URL)
		return nil
	}

	stamp := prettyTime(msg.Time)

	htmlFile := filepath.Join(tmpDir, fmt.Sprintf("msg_%d.html", msg.ID))
	page := BuildPopupHTML(title, msg, stamp, seconds)
	// BOM keeps the IE engine from ever mis-sniffing the Hebrew as ANSI.
	if err := os.WriteFile(htmlFile, append([]byte{0xEF, 0xBB, 0xBF}, []byte(page)...), 0o644); err != nil {
		return err
	}

	// Plain-text copy feeds the WinForms fallback path.
	textFile := filepath.Join(tmpDir, fmt.Sprintf("msg_%d.txt", msg.ID))
	if err := os.WriteFile(textFile, []byte(msg.Text), 0o644); err != nil {
		return err
	}

	soundFlag := "0"
	if sound {
		soundFlag = "1"
	}

	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden",
		"-File", scriptPath,
		"-HtmlFile", htmlFile,
		"-TextFile", textFile,
		"-Title", title,
		"-Url", PopupClickURL,
		"-Stamp", stamp,
		"-Seconds", strconv.Itoa(seconds),
		"-Offset", strconv.Itoa(offset),
		"-Sound", soundFlag,
	)
	hideWindow(cmd)

	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
		_ = os.Remove(textFile)
		_ = os.Remove(htmlFile)
	}()
	return nil
}
