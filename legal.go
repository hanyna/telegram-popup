package main

import (
	"html"
	"net/http"
	"os"
	"strings"
)

// Legal pages: terms of use + privacy (תקנון) and the accessibility statement
// (הצהרת נגישות) that Israeli regulations require of a website — Equal Rights
// for Persons with Disabilities (Service Accessibility Adjustments) Regulations
// 2013, section 35, and Israeli Standard 5568 (based on WCAG 2.0 level AA).
//
// Both pages are public (reachable from the login page, before the key), carry
// no feed data, and are plain server-rendered HTML that works with screen
// readers, keyboard and zoom.

const legalUpdated = "נובמבר 2026"

// contactLine: who to contact about accessibility / the terms. Set
// TGPOPUP_CONTACT (e.g. "טלפון 02-0000000 או דוא״ל x@y.com") on the server.
func contactLine() string {
	if c := strings.TrimSpace(os.Getenv("TGPOPUP_CONTACT")); c != "" {
		return html.EscapeString(c)
	}
	return "באמצעות פרטי הקשר של מפעיל האתר"
}

const legalShell = `<!doctype html>
<html lang="he" dir="rtl"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" content="#0d1016">
<link rel="icon" href="/icon-192.png">
<title>%%TITLE%% — מבזקי ארץ ישראל</title>
<style>
  :root { --bg:#0d1016; --panel:#141922; --fg:#e6e9ef; --muted:#9aa2b0; --line:rgba(255,255,255,0.10); --accent:#6fb1dd; }
  @media (prefers-color-scheme: light) {
    :root { --bg:#eef1f5; --panel:#ffffff; --fg:#20262f; --muted:#4d5666; --line:rgba(22,30,46,0.14); --accent:#1f6ea3; }
  }
  * { box-sizing:border-box; }
  body { margin:0; background:var(--bg); color:var(--fg);
         font-family:system-ui,-apple-system,"Segoe UI","Noto Sans Hebrew",Arial,sans-serif;
         font-size:16px; line-height:1.75; }
  .skip { position:absolute; right:-9999px; top:8px; background:var(--accent); color:#fff; padding:8px 14px; border-radius:8px; }
  .skip:focus { right:8px; }
  main { max-width:760px; margin:0 auto; padding:28px 18px 60px; }
  .card { background:var(--panel); border:1px solid var(--line); border-radius:16px; padding:24px 22px; }
  h1 { font-size:24px; margin:0 0 4px; }
  .upd { color:var(--muted); font-size:14px; margin-bottom:18px; }
  h2 { font-size:18px; margin:26px 0 6px; }
  p, li { margin:0 0 10px; }
  ul { padding-inline-start:22px; margin:0 0 10px; }
  a { color:var(--accent); }
  a:focus-visible, button:focus-visible { outline:3px solid var(--accent); outline-offset:2px; border-radius:4px; }
  nav { display:flex; gap:16px; flex-wrap:wrap; margin-bottom:18px; font-size:15px; }
  .note { color:var(--muted); font-size:14px; }
</style></head><body>
<a class="skip" href="#content">דלג לתוכן</a>
<main id="content">
  <nav aria-label="ניווט">
    <a href="/">חזרה לאתר</a>
    <a href="/terms">תקנון ופרטיות</a>
    <a href="/accessibility">הצהרת נגישות</a>
  </nav>
  <div class="card">
%%BODY%%
  </div>
</main>
</body></html>`

func renderLegal(title, body string) string {
	s := strings.Replace(legalShell, "%%TITLE%%", title, 1)
	return strings.Replace(s, "%%BODY%%", body, 1)
}

