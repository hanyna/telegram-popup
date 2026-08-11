package main

// The messenger UI: a two-pane chat application served as one self-contained
// document. Sidebar lists channels (avatar, last-message preview, unread
// badge, sorted by recency); the main pane is the selected conversation —
// oldest at the top, NEW MESSAGES AT THE BOTTOM, date separators, auto-scroll
// when you're at the bottom, a floating jump pill when you're not, and
// automatic history loading when you scroll to the top.

const feedPage = `<!DOCTYPE html>
<html lang="he" dir="rtl">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ערוץ חי</title>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'%3E%3Cdefs%3E%3ClinearGradient id='g' x1='0' y1='0' x2='1' y2='1'%3E%3Cstop offset='0' stop-color='%2337aee2'/%3E%3Cstop offset='1' stop-color='%2331c48d'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect x='4' y='4' width='56' height='56' rx='16' fill='url(%23g)'/%3E%3Cpath d='M18 20h28a4 4 0 014 4v12a4 4 0 01-4 4H31l-8 7v-7h-5a4 4 0 01-4-4V24a4 4 0 014-4z' fill='%23fff'/%3E%3C/svg%3E">
<style>
  :root {
    --bg:#0b0e18; --bg2:#0f1320;
    --panel:#12172a; --panel2:#161c31;
    --bubble:#1a2138; --bubble2:#202946;
    --line:rgba(255,255,255,0.06);
    --fg:#eceff8; --muted:#8b93a9; --dim:#5a6378;
    --accent:#37aee2; --accent2:#31c48d;
    --headbg:rgba(15,19,32,0.85);
    --markbg:rgba(55,174,226,0.35);
  }
  body.light {
    --bg:#eef1f7; --bg2:#e6eaf2;
    --panel:#ffffff; --panel2:#f7f9fd;
    --bubble:#ffffff; --bubble2:#f4f7fc;
    --line:rgba(20,30,60,0.10);
    --fg:#1c2333; --muted:#5d6680; --dim:#8a92a8;
    --accent:#1e88d2; --accent2:#149a6e;
    --headbg:rgba(255,255,255,0.85);
    --markbg:rgba(30,136,210,0.25);
  }
  * { margin:0; padding:0; box-sizing:border-box; }
  html, body { height:100%; }
  body {
    background:var(--bg);
    color:var(--fg);
    font-family:'Segoe UI','Segoe UI Emoji','Noto Sans Hebrew',Arial,sans-serif;
    overflow:hidden;
  }
  ::selection { background:rgba(55,174,226,0.35); }
  ::-webkit-scrollbar { width:8px; height:8px; }
  ::-webkit-scrollbar-thumb { background:rgba(255,255,255,0.10); border-radius:99px; }
  ::-webkit-scrollbar-thumb:hover { background:rgba(255,255,255,0.18); }
  ::-webkit-scrollbar-track { background:transparent; }

  #app { display:flex; height:100vh; }

  /* ===== sidebar ========================================================= */
  #side {
    width:300px; flex:none;
    background:linear-gradient(180deg,var(--panel) 0%,var(--bg2) 100%);
    border-left:1px solid var(--line);
    display:flex; flex-direction:column;
  }
  .sidehead {
    display:flex; align-items:center; gap:11px;
    padding:16px 16px 12px;
    border-bottom:1px solid var(--line);
  }
  .applogo {
    width:38px; height:38px; border-radius:12px; flex:none;
    background:linear-gradient(135deg,#37aee2,#1e88d2);
    display:flex; align-items:center; justify-content:center;
    box-shadow:0 3px 12px rgba(30,136,210,0.4);
  }
  .applogo svg { width:20px; height:20px; }
  .appmeta { min-width:0; }
  .appname { font-size:14px; font-weight:700; letter-spacing:0.2px; }
  .status { font-size:11px; color:var(--muted); display:flex; align-items:center; gap:5px; margin-top:1px; }
  .dot { width:7px; height:7px; border-radius:50%; background:#e05252; flex:none; transition:background 0.3s; }
  .dot.on { background:var(--accent2); box-shadow:0 0 6px rgba(49,196,141,0.8); }

  #chanlist { flex:1; overflow-y:auto; padding:8px; }

  .row {
    display:flex; align-items:center; gap:11px;
    padding:9px 10px; border-radius:12px;
    cursor:pointer; position:relative;
    transition:background 0.12s ease;
    user-select:none;
  }
  .row:hover { background:rgba(255,255,255,0.045); }
  .row.active {
    background:linear-gradient(135deg,rgba(55,174,226,0.22),rgba(30,136,210,0.12));
  }
  .row.active::before {
    content:''; position:absolute; right:-8px; top:20%;
    height:60%; width:3px; border-radius:99px;
    background:linear-gradient(180deg,var(--accent),var(--accent2));
  }
  .ava {
    width:44px; height:44px; border-radius:50%; flex:none;
    display:flex; align-items:center; justify-content:center;
    color:#fff; font-weight:700; font-size:17px;
    overflow:hidden;
  }
  .ava img, #headava img { width:100%; height:100%; object-fit:cover; border-radius:50%; display:block; }
  #headava { overflow:hidden; }
  .mutedmark { font-size:11px; opacity:0.8; }
  .rowmid { flex:1; min-width:0; }
  .rowname {
    font-size:13.5px; font-weight:600;
    white-space:nowrap; overflow:hidden; text-overflow:ellipsis;
  }
  .rowprev {
    font-size:12px; color:var(--muted); margin-top:2px;
    white-space:nowrap; overflow:hidden; text-overflow:ellipsis;
  }
  .rowside { display:flex; flex-direction:column; align-items:flex-start; gap:4px; flex:none; }
  .rowtime { font-size:10.5px; color:var(--dim); }
  .badge {
    min-width:19px; height:19px; padding:0 6px;
    border-radius:99px; display:none;
    align-items:center; justify-content:center;
    background:linear-gradient(135deg,#37aee2,#1e88d2);
    color:#fff; font-size:11px; font-weight:700;
  }
  .badge.show { display:flex; }
  .rowx {
    position:absolute; top:6px; left:6px;
    width:18px; height:18px; line-height:16px; text-align:center;
    border-radius:50%; font-size:10px; color:var(--dim);
    opacity:0; transition:opacity 0.12s;
  }
  .row:hover .rowx { opacity:1; }
  .rowx:hover { background:rgba(224,82,82,0.3); color:#ffb0b0; }

  .autostartrow {
    padding:8px 16px; border-top:1px solid var(--line);
    font-size:12px; color:var(--muted); user-select:none;
  }
  .autostartrow label { display:flex; align-items:center; gap:8px; cursor:pointer; }
  .autostartrow input { accent-color:var(--accent); cursor:pointer; }

  #lightbox {
    position:fixed; inset:0; z-index:50;
    background:rgba(5,7,14,0.9);
    display:none; align-items:center; justify-content:center;
    cursor:zoom-out;
  }
  #lightbox img {
    max-width:92vw; max-height:92vh;
    border-radius:12px; box-shadow:0 20px 80px rgba(0,0,0,0.6);
    animation:pop2 0.18s ease-out;
  }

  .addwrap { padding:10px 12px 14px; border-top:1px solid var(--line); }
  #addbtn {
    width:100%; font:inherit; font-size:13px; font-weight:600;
    color:#fff; border:none; border-radius:12px;
    background:linear-gradient(135deg,#37aee2,#1e88d2);
    padding:11px 0; cursor:pointer;
    box-shadow:0 4px 14px rgba(30,136,210,0.35);
    transition:transform 0.12s ease, box-shadow 0.12s ease;
  }
  #addbtn:hover { transform:translateY(-1px); box-shadow:0 6px 18px rgba(30,136,210,0.5); }

  /* ===== chat pane ======================================================= */
  #main { flex:1; display:flex; flex-direction:column; min-width:0; position:relative; }
  #chathead {
    display:flex; align-items:center; gap:11px;
    padding:11px 20px;
    background:var(--headbg);
    backdrop-filter:blur(12px); -webkit-backdrop-filter:blur(12px);
    border-bottom:1px solid var(--line);
    z-index:5;
  }
  #headava { width:38px; height:38px; border-radius:50%; flex:none;
    display:flex; align-items:center; justify-content:center;
    color:#fff; font-weight:700; font-size:15px; }
  .headmid { flex:1; min-width:0; }
  #headtitle { font-size:14.5px; font-weight:700; }
  #headsub { font-size:11.5px; color:var(--muted); margin-top:1px; }
  #headlink {
    font-size:12.5px; color:var(--accent); text-decoration:none;
    border:1px solid rgba(55,174,226,0.35); border-radius:99px;
    padding:6px 14px; flex:none;
    transition:background 0.12s;
  }
  #headlink:hover { background:rgba(55,174,226,0.12); }

  .iconbtn {
    width:36px; height:36px; border-radius:50%; flex:none;
    display:flex; align-items:center; justify-content:center;
    background:rgba(255,255,255,0.05); border:1px solid var(--line);
    font-size:16px; cursor:pointer; user-select:none;
    transition:background 0.12s, border-color 0.12s, opacity 0.12s;
  }
  .iconbtn:hover { background:rgba(55,174,226,0.14); border-color:rgba(55,174,226,0.4); }
  .iconbtn.off { opacity:0.45; }
  #refreshbtn { font-size:19px; color:var(--accent); }
  #refreshbtn.spinning { animation:spin 0.8s linear infinite; pointer-events:none; opacity:0.7; }

  #searchbar {
    display:none; align-items:center; gap:10px;
    padding:10px 20px;
    background:var(--headbg);
    border-bottom:1px solid var(--line);
    z-index:4;
  }
  #searchbar.open { display:flex; }
  #searchinput {
    flex:1; background:var(--bg); color:var(--fg);
    border:1px solid var(--line); border-radius:99px;
    font:inherit; font-size:14px; padding:9px 18px;
    outline:none; transition:border-color 0.15s;
  }
  #searchinput:focus { border-color:rgba(55,174,226,0.55); box-shadow:0 0 0 3px rgba(55,174,226,0.10); }
  #resultsmeta {
    max-width:720px; margin:0 auto 14px;
    color:var(--muted); font-size:12.5px; text-align:center;
    padding:6px 0;
  }
  #resultslist { max-width:720px; margin:0 auto; }
  #resultslist .msg { max-width:100%; animation:none; }
  mark {
    background:var(--markbg); color:inherit;
    border-radius:3px; padding:0 2px;
  }

  #toast {
    position:absolute; bottom:80px; left:50%; transform:translateX(-50%);
    background:rgba(18,23,42,0.95); border:1px solid var(--line);
    color:var(--fg); font-size:13px;
    border-radius:99px; padding:9px 20px;
    box-shadow:0 8px 24px rgba(0,0,0,0.4);
    z-index:25; display:none;
    animation:pop 0.18s ease-out;
  }

  .photo img { opacity:0; transition:opacity 0.35s ease; }
  .photo img.ld { opacity:1; }

  #chat {
    flex:1; overflow-y:auto;
    background:
      radial-gradient(1000px 500px at 85% -5%, rgba(55,174,226,0.07), transparent 55%),
      radial-gradient(800px 400px at 10% 105%, rgba(49,196,141,0.05), transparent 55%),
      var(--bg);
    padding:18px 22px 26px;
  }
  #msgs { max-width:720px; margin:0 auto; }

  /* Status bar: says out loud why the feed looks the way it does, so an
     empty page is never mistaken for a broken app. */
  #statusbar {
    display:flex; align-items:center; gap:9px;
    padding:9px 16px; font-size:13px; line-height:1.5;
    background:rgba(255,176,32,0.10);
    border-bottom:1px solid rgba(255,176,32,0.28);
    color:var(--fg);
  }
  #statusbar.calm {
    background:rgba(55,174,226,0.09);
    border-bottom-color:rgba(55,174,226,0.26);
  }
  #statusicon { font-size:15px; flex:none; }
  #statustext { flex:1; }
  #statusmore {
    flex:none; cursor:pointer; color:var(--accent);
    font-size:12px; text-decoration:underline; user-select:none;
  }
  #statuspanel {
    padding:8px 16px 12px; border-bottom:1px solid var(--line);
    font-size:12px; color:var(--muted); background:var(--bg2);
  }
  .strow {
    display:flex; align-items:center; gap:8px;
    padding:4px 0; border-bottom:1px solid var(--line);
  }
  .strow:last-child { border-bottom:none; }
  .stname { flex:1; color:var(--fg); font-size:12px; }
  .stbadge {
    flex:none; font-size:11px; padding:1px 7px; border-radius:20px;
    background:rgba(255,255,255,0.06); color:var(--muted);
  }
  .stbadge.good { background:rgba(49,196,141,0.16); color:var(--accent2); }
  .stbadge.bad  { background:rgba(255,120,100,0.16); color:#ff8a72; }
  .stbadge.wait { background:rgba(255,176,32,0.16); color:#f0a828; }

  #topload {
    display:none; text-align:center; padding:6px 0 14px;
    color:var(--muted); font-size:12px;
  }
  .spin {
    display:inline-block; width:15px; height:15px;
    border:2px solid rgba(255,255,255,0.15); border-top-color:var(--accent);
    border-radius:50%; vertical-align:-3px; margin-left:7px;
    animation:spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform:rotate(360deg); } }

  .datesep { display:flex; justify-content:center; margin:16px 0 12px; }
  .datesep span {
    background:rgba(255,255,255,0.05); border:1px solid var(--line);
    color:var(--muted); font-size:11.5px;
    border-radius:99px; padding:4px 14px;
    backdrop-filter:blur(4px);
  }

  .msg {
    background:linear-gradient(160deg,var(--bubble2),var(--bubble));
    border:1px solid var(--line);
    border-radius:16px 16px 16px 5px;
    padding:11px 15px 12px;
    margin-bottom:10px;
    max-width:86%;
    box-shadow:0 2px 10px rgba(0,0,0,0.18);
    animation:rise 0.22s ease-out;
  }
  .msg.fresh { box-shadow:0 0 0 1px rgba(55,174,226,0.5), 0 6px 24px rgba(55,174,226,0.15); }
  .msg.hidden { display:none; }
  @keyframes rise { from { opacity:0; transform:translateY(8px); } to { opacity:1; transform:none; } }

  .msghead {
    display:flex; align-items:center; justify-content:space-between; gap:10px;
    margin-bottom:6px; font-size:11.5px;
  }
  .msgchan { color:var(--accent2); font-weight:700; cursor:pointer; }
  .msgchan:hover { text-decoration:underline; }
  body.single .msgchan { display:none; }
  .msgtime { color:var(--dim); text-decoration:none; }
  .msgtime:hover { color:var(--accent); }

  .msgtext { font-size:14px; line-height:1.6; word-wrap:break-word; }
  .msgtext a { color:#5cb8ff; text-decoration:none; border-bottom:1px solid rgba(92,184,255,0.35); }

  .photo { margin:2px 0 8px; padding:0; }
  .photo img, .photo video { width:100%; display:block; border-radius:11px; border:1px solid var(--line); }
  .vidwrap {
    position:relative; height:0; padding-bottom:56.25%; overflow:hidden;
    border-radius:11px; border:1px solid var(--line); background:#0d1120;
  }
  .vidwrap video { position:absolute; top:0; left:0; width:100%; height:100%; border:none; border-radius:0; }
  .vidwrap img { position:absolute; top:50%; left:0; width:100%; height:auto; transform:translateY(-50%); border:none; border-radius:0; }
  .playbtn {
    position:absolute; top:50%; left:50%;
    width:52px; height:52px; margin:-26px 0 0 -26px;
    border-radius:50%; background:rgba(13,17,28,0.72);
    border:2px solid rgba(255,255,255,0.9);
    transition:transform 0.15s ease, background 0.15s ease;
  }
  .vidwrap:hover .playbtn { transform:scale(1.1); background:rgba(30,136,210,0.8); }
  .playbtn span {
    position:absolute; top:50%; left:50%;
    margin:-10px 0 0 -6px; width:0; height:0;
    border-top:10px solid transparent; border-bottom:10px solid transparent;
    border-left:16px solid #fff;
  }
  .durbadge {
    position:absolute; bottom:8px; left:8px; direction:ltr;
    background:rgba(10,13,22,0.75); color:#fff;
    font-size:11px; padding:2px 8px; border-radius:10px;
    pointer-events:none;
  }
  .durbadge.durtop { top:8px; bottom:auto; }
  .vidwrap iframe {
    position:absolute; top:0; left:0; width:100%; height:100%;
    border:none; background:#0d1120;
  }
  .embedwrap { cursor:pointer; }
  .vidready { box-shadow:0 0 0 2px rgba(49,196,141,0.7), 0 0 22px rgba(49,196,141,0.35); }
  .vidready .playbtn { background:rgba(20,120,80,0.8); border-color:#fff; }

  .msgtext.clamped { max-height:230px; overflow:hidden; position:relative; }
  .msgtext.clamped::after {
    content:''; position:absolute; bottom:0; left:0; right:0; height:52px;
    background:linear-gradient(transparent, var(--bubble));
  }
  .morebtn {
    display:inline-block; margin-top:7px;
    color:var(--accent); font-size:12.5px; cursor:pointer;
    border-bottom:1px dashed rgba(55,174,226,0.4);
  }
  .morebtn:hover { border-bottom-style:solid; }

  #downbtn {
    position:absolute; bottom:24px; left:22px;
    width:46px; height:46px; border-radius:50%;
    display:none; align-items:center; justify-content:center;
    background:rgba(22,28,49,0.92); color:var(--accent);
    border:1px solid rgba(55,174,226,0.35);
    font-size:20px; cursor:pointer;
    box-shadow:0 6px 18px rgba(0,0,0,0.4);
    z-index:14;
    transition:transform 0.12s ease, background 0.12s ease;
  }
  #downbtn:hover { transform:translateY(-2px); background:rgba(30,136,210,0.35); color:#fff; }

  .empty {
    display:flex; flex-direction:column; align-items:center; justify-content:center;
    padding:80px 0 40px; color:var(--muted); gap:14px; text-align:center;
  }
  .empty .big { font-size:40px; opacity:0.5; }
  .empty .t { font-size:14px; }

  #jump {
    position:absolute; bottom:24px; left:50%; transform:translateX(-50%);
    display:none; align-items:center; gap:8px;
    background:linear-gradient(135deg,#37aee2,#1e88d2);
    color:#fff; border:none; border-radius:99px;
    font:inherit; font-size:13px; font-weight:700;
    padding:10px 20px; cursor:pointer;
    box-shadow:0 8px 24px rgba(30,136,210,0.5);
    z-index:15;
    animation:pop 0.2s ease-out;
  }
  @keyframes pop { from { transform:translateX(-50%) scale(0.85); opacity:0; } to { transform:translateX(-50%) scale(1); opacity:1; } }
  #jump .cnt { background:rgba(255,255,255,0.28); border-radius:99px; padding:1px 8px; font-size:11.5px; }

  /* ===== modal =========================================================== */
  .modal-back {
    position:fixed; inset:0; background:rgba(5,7,14,0.75);
    display:none; align-items:center; justify-content:center; z-index:40;
    backdrop-filter:blur(4px);
  }
  .modal {
    background:linear-gradient(160deg,var(--panel2),var(--panel));
    border:1px solid var(--line); border-radius:18px;
    padding:24px; width:min(370px, 90vw);
    box-shadow:0 24px 60px rgba(0,0,0,0.5);
    animation:pop2 0.18s ease-out;
  }
  @keyframes pop2 { from { transform:scale(0.92); opacity:0; } to { transform:scale(1); opacity:1; } }
  .modal h3 { font-size:15.5px; margin-bottom:6px; }
  .modal p { font-size:12.5px; color:var(--muted); margin-bottom:14px; line-height:1.55; }
  .modal input {
    width:100%; background:var(--bg); color:var(--fg);
    border:1px solid var(--line); border-radius:11px;
    font:inherit; font-size:14px; padding:11px 14px;
    direction:ltr; text-align:left; outline:none;
    transition:border-color 0.15s;
  }
  .modal input:focus { border-color:rgba(55,174,226,0.6); box-shadow:0 0 0 3px rgba(55,174,226,0.12); }
  .modal .err { color:#ff9c9c; font-size:12px; min-height:16px; margin-top:8px; }
  .modal .btns { display:flex; gap:10px; margin-top:10px; }
  .modal button {
    flex:1; font:inherit; font-size:13.5px; border-radius:11px;
    padding:10px 0; cursor:pointer; border:1px solid var(--line);
  }
  .modal .ok { background:linear-gradient(135deg,#37aee2,#1e88d2); color:#fff; border:none; font-weight:700; }
  .modal .ok:disabled { opacity:0.55; cursor:default; }
  .modal .cancel { background:transparent; color:var(--muted); }

  @media (max-width:680px) {
    #side { width:76px; }
    .rowmid, .rowside, .appmeta { display:none; }
    .row { justify-content:center; padding:9px 4px; }
    .sidehead { justify-content:center; }
    #addbtn { font-size:18px; padding:8px 0; }
    #addbtn .txt { display:none; }
  }
</style>
</head>
<body>
<div id="app">
  <aside id="side">
    <div class="sidehead">
      <div class="applogo"><svg viewBox="0 0 64 64"><path d="M14 14h36a6 6 0 016 6v18a6 6 0 01-6 6H32l-11 9v-9h-7a6 6 0 01-6-6V20a6 6 0 016-6z" fill="#fff"/></svg></div>
      <div class="appmeta">
        <div class="appname">ערוץ חי <span style="font-size:10px;color:#5a6378;font-weight:400">v%%VERSION%%</span></div>
        <div class="status"><span class="dot" id="dot"></span><span id="statustext">מתחבר…</span></div>
      </div>
    </div>
    <div id="chanlist"></div>
    <div class="autostartrow" id="autostartrow" style="display:none">
      <label><input type="checkbox" id="autostartchk"> הפעל עם ווינדוס</label>
    </div>
    <div class="addwrap"><button id="addbtn"><span class="txt">+ הוסף ערוץ</span><span style="display:none">+</span></button></div>
  </aside>

  <main id="main">
    <div id="chathead">
      <div id="headava">✦</div>
      <div class="headmid">
        <div id="headtitle">כל הערוצים</div>
        <div id="headsub"></div>
      </div>
      <div class="iconbtn" id="themebtn" title="מצב בהיר/כהה">🌓</div>
      <div class="iconbtn" id="searchbtn" title="חיפוש בארכיון">🔍</div>
      <div class="iconbtn" id="refreshbtn" title="עדכן עכשיו">⟳</div>
      <div class="iconbtn" id="tglmute" style="display:none">🔔</div>
      <div class="iconbtn" id="tglpopups" title="חלוניות קופצות (כל הערוצים)">🔔</div>
      <div class="iconbtn" id="tglsound" title="צליל התראה">🔊</div>
    </div>
    <div id="searchbar">
      <input id="searchinput" placeholder="חיפוש בכל ההיסטוריה שנשמרה…" spellcheck="false">
      <div class="iconbtn" id="searchclose" title="סגור חיפוש">✕</div>
    </div>
    <div id="statusbar" style="display:none">
      <span id="statusicon">⏳</span>
      <span id="statustext"></span>
      <span id="statusmore" title="פירוט לפי ערוץ">פירוט</span>
    </div>
    <div id="statuspanel" style="display:none"></div>
    <div id="chat">
      <div id="topload"><span class="spin"></span>טוען הודעות ישנות…</div>
      <div id="msgs"></div>
      <div id="results" style="display:none">
        <div id="resultsmeta"></div>
        <div id="resultslist"></div>
      </div>
    </div>
    <button id="jump">⬇ הודעות חדשות <span class="cnt" id="jumpcnt">1</span></button>
    <button id="downbtn" title="גלול לתחתית">⌄</button>
    <div id="toast"></div>
  </main>
</div>

<div id="lightbox"><img id="lightboximg" alt=""></div>

<div class="modal-back" id="modalback">
  <div class="modal">
    <h3>הוספת ערוץ</h3>
    <p>הדבק שם משתמש או קישור של ערוץ טלגרם ציבורי.<br>לדוגמה: durov או https://t.me/durov</p>
    <input id="chaninput" placeholder="@channel" spellcheck="false">
    <div class="err" id="chanerr"></div>
    <div class="btns">
      <button class="cancel" id="chancancel">ביטול</button>
      <button class="ok" id="chanok">הוסף</button>
    </div>
  </div>
</div>

<script>
  // A blank page is the worst failure mode — if anything throws, surface it
  // on screen instead of leaving the user staring at nothing.
  window.onerror = function (msg, src, line, col) {
    try {
      var b = document.getElementById('booterr');
      if (!b) {
        b = document.createElement('div');
        b.id = 'booterr';
        b.style.cssText = 'position:fixed;top:0;left:0;right:0;z-index:99;background:#7a1f1f;color:#fff;font:13px Segoe UI;padding:10px 16px;direction:rtl;text-align:center';
        document.body.appendChild(b);
      }
      b.textContent = 'שגיאה בטעינת הדף: ' + msg + ' (שורה ' + line + '). צלם מסך ושלח.';
    } catch (e) {}
    return false;
  };

  var baseTitle = document.title;
  var titleUnread = 0;
  var current = 'all';
  var channels = [];
  var unread = {};      // channel -> count
  var lastMsg = {};     // channel -> {ts, preview}
  var oldestPer = {};   // channel -> smallest loaded id
  var donePer = {};     // channel -> no more history
  var loadingOlder = false;

  var chat = document.getElementById('chat');
  var msgs = document.getElementById('msgs');

  function openUrl(u) { window.open(u, '_blank', 'noopener'); return false; }

  function hue(name) {
    var h = 0;
    for (var i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360;
    return h;
  }
  function chanByName(name) {
    for (var i = 0; i < channels.length; i++) if (channels[i].name === name) return channels[i];
    return null;
  }
  function chanNames() { return channels.map(function (c) { return c.name; }); }

  // Real profile picture when Telegram provides one; colored initial as a
  // graceful fallback (also when the image fails to load).
  function avaStyle(el, ch) {
    var name = (typeof ch === 'string') ? ch : ch.name;
    var photo = (typeof ch === 'object' && ch && ch.photo) ? ch.photo : '';
    var title = (typeof ch === 'object' && ch && ch.title) ? ch.title : name;
    var h = hue(name);
    el.style.background = 'linear-gradient(135deg, hsl(' + h + ',62%,52%), hsl(' + ((h + 35) % 360) + ',62%,40%))';
    el.textContent = title.charAt(0).toUpperCase();
    if (photo) {
      var img = document.createElement('img');
      img.alt = '';
      img.onload = function () { el.textContent = ''; el.appendChild(img); };
      img.src = photo;
    }
  }

  function setStatus(on) {
    document.getElementById('dot').className = on ? 'dot on' : 'dot';
    document.getElementById('statustext').textContent = on ? 'מחובר · זמן אמת' : 'מתחבר מחדש…';
  }

  function fmtTime(ts) {
    if (!ts) return '';
    var d = new Date(ts * 1000);
    return ('0' + d.getHours()).slice(-2) + ':' + ('0' + d.getMinutes()).slice(-2);
  }
  function fmtRowTime(ts) {
    if (!ts) return '';
    var d = new Date(ts * 1000), now = new Date();
    if (d.toDateString() === now.toDateString()) return fmtTime(ts);
    return ('0' + d.getDate()).slice(-2) + '/' + ('0' + (d.getMonth() + 1)).slice(-2);
  }
  function dayKey(ts) { return new Date(ts * 1000).toDateString(); }
  function dayLabel(ts) {
    var d = new Date(ts * 1000), now = new Date();
    var yd = new Date(now.getTime() - 86400000);
    if (d.toDateString() === now.toDateString()) return 'היום';
    if (d.toDateString() === yd.toDateString()) return 'אתמול';
    return ('0' + d.getDate()).slice(-2) + '.' + ('0' + (d.getMonth() + 1)).slice(-2) + '.' + d.getFullYear();
  }

  function nearBottom() { return chat.scrollTop + chat.clientHeight >= chat.scrollHeight - 140; }
  function scrollBottom(now) { chat.scrollTo({ top: chat.scrollHeight, behavior: now ? 'auto' : 'smooth' }); }

  // ----- sidebar ------------------------------------------------------------
  function renderSidebar() {
    var list = document.getElementById('chanlist');
    list.innerHTML = '';

    var allRow = document.createElement('div');
    allRow.className = 'row' + (current === 'all' ? ' active' : '');
    var allAva = document.createElement('div');
    allAva.className = 'ava';
    allAva.style.background = 'linear-gradient(135deg,#37aee2,#1e88d2)';
    allAva.textContent = '✦';
    allRow.appendChild(allAva);
    var allMid = document.createElement('div');
    allMid.className = 'rowmid';
    allMid.innerHTML = '<div class="rowname">כל הערוצים</div><div class="rowprev">ציר זמן ממוזג</div>';
    allRow.appendChild(allMid);
    allRow.onclick = function () { select('all'); };
    list.appendChild(allRow);

    var ordered = channels.slice().sort(function (a, b) {
      return ((lastMsg[b.name] || {}).ts || 0) - ((lastMsg[a.name] || {}).ts || 0);
    });

    ordered.forEach(function (ch) {
      var row = document.createElement('div');
      row.className = 'row' + (current === ch.name ? ' active' : '');

      var ava = document.createElement('div');
      ava.className = 'ava';
      avaStyle(ava, ch);
      row.appendChild(ava);

      var mid = document.createElement('div');
      mid.className = 'rowmid';
      var nm = document.createElement('div');
      nm.className = 'rowname';
      nm.textContent = ch.title || ('@' + ch.name);
      if (ch.muted) {
        var mm = document.createElement('span');
        mm.className = 'mutedmark';
        mm.textContent = ' 🔕';
        nm.appendChild(mm);
      }
      var pv = document.createElement('div');
      pv.className = 'rowprev';
      pv.textContent = (lastMsg[ch.name] || {}).preview || 'אין הודעות עדיין';
      mid.appendChild(nm); mid.appendChild(pv);
      row.appendChild(mid);

      var side = document.createElement('div');
      side.className = 'rowside';
      var tm = document.createElement('div');
      tm.className = 'rowtime';
      tm.textContent = fmtRowTime((lastMsg[ch.name] || {}).ts);
      var bd = document.createElement('div');
      bd.className = 'badge' + (unread[ch.name] ? ' show' : '');
      bd.textContent = unread[ch.name] || '';
      side.appendChild(tm); side.appendChild(bd);
      row.appendChild(side);

      var x = document.createElement('div');
      x.className = 'rowx';
      x.textContent = '✕';
      x.title = 'הסר ערוץ';
      x.onclick = function (ev) {
        ev.stopPropagation();
        if (!confirm('להסיר את ' + (ch.title || '@' + ch.name) + '?')) return;
        fetch('/api/channels?name=' + encodeURIComponent(ch.name), { method: 'DELETE' })
          .then(function (r) { return r.json(); })
          .then(function () { location.reload(); });
      };
      row.appendChild(x);

      row.onclick = function () { select(ch.name); };
      list.appendChild(row);
    });
  }

  function renderHead() {
    var ava = document.getElementById('headava');
    var muteBtn = document.getElementById('tglmute');
    var n = visibleCount();
    if (current === 'all') {
      ava.innerHTML = '';
      ava.style.background = 'linear-gradient(135deg,#37aee2,#1e88d2)';
      ava.textContent = '✦';
      document.getElementById('headtitle').textContent = 'כל הערוצים';
      document.getElementById('headsub').textContent = n ? n + ' הודעות נטענו' : '';
      muteBtn.style.display = 'none';
    } else {
      var ch = chanByName(current);
      ava.innerHTML = '';
      avaStyle(ava, ch || current);
      document.getElementById('headtitle').textContent = (ch && ch.title) || ('@' + current);
      document.getElementById('headsub').textContent = '@' + current + (n ? ' · ' + n + ' הודעות' : '');
      var isMuted = !!(ch && ch.muted);
      muteBtn.textContent = isMuted ? '🔕' : '🔔';
      muteBtn.className = 'iconbtn' + (isMuted ? ' off' : '');
      muteBtn.title = isMuted ? 'בטל השתקה לערוץ' : 'השתק התראות לערוץ הזה';
      muteBtn.style.display = 'flex';
    }
  }

  // Channel name on a bubble filters to that channel — nothing opens Telegram.
  msgs.addEventListener('click', function (ev) {
    var chanTag = ev.target.closest ? ev.target.closest('.msgchan') : null;
    if (chanTag) {
      var c = chanTag.getAttribute('data-chan');
      if (c && chanByName(c)) select(c);
    }
  });

  document.getElementById('tglmute').onclick = function () {
    if (current === 'all') return;
    var ch = chanByName(current);
    var target = !(ch && ch.muted);
    fetch('/api/mute', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ channel: current, muted: target })
    }).then(function (r) { return r.json(); }).then(function (d) {
      if (!d.ok) { toast(d.error || 'שגיאה'); return; }
      if (ch) ch.muted = d.muted;
      renderHead();
      renderSidebar();
      toast(d.muted ? 'ההתראות מהערוץ הושתקו — ההודעות ממשיכות להגיע לדף' : 'ההתראות מהערוץ הופעלו מחדש');
    }).catch(function () { toast('שגיאת תקשורת'); });
  };

  function visibleCount() {
    return msgs.querySelectorAll('.msg:not(.hidden)').length;
  }

  function select(ch) {
    current = ch;
    if (ch === 'all') { channels.forEach(function (c) { unread[c.name] = 0; }); }
    else unread[ch] = 0;
    // Toggle only the 'single' class — never reset className, or the active
    // theme ('light') would be wiped every time you switch channels.
    document.body.classList.toggle('single', ch !== 'all');
    var nodes = msgs.querySelectorAll('.msg');
    for (var i = 0; i < nodes.length; i++) {
      var n = nodes[i];
      if (ch === 'all' || n.getAttribute('data-channel') === ch) n.classList.remove('hidden');
      else n.classList.add('hidden');
    }
    rebuildSeparators();
    renderSidebar();
    renderHead();
    hideJump();
    renderEmptyState();
    scrollBottom(true);
  }

  // ----- date separators ----------------------------------------------------
  function rebuildSeparators() {
    var seps = msgs.querySelectorAll('.datesep');
    for (var i = 0; i < seps.length; i++) seps[i].remove();
    var nodes = msgs.querySelectorAll('.msg:not(.hidden)');
    var lastDay = '';
    for (var j = 0; j < nodes.length; j++) {
      var ts = parseInt(nodes[j].getAttribute('data-ts') || '0', 10);
      if (!ts) continue;
      var k = dayKey(ts);
      if (k !== lastDay) {
        lastDay = k;
        var sep = document.createElement('div');
        sep.className = 'datesep';
        var s = document.createElement('span');
        s.textContent = dayLabel(ts);
        sep.appendChild(s);
        msgs.insertBefore(sep, nodes[j]);
      }
    }
  }

  function renderEmptyState() {
    var ex = document.getElementById('emptystate');
    if (ex) ex.remove();
    if (visibleCount() === 0) {
      var e = document.createElement('div');
      e.className = 'empty'; e.id = 'emptystate';
      e.innerHTML = '<div class="big">📭</div><div class="t">אין הודעות עדיין — הן יופיעו כאן ברגע שיגיעו.<br>' +
        'אם זה נמשך, לחץ "פירוט" בפס העליון כדי לראות מה קורה מול טלגרם.</div>';
      msgs.appendChild(e);
    }
  }

  // ----- message flow -------------------------------------------------------
  function noteItem(it) {
    if (!(it.channel in oldestPer) || it.id < oldestPer[it.channel]) oldestPer[it.channel] = it.id;
    var lm = lastMsg[it.channel];
    if (!lm || it.ts >= lm.ts) lastMsg[it.channel] = { ts: it.ts, preview: it.preview };
  }

  function makeNode(it) {
    var holder = document.createElement('div');
    holder.innerHTML = it.html || '';
    // Skip any leading text/whitespace node so we always grab the <article>.
    var node = holder.querySelector ? holder.querySelector('.msg') : holder.firstElementChild;
    if (!node) { return null; }
    node.setAttribute('data-ts', it.ts);
    noteItem(it);
    if (current !== 'all' && it.channel !== current) node.classList.add('hidden');
    // Images fade in as they load instead of popping.
    var imgs = node.querySelectorAll('.photo img');
    for (var i = 0; i < imgs.length; i++) {
      (function (im) {
        if (im.complete) { im.classList.add('ld'); return; }
        im.onload = function () { im.classList.add('ld'); };
        im.onerror = function () { im.classList.add('ld'); };
      })(imgs[i]);
    }
    return node;
  }

  // Keep the DOM bounded on very long sessions — but never yank content out
  // from under someone who scrolled up to read.
  function trimDom() {
    if (!nearBottom()) return;
    var nodes = msgs.querySelectorAll('.msg');
    var extra = nodes.length - 1500;
    for (var i = 0; i < extra; i++) nodes[i].remove();
  }

  // Long messages collapse with a fade + "show more" toggle.
  function applyClamp(node) {
    var t = node.querySelector('.msgtext');
    if (!t || t.getAttribute('data-clamped')) return;
    t.setAttribute('data-clamped', '1');
    if (t.scrollHeight <= 290) return;
    t.classList.add('clamped');
    var b = document.createElement('div');
    b.className = 'morebtn';
    b.textContent = 'הצג עוד';
    b.onclick = function () {
      if (t.classList.contains('clamped')) {
        t.classList.remove('clamped');
        b.textContent = 'הצג פחות';
      } else {
        t.classList.add('clamped');
        b.textContent = 'הצג עוד';
      }
    };
    t.parentNode.insertBefore(b, t.nextSibling);
  }

  function insertNodeBottom(it) {
    var n = makeNode(it);
    if (!n) return null;
    msgs.appendChild(n);
    applyClamp(n);
    return n;
  }

  // Any inline video that fails to stream straight from the CDN gets one
  // automatic retry THROUGH the app's own streaming proxy.
  msgs.addEventListener('error', function (ev) {
    var v = ev.target;
    if (!v || v.tagName !== 'VIDEO') return;
    var proxy = v.getAttribute('data-proxy');
    if (!proxy || v.getAttribute('data-proxied')) return;
    v.setAttribute('data-proxied', '1');
    var t = v.currentTime || 0;
    v.src = proxy;
    v.load();
    if (t > 0.5) { v.currentTime = t; }
    var p = v.play();
    if (p && p.catch) p.catch(function () {});
  }, true);

  // Long videos: clicking the preview asks OUR server for the direct video
  // file and plays it in the page's own player. Fallback chain, all
  // automatic: direct CDN → streaming through the app → clear message.
  msgs.addEventListener('click', function (ev) {
    var wrap = ev.target.closest ? ev.target.closest('.embedwrap') : null;
    if (!wrap || wrap.getAttribute('data-loading')) return;
    var post = wrap.getAttribute('data-embed');
    if (!post || !/^[A-Za-z0-9_]+\/\d+$/.test(post)) return;
    var parts = post.split('/');
    var proxyURL = '/api/media?channel=' + encodeURIComponent(parts[0]) + '&id=' + parts[1];
    var poster = '';
    var img = wrap.querySelector('img');
    if (img) poster = img.src;
    var savedHTML = wrap.innerHTML;

    wrap.setAttribute('data-loading', '1');
    var pb = wrap.querySelector('.playbtn');
    if (pb) pb.innerHTML = '<span class="spin" style="margin:0"></span>';

    function restore(msg) {
      wrap.innerHTML = savedHTML;
      wrap.classList.add('embedwrap');
      wrap.removeAttribute('data-loading');
      if (msg) toast(msg);
    }

    function playWith(src, allowProxyRetry) {
      wrap.classList.remove('embedwrap');
      wrap.removeAttribute('data-loading');
      wrap.innerHTML = '';
      var v = document.createElement('video');
      v.src = src;
      if (poster) v.poster = poster;
      v.controls = true;
      v.autoplay = true;
      v.setAttribute('playsinline', '');
      if (allowProxyRetry) {
        v.setAttribute('data-proxy', proxyURL); // global error handler retries via proxy
      } else {
        v.onerror = function () { restore('הסרטון לא זמין לצפייה כרגע — נסה שוב מאוחר יותר'); };
      }
      wrap.appendChild(v);
      var p = v.play();
      if (p && p.catch) p.catch(function () {});
    }

    fetch('/api/video?channel=' + encodeURIComponent(parts[0]) + '&id=' + parts[1])
      .then(function (r) { return r.json(); })
      .then(function (d) {
        if (d.ok && d.big) {
          // Large video via the connected Telegram account — the proxy
          // downloads it server-side (may take a bit for big files).
          toast('מוריד סרטון ארוך דרך חשבון הטלגרם… (עשוי לקחת רגע)');
          playWith(proxyURL, false);
        } else if (d.ok && d.url) {
          playWith(d.url, true); // direct first; proxy retry is automatic
        } else if (d.pending) {
          // Not available yet (usually still processing on Telegram's side).
          // The app keeps retrying in the background and will announce here
          // the moment it becomes playable.
          restore('הסרטון עדיין בעיבוד אצל טלגרם — אנסה שוב ברקע ואודיע ברגע שיהיה מוכן');
        } else {
          // Resolution failed for another reason — the proxy re-resolves
          // server-side with every strategy, so give it the last word.
          playWith(proxyURL, false);
        }
      })
      .catch(function () { restore('שגיאת תקשורת'); });
  });

  function appendBottom(it, fresh) {
    if (document.getElementById('m' + it.key)) return;
    var wasNear = nearBottom();
    var node = makeNode(it);
    if (!node) return;
    if (fresh) {
      node.classList.add('fresh');
      setTimeout(function () { node.classList.remove('fresh'); }, 4000);
    }
    msgs.appendChild(node);
    applyClamp(node);
    trimDom();
    rebuildSeparators();
    renderEmptyState();

    var visible = (current === 'all' || it.channel === current);
    if (fresh) {
      if (visible && wasNear && !document.hidden) {
        scrollBottom(false);
      } else if (visible) {
        pendingJump++; showJump();
      } else {
        unread[it.channel] = (unread[it.channel] || 0) + 1;
      }
      if (document.hidden) {
        titleUnread++;
        document.title = '(' + titleUnread + ') ' + baseTitle;
      }
    }
    renderSidebar();
    renderHead();
  }

  function prependTop(items) { // oldest..newest batch
    var before = chat.scrollHeight;
    for (var i = items.length - 1; i >= 0; i--) {
      var it = items[i];
      if (document.getElementById('m' + it.key)) continue;
      var n = makeNode(it);
      if (!n) continue;
      msgs.insertBefore(n, msgs.firstChild);
      applyClamp(n);
    }
    rebuildSeparators();
    chat.scrollTop += (chat.scrollHeight - before);
    renderHead();
  }

  var pendingJump = 0;
  function showJump() {
    document.getElementById('jumpcnt').textContent = pendingJump;
    document.getElementById('jump').style.display = 'flex';
  }
  function hideJump() {
    pendingJump = 0;
    document.getElementById('jump').style.display = 'none';
  }
  document.getElementById('jump').onclick = function () { scrollBottom(false); hideJump(); };
  document.getElementById('downbtn').onclick = function () { scrollBottom(false); hideJump(); };
  chat.addEventListener('scroll', function () {
    if (nearBottom()) hideJump();
    document.getElementById('downbtn').style.display = nearBottom() ? 'none' : 'flex';
    if (chat.scrollTop < 70) maybeLoadOlder();
  });

  document.addEventListener('visibilitychange', function () {
    if (!document.hidden) {
      titleUnread = 0;
      document.title = baseTitle;
      resync(); // pick up anything that arrived while the tab slept
    }
  });

  // ----- history ------------------------------------------------------------
  function scopeChannels() { return current === 'all' ? chanNames() : [current]; }
  function scopeDone() {
    var sc = scopeChannels();
    for (var i = 0; i < sc.length; i++) if (!donePer[sc[i]]) return false;
    return sc.length > 0;
  }

  function maybeLoadOlder() {
    if (loadingOlder || scopeDone() || visibleCount() === 0) return;
    loadingOlder = true;
    document.getElementById('topload').style.display = 'block';

    var collected = [];
    var chain = Promise.resolve();
    scopeChannels().forEach(function (ch) {
      chain = chain.then(function () {
        if (donePer[ch]) return;
        var before = oldestPer[ch];
        if (!before) { donePer[ch] = true; return; }
        return fetch('/api/older?channel=' + encodeURIComponent(ch) + '&before=' + before)
          .then(function (r) { return r.json(); })
          .then(function (data) {
            var fresh = (data.items || []).filter(function (it) {
              return !document.getElementById('m' + it.key);
            });
            if (!fresh.length) donePer[ch] = true;
            collected = collected.concat(fresh);
          });
      });
    });
    chain.then(function () {
      collected.sort(function (a, b) { return (a.ts - b.ts) || (a.id - b.id); });
      if (collected.length) prependTop(collected);
      loadingOlder = false;
      document.getElementById('topload').style.display = 'none';
    }).catch(function () {
      loadingOlder = false;
      document.getElementById('topload').style.display = 'none';
    });
  }

  // ----- add-channel modal --------------------------------------------------
  function openModal() {
    document.getElementById('chanerr').textContent = '';
    document.getElementById('chaninput').value = '';
    document.getElementById('modalback').style.display = 'flex';
    setTimeout(function () { document.getElementById('chaninput').focus(); }, 50);
  }
  function closeModal() { document.getElementById('modalback').style.display = 'none'; }
  document.getElementById('addbtn').onclick = openModal;
  document.getElementById('chancancel').onclick = closeModal;
  document.getElementById('modalback').onclick = function (ev) { if (ev.target === this) closeModal(); };
  document.getElementById('chaninput').onkeydown = function (ev) { if (ev.key === 'Enter') submitChannel(); };
  document.getElementById('chanok').onclick = submitChannel;

  function submitChannel() {
    var v = document.getElementById('chaninput').value.trim();
    if (!v) return;
    var ok = document.getElementById('chanok');
    ok.disabled = true; ok.textContent = 'בודק…';
    fetch('/api/channels', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ channel: v })
    })
    .then(function (r) { return r.json(); })
    .then(function (data) {
      if (data.ok) { location.reload(); return; }
      document.getElementById('chanerr').textContent = data.error || 'שגיאה לא ידועה';
      ok.disabled = false; ok.textContent = 'הוסף';
    })
    .catch(function () {
      document.getElementById('chanerr').textContent = 'שגיאת תקשורת';
      ok.disabled = false; ok.textContent = 'הוסף';
    });
  }

  // ----- boot ---------------------------------------------------------------
  // Each message renders in isolation: one bad post can never blank the page.
  fetch('/api/channels').then(function (r) { return r.json(); }).then(function (d) {
    channels = d.channels || [];
    renderSidebar();
    return fetch('/api/messages');
  }).then(function (r) { return r.json(); }).then(function (data) {
    var items = (data.items || []).slice();
    items.sort(function (a, b) { return (a.ts - b.ts) || (a.id - b.id); }); // oldest first
    items.forEach(function (it) {
      try {
        if (!document.getElementById('m' + it.key)) insertNodeBottom(it);
      } catch (e) {}
    });
    rebuildSeparators();
    renderEmptyState();
    renderSidebar();
    renderHead();
    scrollBottom(true);
  }).catch(function (e) {
    var meta = document.getElementById('emptystate');
    var m = document.getElementById('msgs');
    if (m) m.innerHTML = '<div class="empty"><div class="big">⚠️</div><div class="t">לא הצלחתי לטעון את ההודעות. ודא שהתוכנה רצה (Start.bat) ורענן.</div></div>';
  });

  // ----- connection status --------------------------------------------------
  // Polls the server's own view of Telegram. When Telegram throttles us the
  // feed goes quiet through no fault of the app, and the user deserves to be
  // told that in words rather than left staring at an empty pane.
  var statusOpen = false;

  function fmtAgo(unix) {
    if (!unix) return 'טרם';
    var s = Math.max(0, Math.floor(Date.now() / 1000 - unix));
    if (s < 60) return 'לפני ' + s + ' שנ׳';
    if (s < 3600) return 'לפני ' + Math.floor(s / 60) + ' דק׳';
    return 'לפני ' + Math.floor(s / 3600) + ' שע׳';
  }

  function renderStatusPanel(rows) {
    var p = document.getElementById('statuspanel');
    if (!p) return;
    if (!statusOpen) { p.style.display = 'none'; return; }
    p.style.display = 'block';
    p.innerHTML = '';
    (rows || []).forEach(function (s) {
      var row = document.createElement('div');
      row.className = 'strow';

      var name = document.createElement('div');
      name.className = 'stname';
      name.textContent = '@' + s.channel;
      row.appendChild(name);

      var badge = document.createElement('span');
      if (s.ok) {
        badge.className = 'stbadge good';
        badge.textContent = s.messages + ' הודעות · ' + fmtAgo(s.last_ok);
      } else if (s.retry_in > 0) {
        badge.className = 'stbadge wait';
        badge.textContent = 'ממתין ' + s.retry_in + ' שנ׳';
      } else if (s.error) {
        badge.className = 'stbadge bad';
        badge.textContent = s.error;
      } else {
        badge.className = 'stbadge';
        badge.textContent = 'טרם נבדק';
      }
      row.appendChild(badge);
      p.appendChild(row);
    });
  }

  function pollStatus() {
    fetch('/api/status').then(function (r) { return r.json(); }).then(function (d) {
      var bar = document.getElementById('statusbar');
      var txt = document.getElementById('statustext');
      var ico = document.getElementById('statusicon');
      if (!bar || !txt) return;

      if (d.banner) {
        bar.style.display = 'flex';
        txt.textContent = d.banner;
        if (d.blocked) {
          bar.classList.remove('calm');
          ico.textContent = '⏳';
        } else {
          bar.classList.add('calm');
          ico.textContent = 'ℹ️';
        }
      } else {
        bar.style.display = 'none';
        if (!statusOpen) {
          var p = document.getElementById('statuspanel');
          if (p) p.style.display = 'none';
        }
      }
      renderStatusPanel(d.channels);
    }).catch(function () {});
  }

  (function () {
    var more = document.getElementById('statusmore');
    if (more) {
      more.onclick = function () {
        statusOpen = !statusOpen;
        more.textContent = statusOpen ? 'הסתר' : 'פירוט';
        pollStatus();
      };
    }
    pollStatus();
    setInterval(pollStatus, 10000);
  })();

  // ----- archive search -----------------------------------------------------
  var searchTimer = null;
  var searchOpen = false;

  function openSearch() {
    searchOpen = true;
    document.getElementById('searchbar').classList.add('open');
    document.getElementById('msgs').style.display = 'none';
    document.getElementById('topload').style.display = 'none';
    document.getElementById('results').style.display = 'block';
    document.getElementById('resultsmeta').textContent = 'הקלד כדי לחפש בכל מה שנשמר אי פעם';
    document.getElementById('resultslist').innerHTML = '';
    setTimeout(function () { document.getElementById('searchinput').focus(); }, 40);
  }
  function closeSearch() {
    searchOpen = false;
    document.getElementById('searchbar').classList.remove('open');
    document.getElementById('searchinput').value = '';
    document.getElementById('results').style.display = 'none';
    document.getElementById('msgs').style.display = 'block';
    scrollBottom(true);
  }
  document.getElementById('searchbtn').onclick = function () {
    if (searchOpen) closeSearch(); else openSearch();
  };
  document.getElementById('searchclose').onclick = closeSearch;
  document.addEventListener('keydown', function (ev) {
    if (ev.key === 'Escape' && searchOpen) closeSearch();
  });

  function markHits(node, q) {
    var texts = node.querySelectorAll('.msgtext');
    for (var i = 0; i < texts.length; i++) {
      var walker = document.createTreeWalker(texts[i], NodeFilter.SHOW_TEXT);
      var found = [];
      while (walker.nextNode()) found.push(walker.currentNode);
      found.forEach(function (tn) {
        var idx = tn.textContent.toLowerCase().indexOf(q.toLowerCase());
        if (idx < 0) return;
        var range = document.createRange();
        range.setStart(tn, idx);
        range.setEnd(tn, idx + q.length);
        var m = document.createElement('mark');
        try { range.surroundContents(m); } catch (e) {}
      });
    }
  }

  function runSearch() {
    var q = document.getElementById('searchinput').value.trim();
    var meta = document.getElementById('resultsmeta');
    var list = document.getElementById('resultslist');
    if (q.length < 2) {
      meta.textContent = 'הקלד לפחות שני תווים';
      list.innerHTML = '';
      return;
    }
    meta.innerHTML = '<span class="spin"></span> מחפש…';
    var scope = (current === 'all') ? '' : current;
    fetch('/api/search?q=' + encodeURIComponent(q) + '&channel=' + encodeURIComponent(scope))
      .then(function (r) { return r.json(); })
      .then(function (d) {
        if (!searchOpen) return;
        var items = d.items || [];
        list.innerHTML = '';
        items.forEach(function (it) {
          var holder = document.createElement('div');
          holder.innerHTML = it.html || '';
          var node = holder.querySelector('.msg');
          if (!node) return;
          node.id = 'sr_' + it.key; // never collide with the live timeline
          markHits(node, q);
          list.appendChild(node);
          applyClamp(node);
        });
        var scopeTxt = scope ? ('ב-@' + scope) : 'בכל הערוצים';
        meta.textContent = items.length
          ? items.length + ' תוצאות ' + scopeTxt + ' · הארכיון מחזיק ' + (d.total || 0) + ' הודעות'
          : 'אין תוצאות ' + scopeTxt + ' · הארכיון מחזיק ' + (d.total || 0) + ' הודעות';
        chat.scrollTop = 0;
      })
      .catch(function () { meta.textContent = 'שגיאת תקשורת'; });
  }
  document.getElementById('searchinput').addEventListener('input', function () {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(runSearch, 300);
  });

  // ----- photo lightbox -----------------------------------------------------
  msgs.addEventListener('click', function (ev) {
    var img = ev.target;
    if (!img || img.tagName !== 'IMG') return;
    if (img.closest && img.closest('.vidwrap')) return; // video thumbs have their own click
    document.getElementById('lightboximg').src = img.src;
    document.getElementById('lightbox').style.display = 'flex';
  });
  document.getElementById('lightbox').onclick = function () { this.style.display = 'none'; };
  document.addEventListener('keydown', function (ev) {
    if (ev.key === 'Escape') document.getElementById('lightbox').style.display = 'none';
  });

  // ----- theme --------------------------------------------------------------
  var theme = 'dark';
  function applyTheme() {
    document.body.classList.toggle('light', theme === 'light');
    document.getElementById('themebtn').textContent = theme === 'light' ? '🌙' : '🌓';
  }
  document.getElementById('themebtn').onclick = function () {
    theme = (theme === 'light') ? 'dark' : 'light';
    applyTheme();
    fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ theme: theme })
    }).catch(function () {});
  };

  // ----- autostart ----------------------------------------------------------
  fetch('/api/autostart').then(function (r) { return r.json(); }).then(function (d) {
    var row = document.getElementById('autostartrow');
    row.style.display = 'block';
    document.getElementById('autostartchk').checked = !!d.on;
  }).catch(function () {});
  document.getElementById('autostartchk').onchange = function () {
    var want = this.checked;
    var chk = this;
    fetch('/api/autostart', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ on: want })
    }).then(function (r) { return r.json(); }).then(function (d) {
      if (d.ok) {
        toast(want ? 'התוכנה תעלה אוטומטית עם ווינדוס' : 'ההפעלה האוטומטית בוטלה');
      } else {
        chk.checked = !want;
        toast(d.error || 'שגיאה');
      }
    }).catch(function () { chk.checked = !want; toast('שגיאת תקשורת'); });
  };

  // ----- settings toggles ---------------------------------------------------
  var setPopups = true, setSound = true;
  function paintToggles() {
    var p = document.getElementById('tglpopups');
    p.textContent = setPopups ? '🔔' : '🔕';
    p.className = 'iconbtn' + (setPopups ? '' : ' off');
    var s = document.getElementById('tglsound');
    s.textContent = setSound ? '🔊' : '🔇';
    s.className = 'iconbtn' + (setSound ? '' : ' off');
  }
  function toast(text) {
    var t = document.getElementById('toast');
    t.textContent = text;
    t.style.display = 'block';
    clearTimeout(t._h);
    t._h = setTimeout(function () { t.style.display = 'none'; }, 2200);
  }
  function pushSettings(body, after) {
    fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }).then(function (r) { return r.json(); }).then(function (d) {
      if (typeof d.popups === 'boolean') setPopups = d.popups;
      if (typeof d.sound === 'boolean') setSound = d.sound;
      paintToggles();
      if (after) after();
    }).catch(function () { toast('שגיאת תקשורת'); });
  }
  document.getElementById('refreshbtn').onclick = function () {
    var btn = this;
    btn.classList.add('spinning');
    fetch('/api/refresh', { method: 'POST' })
      .then(function (r) { return r.json(); })
      .then(function () {
        toast('בודק עכשיו את כל הערוצים…');
        // The scan itself takes a few seconds; also resync in case something
        // arrived while we were not looking.
        setTimeout(function () { resync(); btn.classList.remove('spinning'); }, 4000);
      })
      .catch(function () {
        btn.classList.remove('spinning');
        toast('שגיאת תקשורת');
      });
  };

  document.getElementById('tglpopups').onclick = function () {
    pushSettings({ popups: !setPopups }, function () {
      toast(setPopups ? 'חלוניות קופצות הופעלו' : 'חלוניות קופצות כובו — ההודעות ממשיכות להגיע לדף');
    });
  };
  document.getElementById('tglsound').onclick = function () {
    pushSettings({ sound: !setSound }, function () {
      toast(setSound ? 'צליל התראה הופעל' : 'צליל התראה כובה');
    });
  };
  fetch('/api/settings').then(function (r) { return r.json(); }).then(function (d) {
    setPopups = d.popups !== false;
    setSound = d.sound !== false;
    if (d.theme === 'light') { theme = 'light'; applyTheme(); }
    paintToggles();
  });

  // ----- live stream, with self-healing resync ------------------------------
  // If the app restarted or the connection blipped, the tab quietly refetches
  // everything it missed instead of silently going stale.
  function resync() {
    fetch('/api/messages').then(function (r) { return r.json(); }).then(function (data) {
      var items = (data.items || []).slice();
      items.sort(function (a, b) { return (a.ts - b.ts) || (a.id - b.id); });
      var added = 0;
      items.forEach(function (it) {
        if (!document.getElementById('m' + it.key)) {
          insertNodeBottom(it);
          added++;
        }
      });
      if (added) {
        trimDom();
        rebuildSeparators();
        renderEmptyState();
        renderSidebar();
        renderHead();
        if (nearBottom()) scrollBottom(true);
      }
    }).catch(function () {});
  }

  var es = new EventSource('/api/stream');
  es.onopen = function () {
    setStatus(true);
    // Always pull current state on connect — covers the first-run case where
    // the tab opened before the initial scan populated anything, and any
    // messages that landed during a brief disconnect.
    resync();
  };
  es.onerror = function () { setStatus(false); };
  // Safety net: a slow first scan can finish after boot; poll a few times
  // early on so the page never sits empty waiting for a broadcast.
  var bootPolls = 0;
  var bootTimer = setInterval(function () {
    bootPolls++;
    resync();
    if (bootPolls >= 6) clearInterval(bootTimer); // ~30s of coverage
  }, 5000);
  es.onmessage = function (ev) {
    try {
      var d = JSON.parse(ev.data);
      if (d.type === 'videoready') {
        // A video that previously failed is now playable — light it up.
        var wrap = document.querySelector('[data-embed="' + d.channel + '/' + d.id + '"]');
        if (wrap) {
          wrap.classList.add('vidready');
          var pb = wrap.querySelector('.playbtn');
          if (pb) pb.innerHTML = '<span></span>';
        }
        toast('סרטון שלא היה זמין מוכן עכשיו לצפייה 🎬');
        return;
      }
      if (d.html) appendBottom(d, true);
    } catch (e) {}
  };
</script>
</body>
</html>`