func termsBody() string {
	return `
<h1>תקנון ותנאי שימוש</h1>
<div class="upd">עודכן לאחרונה: ` + legalUpdated + `</div>

<h2>1. כללי</h2>
<p>האתר "מבזקי ארץ ישראל" (להלן: "האתר") מרכז ומציג במקום אחד עדכונים מערוצי טלגרם ציבוריים. השימוש באתר מהווה הסכמה לתנאים שלהלן. אם אינך מסכים להם, אנא הימנע משימוש באתר.</p>
<p>התקנון מנוסח בלשון זכר מטעמי נוחות בלבד, והוא מתייחס לכל המגדרים.</p>

<h2>2. מקור התוכן</h2>
<ul>
  <li>כל ההודעות, התמונות והסרטונים המוצגים באתר נלקחים מערוצי טלגרם <strong>ציבוריים</strong>, כפי שהם מפורסמים על ידי בעלי הערוצים.</li>
  <li>הזכויות בתוכן שייכות לבעלי הערוצים. האתר אינו יוצר את התוכן, אינו עורך אותו ואינו מאמץ אותו.</li>
  <li>האתר אינו אחראי לנכונות, לדיוק, לשלמות או לעדכניות של התוכן, ואין לראות בו מקור רשמי.</li>
</ul>

<h2>3. שימוש מותר</h2>
<p>התוכן מוצג לשימוש אישי ולא מסחרי. אין להעתיק, לשכפל או להפיץ את התוכן באופן מסחרי, ואין לבצע באתר פעולות שעלולות לפגוע בפעילותו או באבטחתו.</p>

<h2>4. זמינות השירות</h2>
<p>השירות ניתן כפי שהוא ("AS IS"). ייתכנו עיכובים, תקלות או הפסקות — למשל כשהשרת "מתעורר" אחרי זמן ללא שימוש, או כשטלגרם מגבילה את קצב הקריאה. האתר אינו מתחייב לזמינות רציפה.</p>

<h2>5. פרטיות</h2>
<p>האתר נבנה כך שיאסוף כמה שפחות מידע:</p>
<ul>
  <li><strong>עוגיית כניסה</strong> — כדי שלא תצטרך להזין את מפתח הגישה בכל פעם.</li>
  <li><strong>מזהה מכשיר אנונימי</strong> — מספר אקראי, לצורך ספירת כניסות בלבד. אינו מזהה אותך באופן אישי.</li>
  <li><strong>תגובות שבחרת</strong> (אימוג'י) — נשמרות באתר עצמו ואינן נשלחות לטלגרם.</li>
  <li><strong>הגדרות תצוגה ונגישות</strong> — נשמרות בדפדפן שלך בלבד.</li>
</ul>
<p>האתר <strong>אינו</strong> אוסף שם, טלפון או כתובת דוא״ל, <strong>אינו</strong> משתמש בכלי מעקב או פרסום של צד שלישי, ו<strong>אינו</strong> מעביר מידע לגורמים אחרים.</p>

<h2>6. קישורים חיצוניים</h2>
<p>האתר עשוי לכלול קישורים לטלגרם ולאתרים אחרים. האתר אינו אחראי לתוכן או למדיניות של אתרים אלה.</p>

<h2>7. שינויים בתקנון</h2>
<p>התקנון עשוי להתעדכן מעת לעת. תאריך העדכון האחרון מופיע בראש העמוד.</p>

<h2>8. דין וסמכות שיפוט</h2>
<p>על התקנון ועל השימוש באתר יחולו דיני מדינת ישראל.</p>

<h2>9. יצירת קשר</h2>
<p>לשאלות בנושא התקנון ניתן לפנות ` + contactLine() + `.</p>
`
}

func accessibilityBody() string {
	return `
<h1>הצהרת נגישות</h1>
<div class="upd">עודכן לאחרונה: ` + legalUpdated + `</div>

<p>אנו רואים חשיבות רבה בהנגשת האתר לכלל המשתמשים, ובכלל זה לאנשים עם מוגבלות. האתר פועל להנגשה בהתאם לתקנות שוויון זכויות לאנשים עם מוגבלות (התאמות נגישות לשירות), התשע"ג-2013, ולתקן הישראלי ת"י 5568, המבוסס על הנחיות WCAG 2.0 ברמה AA.</p>

<h2>התאמות הנגישות באתר</h2>
<ul>
  <li><strong>תפריט נגישות</strong> (כפתור הנגישות שבצד המסך): הגדלה והקטנה של הטקסט, ניגודיות גבוהה, גווני אפור, הדגשת קישורים, גופן קריא, עצירת אנימציות ואיפוס.</li>
  <li>ניווט מלא במקלדת, עם סימון ברור של הרכיב שבפוקוס, וקישור "דלג לתוכן".</li>
  <li>תיאורים טקסטואליים לכפתורים ולרכיבים, לשימוש בקוראי מסך.</li>
  <li>מבנה עברי מימין לשמאל, שפת עמוד מוגדרת, וכותרות היררכיות.</li>
  <li>התאמה לטלפונים ולמסכים בכל גודל, ותמיכה בהגדלת תצוגה (זום).</li>
  <li>ניגודיות צבעים מספקת, ללא הבהובים, וכיבוד הגדרת "הפחתת תנועה" של המכשיר.</li>
  <li>הודעות מערכת מוקראות לקוראי מסך (אזור חי).</li>
</ul>

<h2>הסתייגויות</h2>
<p>התוכן המוצג באתר מגיע מערוצי טלגרם ציבוריים, ואינו בשליטת האתר. לכן:</p>
<ul>
  <li>לתמונות אין תיאור טקסטואלי מפורט, מכיוון שבעלי הערוצים אינם מספקים אותו.</li>
  <li>לסרטונים ולהקלטות אין כתוביות או תמלול.</li>
  <li>חלק מהסרטונים הארוכים זמינים לצפייה רק בתוך טלגרם.</li>
</ul>
<p>אנו ממשיכים לפעול לשיפור הנגישות באופן שוטף.</p>

<h2>נתקלת בבעיה?</h2>
<p>אם נתקלת בקושי בגלישה או ברכיב שאינו נגיש, נשמח לשמוע ולתקן. ניתן לפנות לרכז הנגישות ` + contactLine() + `. כדי שנוכל לטפל בפנייה, אנא פרט את הבעיה, את העמוד שבו נתקלת בה ואת סוג המכשיר והדפדפן.</p>
`
}

func (f *Feed) handleTerms(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=600")
	_, _ = w.Write([]byte(renderLegal("תקנון ופרטיות", termsBody())))
}

func (f *Feed) handleAccessibility(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=600")
	_, _ = w.Write([]byte(renderLegal("הצהרת נגישות", accessibilityBody())))
}
