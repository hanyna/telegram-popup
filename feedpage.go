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
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="referrer" content="no-referrer">
<meta name="theme-color" content="#0f1320" media="(prefers-color-scheme: dark)">
<meta name="theme-color" content="#dfe6f0" media="(prefers-color-scheme: light)">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
<meta name="apple-mobile-web-app-title" content="מבזקי ארץ ישראל">
<meta name="mobile-web-app-capable" content="yes">
<link rel="manifest" href="/manifest.webmanifest">
<link rel="apple-touch-icon" href="/apple-touch-icon.png">
<title>מבזקי ארץ ישראל</title>
<script>
  // Theme before first paint: no dark flash for light-mode users (and the
  // other way round). The server's saved theme still wins once it loads.
  try { if (localStorage.getItem('tgpopup-theme') === 'light') document.documentElement.classList.add('light'); } catch (e) {}
  try {
    var A = JSON.parse(localStorage.getItem('tgpopup-a11y') || '{}') || {};
    var H = document.documentElement;
    ['contrast','gray','links','font','still'].forEach(function (k) { if (A[k]) H.classList.add('a11y-' + k); });
    if (A.z) H.style.setProperty('--z', A.z);
  } catch (e) {}
</script>
<link rel="icon" href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'%3E%3Cdefs%3E%3ClinearGradient id='g' x1='0' y1='0' x2='1' y2='1'%3E%3Cstop offset='0' stop-color='%2337aee2'/%3E%3Cstop offset='1' stop-color='%2331c48d'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect x='4' y='4' width='56' height='56' rx='16' fill='url(%23g)'/%3E%3Cpath d='M18 20h28a4 4 0 014 4v12a4 4 0 01-4 4H31l-8 7v-7h-5a4 4 0 01-4-4V24a4 4 0 014-4z' fill='%23fff'/%3E%3C/svg%3E">
<style>
  /* Quiet palette: low-saturation slate, one calm blue accent, soft lines.
     Nothing shouts; structure comes from spacing, not colour. */
  :root {
    --bg:#0d1016; --bg2:#11151c;
    --panel:#141922; --panel2:#171d27;
    --bubble:#1a212b; --bubble2:#1f2732;
    --line:rgba(255,255,255,0.065);
    --line2:rgba(255,255,255,0.11);
    --fg:#e6e9ef; --muted:#8c94a2; --dim:#5f6775;
    --accent:#4a93c4; --accent2:#4aa88a;
    --headbg:#11151c;
    --markbg:rgba(74,147,196,0.30);
  }
  body.light, html.light body {
    --bg:#eef1f5; --bg2:#e8ecf2;
    --panel:#ffffff; --panel2:#f6f8fb;
    --bubble:#ffffff; --bubble2:#f8fafc;
    --line:rgba(22,30,46,0.10);
    --line2:rgba(22,30,46,0.17);
    --fg:#20262f; --muted:#5a6475; --dim:#8690a0;
    --accent:#2b7fb8; --accent2:#2e8f6e;
    --headbg:#ffffff;
    --markbg:rgba(43,127,184,0.22);
  }
  * { margin:0; padding:0; box-sizing:border-box; }
  html, body { height:100%; }
  body {
    background:var(--bg);
    color:var(--fg);
    font-family:system-ui,-apple-system,'Segoe UI','Noto Sans Hebrew','Heebo',Arial,sans-serif;
    font-size:14px; -webkit-font-smoothing:antialiased; text-rendering:optimizeLegibility;
    overflow:hidden;
  }
  ::selection { background:rgba(74,147,196,0.35); }
  ::-webkit-scrollbar { width:8px; height:8px; }
  ::-webkit-scrollbar-thumb { background:rgba(255,255,255,0.10); border-radius:99px; }
  ::-webkit-scrollbar-thumb:hover { background:rgba(255,255,255,0.18); }
  ::-webkit-scrollbar-track { background:transparent; }

  #app { display:flex; height:100vh; height:100dvh; }
  /* Reduced motion: no entrance animations, no spinners spinning. */
  @media (prefers-reduced-motion: reduce) {
    .msg, #jump, #toast, .modal, #lightbox img { animation:none !important; }
  }

  /* ===== sidebar ========================================================= */
  #side {
    width:288px; flex:none;
    background:var(--panel);
    border-left:1px solid var(--line);
    display:flex; flex-direction:column;
  }
  .sidehead {
    display:flex; align-items:center; gap:11px;
    padding:16px 16px 12px;
    border-bottom:1px solid var(--line);
  }
  .applogo {
    width:36px; height:36px; border-radius:11px; flex:none;
    background:linear-gradient(135deg,var(--accent),var(--accent2));
    display:flex; align-items:center; justify-content:center;
    box-shadow:0 2px 8px rgba(0,0,0,0.25);
  }
  .applogo svg { width:20px; height:20px; }
  .appmeta { min-width:0; }
  .appname { font-size:14px; font-weight:700; letter-spacing:0.2px; }
  .status { font-size:11px; color:var(--muted); display:flex; align-items:center; gap:5px; margin-top:1px; }
  .dot { width:7px; height:7px; border-radius:50%; background:#e05252; flex:none; transition:background 0.3s; }
  .dot.on { background:var(--accent2); box-shadow:0 0 6px rgba(74,168,138,0.8); }

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
    background:linear-gradient(135deg,rgba(74,147,196,0.22),rgba(30,136,210,0.12));
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
    background:linear-gradient(135deg,var(--accent),var(--accent));
    color:#fff; font-size:11px; font-weight:700;
  }
  .badge.show { display:flex; }
  .avawrap { position:relative; flex:none; }
  .avabadge {
    position:absolute; top:-3px; left:-3px; display:none;
    min-width:18px; height:18px; padding:0 5px; border-radius:99px;
    background:linear-gradient(135deg,var(--accent),var(--accent)); color:#fff;
    font-size:10.5px; font-weight:700; align-items:center; justify-content:center;
    border:2px solid var(--panel);
  }
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
    background:linear-gradient(135deg,var(--accent),var(--accent));
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
    border-bottom:1px solid var(--line);
    z-index:5;
  }
  #headava { width:38px; height:38px; border-radius:50%; flex:none;
    display:flex; align-items:center; justify-content:center;
    color:#fff; font-weight:700; font-size:15px; }
  .headmid { flex:1; min-width:0; }
  #headtitle { font-size:14.5px; font-weight:700; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  #headsub { font-size:11.5px; color:var(--muted); margin-top:1px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .headbtns { display:flex; gap:8px; flex:none; }
  #morebtn { display:none; }
  #moremenu {
    position:absolute; top:58px; left:16px; z-index:30; display:none;
    background:var(--panel); border:1px solid var(--line); border-radius:14px;
    box-shadow:0 14px 40px rgba(0,0,0,0.45); padding:6px; min-width:210px;
  }
  #moremenu.open { display:block; }
  /* media filter chips */
  #filters { display:none; gap:6px; padding:8px 14px 0; overflow-x:auto; scrollbar-width:none; -webkit-overflow-scrolling:touch; }
  #filters::-webkit-scrollbar { display:none; }
  .fchip { flex:none; }
  #filters.open { display:flex; }
  .fchip { font-size:12.5px; padding:5px 12px; border-radius:99px; border:1px solid var(--line); cursor:pointer; color:var(--muted); user-select:none; }
  .fchip.on { background:rgba(74,147,196,0.15); color:var(--accent); border-color:rgba(74,147,196,0.4); }
  .msg.fhide { display:none; }
  /* message action sheet (long press / right click) */
  #msgsheet { position:fixed; left:0; right:0; bottom:0; z-index:46; display:none; background:var(--panel);
    border-top:1px solid var(--line); border-radius:18px 18px 0 0; padding:8px 8px calc(10px + env(safe-area-inset-bottom));
    box-shadow:0 -10px 40px rgba(0,0,0,0.45); }
  #msgsheet.open { display:block; }
  #sheetback { position:fixed; inset:0; z-index:45; display:none; background:rgba(0,0,0,0.35); }
  #sheetback.open { display:block; }
  .sitem { display:flex; align-items:center; gap:12px; padding:13px 14px; border-radius:12px; font-size:15px; cursor:pointer; }
  .sitem:hover { background:rgba(74,147,196,0.12); }
  .sitem .mi { width:24px; text-align:center; }
  @media (min-width:681px) { #msgsheet { left:50%; right:auto; bottom:auto; top:50%; transform:translate(-50%,-50%); width:320px; border-radius:16px; border:1px solid var(--line); } }
  .mitem {
    display:flex; align-items:center; gap:10px; padding:10px 12px;
    border-radius:10px; font-size:13.5px; cursor:pointer; user-select:none;
  }
  .mitem:hover { background:rgba(74,147,196,0.12); }
  .mitem .mi { width:22px; text-align:center; font-size:16px; }
  #installrow { display:none; padding:10px 12px 0; }
  #installbtn {
    width:100%; font:inherit; font-size:12.5px; font-weight:600; color:var(--fg);
    border:1px solid rgba(74,147,196,0.4); border-radius:12px; padding:9px 0;
    background:rgba(74,147,196,0.10); cursor:pointer;
  }
  #iosinstall { font-size:11.5px; color:var(--muted); line-height:1.5; }
  #wakehint { display:none; text-align:center; color:var(--muted); font-size:13px; padding:8px 0 16px; line-height:1.6; }

  .iconbtn svg { width:19px; height:19px; stroke:currentColor; fill:none; stroke-width:1.7; stroke-linecap:round; stroke-linejoin:round; display:block; }
  .iconbtn.on { color:var(--accent); background:rgba(74,147,196,0.14); }
  .iconbtn {
    width:36px; height:36px; border-radius:50%; flex:none;
    display:flex; align-items:center; justify-content:center;
    background:rgba(255,255,255,0.05); border:1px solid var(--line);
    font-size:16px; cursor:pointer; user-select:none;
    transition:background 0.12s, border-color 0.12s, opacity 0.12s;
  }
  .iconbtn:hover { background:rgba(74,147,196,0.14); border-color:rgba(74,147,196,0.4); }
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
    font:inherit; font-size:16px; padding:9px 18px; min-width:0;
    outline:none; transition:border-color 0.15s;
  }
  #searchinput:focus { border-color:rgba(74,147,196,0.55); box-shadow:0 0 0 3px rgba(74,147,196,0.10); }
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

  .photo img { opacity:0; transition:opacity 0.35s ease; background:var(--bubble2);
    max-height:min(46vh, 420px); object-fit:cover; cursor:zoom-in; }
  .album img { max-height:min(34vh, 300px); }
  .photo img.ld { opacity:1; }
  .photo { position:relative; }
  /* a blocked/held image keeps its reserved box and offers a reload tap */
  .imgretry { position:absolute; inset:0; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:6px;
    background:var(--bubble2); border:1px solid var(--line); border-radius:11px; color:var(--muted); font-size:12.5px; cursor:pointer; text-align:center; padding:10px; }
  .imgretry svg { width:22px; height:22px; stroke:currentColor; fill:none; stroke-width:1.7; }
  .imgretry.spin svg { animation:spin 0.8s linear infinite; }

  #chat {
    flex:1; overflow-y:auto;
    background:
      radial-gradient(1000px 500px at 85% -5%, rgba(74,147,196,0.07), transparent 55%),
      radial-gradient(800px 400px at 10% 105%, rgba(74,168,138,0.05), transparent 55%),
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
    background:rgba(74,147,196,0.09);
    border-bottom-color:rgba(74,147,196,0.26);
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
  .stbadge.good { background:rgba(74,168,138,0.16); color:var(--accent2); }
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
  }

  .msg {
    position:relative;
    background:var(--bubble);
    border:1px solid var(--line);
    border-radius:14px 14px 14px 4px;
    padding:8px 12px 9px;
    margin-bottom:7px;
    width:fit-content;
    min-width:180px;
    max-width:min(600px, 86%);
    box-shadow:0 1px 2px rgba(0,0,0,0.12);
    /* A thin edge in the channel's colour — quiet identity, no loud header. */
    border-inline-start:2.5px solid var(--chan, var(--line2));
    /* Off-screen bubbles skip layout & paint entirely — with hundreds of
       messages loaded this is the difference between smooth and sluggish. */
    content-visibility:auto;
    contain-intrinsic-size:auto 140px;
  }
  /* Only a message that just arrived animates in — switching channels used
     to replay the entrance on every one of hundreds of bubbles. */
  .msg.fresh { animation:rise 0.22s ease-out; }
  .msg.fresh::after { content:''; position:absolute; inset:-1px; border-radius:inherit; box-shadow:0 0 0 1px rgba(74,147,196,0.45); pointer-events:none; animation:fadeedge 2.4s ease-out forwards; }
  @keyframes fadeedge { from { opacity:1; } to { opacity:0; } }
  .msg.hidden { display:none; }
  @keyframes rise { from { opacity:0; transform:translateY(8px); } to { opacity:1; transform:none; } }

  .msghead {
    display:flex; align-items:center; justify-content:space-between; gap:10px;
    margin-bottom:3px; font-size:11px;
  }
  .msgchan { color:var(--chan, var(--accent2)); font-weight:600; cursor:pointer; letter-spacing:0.1px; direction:ltr; unicode-bidi:isolate; }
  .msgchan:hover { text-decoration:underline; }
  body.single .msgchan { display:none; }
  .msgtime { color:var(--muted); direction:ltr; }

  :root { --txt:15px; }
  .msg:has(.photo, .album, .vidwrap, .toobig, .linkcard) { width:min(460px, 86%); }
  body.txt-l { --txt:16px; }
  body.txt-xl { --txt:18.5px; }
  .msgtext { font-size:var(--txt); line-height:1.6; word-wrap:break-word; overflow-wrap:anywhere; unicode-bidi:plaintext; }
  /* reactions: read-only counts from Telegram's public page */
  .reacts { display:flex; flex-wrap:wrap; gap:5px; margin-top:8px; }
  .react { display:inline-flex; align-items:center; gap:4px; padding:2px 8px 2px 7px;
    background:var(--bubble2); border:1px solid var(--line); border-radius:99px;
    font-size:12px; line-height:1.5; color:var(--muted); }
  .react .re { font-size:13px; }
  .react .rc { font-variant-numeric:tabular-nums; }
  /* the viewer's own reaction (saved on this device, never sent anywhere) */
  .react.mine { background:rgba(74,147,196,0.18); border-color:rgba(74,147,196,0.5); color:var(--fg); cursor:pointer; }
  .addreact { position:absolute; left:8px; bottom:-9px; z-index:2; background:var(--panel); display:inline-flex; align-items:center; justify-content:center; width:24px; height:22px;
    border:1px dashed var(--line2); border-radius:99px; color:var(--muted); cursor:pointer; font-size:13px;
    opacity:0; transition:opacity 0.12s; }
  .msg:hover .addreact, .addreact:focus-visible { opacity:1; }
  @media (hover:none) { .addreact { display:none; } } /* touch: use long-press */
  .addreact:hover { color:var(--accent); border-color:var(--accent); }
  .addreact svg { width:14px; height:14px; stroke:currentColor; fill:none; stroke-width:1.8; }
  /* emoji picker popover */
  #reactpick { position:fixed; z-index:47; display:none; gap:2px; padding:6px;
    background:var(--panel); border:1px solid var(--line2); border-radius:14px; box-shadow:0 10px 30px rgba(0,0,0,0.4); }
  #reactpick.open { display:flex; }
  #reactpick span { font-size:22px; padding:4px 6px; border-radius:9px; cursor:pointer; line-height:1; }
  #reactpick span:hover { background:rgba(74,147,196,0.16); transform:scale(1.15); }
  .rowprev, .linktitle, .linkdesc, .docname, .pollq, .pollmeta span { unicode-bidi:plaintext; }
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
  /* ===== accessibility (IS 5568 / WCAG 2.0 AA) ===== */
  :focus-visible { outline:3px solid var(--accent); outline-offset:2px; }
  .skip { position:fixed; right:-9999px; top:8px; z-index:100; background:var(--accent); color:#fff; padding:8px 14px; border-radius:8px; text-decoration:none; }
  .skip:focus { right:8px; }
  #app { zoom:var(--z, 1); height:calc(100vh / var(--z, 1)); height:calc(100dvh / var(--z, 1)); }
  html.a11y-contrast body, html.a11y-contrast body.light {
    --bg:#000; --bg2:#000; --panel:#000; --panel2:#000; --bubble:#000; --bubble2:#0a0a0a;
    --fg:#fff; --muted:#fff; --dim:#e6e6e6; --line:#fff; --line2:#fff;
    --accent:#ffe14d; --accent2:#ffe14d; --headbg:#000; --markbg:#ffe14d55;
  }
  html.a11y-contrast .msg { border:1px solid #fff; }
  html.a11y-contrast a { color:#ffe14d !important; }
  html.a11y-gray { filter:grayscale(1); }
  html.a11y-links a, html.a11y-links .msgchan { text-decoration:underline !important; text-underline-offset:3px; outline:1px dashed currentColor; outline-offset:2px; }
  html.a11y-font body { font-family:Arial,"Noto Sans Hebrew",sans-serif !important; letter-spacing:0.02em; word-spacing:0.1em; }
  html.a11y-font .msgtext { line-height:1.85; }
  html.a11y-still *, html.a11y-still *::before, html.a11y-still *::after { animation:none !important; transition:none !important; scroll-behavior:auto !important; }
  #a11ybtn { position:fixed; left:0; top:42%; z-index:44; width:38px; height:42px; border:0; border-radius:0 12px 12px 0;
    background:var(--accent); color:#fff; display:flex; align-items:center; justify-content:center; cursor:pointer;
    box-shadow:0 2px 8px rgba(0,0,0,0.3); opacity:0.88; }
  #a11ybtn:hover, #a11ybtn:focus-visible { opacity:1; }
  #a11ybtn svg { width:24px; height:24px; fill:currentColor; }
  #a11yback { position:fixed; inset:0; z-index:49; display:none; align-items:center; justify-content:center; background:rgba(0,0,0,0.5); }
  #a11yback.open { display:flex; }
  .a11ycard { background:var(--panel); border:1px solid var(--line2); border-radius:18px; width:min(94vw,400px); max-height:90vh; overflow-y:auto; padding:18px; }
  .a11ycard h2 { font-size:18px; margin:0 0 12px; display:flex; justify-content:space-between; align-items:center; }
  .a11ygrid { display:grid; grid-template-columns:1fr 1fr; gap:8px; }
  .a11yopt { display:flex; flex-direction:column; align-items:center; justify-content:center; gap:6px; min-height:74px; padding:10px 8px;
    background:var(--bubble); border:1px solid var(--line); border-radius:12px; color:var(--fg); font:inherit; font-size:13.5px; cursor:pointer; text-align:center; }
  .a11yopt .ai { font-size:20px; line-height:1; }
  .a11yopt[aria-pressed="true"] { border-color:var(--accent); background:rgba(74,147,196,0.16); color:var(--fg); font-weight:600; }
  .a11ysize { grid-column:1 / -1; display:flex; align-items:center; justify-content:space-between; gap:8px; padding:8px 10px;
    background:var(--bubble); border:1px solid var(--line); border-radius:12px; }
  .a11ysize button { width:44px; height:40px; border:1px solid var(--line2); border-radius:10px; background:transparent; color:var(--fg); font:inherit; font-size:18px; font-weight:700; cursor:pointer; }
  .a11ysize span { font-size:14px; }
  .a11yfoot { display:flex; justify-content:space-between; align-items:center; margin-top:12px; gap:8px; flex-wrap:wrap; font-size:13px; }
  .a11yfoot a { color:var(--accent); }
  .a11yfoot button { padding:8px 14px; border:1px solid var(--line2); border-radius:10px; background:transparent; color:var(--fg); font:inherit; font-size:13px; cursor:pointer; }
  .legalfoot { padding:6px 10px 2px; font-size:11.5px; text-align:center; }
  .legalfoot a { color:var(--dim); text-decoration:none; }
  .legalfoot a:hover { text-decoration:underline; }
  @media (max-width:680px) {
    .legalfoot { display:none; }
    /* phones: sit in the empty bottom of the channel rail, never over messages */
    #a11ybtn { left:auto; right:9px; top:auto; bottom:calc(14px + env(safe-area-inset-bottom)); width:40px; height:40px; border-radius:12px; }
  }

  /* stats panel */
  #statsback { position:fixed; inset:0; z-index:48; display:none; align-items:center; justify-content:center; background:rgba(0,0,0,0.5); }
  #statsback.open { display:flex; }
  .statscard { background:var(--panel); border:1px solid var(--line2); border-radius:18px; width:min(94vw,420px); max-height:88vh; overflow-y:auto; padding:20px; }
  .statscard h2 { font-size:17px; margin:0 0 4px; }
  .statscard .sub { font-size:12px; color:var(--muted); margin-bottom:16px; }
  .statgrid { display:grid; grid-template-columns:1fr 1fr; gap:10px; margin-bottom:16px; }
  .statbox { background:var(--bubble); border:1px solid var(--line); border-radius:12px; padding:12px 14px; }
  .statbox .n { font-size:24px; font-weight:700; font-variant-numeric:tabular-nums; }
  .statbox .l { font-size:12px; color:var(--muted); margin-top:2px; }
  .statbox.live .n { color:var(--accent2); }
  .statbars { display:flex; align-items:flex-end; gap:6px; height:70px; padding:6px 2px 0; }
  .statbars .bar { flex:1; display:flex; flex-direction:column; align-items:center; gap:4px; height:100%; justify-content:flex-end; }
  .statbars .bar .fill { width:100%; background:var(--accent); border-radius:4px 4px 0 0; min-height:2px; opacity:0.85; }
  .statbars .bar .dl { font-size:9.5px; color:var(--dim); }
  .statbars .bar .bv { font-size:10px; color:var(--muted); }
  .statnote { font-size:11.5px; color:var(--dim); margin-top:14px; line-height:1.5; }
  .statclose { margin-top:16px; width:100%; padding:10px; border:1px solid var(--line2); border-radius:12px; background:transparent; color:var(--fg); font:inherit; font-size:14px; cursor:pointer; }

  /* digest ("מה היה היום") */
  #digestback { position:fixed; inset:0; background:var(--bg); z-index:45; display:none; flex-direction:column; }
  #digestback.open { display:flex; }
  .dhead { display:flex; align-items:center; gap:10px; padding:12px 16px; border-bottom:1px solid var(--line); background:var(--panel); }
  .dhead h2 { font-size:16px; margin:0; flex:1; }
  .dtabs { display:flex; gap:6px; padding:10px 16px 0; }
  .dtab { font-size:13px; padding:6px 14px; border-radius:99px; border:1px solid var(--line); cursor:pointer; color:var(--muted); }
  .dtab.on { background:rgba(74,147,196,0.15); color:var(--accent); border-color:rgba(74,147,196,0.4); }
  #dlist { flex:1; overflow-y:auto; padding:10px 16px calc(24px + env(safe-area-inset-bottom)); }
  .dchan { margin:14px 0 6px; display:flex; align-items:center; gap:8px; font-weight:700; font-size:14px; }
  .dchan .ava { width:26px; height:26px; font-size:11px; }
  .dchan .cnt { font-weight:400; color:var(--muted); font-size:12px; }
  .drow { display:flex; gap:10px; padding:7px 0; border-bottom:1px solid var(--line); cursor:pointer; font-size:13.5px; line-height:1.45; }
  .drow:hover { background:rgba(74,147,196,0.06); }
  .drow .dt { flex:none; color:var(--dim); font-size:12px; direction:ltr; min-width:40px; padding-top:2px; }
  .drow .dx { flex:1; min-width:0; overflow:hidden; display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; unicode-bidi:plaintext; }
  .drow .dm { flex:none; font-size:14px; }
  .dempty { text-align:center; color:var(--muted); padding:40px 0; }
  .toobig { display:block; position:relative; text-decoration:none; color:inherit; }
  .toobig img { filter:brightness(0.55); }
  .toobiglabel {
    position:absolute; inset:0; display:flex; flex-direction:column; align-items:center; justify-content:center;
    gap:4px; text-align:center; padding:12px; color:#fff; text-shadow:0 1px 3px rgba(0,0,0,0.7);
  }
  .toobiglabel b { font-size:15px; }
  .toobiglabel span { font-size:12px; opacity:0.9; }
  .toobiglabel::before { content:'🔒'; font-size:26px; margin-bottom:4px; }
  .vidready { box-shadow:0 0 0 2px rgba(74,168,138,0.7), 0 0 22px rgba(74,168,138,0.35); }
  .vidready .playbtn { background:rgba(20,120,80,0.8); border-color:#fff; }

  .msgtext.clamped { max-height:230px; overflow:hidden; position:relative; }
  .msgtext.clamped::after {
    content:''; position:absolute; bottom:0; left:0; right:0; height:52px;
    background:linear-gradient(transparent, var(--bubble));
  }
  .morebtn {
    display:inline-block; margin-top:7px;
    color:var(--accent); font-size:12.5px; cursor:pointer;
    border-bottom:1px dashed rgba(74,147,196,0.4);
  }
  .morebtn:hover { border-bottom-style:solid; }

  #downbtn {
    position:absolute; bottom:24px; left:22px;
    width:46px; height:46px; border-radius:50%;
    display:none; align-items:center; justify-content:center;
    background:rgba(22,28,49,0.92); color:var(--accent);
    border:1px solid rgba(74,147,196,0.35);
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
    background:linear-gradient(135deg,var(--accent),var(--accent));
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
    font:inherit; font-size:16px; padding:11px 14px;
    direction:ltr; text-align:left; outline:none;
    transition:border-color 0.15s;
  }
  .modal input:focus { border-color:rgba(74,147,196,0.6); box-shadow:0 0 0 3px rgba(74,147,196,0.12); }
  .modal .err { color:#ff9c9c; font-size:12px; min-height:16px; margin-top:8px; }
  .modal .btns { display:flex; gap:10px; margin-top:10px; }
  .modal button {
    flex:1; font:inherit; font-size:13.5px; border-radius:11px;
    padding:10px 0; cursor:pointer; border:1px solid var(--line);
  }
  .modal .ok { background:linear-gradient(135deg,var(--accent),var(--accent)); color:#fff; border:none; font-weight:700; }
  .modal .ok:disabled { opacity:0.55; cursor:default; }
  .modal .cancel { background:transparent; color:var(--muted); }

  /* ===== loading skeleton ================================================ */
  .skel {
    border-radius:16px 16px 16px 5px; border:1px solid var(--line);
    background:linear-gradient(160deg,var(--bubble2),var(--bubble));
    padding:14px 15px; margin-bottom:10px; max-width:86%;
  }
  .skel .l {
    height:11px; border-radius:99px; margin-bottom:9px;
    background:linear-gradient(90deg,rgba(127,140,170,0.12) 25%,rgba(127,140,170,0.28) 50%,rgba(127,140,170,0.12) 75%);
    background-size:200% 100%;
    animation:shimmer 1.3s linear infinite;
  }
  .skel .l:last-child { margin-bottom:0; }
  @keyframes shimmer { to { background-position:-200% 0; } }

  /* ===== lightbox album navigation ======================================= */
  .lbnav {
    position:fixed; top:50%; transform:translateY(-50%);
    width:52px; height:52px; border-radius:50%;
    background:rgba(18,23,42,0.8); border:1px solid rgba(255,255,255,0.25);
    color:#fff; font-size:26px; line-height:50px; text-align:center;
    cursor:pointer; user-select:none; z-index:51;
    transition:background 0.12s;
  }
  .lbnav:hover { background:rgba(30,136,210,0.8); }
  #lbprev { right:26px; }
  #lbnext { left:26px; }
  #lbcount {
    position:fixed; top:22px; left:50%; transform:translateX(-50%);
    color:#fff; font-size:13px; z-index:51;
    background:rgba(18,23,42,0.75); border-radius:99px; padding:5px 16px;
  }

  /* ===== rich media ====================================================== */
  .album {
    display:grid; grid-template-columns:1fr 1fr; gap:4px;
    border-radius:11px; overflow:hidden; border:1px solid var(--line);
  }
  .album img {
    width:100%; height:100%; object-fit:cover; display:block;
    aspect-ratio:1/1; border:none; border-radius:0; cursor:zoom-in;
  }
  .album.two img { aspect-ratio:4/5; }
  .album img:first-child:nth-last-child(odd):not(:only-child) { grid-column:span 2; aspect-ratio:2/1; }

  .roundwrap { display:flex; justify-content:center; padding:6px 0; }
  .roundvid {
    width:260px; height:260px; max-width:70vw; max-height:70vw;
    border-radius:50%; object-fit:cover;
    border:3px solid var(--accent); background:#0d1120;
    box-shadow:0 8px 28px rgba(30,136,210,0.25);
  }

  .voicebox {
    display:flex; align-items:center; gap:10px;
    background:rgba(74,147,196,0.08); border:1px solid rgba(74,147,196,0.22);
    border-radius:14px; padding:8px 12px; margin:4px 0 8px;
  }
  .voiceico { font-size:18px; flex:none; }
  .voicebox audio { flex:1; height:36px; min-width:0; }
  .voicedur { font-size:11.5px; color:var(--muted); flex:none; direction:ltr; }

  .stickerbox { padding:2px 0 6px; }
  .stickerbox img { max-width:170px; max-height:170px; display:block; }

  .pollbox {
    background:rgba(74,147,196,0.06); border:1px solid var(--line);
    border-radius:14px; padding:12px 14px; margin:4px 0 8px;
  }
  .pollq { font-weight:700; font-size:13.5px; margin-bottom:10px; }
  .pollopt { margin-bottom:9px; }
  .pollopt:last-child { margin-bottom:2px; }
  .pollmeta {
    display:flex; justify-content:space-between; gap:10px;
    font-size:12.5px; margin-bottom:4px;
  }
  .pollmeta b { color:var(--accent); direction:ltr; }
  .pollbar {
    height:6px; border-radius:99px; overflow:hidden;
    background:rgba(127,140,170,0.18);
  }
  .pollbar i {
    display:block; height:100%; border-radius:99px;
    background:linear-gradient(90deg,var(--accent),var(--accent2));
    transition:width 0.4s ease;
  }

  .docbox {
    display:flex; align-items:center; gap:11px;
    background:rgba(127,140,170,0.08); border:1px solid var(--line);
    border-radius:14px; padding:10px 13px; margin:4px 0 8px;
  }
  .docico { font-size:20px; flex:none; }
  .docname { font-size:13px; font-weight:600; word-break:break-all; }
  .docsize { font-size:11.5px; color:var(--muted); margin-top:2px; }

  .linkcard {
    display:flex; gap:11px; align-items:stretch;
    background:rgba(74,147,196,0.05);
    border:1px solid var(--line); border-right:3px solid var(--accent);
    border-radius:11px; overflow:hidden;
    margin:6px 0 4px; padding:10px 12px;
    text-decoration:none; color:inherit;
    transition:background 0.12s;
  }
  a.linkcard:hover { background:rgba(74,147,196,0.12); }
  .linkcard img {
    width:64px; height:64px; object-fit:cover; flex:none;
    border-radius:8px; border:none;
  }
  .linkmeta { min-width:0; }
  .linktitle { font-size:13px; font-weight:700; color:var(--accent); }
  .linkdesc { font-size:12px; color:var(--muted); margin-top:3px; line-height:1.5; }

  /* ===== pinned + unread ================================================= */
  .rowpin {
    position:absolute; top:6px; right:6px;
    width:18px; height:18px; line-height:17px; text-align:center;
    border-radius:50%; font-size:10px;
    opacity:0; transition:opacity 0.12s;
  }
  .row:hover .rowpin { opacity:0.85; }
  .rowpin:hover { background:rgba(74,147,196,0.25); }
  .row.pinned .rowpin { opacity:0.85; }
  .pinmark { font-size:10.5px; opacity:0.75; }

  .unreadline {
    display:flex; align-items:center; gap:12px;
    margin:14px 0 10px; color:var(--accent);
    font-size:12px; font-weight:700;
  }
  .unreadline::before, .unreadline::after {
    content:''; flex:1; height:1px;
    background:linear-gradient(90deg,transparent,rgba(74,147,196,0.55),transparent);
  }

  /* ===== light-mode contrast fixes =======================================
     White bubbles were disappearing into a near-white background, and several
     fixed dark surfaces (toast, hovers, scrollbars) inverted badly. Every
     hard-coded dark/white below gets a light twin. */
  body.light .msg {
    border-color:rgba(20,30,60,0.13);
    box-shadow:0 1px 4px rgba(25,35,70,0.10), 0 4px 16px rgba(25,35,70,0.07);
  }
  body.light .msg.fresh { box-shadow:0 0 0 1px rgba(30,136,210,0.55), 0 6px 24px rgba(30,136,210,0.18); }
  body.light #toast {
    background:rgba(255,255,255,0.98); border-color:rgba(20,30,60,0.15);
    box-shadow:0 8px 24px rgba(25,35,70,0.25);
  }
  body.light ::-webkit-scrollbar-thumb { background:rgba(20,30,60,0.22); }
  body.light ::-webkit-scrollbar-thumb:hover { background:rgba(20,30,60,0.34); }
  body.light .row:hover { background:rgba(20,30,60,0.055); }
  body.light .iconbtn { background:rgba(20,30,60,0.05); }
  body.light .datesep span { background:rgba(255,255,255,0.85); }
  body.light .msgtext a { color:#1565c0; border-bottom-color:rgba(21,101,192,0.35); }
  body.light #downbtn {
    background:rgba(255,255,255,0.96); border-color:rgba(30,136,210,0.4);
    box-shadow:0 6px 18px rgba(25,35,70,0.2);
  }
  body.light .stbadge { background:rgba(20,30,60,0.07); }
  body.light .msgtext.clamped::after { background:linear-gradient(transparent,#ffffff); }
  body.light .durbadge { background:rgba(10,13,22,0.65); }
  body.light #statusbar { background:rgba(224,150,10,0.12); border-bottom-color:rgba(200,130,0,0.3); }
  body.light #statusbar.calm { background:rgba(30,136,210,0.08); border-bottom-color:rgba(30,136,210,0.25); }
  body.light #moremenu { box-shadow:0 14px 40px rgba(25,35,70,0.25); }
  body.light .avabadge { border-color:#fff; }
  body.light .spin { border-color:rgba(20,30,60,0.15); border-top-color:var(--accent); }

  @media (max-width:680px) {
    #side { width:58px; }
    .sidehead { display:none !important; }
    .row .ava, .row .avawrap > .ava { width:40px; height:40px; font-size:15px; }
    .row { width:48px; height:48px; margin:4px auto; padding:0; border-radius:14px; display:flex; align-items:center; justify-content:center; }
    .row.active::before { display:none; }
    .photo img { max-height:min(36vh, 300px); }
    .album img { max-height:min(26vh, 220px); }
    .rowmid, .rowside, .appmeta { display:none; }
    .row { justify-content:center; padding:8px 2px; }
    .row.active::before { right:-2px; }
    .sidehead { justify-content:center; padding:12px 6px 10px; }
    .avabadge.show { display:flex; }
    #addbtn { font-size:20px; padding:6px 0; }
    #addbtn .txt { display:none; }
    #addbtn .plus { display:inline !important; }
    #chathead { padding:8px 10px; gap:7px; }
    #headava { width:32px; height:32px; font-size:13px; }
    #headtitle { font-size:13.5px; }
    #headsub { font-size:10.5px; }
    .headbtns { gap:5px; }
    .headbtns .iconbtn { width:32px; height:32px; font-size:14px; }
    #refreshbtn { font-size:17px; }
    #chat { padding:10px 8px 18px; }
    .msg { max-width:100%; min-width:140px; }
    .msg:has(.photo, .album, .vidwrap, .toobig, .linkcard) { width:100%; }
    .msgtext { font-size:var(--txt); line-height:1.5; }
    .headbtns .iconbtn { display:none !important; }
    .headbtns .iconbtn.keep, #morebtn { display:flex !important; }
    #searchbar { padding:8px 10px; }
    #installbtn { font-size:0; padding:8px 0; }
    #installbtn::before { content:'📲'; font-size:18px; }
    #iosinstall { display:none; }
    #jump, #downbtn { bottom:calc(16px + env(safe-area-inset-bottom)); }
    .addwrap { padding:8px 8px calc(10px + env(safe-area-inset-bottom)); }
    .rowpin { opacity:0 !important; pointer-events:none; }
  }
</style>
</head>
<body>
<a class="skip" href="#chat">דלג לתוכן</a>
<div id="app">
  <aside id="side">
    <div class="sidehead">
      <div class="applogo"><svg viewBox="0 0 64 64"><path d="M14 14h36a6 6 0 016 6v18a6 6 0 01-6 6H32l-11 9v-9h-7a6 6 0 01-6-6V20a6 6 0 016-6z" fill="#fff"/></svg></div>
      <div class="appmeta">
        <div class="appname">מבזקי ארץ ישראל <span style="font-size:10px;color:#5a6378;font-weight:400">v%%VERSION%%</span></div>
        <div class="status"><span class="dot" id="dot"></span><span id="statustext">מתחבר…</span></div>
      </div>
    </div>
    <div id="chanlist"></div>
    <div id="updrow" style="display:none;padding:10px 12px 0">
      <button id="updbtn" style="width:100%;font:600 12.5px 'Segoe UI',sans-serif;color:#fff;border:none;border-radius:12px;padding:10px 0;cursor:pointer;background:linear-gradient(135deg,var(--accent2),var(--accent2));box-shadow:0 4px 14px rgba(20,154,110,0.35)">
        ⬇ גרסה חדשה זמינה — לחץ לעדכון
      </button>
    </div>
    <div id="installrow">
      <button id="installbtn" title="התקן כאפליקציה במסך הבית">📲 התקן כאפליקציה</button>
      <div id="iosinstall" style="display:none"></div>
    </div>
    <div class="autostartrow" id="autostartrow" style="display:none">
      <label><input type="checkbox" id="autostartchk"> הפעל עם ווינדוס</label>
    </div>
    <div class="legalfoot"><a href="/terms" target="_blank" rel="noopener">תקנון</a> · <a href="/accessibility" target="_blank" rel="noopener">נגישות</a></div>
    <div class="addwrap"><button id="addbtn"><span class="txt">הוסף ערוץ</span><span class="plus" data-icon="plus" style="display:none"></span></button></div>
  </aside>

  <main id="main">
    <div id="chathead">
      <div id="headava">✦</div>
      <div class="headmid">
        <div id="headtitle">כל הערוצים</div>
        <div id="headsub"></div>
      </div>
      <div class="headbtns">
        <div class="iconbtn" id="kwbtn" data-icon="settings" role="button" tabindex="0" title="מילות מפתח להתראות" aria-label="מילות מפתח להתראות"></div>
        <div class="iconbtn" id="themebtn" data-icon="moon" role="button" tabindex="0" title="מצב בהיר/כהה" aria-label="מצב בהיר/כהה"></div>
        <div class="iconbtn keep" id="searchbtn" data-icon="search" role="button" tabindex="0" title="חיפוש בארכיון" aria-label="חיפוש בארכיון"></div>
        <div class="iconbtn" id="filterbtn" data-icon="filter" role="button" tabindex="0" title="סינון לפי סוג" aria-label="סינון"></div>
        <div class="iconbtn keep" id="digestbtn" data-icon="list" role="button" tabindex="0" title="מה פספסתי / מה היה היום" aria-label="תקציר"></div>
        <div class="iconbtn" id="statsbtn" data-icon="chart" role="button" tabindex="0" title="נתוני כניסות" aria-label="נתונים" style="display:none"></div>
        <div class="iconbtn keep" id="refreshbtn" data-icon="refresh" role="button" tabindex="0" title="עדכן עכשיו" aria-label="עדכן עכשיו"></div>
        <div class="iconbtn" id="tglmute" data-icon="bell" role="button" tabindex="0" style="display:none"></div>
        <div class="iconbtn" id="tglpopups" data-icon="monitor" role="button" tabindex="0" title="חלוניות קופצות במחשב (כל הערוצים)" aria-label="חלוניות קופצות"></div>
        <div class="iconbtn" id="tglsound" data-icon="volume" role="button" tabindex="0" title="צליל התראה" aria-label="צליל התראה"></div>
        <div class="iconbtn keep" id="tglnotif" data-icon="bell" role="button" tabindex="0" style="display:none" title="התראות בדפדפן" aria-label="התראות בדפדפן"></div>
        <div class="iconbtn" id="morebtn" data-icon="more" role="button" tabindex="0" title="עוד" aria-label="עוד"></div>
      </div>
      <div id="moremenu" role="menu"></div>
    </div>
    <div id="searchbar">
      <input id="searchinput" placeholder="חיפוש בכל ההיסטוריה שנשמרה…" spellcheck="false">
      <div class="iconbtn" id="searchclose" data-icon="close" title="סגור חיפוש"></div>
    </div>
    <!-- NOTE: this bar's text span is sbartext — NOT statustext, which is the
         sidebar's connection label. They were once the same id, and the
         banner silently wrote its words into the wrong element. -->
    <div id="statusbar" style="display:none">
      <span id="statusicon">⏳</span>
      <span id="sbartext"></span>
      <span id="statusmore" title="פירוט לפי ערוץ">פירוט</span>
    </div>
    <div id="statuspanel" style="display:none"></div>
    <div id="filters" role="group" aria-label="סינון">
      <span class="fchip on" data-f="all">הכל</span>
      <span class="fchip" data-f="text">טקסט</span>
      <span class="fchip" data-f="photo">תמונות</span>
      <span class="fchip" data-f="video">סרטונים</span>
      <span class="fchip" data-f="voice">קול</span>
    </div>
    <div id="chat">
      <div id="topload"><span class="spin"></span>טוען הודעות ישנות…</div>
      <div id="wakehint"></div>
      <div id="msgs"></div>
      <div id="results" style="display:none">
        <div id="resultsmeta"></div>
        <div id="resultslist"></div>
      </div>
    </div>
    <button id="jump"><span class="ji" data-icon="down"></span> הודעות חדשות <span class="cnt" id="jumpcnt">1</span></button>
    <button id="downbtn" data-icon="down" title="גלול לתחתית"></button>
    <div id="toast" role="status" aria-live="polite"></div>
  </main>
</div>

<div id="lightbox">
  <div class="lbnav" id="lbprev" style="display:none">‹</div>
  <img id="lightboximg" alt="">
  <div class="lbnav" id="lbnext" style="display:none">›</div>
  <div id="lbcount" style="display:none"></div>
</div>

<div id="sheetback"></div>
<div id="reactpick"></div>
<div id="msgsheet" role="menu"></div>
<button id="a11ybtn" aria-label="תפריט נגישות" title="נגישות"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="4" r="2"/><path d="M19 8.5c-2 .6-4.3 1-7 1s-5-.4-7-1l-.5 1.9c1.8.6 3.8.9 5.5 1.1v3.3l-2.3 6.4 1.9.7 2.4-6.2 2.4 6.2 1.9-.7-2.3-6.4v-3.3c1.7-.2 3.7-.5 5.5-1.1z"/></svg></button>
<div id="a11yback" role="dialog" aria-modal="true" aria-labelledby="a11ytitle">
  <div class="a11ycard">
    <h2><span id="a11ytitle">תפריט נגישות</span><button class="iconbtn" id="a11yclose" data-icon="close" aria-label="סגור תפריט נגישות"></button></h2>
    <div class="a11ygrid">
      <div class="a11ysize" role="group" aria-label="גודל תצוגה">
        <button id="a11yminus" aria-label="הקטנת תצוגה">א−</button>
        <span id="a11yzlabel" aria-live="polite">גודל תצוגה: 100%</span>
        <button id="a11yplus" aria-label="הגדלת תצוגה">א+</button>
      </div>
      <button class="a11yopt" data-a11y="contrast" aria-pressed="false"><span class="ai" aria-hidden="true">◐</span>ניגודיות גבוהה</button>
      <button class="a11yopt" data-a11y="gray" aria-pressed="false"><span class="ai" aria-hidden="true">▦</span>גווני אפור</button>
      <button class="a11yopt" data-a11y="links" aria-pressed="false"><span class="ai" aria-hidden="true">🔗</span>הדגשת קישורים</button>
      <button class="a11yopt" data-a11y="font" aria-pressed="false"><span class="ai" aria-hidden="true">Aa</span>גופן קריא</button>
      <button class="a11yopt" data-a11y="still" aria-pressed="false"><span class="ai" aria-hidden="true">⏸</span>עצירת אנימציות</button>
      <button class="a11yopt" id="a11ytheme" aria-pressed="false"><span class="ai" aria-hidden="true">☀</span>רקע בהיר</button>
    </div>
    <div class="a11yfoot">
      <a href="/accessibility" target="_blank" rel="noopener">הצהרת נגישות</a>
      <a href="/terms" target="_blank" rel="noopener">תקנון</a>
      <button id="a11yreset">איפוס הגדרות</button>
    </div>
  </div>
</div>
<div id="statsback" role="dialog" aria-modal="true" aria-labelledby="stitle">
  <div class="statscard">
    <h2 id="stitle">נתוני כניסות</h2>
    <div class="sub" id="ssince"></div>
    <div class="statgrid">
      <div class="statbox live"><div class="n" id="st_active">—</div><div class="l">מחוברים כרגע</div></div>
      <div class="statbox"><div class="n" id="st_today">—</div><div class="l">כניסות היום</div></div>
      <div class="statbox"><div class="n" id="st_views">—</div><div class="l">סה״כ כניסות</div></div>
      <div class="statbox"><div class="n" id="st_devices">—</div><div class="l">מכשירים שונים</div></div>
    </div>
    <div style="font-size:12px;color:var(--muted);margin-bottom:6px">7 הימים האחרונים</div>
    <div class="statbars" id="st_bars"></div>
    <div class="statnote" id="st_note"></div>
    <button class="statclose" id="st_unlock" style="display:none;margin-top:10px;border-color:rgba(74,147,196,0.5);color:var(--accent)">📱 העתק קישור לפתיחה בטלפון</button>
    <button class="statclose" id="st_close">סגור</button>
  </div>
</div>
<div id="digestback" role="dialog" aria-modal="true" aria-labelledby="dtitle">
  <div class="dhead">
    <div class="iconbtn" id="dclose" data-icon="close" role="button" tabindex="0" title="סגור" aria-label="סגור"></div>
    <h2 id="dtitle">מה היה היום</h2>
  </div>
  <div class="dtabs">
    <span class="dtab on" data-mode="unread">מאז שקראתי</span>
    <span class="dtab" data-mode="today">היום</span>
    <span class="dtab" data-mode="yesterday">אתמול</span>
  </div>
  <div id="dlist"></div>
</div>
<div class="modal-back" id="kwback">
  <div class="modal" role="dialog" aria-modal="true" aria-labelledby="kwtitle">
    <h3 id="kwtitle">מילות מפתח להתראות</h3>
    <p>שולט רק על החלוניות הקופצות — הדף תמיד מציג הכל.<br>
       הפרד מילים בפסיק או בשורה חדשה. השאר ריק = הכל קופץ.</p>
    <div style="font-size:12px;color:var(--muted);margin-bottom:5px">הקפץ רק הודעות שמכילות:</div>
    <textarea id="kwinclude" rows="2" style="width:100%;background:var(--bg);color:var(--fg);border:1px solid var(--line);border-radius:11px;font:inherit;font-size:13px;padding:9px 12px;outline:none;resize:vertical"></textarea>
    <div style="font-size:12px;color:var(--muted);margin:10px 0 5px">אל תקפיץ הודעות שמכילות:</div>
    <textarea id="kwexclude" rows="2" style="width:100%;background:var(--bg);color:var(--fg);border:1px solid var(--line);border-radius:11px;font:inherit;font-size:13px;padding:9px 12px;outline:none;resize:vertical"></textarea>
    <div class="err" id="kwerr"></div>
    <div class="btns">
      <button class="cancel" id="kwcancel">ביטול</button>
      <button class="ok" id="kwsave">שמור</button>
    </div>
  </div>
</div>

<div class="modal-back" id="modalback">
  <div class="modal" role="dialog" aria-modal="true" aria-labelledby="addtitle">
    <h3 id="addtitle">הוספת ערוץ</h3>
    <p>הדבק שם משתמש או קישור של ערוץ טלגרם ציבורי.<br>לדוגמה: durov או https://t.me/durov</p>
    <input id="chaninput" placeholder="@channel" spellcheck="false" autocapitalize="off" autocorrect="off">
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
  var channelsLocked = false; // locked list: no add button, no remove ✕
  var unread = {};      // channel -> count
  var unreadFirst = {}; // channel -> key of the first message that arrived unseen
  // Read position per channel, kept on the device: the newest message
  // timestamp you have actually had on screen. Survives reloads, so the
  // badges and the "הודעות חדשות" line pick up where you left off, even
  // after closing the app for hours.
  var readUpTo = {};
  try { readUpTo = JSON.parse(localStorage.getItem('tgpopup-read') || '{}') || {}; } catch (e) { readUpTo = {}; }
  var readSaveTimer = null;
  function saveRead() {
    if (readSaveTimer) return;
    readSaveTimer = setTimeout(function () {
      readSaveTimer = null;
      try { localStorage.setItem('tgpopup-read', JSON.stringify(readUpTo)); } catch (e) {}
    }, 400);
  }
  // Mark everything currently on screen (in the active view) as read.
  function markReadVisible() {
    if (document.hidden || searchOpen) return;
    var top = chat.getBoundingClientRect().top, bottom = top + chat.clientHeight;
    var nodes = msgs.querySelectorAll('.msg:not(.hidden)');
    var changed = false;
    // Walk from the bottom: the first node whose top is inside the viewport
    // is the newest one you can see; everything older counts as read.
    for (var i = nodes.length - 1; i >= 0; i--) {
      var r = nodes[i].getBoundingClientRect();
      if (r.top < bottom && r.bottom > top) {
        var ts = parseInt(nodes[i].getAttribute('data-ts') || '0', 10);
        var ch = nodes[i].getAttribute('data-channel');
        if (!ts) break;
        if (current === 'all') {
          // In the combined view, seeing a message means you also saw every
          // older one of every channel above it.
          channels.forEach(function (c) {
            var lm = newestVisibleTs(c.name, ts);
            if (lm && (!readUpTo[c.name] || lm > readUpTo[c.name])) { readUpTo[c.name] = lm; changed = true; }
          });
        } else if (!readUpTo[ch] || ts > readUpTo[ch]) {
          readUpTo[ch] = ts; changed = true;
        }
        break;
      }
    }
    if (changed) { saveRead(); recountUnread(); renderSidebar(); }
  }
  // Newest timestamp of a channel's loaded messages that is <= limit.
  function newestVisibleTs(ch, limit) {
    var best = 0;
    var ns = msgs.querySelectorAll('.msg[data-channel="' + ch + '"]');
    for (var i = ns.length - 1; i >= 0; i--) {
      var t = parseInt(ns[i].getAttribute('data-ts') || '0', 10);
      if (t && t <= limit) { best = t; break; }
    }
    return best;
  }
  // Recompute badges from the read positions (after boot, resync, markRead).
  function recountUnread() {
    channels.forEach(function (c) {
      var n = 0, first = null;
      var r = readUpTo[c.name] || 0;
      var ns = msgs.querySelectorAll('.msg[data-channel="' + c.name + '"]');
      for (var i = 0; i < ns.length; i++) {
        var t = parseInt(ns[i].getAttribute('data-ts') || '0', 10);
        if (t > r) { n++; if (!first) first = ns[i].id.slice(1); }
      }
      unread[c.name] = n;
      if (n) { if (!unreadFirst[c.name]) unreadFirst[c.name] = first; }
      else delete unreadFirst[c.name];
    });
  }
  var lastMsg = {};     // channel -> {ts, preview}
  var oldestPer = {};   // channel -> smallest loaded id
  var donePer = {};     // channel -> no more history
  var loadingOlder = false;
  var BOOT_MAX = 200;
  var heldOlder = []; // older items from the first load, not yet in the DOM

  // ----- line icons (quiet, monochrome SVG — no emoji in the chrome) --------
  var ICONS = {
    settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-2.7 1.1V21a2 2 0 1 1-4 0v-.1A1.6 1.6 0 0 0 6.6 19l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1A1.6 1.6 0 0 0 3 12.6H3a2 2 0 1 1 0-4h.1A1.6 1.6 0 0 0 4.6 6l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1A1.6 1.6 0 0 0 9 3.6V3a2 2 0 1 1 4 0v.1A1.6 1.6 0 0 0 15 4.6l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0-.3 1.8v.1a1.6 1.6 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1z"/>',
    moon: '<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
    search: '<circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/>',
    filter: '<path d="M3 5h18l-7 8v6l-4 2v-8z"/>',
    list: '<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>',
    refresh: '<path d="M21 12a9 9 0 1 1-2.6-6.4M21 4v5h-5"/>',
    bell: '<path d="M18 8a6 6 0 1 0-12 0c0 7-3 9-3 9h18s-3-2-3-9M13.7 21a2 2 0 0 1-3.4 0"/>',
    belloff: '<path d="M8.7 3A6 6 0 0 1 18 8c0 2.3.3 4 .8 5.2M18 17H3s3-2 3-9M13.7 21a2 2 0 0 1-3.4 0M2 2l20 20"/>',
    monitor: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
    volume: '<path d="M11 5 6 9H2v6h4l5 4zM15.5 8.5a5 5 0 0 1 0 7M18.5 5.5a9 9 0 0 1 0 13"/>',
    volumeoff: '<path d="M11 5 6 9H2v6h4l5 4zM22 9l-6 6M16 9l6 6"/>',
    more: '<circle cx="5" cy="12" r="1.3"/><circle cx="12" cy="12" r="1.3"/><circle cx="19" cy="12" r="1.3"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    down: '<path d="M6 9l6 6 6-6"/>',
    close: '<path d="M6 6l12 12M18 6 6 18"/>',
    share: '<circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><path d="M8.6 10.6l6.8-4M8.6 13.4l6.8 4"/>',
    copy: '<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/>',
    pin: '<path d="M9 4h6l-1 6 3 3v2H7v-2l3-3zM12 17v4"/>',
    textsize: '<path d="M4 7V5h10v2M9 5v14M7 19h4M15 13v-1h6v1M18 12v7M16.5 19h3"/>',
    eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z"/><circle cx="12" cy="12" r="3"/>',
    chart: '<path d="M3 3v18h18"/><rect x="7" y="11" width="3" height="6"/><rect x="12" y="7" width="3" height="10"/><rect x="17" y="13" width="3" height="4"/>'
  };
  function svgIcon(name) { return ICONS[name] ? '<svg viewBox="0 0 24 24" aria-hidden="true">' + ICONS[name] + '</svg>' : ''; }
  function applyIcons(root) {
    (root || document).querySelectorAll('[data-icon]').forEach(function (el) {
      var n = el.getAttribute('data-icon');
      if (ICONS[n]) el.innerHTML = svgIcon(n);
    });
  }
  applyIcons();

  var chat = document.getElementById('chat');
  var msgs = document.getElementById('msgs');
  var scrollPos = {};  // channel -> saved scroll position when switching away
  var lastSeq = 0;     // newest feed sequence number this tab has seen
  var isCloud = false; // set from /api/settings: no desktop features online
  var booted = false;  // first /api/messages answered

  // Loading skeleton: shimmering placeholder bubbles instead of a blank pane
  // while the first batch loads.
  (function () {
    for (var i = 0; i < 3; i++) {
      var sk = document.createElement('div');
      sk.className = 'skel';
      sk.setAttribute('data-skel', '1');
      var widths = [[70, 95, 40], [88, 55], [60, 92, 78]][i];
      for (var j = 0; j < widths.length; j++) {
        var l = document.createElement('div');
        l.className = 'l';
        l.style.width = widths[j] + '%';
        sk.appendChild(l);
      }
      msgs.appendChild(sk);
    }
  })();
  function clearSkeleton() {
    var sks = msgs.querySelectorAll('[data-skel]');
    for (var i = 0; i < sks.length; i++) sks[i].remove();
  }

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
      img.referrerPolicy = 'no-referrer'; // Telegram's CDN must not see a local referrer
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
    return ('0' + d.getDate()).slice(-2) + '.' + ('0' + (d.getMonth() + 1)).slice(-2);
  }
  // Bubble stamp, rendered HERE in the viewer's own time zone. The server
  // used to format it, which meant UTC when it runs in the cloud.
  function fmtStamp(ts) {
    if (!ts) return '';
    var d = new Date(ts * 1000), now = new Date();
    if (d.toDateString() === now.toDateString()) return fmtTime(ts);
    return fmtRowTime(ts) + ' ' + fmtTime(ts);
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
  // "Pinned" = the reader is at the bottom. While pinned, the view follows
  // every height change (late images, the browser re-measuring off-screen
  // bubbles) so "scroll to bottom" really ends at the last message.
  var pinned = true;
  function scrollBottom(now) {
    pinned = true;
    chat.scrollTo({ top: chat.scrollHeight, behavior: now ? 'auto' : 'smooth' });
    if (now) requestAnimationFrame(function () { if (pinned) chat.scrollTop = chat.scrollHeight; });
  }
  if (window.ResizeObserver) {
    new ResizeObserver(function () { if (pinned) chat.scrollTop = chat.scrollHeight; }).observe(msgs);
  }
  chat.addEventListener('scroll', function () {
    // Only a scroll the user made can unpin; programmatic bottom-scrolls keep it.
    pinned = chat.scrollTop + chat.clientHeight >= chat.scrollHeight - 24;
  }, { passive: true });

  // ----- sidebar ------------------------------------------------------------
  function renderSidebar() {
    var list = document.getElementById('chanlist');
    list.innerHTML = '';

    var allRow = document.createElement('div');
    allRow.className = 'row' + (current === 'all' ? ' active' : '');
    var allAva = document.createElement('div');
    allAva.className = 'ava';
    allAva.style.background = 'linear-gradient(135deg,var(--accent),var(--accent))';
    allAva.textContent = '✦';
    var allWrap = document.createElement('div');
    allWrap.className = 'avawrap';
    allWrap.appendChild(allAva);
    allRow.appendChild(allWrap);
    var allMid = document.createElement('div');
    allMid.className = 'rowmid';
    allMid.innerHTML = '<div class="rowname">כל הערוצים</div><div class="rowprev">ציר זמן ממוזג</div>';
    allRow.appendChild(allMid);
    // Total unread across every channel, so nothing gets missed while a
    // single channel is in focus.
    var totalUnread = 0;
    channels.forEach(function (c) { totalUnread += (unread[c.name] || 0); });
    if (totalUnread > 0 && current !== 'all') {
      var allSide = document.createElement('div');
      allSide.className = 'rowside';
      var allBd = document.createElement('div');
      allBd.className = 'badge show';
      allBd.textContent = totalUnread;
      allSide.appendChild(allBd);
      allRow.appendChild(allSide);
      var allAb = document.createElement('div');
      allAb.className = 'avabadge show';
      allAb.textContent = totalUnread;
      allWrap.appendChild(allAb);
    }
    allRow.setAttribute('role', 'button');
    allRow.setAttribute('tabindex', '0');
    allRow.onclick = function () { select('all'); };
    allRow.onkeydown = function (ev) { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); select('all'); } };
    list.appendChild(allRow);

    // Pinned channels float to the top; inside each group, recency wins.
    var ordered = channels.slice().sort(function (a, b) {
      if (!!b.pinned !== !!a.pinned) return b.pinned ? 1 : -1;
      return ((lastMsg[b.name] || {}).ts || 0) - ((lastMsg[a.name] || {}).ts || 0);
    });

    ordered.forEach(function (ch) {
      var row = document.createElement('div');
      row.className = 'row' + (current === ch.name ? ' active' : '') + (ch.pinned ? ' pinned' : '');
      row.setAttribute('data-chan', ch.name);

      var ava = document.createElement('div');
      ava.className = 'ava';
      avaStyle(ava, ch);
      var wrap = document.createElement('div');
      wrap.className = 'avawrap';
      wrap.appendChild(ava);
      if (unread[ch.name]) {
        // Narrow layout hides the text columns — the count rides the avatar.
        var ab = document.createElement('div');
        ab.className = 'avabadge show';
        ab.textContent = unread[ch.name];
        wrap.appendChild(ab);
      }
      row.appendChild(wrap);

      var mid = document.createElement('div');
      mid.className = 'rowmid';
      var nm = document.createElement('div');
      nm.className = 'rowname';
      nm.textContent = ch.title || ('@' + ch.name);
      if (ch.pinned) {
        var pm = document.createElement('span');
        pm.className = 'pinmark';
        pm.textContent = ' 📌';
        nm.appendChild(pm);
      }
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

      if (!channelsLocked) {
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
      }

      var pin = document.createElement('div');
      pin.className = 'rowpin';
      pin.textContent = '📌';
      pin.title = ch.pinned ? 'בטל נעיצה' : 'נעץ למעלה';
      pin.onclick = function (ev) {
        ev.stopPropagation();
        var target = !ch.pinned;
        fetch('/api/pin', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ channel: ch.name, pinned: target })
        }).then(function (r) { return r.json(); }).then(function (d) {
          if (!d.ok) { toast(d.error || 'שגיאה'); return; }
          ch.pinned = d.pinned;
          renderSidebar();
          toast(d.pinned ? 'הערוץ ננעץ לראש הרשימה' : 'הנעיצה בוטלה');
        }).catch(function () { toast('שגיאת תקשורת'); });
      };
      row.appendChild(pin);

      row.setAttribute('role', 'button');
      row.setAttribute('tabindex', '0');
      row.onclick = function () { select(ch.name); };
      row.onkeydown = function (ev) { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); select(ch.name); } };
      list.appendChild(row);
    });
  }

  // "עודכן לפני…": when the page last heard from the server. Tells you at a
  // glance whether what you see is live or a cached snapshot.
  var lastUpdateAt = 0, fromCache = false;
  function touchFresh() { lastUpdateAt = Date.now(); fromCache = false; }
  function freshLabel(sep) {
    if (fromCache) return sep + 'מהמטמון — מתעדכן…';
    if (!lastUpdateAt) return '';
    var s = Math.floor((Date.now() - lastUpdateAt) / 1000);
    if (s < 20) return sep + 'עודכן עכשיו';
    if (s < 60) return sep + 'עודכן לפני ' + s + ' שנ׳';
    var m = Math.floor(s / 60);
    if (m < 60) return sep + 'עודכן לפני ' + m + ' דק׳';
    return sep + 'עודכן לפני ' + Math.floor(m / 60) + ' שע׳';
  }
  setInterval(function () { if (!document.hidden) renderHead(); }, 30000);

  function renderHead() {
    var ava = document.getElementById('headava');
    var muteBtn = document.getElementById('tglmute');
    var n = visibleCount();
    if (current === 'all') {
      ava.innerHTML = '';
      ava.style.background = 'linear-gradient(135deg,var(--accent),var(--accent2))';
      ava.textContent = '✦';
      document.getElementById('headtitle').textContent = 'כל הערוצים';
      document.getElementById('headsub').textContent = (n ? n + ' הודעות' : '') + freshLabel(n ? ' · ' : '');
      muteBtn.style.display = 'none';
    } else {
      var ch = chanByName(current);
      ava.innerHTML = '';
      avaStyle(ava, ch || current);
      document.getElementById('headtitle').textContent = (ch && ch.title) || ('@' + current);
      document.getElementById('headsub').textContent = '@' + current + (n ? ' · ' + n + ' הודעות' : '') + freshLabel(' · ');
      var isMuted = !!(ch && ch.muted);
      muteBtn.innerHTML = svgIcon(isMuted ? 'belloff' : 'bell');
      muteBtn.className = 'iconbtn' + (isMuted ? ' off' : '');
      muteBtn.title = isMuted ? 'בטל השתקה לערוץ' : 'השתק התראות לערוץ הזה';
      muteBtn.style.display = 'flex';
    }
  }

  // Channel name on a bubble filters to that channel — nothing opens Telegram.
  chat.addEventListener('click', function (ev) {
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
    if (ch !== current) scrollPos[current] = chat.scrollTop; // remember where you were
    current = ch;
    try { localStorage.setItem('tgpopup-channel', ch); } catch (e) {}
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

    // "עד כאן קראת": entering a channel that accumulated unread messages
    // drops a marker line at the first unseen one and lands you THERE, so you
    // read forward naturally instead of hunting for where you left off.
    var old = document.getElementById('unreadline');
    if (old) old.remove();
    var anchorKey = firstUnreadKey(ch);
    if (anchorKey) {
      var anchor = document.getElementById('m' + anchorKey);
      if (anchor) {
        var line = document.createElement('div');
        line.className = 'unreadline';
        line.id = 'unreadline';
        var cnt = ch === 'all' ? totalUnreadCount() : (unread[ch] || 0);
        line.textContent = cnt > 1 ? cnt + ' הודעות חדשות' : 'הודעות חדשות';
        msgs.insertBefore(line, anchor);
        line.scrollIntoView({ block: 'center' });
        setTimeout(markReadVisible, 250);
        return;
      }
    }
    // No unread anchor: return to where you were in this channel last time,
    // or to the bottom on first visit.
    if (typeof scrollPos[ch] === 'number') {
      chat.scrollTop = scrollPos[ch];
    } else {
      scrollBottom(true);
    }
    setTimeout(markReadVisible, 250);
  }
  function totalUnreadCount() {
    var t = 0; channels.forEach(function (c) { t += unread[c.name] || 0; }); return t;
  }
  // The oldest unread message in the view: by timestamp across channels in
  // "all", or the channel's own first unread.
  function firstUnreadKey(ch) {
    if (ch !== 'all') return unreadFirst[ch] || null;
    var bestKey = null, bestTs = 0;
    channels.forEach(function (c) {
      var k = unreadFirst[c.name]; if (!k) return;
      var n = document.getElementById('m' + k); if (!n) return;
      var t = parseInt(n.getAttribute('data-ts') || '0', 10);
      if (!bestKey || t < bestTs) { bestKey = k; bestTs = t; }
    });
    return bestKey;
  }

  // ----- date separators ----------------------------------------------------
  function rebuildSeparators() {
    var seps = msgs.querySelectorAll('.datesep');
    for (var i = 0; i < seps.length; i++) seps[i].remove();
    var nodes = msgs.querySelectorAll('.msg:not(.hidden):not(.fhide)');
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
    if (msgs.querySelector('[data-skel]')) return; // still loading — skeleton shows
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

  // ----- device snapshot: instant first paint ---------------------------------
  // The newest ~120 items are kept in localStorage. On open they are drawn
  // immediately — before the (possibly sleeping) server answers — so the
  // app never opens on a blank screen. The server's answer then merges in.
  var snapItems = {};
  var snapTimer = null;
  function rememberItem(it) {
    if (!it || !it.key || !it.html) return;
    snapItems[it.key] = { key: it.key, id: it.id, channel: it.channel, ts: it.ts, html: it.html, preview: it.preview, seq: it.seq };
    if (snapTimer) return;
    snapTimer = setTimeout(saveSnapshot, 1500);
  }
  function saveSnapshot() {
    snapTimer = null;
    var arr = Object.keys(snapItems).map(function (k) { return snapItems[k]; });
    arr.sort(function (a, b) { return (b.ts - a.ts) || (b.id - a.id); });
    arr = arr.slice(0, 120);
    var keep = {}; arr.forEach(function (it) { keep[it.key] = it; }); snapItems = keep;
    try {
      var json = JSON.stringify({ at: Date.now(), items: arr });
      if (json.length > 2500000) { arr = arr.slice(0, 60); json = JSON.stringify({ at: Date.now(), items: arr }); }
      localStorage.setItem('tgpopup-snap', json);
    } catch (e) {}
  }
  function paintSnapshot() {
    var snap = null;
    try { snap = JSON.parse(localStorage.getItem('tgpopup-snap') || 'null'); } catch (e) {}
    if (!snap || !snap.items || !snap.items.length) return false;
    var items = snap.items.slice().sort(function (a, b) { return (a.ts - b.ts) || (a.id - b.id); });
    clearSkeleton();
    var frag = document.createDocumentFragment();
    items.forEach(function (it) {
      try {
        if (document.getElementById('m' + it.key)) return;
        var n = makeNode(it, true);
        if (n) { frag.appendChild(n); applyClamp(n); }
      } catch (e) {}
    });
    msgs.appendChild(frag);
    fromCache = true;
    lastUpdateAt = snap.at || 0;
    rebuildSeparators();
    recountUnread();
    renderSidebar();
    renderHead();
    return true;
  }

  // ----- my reactions (local, per device) ------------------------------------
  // The viewer taps an emoji on a message; it is saved in this browser only.
  // Telegram has no idea — there is no account to send a reaction with.
  var REACT_SET = ['👍', '❤️', '🔥', '😮', '😢', '😂', '🙏', '✅'];
  // Reactions live in "ערוץ חי" itself (server), with a localStorage copy for
  // instant paint and offline use. The server is the source of truth, so the
  // same reactions show on every device that logs in with the same key.
  var myReacts = {};
  try { myReacts = JSON.parse(localStorage.getItem('tgpopup-myreact') || '{}') || {}; } catch (e) {}
  function cacheMyReacts() { try { localStorage.setItem('tgpopup-myreact', JSON.stringify(myReacts)); } catch (e) {} }
  function loadReactsFromServer() {
    fetch('/api/reactions', { cache: 'no-store' }).then(function (r) { return r.json(); }).then(function (d) {
      if (!d || !d.reactions) return;
      myReacts = d.reactions;
      cacheMyReacts();
      // Re-apply to everything already on screen.
      msgs.querySelectorAll('.msg').forEach(function (n) {
        var k = (n.id || '').slice(1); if (k) renderMyReact(n, k);
      });
    }).catch(function () {});
  }
  function reactsRow(node) {
    var r = node.querySelector('.reacts');
    if (!r) {
      r = document.createElement('div'); r.className = 'reacts';
      node.appendChild(r);
    }
    return r;
  }
  var canReact = true; // false online without a key, for everyone but the admin
  function renderMyReact(node, key) {
    var existing = node.querySelector('.react.mine');
    if (existing) existing.remove();
    // The hover "+" sits in the bubble corner, absolutely positioned, so it no
    // longer adds an empty strip under every message.
    if (canReact && !node.querySelector('.addreact')) {
      var add = document.createElement('span');
      add.className = 'addreact'; add.title = 'הוסף תגובה';
      add.setAttribute('role', 'button'); add.setAttribute('tabindex', '0'); add.setAttribute('aria-label', 'הוסף תגובה');
      add.innerHTML = svgIcon('plus');
      add.onclick = function (ev) { ev.stopPropagation(); openReactPick(node, key, add); };
      add.onkeydown = function (ev) { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); ev.stopPropagation(); openReactPick(node, key, add); } };
      node.appendChild(add);
    }
    var emoji = myReacts[key];
    if (emoji) {
      var row = reactsRow(node);
      var chip = document.createElement('span');
      chip.className = 'react mine'; chip.title = 'התגובה שלך — לחיצה מסירה';
      chip.innerHTML = '<span class="re">' + emoji + '</span>';
      if (canReact) chip.onclick = function (ev) { ev.stopPropagation(); setMyReact(key, null); };
      row.insertBefore(chip, row.firstChild);
    } else {
      var r = node.querySelector('.reacts');
      if (r && !r.children.length) r.remove();
    }
  }

  function applyMyReacts(node, key) { renderMyReact(node, key); }
  function setMyReact(key, emoji) {
    if (!key) return;
    if (emoji) myReacts[key] = emoji; else delete myReacts[key];
    cacheMyReacts();
    var node = document.getElementById('m' + key) || document.getElementById('sr_' + key);
    if (node) renderMyReact(node, key);
    // Save into the app itself so it follows you to other devices.
    fetch('/api/reactions', { method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ key: key, emoji: emoji || '' }) }).catch(function () {});
  }
  function openReactPick(node, key, anchorEl) {
    var pick = document.getElementById('reactpick');
    pick.innerHTML = '';
    REACT_SET.forEach(function (em) {
      var s = document.createElement('span'); s.textContent = em;
      s.onclick = function (ev) { ev.stopPropagation(); setMyReact(key, myReacts[key] === em ? null : em); closeReactPick(); };
      pick.appendChild(s);
    });
    pick.classList.add('open');
    var r = (anchorEl || node).getBoundingClientRect();
    var pw = pick.offsetWidth, ph = pick.offsetHeight;
    var left = Math.min(Math.max(8, r.left + r.width / 2 - pw / 2), window.innerWidth - pw - 8);
    var top = r.top - ph - 8; if (top < 8) top = r.bottom + 8;
    pick.style.left = left + 'px'; pick.style.top = top + 'px';
  }
  function closeReactPick() { document.getElementById('reactpick').classList.remove('open'); }
  document.addEventListener('click', function (ev) {
    if (!ev.target.closest || (!ev.target.closest('#reactpick') && !ev.target.closest('.addreact'))) closeReactPick();
  });

  var itemMeta = {}; // key -> light record for the digest
  function makeNode(it, fromSnap) {
    if (!fromSnap) rememberItem(it);
    itemMeta[it.key] = { key: it.key, id: it.id, channel: it.channel, ts: it.ts, preview: it.preview || '', text: it.text || '' };
    clearSkeleton();
    var holder = document.createElement('div');
    holder.innerHTML = it.html || '';
    // Skip any leading text/whitespace node so we always grab the <article>.
    var node = holder.querySelector ? holder.querySelector('.msg') : holder.firstElementChild;
    if (!node) { return null; }
    node.setAttribute('data-ts', it.ts);
    if (it.channel) node.style.setProperty('--chan', 'hsl(' + hue(it.channel) + ',42%,58%)');
    var tm = node.querySelector('.msgtime');
    if (tm && it.ts) { tm.textContent = fmtStamp(it.ts); tm.title = new Date(it.ts * 1000).toLocaleString('he-IL'); }
    if (it.seq && it.seq > lastSeq) lastSeq = it.seq;
    noteItem(it);
    if (current !== 'all' && it.channel !== current) node.classList.add('hidden');
    applyMyReacts(node, it.key);
    // Images fade in as they load instead of popping.
    var imgs = node.querySelectorAll('.photo img');
    for (var i = 0; i < imgs.length; i++) {
      (function (im) {
        if (im.complete && im.naturalWidth > 0) { im.classList.add('ld'); return; }
        im.onload = function () {
          im.classList.add('ld');
          var par = im.parentNode;
          if (par) { var o = par.querySelector('.imgretry'); if (o) o.remove(); if (par.style) par.style.minHeight = ''; }
        };
        im.onerror = function () {
          im.classList.add('ld');
          if (im.closest && im.closest('.photo, .stickerbox, .album')) handleImgError(im);
        };
      })(imgs[i]);
    }
    // GIF-like previews play ONLY while on screen, always muted. Playing
    // every preview at once (the old autoplay attribute) both dragged the
    // whole page down and — through the retry path — caused the audio storm.
    var gifs = node.querySelectorAll('video.gifvid');
    for (var vi = 0; vi < gifs.length; vi++) {
      (function (v) {
        v.muted = true;
        v.volume = 0;
        v.addEventListener('volumechange', function () {
          if (!v.muted) { v.muted = true; v.volume = 0; }
        });
        if (gifObserver) {
          gifObserver.observe(v);
        } else {
          v.autoplay = true; // ancient browser fallback — still muted
          var p = v.play();
          if (p && p.catch) p.catch(function () {});
        }
      })(gifs[vi]);
    }
    return node;
  }

  // Plays each GIF preview only while visible; pauses (and frees the
  // decoder) the moment it scrolls away.
  var gifObserver = ('IntersectionObserver' in window)
    ? new IntersectionObserver(function (entries) {
        for (var i = 0; i < entries.length; i++) {
          var v = entries[i].target;
          if (entries[i].isIntersecting) {
            if (document.documentElement.classList.contains('a11y-still')) continue;
            v.muted = true; v.volume = 0;
            var p = v.play();
            if (p && p.catch) p.catch(function () {});
          } else {
            try { v.pause(); } catch (e) {}
          }
        }
      }, { root: chat, rootMargin: '250px' })
    : null;

  // Keep the DOM bounded on very long sessions — but never yank content out
  // from under someone who scrolled up to read.
  function trimDom() {
    if (!nearBottom()) return;
    var nodes = msgs.querySelectorAll('.msg');
    var extra = nodes.length - 1500;
    for (var i = 0; i < extra; i++) nodes[i].remove();
  }

  // Long messages collapse with a fade + "show more" toggle. Measuring text
  // height forces layout, and doing that for 700 history bubbles froze the
  // boot — so measurement now waits until a bubble first scrolls into view.
  var clampObserver = ('IntersectionObserver' in window)
    ? new IntersectionObserver(function (entries) {
        for (var i = 0; i < entries.length; i++) {
          if (entries[i].isIntersecting) {
            clampNow(entries[i].target);
            clampObserver.unobserve(entries[i].target);
          }
        }
      }, { root: chat, rootMargin: '300px' })
    : null;

  function applyClamp(node) {
    if (clampObserver) { clampObserver.observe(node); return; }
    clampNow(node);
  }

  function clampNow(node) {
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

  // Insert by time, not blindly at the end: after a restart, a resync or a
  // replay, items can arrive out of order. The common case (newest) is one
  // comparison and an append.
  function insertOrdered(n) {
    var ts = parseInt(n.getAttribute('data-ts') || '0', 10);
    var id = parseInt((n.id || '').replace(/^.*_/, ''), 10) || 0;
    var prev = msgs.lastElementChild;
    while (prev && (prev.classList.contains('datesep') || prev.classList.contains('unreadline') || prev.id === 'emptystate' || prev.hasAttribute('data-skel'))) prev = prev.previousElementSibling;
    if (!prev) { msgs.appendChild(n); return; }
    var guard = 0;
    while (prev && guard++ < 4000) {
      if (!prev.classList.contains('msg')) { prev = prev.previousElementSibling; continue; }
      var pts = parseInt(prev.getAttribute('data-ts') || '0', 10);
      var pid = parseInt((prev.id || '').replace(/^.*_/, ''), 10) || 0;
      if (pts < ts || (pts === ts && pid <= id)) break;
      prev = prev.previousElementSibling;
    }
    if (!prev) msgs.insertBefore(n, msgs.firstChild);
    else if (prev.nextSibling) msgs.insertBefore(n, prev.nextSibling);
    else msgs.appendChild(n);
  }

  function insertNodeBottom(it) {
    var n = makeNode(it);
    if (!n) return null;
    insertOrdered(n);
    applyClamp(n);
    return n;
  }

  // Any inline video that fails to stream straight from the CDN gets one
  // automatic retry THROUGH the app's own streaming proxy.
  //
  // CRITICAL: the retry must NEVER start playback on its own. Archived
  // messages carry video URLs whose tokens expire; on page load dozens of
  // them fire 'error' at once, and an unconditional play() here turned that
  // into a wall of overlapping sound ("the videos open by themselves" — the
  // user diagnosed it exactly). Only muted GIF-like previews (autoplay attr)
  // resume; everything else just gets a working source and WAITS for a click.
  function handleImgError(img) {
    var n = parseInt(img.getAttribute('data-try') || '0', 10);
    var box = img.parentNode;
    if (!box) return;
    if (n < 2) {
      img.setAttribute('data-try', n + 1);
      var src = img.getAttribute('src');
      // Re-request the same URL after a short wait (NetFree releases held
      // images after review; a plain reload then succeeds).
      setTimeout(function () { try { img.src = ''; img.src = src; } catch (e) {} }, n === 0 ? 2500 : 6000);
      return;
    }
    // Give up auto-retrying: offer a manual reload over the reserved box.
    if (box.querySelector('.imgretry')) return;
    if (box.classList && box.classList.contains('photo')) box.style.minHeight = box.style.minHeight || '110px';
    var o = document.createElement('div'); o.className = 'imgretry';
    o.innerHTML = svgIcon('refresh') + '<span>התמונה עדיין נטענת — הקש לניסיון חוזר</span>';
    o.onclick = function (ev2) {
      ev2.stopPropagation();
      o.classList.add('spin');
      img.setAttribute('data-try', '0');
      var src = img.getAttribute('src');
      try { img.src = ''; img.src = src; } catch (e) {}
      img.onload = function () { o.remove(); if (box.style) box.style.minHeight = ''; };
      setTimeout(function () { o.classList.remove('spin'); }, 4000);
    };
    box.appendChild(o);
  }

  chat.addEventListener('error', function (ev) {
    var v = ev.target;
    if (!v || v.tagName !== 'VIDEO') return;
    var proxy = v.getAttribute('data-proxy');
    if (!proxy || v.getAttribute('data-proxied')) return;
    v.setAttribute('data-proxied', '1');
    var t = v.currentTime || 0;
    v.src = proxy;
    v.load();
    if (t > 0.5) { v.currentTime = t; }
    // NEVER auto-plays anything. A repaired GIF preview resumes through the
    // visibility observer (re-observe forces an immediate visibility check);
    // a user video waits for the user's click.
    if (gifObserver && v.classList.contains('gifvid')) {
      gifObserver.unobserve(v);
      gifObserver.observe(v);
    }
  }, true);

  // Telegram refuses the file to anonymous clients: swap the play button
  // for a clear label so nobody keeps tapping it.
  function markTooBig(wrap, msg) {
    if (!wrap) return;
    wrap.classList.remove('embedwrap');
    wrap.removeAttribute('data-loading');
    wrap.classList.add('toobig');
    var pb = wrap.querySelector('.playbtn'); if (pb) pb.remove();
    var spin = wrap.querySelector('.spin'); if (spin) spin.remove();
    if (!wrap.querySelector('.toobiglabel')) {
      var l = document.createElement('div'); l.className = 'toobiglabel';
      var b = document.createElement('b'); b.textContent = 'סרטון ארוך';
      var sp = document.createElement('span'); sp.textContent = 'זמין לצפייה רק בתוך טלגרם';
      l.appendChild(b); l.appendChild(sp); wrap.appendChild(l);
    }
    var art = wrap.closest('.msg');
    var link = art ? art.querySelector('a.msgtime, .msghead a') : null;
    if (msg) toast(msg);
  }

  // Long videos: clicking the preview asks OUR server for the direct video
  // file and plays it in the page's own player. Fallback chain, all
  // automatic: direct CDN → streaming through the app → clear message.
  chat.addEventListener('click', function (ev) {
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
        if (d.toobig) {
          // Telegram's final answer: relabel the frame, no retries.
          markTooBig(wrap, d.error);
          return;
        }
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

  // Coalesced UI refresh: a burst of incoming messages triggers ONE sidebar
  // rebuild and ONE separator pass on the next frame, instead of a full
  // O(everything) re-render per message. This alone removes most of the
  // "everything feels heavy" jank during active hours.
  var _uiPending = false;
  function scheduleUI() {
    if (_uiPending) return;
    _uiPending = true;
    requestAnimationFrame(function () {
      _uiPending = false;
      rebuildSeparators();
      renderEmptyState();
      renderSidebar();
      renderHead();
    });
  }

  function appendBottom(it, fresh) {
    if (document.getElementById('m' + it.key)) return;
    var wasNear = nearBottom();
    var node = makeNode(it);
    if (!node) return;
    if (fresh) {
      node.classList.add('fresh');
      setTimeout(function () { node.classList.remove('fresh'); }, 4000);
    }
    insertOrdered(node);
    applyClamp(node);
    if (mediaFilter !== 'all' && itemKind(node) !== mediaFilter) node.classList.add('fhide');
    trimDom();

    var visible = (current === 'all' || it.channel === current);
    if (fresh) {
      if (visible && wasNear && !document.hidden) {
        scrollBottom(false);
        setTimeout(markReadVisible, 300);
      } else if (visible) {
        pendingJump++; showJump();
        if (!unreadFirst[it.channel]) unreadFirst[it.channel] = it.key;
        unread[it.channel] = (unread[it.channel] || 0) + 1;
      } else {
        unread[it.channel] = (unread[it.channel] || 0) + 1;
        // Remember where the unread run starts, for the "הודעות חדשות" line.
        if (!unreadFirst[it.channel]) unreadFirst[it.channel] = it.key;
      }
      if (document.hidden) {
        titleUnread++;
        document.title = '(' + titleUnread + ') ' + baseTitle;
      }
      maybeNotify(it);
    }
    scheduleUI();
  }

  function prependTop(items) { // oldest..newest batch
    // Anchor on the first message currently on screen and restore its
    // position after the insert — scrollHeight arithmetic is unreliable with
    // lazy images and content-visibility.
    var anchor = null, anchorTop = 0;
    var vis = msgs.querySelectorAll('.msg:not(.hidden)');
    var top = chat.getBoundingClientRect().top;
    for (var a = 0; a < vis.length; a++) {
      var r = vis[a].getBoundingClientRect();
      if (r.bottom > top) { anchor = vis[a]; anchorTop = r.top; break; }
    }
    for (var i = 0; i < items.length; i++) {
      var it = items[i];
      if (document.getElementById('m' + it.key)) continue;
      var n = makeNode(it);
      if (!n) continue;
      insertOrdered(n);
      applyClamp(n);
    }
    rebuildSeparators();
    if (anchor) {
      var now = anchor.getBoundingClientRect().top;
      chat.scrollTop += (now - anchorTop);
    }
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
  var readScrollTimer = null;
  chat.addEventListener('scroll', function () {
    var nb = nearBottom();
    if (nb) hideJump();
    document.getElementById('downbtn').style.display = nb ? 'none' : 'flex';
    if (chat.scrollTop < 70) maybeLoadOlder();
    if (!readScrollTimer) readScrollTimer = setTimeout(function () { readScrollTimer = null; markReadVisible(); }, 350);
  }, { passive: true });

  document.addEventListener('visibilitychange', function () {
    if (!document.hidden) {
      titleUnread = 0;
      document.title = baseTitle;
      resync(); // pick up anything that arrived while the tab slept (cheap: only new seqs)
      ensureStream();
      setTimeout(markReadVisible, 400);
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
    if (heldOlder.length && !loadingOlder) {
      // Feed from memory first, until something visible in this view arrives.
      var added = 0, guard = 0;
      while (heldOlder.length && added === 0 && guard++ < 20) {
        var batch = heldOlder.splice(Math.max(0, heldOlder.length - 60));
        batch = batch.filter(function (it) { return !document.getElementById('m' + it.key); });
        added = batch.filter(function (it) { return current === 'all' || it.channel === current; }).length;
        if (batch.length) prependTop(batch);
      }
      return;
    }
    if (loadingOlder || scopeDone() || visibleCount() === 0) return;
    loadingOlder = true;
    document.getElementById('topload').style.display = 'block';

    var collected = [];
    var queue = scopeChannels().slice();
    function one(ch) {
      if (donePer[ch]) return Promise.resolve();
      var before = oldestPer[ch];
      if (!before) { donePer[ch] = true; return Promise.resolve(); }
      return fetch('/api/older?channel=' + encodeURIComponent(ch) + '&before=' + before)
        .then(function (r) { return r.json(); })
        .then(function (data) {
          var fresh = (data.items || []).filter(function (it) {
            return !document.getElementById('m' + it.key);
          });
          if (!fresh.length) donePer[ch] = true;
          collected = collected.concat(fresh);
        }).catch(function () {});
    }
    // Three channels at a time: faster than one-by-one, gentle on the server.
    function worker() {
      if (!queue.length) return Promise.resolve();
      return one(queue.shift()).then(worker);
    }
    var chain = Promise.all([worker(), worker(), worker()]);
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

  // ----- keyword settings ---------------------------------------------------
  function splitKw(s) {
    return s.split(/[,\n;]+/).map(function (x) { return x.trim(); }).filter(Boolean);
  }
  document.getElementById('kwbtn').onclick = function () {
    fetch('/api/keywords').then(function (r) { return r.json(); }).then(function (d) {
      document.getElementById('kwinclude').value = (d.include || []).join(', ');
      document.getElementById('kwexclude').value = (d.exclude || []).join(', ');
      document.getElementById('kwerr').textContent = '';
      document.getElementById('kwback').style.display = 'flex';
    }).catch(function () { toast('שגיאת תקשורת'); });
  };
  document.getElementById('kwcancel').onclick = function () {
    document.getElementById('kwback').style.display = 'none';
  };
  document.getElementById('kwback').onclick = function (ev) {
    if (ev.target === this) this.style.display = 'none';
  };
  document.getElementById('kwsave').onclick = function () {
    fetch('/api/keywords', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        include: splitKw(document.getElementById('kwinclude').value),
        exclude: splitKw(document.getElementById('kwexclude').value)
      })
    }).then(function (r) { return r.json(); }).then(function (d) {
      if (!d.ok) { document.getElementById('kwerr').textContent = d.error || 'שגיאה'; return; }
      document.getElementById('kwback').style.display = 'none';
      toast('מילות המפתח נשמרו');
    }).catch(function () { document.getElementById('kwerr').textContent = 'שגיאת תקשורת'; });
  };

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
  // Channels are remembered too, so the snapshot can draw with real names
  // and avatars before the server is awake.
  try {
    var cachedCh = JSON.parse(localStorage.getItem('tgpopup-channels') || 'null');
    if (cachedCh && cachedCh.length) { channels = cachedCh; renderSidebar(); }
  } catch (e) {}
  var painted = paintSnapshot();
  if (painted) {
    var want0 = 'all';
    try { want0 = localStorage.getItem('tgpopup-channel') || 'all'; } catch (e) {}
    if (want0 !== 'all' && !chanByName(want0)) want0 = 'all';
    select(want0);
  }
  fetch('/api/channels').then(function (r) { return r.json(); }).then(function (d) {
    channels = d.channels || [];
    channelsLocked = !!d.locked;
    try { localStorage.setItem('tgpopup-channels', JSON.stringify(channels)); } catch (e) {}
    if (channelsLocked) {
      var aw = document.querySelector('.addwrap');
      if (aw) aw.style.display = 'none';
    }
    renderSidebar();
    return fetch('/api/messages');
  }).then(function (r) {
    if (r.status === 401) { location.href = '/login'; throw new Error('login'); }
    return r.json();
  }).then(function (data) {
    booted = true;
    touchFresh();
    document.getElementById('wakehint').style.display = 'none';
    if (data.seq > lastSeq) lastSeq = data.seq;
    var items = (data.items || []).slice();
    items.sort(function (a, b) { return (a.ts - b.ts) || (a.id - b.id); }); // oldest first
    // Phones choke on 800 bubbles at once. Draw the newest BOOT_MAX now; the
    // older ones wait in memory and come in as you scroll up (no server call).
    if (items.length > BOOT_MAX) {
      heldOlder = items.slice(0, items.length - BOOT_MAX);
      items = items.slice(items.length - BOOT_MAX);
      heldOlder.forEach(function (it) {
        itemMeta[it.key] = { key: it.key, id: it.id, channel: it.channel, ts: it.ts, preview: it.preview || '', text: it.text || '' };
      });
    }
    // One fragment, one layout: 800 bubbles appended individually made the
    // boot visibly stutter on phones.
    clearSkeleton();
    var frag = document.createDocumentFragment();
    items.forEach(function (it) {
      try {
        if (document.getElementById('m' + it.key)) { rememberItem(it); return; }
        if (painted) { insertNodeBottom(it); return; } // merge in order into the snapshot
        var n = makeNode(it);
        if (n) { frag.appendChild(n); applyClamp(n); }
      } catch (e) {}
    });
    msgs.appendChild(frag);
    rebuildSeparators();
    recountUnread();
    // First visit ever: nothing is "unread" — everything already there was
    // published before you started using the app on this device.
    if (!Object.keys(readUpTo).length) {
      channels.forEach(function (c) { var lm = newestVisibleTs(c.name, 1e12); if (lm) readUpTo[c.name] = lm; });
      saveRead(); recountUnread();
    }
    if (painted) {
      // Already on screen from the snapshot — keep the reader's place, just
      // refresh the sidebar and follow the bottom if they were there.
      var nb0 = nearBottom();
      renderSidebar(); renderHead();
      if (nb0) scrollBottom(true);
      setTimeout(markReadVisible, 300);
    } else {
      var want = 'all';
      try { want = localStorage.getItem('tgpopup-channel') || 'all'; } catch (e) {}
      if (want !== 'all' && !chanByName(want)) want = 'all';
      select(want); // lands on the "הודעות חדשות" line when there is one
    }
    saveSnapshot();
    loadReactsFromServer(); // pull personal reactions saved in the app
    handleHashTarget(); // popup deep link: land on the exact message
  }).catch(function (e) {
    if (e && e.message === 'login') return;
    clearSkeleton();
    document.getElementById('wakehint').style.display = 'none';
    var m = document.getElementById('msgs');
    var hint = isCloud ? 'בדוק את החיבור לאינטרנט ורענן את הדף.' : 'ודא שהתוכנה רצה (Start.bat) ורענן.';
    if (m) m.innerHTML = '<div class="empty"><div class="big">⚠️</div><div class="t">לא הצלחתי לטעון את ההודעות. ' + hint + '</div></div>';
  });
  // Cloud: a sleeping free-tier server takes up to a minute to answer the
  // first request. Say so instead of shimmering in silence.
  setTimeout(function () {
    if (booted || painted) return;
    var w = document.getElementById('wakehint');
    w.textContent = 'השרת מתעורר משינה… זה לוקח בדרך כלל עד דקה, הדף יתמלא לבד.';
    w.style.display = 'block';
  }, 6000);

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
      var txt = document.getElementById('sbartext');
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
    searchScrollTop = chat.scrollTop;
    document.getElementById('searchbar').classList.add('open');
    document.getElementById('msgs').style.display = 'none';
    document.getElementById('topload').style.display = 'none';
    document.getElementById('results').style.display = 'block';
    document.getElementById('resultsmeta').textContent = 'הקלד כדי לחפש בכל מה שנשמר אי פעם';
    document.getElementById('resultslist').innerHTML = '';
    setTimeout(function () { document.getElementById('searchinput').focus(); }, 40);
  }
  var searchScrollTop = 0;
  function closeSearch() {
    searchOpen = false;
    document.getElementById('searchbar').classList.remove('open');
    document.getElementById('searchinput').value = '';
    document.getElementById('results').style.display = 'none';
    document.getElementById('msgs').style.display = 'block';
    chat.scrollTop = searchScrollTop; // back to where you were, not the bottom
  }
  document.getElementById('searchbtn').onclick = function () {
    if (searchOpen) closeSearch(); else openSearch();
  };
  document.getElementById('searchclose').onclick = closeSearch;
  document.addEventListener('keydown', function (ev) {
    if (ev.key === 'Escape') {
      if (document.getElementById('a11yback').classList.contains('open')) { closeA11y(); return; }
      if (document.getElementById('statsback').classList.contains('open')) { closeStats(); return; }
      if (document.getElementById('reactpick').classList.contains('open')) { closeReactPick(); return; }
      if (document.getElementById('msgsheet').classList.contains('open')) { closeSheet(); return; }
      if (document.getElementById('digestback').classList.contains('open')) { closeDigest(); return; }
      if (document.getElementById('moremenu').classList.contains('open')) { closeMore(); return; }
      if (document.getElementById('kwback').style.display === 'flex') { document.getElementById('kwback').style.display = 'none'; return; }
      if (document.getElementById('modalback').style.display === 'flex') { closeModal(); return; }
      if (searchOpen) closeSearch();
    }
    // "/" or Ctrl+K opens search, like every messenger.
    if ((ev.key === '/' || (ev.key.toLowerCase() === 'k' && (ev.ctrlKey || ev.metaKey))) && !searchOpen) {
      var tag = (document.activeElement && document.activeElement.tagName) || '';
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;
      ev.preventDefault();
      openSearch();
    }
  });

  function markHits(node, q) {
    var texts = node.querySelectorAll('.msgtext');
    for (var i = 0; i < texts.length; i++) {
      var walker = document.createTreeWalker(texts[i], NodeFilter.SHOW_TEXT);
      var found = [];
      while (walker.nextNode()) found.push(walker.currentNode);
      found.forEach(function (tn) {
        var lower = tn.textContent.toLowerCase(), ql = q.toLowerCase();
        var idx = lower.lastIndexOf(ql); // back to front: earlier offsets stay valid
        while (idx >= 0) {
          var range = document.createRange();
          range.setStart(tn, idx);
          range.setEnd(tn, idx + q.length);
          var m = document.createElement('mark');
          try { range.surroundContents(m); } catch (e) {}
          idx = idx > 0 ? lower.lastIndexOf(ql, idx - 1) : -1;
        }
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
          node.setAttribute('data-ts', it.ts);
          var stm = node.querySelector('.msgtime');
          if (stm && it.ts) stm.textContent = fmtStamp(it.ts);
          markHits(node, q);
          // Clicking the stamp lands on the message in the timeline when loaded.
          if (stm && document.getElementById('m' + it.key)) {
            stm.style.cursor = 'pointer'; stm.style.textDecoration = 'underline'; stm.title = 'עבור להודעה בציר הזמן';
            stm.onclick = function () { closeSearch(); gotoMessage(it.channel, it.id); };
          }
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

  // ----- photo lightbox (with album navigation) -----------------------------
  var lbList = [], lbIdx = 0;
  function lbShow(i) {
    if (!lbList.length) return;
    lbIdx = (i + lbList.length) % lbList.length;
    document.getElementById('lightboximg').src = lbList[lbIdx];
    var multi = lbList.length > 1;
    document.getElementById('lbprev').style.display = multi ? 'block' : 'none';
    document.getElementById('lbnext').style.display = multi ? 'block' : 'none';
    var c = document.getElementById('lbcount');
    c.style.display = multi ? 'block' : 'none';
    if (multi) c.textContent = (lbIdx + 1) + ' / ' + lbList.length;
    document.getElementById('lightbox').style.display = 'flex';
  }
  function lbClose() { document.getElementById('lightbox').style.display = 'none'; }
  chat.addEventListener('click', function (ev) {
    var img = ev.target;
    if (!img || img.tagName !== 'IMG') return;
    if (img.closest && img.closest('.vidwrap')) return; // video thumbs have their own click
    if (img.closest && img.closest('.linkcard')) return; // link cards navigate
    // An album groups its photos: collect siblings so ‹ › browses the set.
    var scope = (img.closest && img.closest('.album')) || img;
    var imgsIn = scope.tagName === 'IMG' ? [scope] : scope.querySelectorAll('img');
    lbList = [];
    var start = 0;
    for (var i = 0; i < imgsIn.length; i++) {
      lbList.push(imgsIn[i].src);
      if (imgsIn[i] === img) start = i;
    }
    lbShow(start);
  });
  document.getElementById('lbprev').onclick = function (ev) { ev.stopPropagation(); lbShow(lbIdx - 1); };
  document.getElementById('lbnext').onclick = function (ev) { ev.stopPropagation(); lbShow(lbIdx + 1); };
  document.getElementById('lightbox').onclick = function (ev) {
    if (ev.target.id === 'lightboximg' && lbList.length > 1) { lbShow(lbIdx + 1); return; }
    lbClose();
  };
  document.addEventListener('keydown', function (ev) {
    var open = document.getElementById('lightbox').style.display === 'flex';
    if (!open) return;
    if (ev.key === 'Escape') lbClose();
    if (ev.key === 'ArrowRight') lbShow(lbIdx - 1); // RTL: right = previous
    if (ev.key === 'ArrowLeft') lbShow(lbIdx + 1);
  });

  // ----- theme --------------------------------------------------------------
  var theme = 'dark';
  try { if (localStorage.getItem('tgpopup-theme') === 'light') theme = 'light'; } catch (e) {}
  function applyTheme() {
    document.body.classList.toggle('light', theme === 'light');
    document.documentElement.classList.toggle('light', theme === 'light');
    var tb = document.getElementById('themebtn'); tb.innerHTML = svgIcon(theme === 'light' ? 'sun' : 'moon');
    try { localStorage.setItem('tgpopup-theme', theme); } catch (e) {}
    var tc = document.querySelector('meta[name="theme-color"]:not([media])');
    if (!tc) { tc = document.createElement('meta'); tc.name = 'theme-color'; document.head.appendChild(tc); }
    tc.content = theme === 'light' ? '#ffffff' : '#0f1320';
  }
  applyTheme();
  document.getElementById('themebtn').onclick = function () {
    theme = (theme === 'light') ? 'dark' : 'light';
    applyTheme();
    fetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ theme: theme })
    }).catch(function () {});
  };

  // ----- self-update --------------------------------------------------------
  // The desktop app checks the user's GitHub repo; when a newer version was
  // uploaded there, a green button appears. One click: download, swap,
  // restart — and this tab reconnects to the new version by itself.
  function checkUpdate() {
    fetch('/api/update').then(function (r) { return r.json(); }).then(function (d) {
      var row = document.getElementById('updrow');
      if (!row) return;
      if (d.supported && d.available) {
        row.style.display = 'block';
        document.getElementById('updbtn').textContent =
          '⬇ גרסה ' + d.latest + ' זמינה — לחץ לעדכון';
      } else {
        row.style.display = 'none';
      }
    }).catch(function () {});
  }
  document.getElementById('updbtn').onclick = function () {
    if (!confirm('לעדכן עכשיו? התוכנה תרד לכמה שניות ותעלה מחדש עם הגרסה החדשה.')) return;
    var btn = this;
    btn.disabled = true;
    btn.textContent = '⬇ מוריד ומעדכן…';
    fetch('/api/update', { method: 'POST' })
      .then(function (r) { return r.json(); })
      .then(function (d) {
        if (d.error) {
          btn.disabled = false;
          checkUpdate();
          toast(d.error);
          return;
        }
        btn.textContent = '✓ מעדכן — הדף יתחבר מחדש לבד…';
        // The app restarts; reload once the new version answers.
        var tries = 0;
        var probe = setInterval(function () {
          tries++;
          fetch('/api/settings').then(function () {
            clearInterval(probe);
            location.reload();
          }).catch(function () {
            if (tries > 40) clearInterval(probe); // ~2 min, give up quietly
          });
        }, 3000);
      })
      .catch(function () {
        // Expected mid-swap: the server dies before answering. Probe & reload.
        var tries = 0;
        var probe = setInterval(function () {
          tries++;
          fetch('/api/settings').then(function () {
            clearInterval(probe);
            location.reload();
          }).catch(function () {
            if (tries > 40) clearInterval(probe);
          });
        }, 3000);
      });
  };
  checkUpdate();
  setInterval(checkUpdate, 6 * 3600 * 1000); // twice a day is plenty

  // ----- autostart ----------------------------------------------------------
  fetch('/api/autostart').then(function (r) { return r.json(); }).then(function (d) {
    if (d.supported === false) return; // cloud / phone: nothing to start with Windows
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
    p.innerHTML = svgIcon('monitor');
    p.className = 'iconbtn' + (setPopups ? ' on' : ' off');
    p.title = setPopups ? 'חלוניות במחשב פועלות' : 'חלוניות במחשב כבויות';
    var sB = document.getElementById('tglsound');
    sB.innerHTML = svgIcon(setSound ? 'volume' : 'volumeoff');
    sB.className = 'iconbtn' + (setSound ? ' on' : ' off');
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
    if (!d.cloud && (d.theme === 'light' || d.theme === 'dark')) { theme = d.theme; applyTheme(); }
    isAdmin = !!d.admin;
    if (d.canReact === false && canReact) { canReact = false; document.querySelectorAll('.addreact').forEach(function (b) { b.remove(); }); }
    paintToggles();
    applyCaps(!!d.cloud);
  }).catch(function () {});

  // Online there is no desktop: no popups, no sounds, no keyword filter for
  // popups, no "start with Windows". The phone gets browser notifications.
  var isAdmin = false;
  function applyCaps(cloud) {
    isCloud = cloud;
    // Stats are admin-only: hide the button for ordinary viewers.
    var sb = document.getElementById('statsbtn');
    if (sb) sb.style.display = isAdmin ? 'flex' : 'none';
    var hide = cloud ? ['tglpopups', 'tglsound', 'kwbtn'] : [];
    hide.forEach(function (id) { var el = document.getElementById(id); if (el) el.style.display = 'none'; });
    var nb = document.getElementById('tglnotif');
    if ('Notification' in window) { nb.style.display = 'flex'; paintNotif(); }
    buildMoreMenu();
  }

  // ----- browser notifications (phone / cloud) ------------------------------
  // The desktop app pops its own windows; online the browser does it. Opt-in
  // per device (localStorage), only while the page is hidden or another
  // channel is in focus, and only via the Notification API (no push server —
  // the free cloud plan sleeps, so push could not be delivered anyway).
  var notifOn = false;
  try { notifOn = localStorage.getItem('tgpopup-notif') === '1'; } catch (e) {}
  function paintNotif() {
    var b = document.getElementById('tglnotif');
    var granted = ('Notification' in window) && Notification.permission === 'granted';
    var on = notifOn && granted;
    b.innerHTML = svgIcon(on ? 'bell' : 'belloff');
    b.className = 'iconbtn keep' + (on ? ' on' : ' off');
    b.title = on ? 'התראות בדפדפן פועלות — לחץ לכיבוי' : 'הפעל התראות בדפדפן';
  }
  document.getElementById('tglnotif').onclick = function () {
    if (!('Notification' in window)) { toast('הדפדפן הזה לא תומך בהתראות'); return; }
    if (notifOn && Notification.permission === 'granted') {
      notifOn = false;
      try { localStorage.setItem('tgpopup-notif', '0'); } catch (e) {}
      paintNotif();
      toast('התראות הדפדפן כובו');
      return;
    }
    Notification.requestPermission().then(function (p) {
      if (p !== 'granted') { toast('הדפדפן חסם התראות. אפשר לאפשר בהגדרות האתר.'); paintNotif(); return; }
      notifOn = true;
      try { localStorage.setItem('tgpopup-notif', '1'); } catch (e) {}
      paintNotif();
      toast('התראות בדפדפן הופעלו — יופיעו כשהדף ברקע');
      try { new Notification('מבזקי ארץ ישראל', { body: 'ההתראות פועלות ✓', icon: '/icon-192.png', silent: true, tag: 'tgpopup-test' }); } catch (e) {}
    });
  };
  var lastNotifAt = 0;
  function maybeNotify(it) {
    if (!notifOn || !('Notification' in window) || Notification.permission !== 'granted') return;
    var ch = chanByName(it.channel);
    if (ch && ch.muted) return;
    var visible = (current === 'all' || it.channel === current);
    if (visible && !document.hidden) return; // you are looking at it already
    var now = Date.now();
    if (now - lastNotifAt < 1500) return; // a burst becomes one notification, not twenty
    lastNotifAt = now;
    var title = (ch && ch.title) || ('@' + it.channel);
    var body = (it.preview || '').replace(/^[\u{1F300}-\u{1FAFF}\u2600-\u27BF]\s*/u, '');
    var url = '/#msg=' + it.channel + '_' + it.id;
    try {
      var opts = { body: body, icon: ch && ch.photo ? ch.photo : '/icon-192.png', badge: '/icon-192.png', tag: 'tgpopup-' + it.channel, renotify: true, data: { url: url } };
      if (navigator.serviceWorker && navigator.serviceWorker.controller && 'showNotification' in ServiceWorkerRegistration.prototype) {
        navigator.serviceWorker.ready.then(function (reg) { reg.showNotification(title, opts); });
      } else {
        var n = new Notification(title, opts);
        n.onclick = function () { window.focus(); gotoMessage(it.channel, it.id); n.close(); };
      }
    } catch (e) {}
  }

  // ----- "more" menu (narrow screens) ---------------------------------------
  function closeMore() { document.getElementById('moremenu').classList.remove('open'); }
  // ----- media filter ---------------------------------------------------------
  var mediaFilter = 'all';
  function itemKind(node) {
    if (node.querySelector('.voicebox')) return 'voice';
    if (node.querySelector('video, .embedwrap, .toobig, .roundwrap')) return 'video';
    if (node.querySelector('.photo img, .album, .stickerbox')) return 'photo';
    return 'text';
  }
  function applyFilter() {
    var nodes = msgs.querySelectorAll('.msg');
    for (var i = 0; i < nodes.length; i++) {
      var n = nodes[i];
      n.classList.toggle('fhide', mediaFilter !== 'all' && itemKind(n) !== mediaFilter);
    }
    document.querySelectorAll('.fchip').forEach(function (c) { c.classList.toggle('on', c.getAttribute('data-f') === mediaFilter); });
    var fb = document.getElementById('filterbtn');
    fb.className = 'iconbtn' + (mediaFilter !== 'all' ? ' on' : '');
    rebuildSeparators();
  }
  document.querySelectorAll('.fchip').forEach(function (c) {
    c.onclick = function () { mediaFilter = c.getAttribute('data-f'); applyFilter(); };
  });
  document.getElementById('filterbtn').onclick = function () {
    var f = document.getElementById('filters');
    f.classList.toggle('open');
    if (!f.classList.contains('open') && mediaFilter !== 'all') { mediaFilter = 'all'; applyFilter(); }
  };

  // ----- text size --------------------------------------------------------
  var txtSizes = ['', 'txt-l', 'txt-xl'];
  var txtIdx = 0;
  try { txtIdx = parseInt(localStorage.getItem('tgpopup-txt') || '0', 10) || 0; } catch (e) {}
  function applyTxt() {
    document.body.classList.remove('txt-l', 'txt-xl');
    if (txtSizes[txtIdx]) document.body.classList.add(txtSizes[txtIdx]);
    try { localStorage.setItem('tgpopup-txt', String(txtIdx)); } catch (e) {}
  }
  function cycleTxt() { txtIdx = (txtIdx + 1) % txtSizes.length; applyTxt(); toast(['גודל טקסט רגיל', 'גודל טקסט גדול', 'גודל טקסט גדול מאוד'][txtIdx]); }
  applyTxt();

  // ----- message actions: long press / right click ---------------------------
  var sheetFor = null;
  function openSheet(node) {
    sheetFor = node;
    var key = (node.id || '').slice(1);
    var meta = itemMeta[key] || {};
    var ch = chanByName(meta.channel || node.getAttribute('data-channel'));
    var sheet = document.getElementById('msgsheet');
    sheet.innerHTML = '';
    function it(icon, label, fn) {
      var d = document.createElement('div'); d.className = 'sitem'; d.setAttribute('role', 'menuitem');
      var i = document.createElement('span'); i.className = 'mi'; i.textContent = icon;
      var t = document.createElement('span'); t.textContent = label;
      d.appendChild(i); d.appendChild(t);
      d.onclick = function () { closeSheet(); fn(); };
      sheet.appendChild(d);
    }
    var text = (meta.text || meta.preview || (node.querySelector('.msgtext') || {}).textContent || '').trim();
    var link = 'https://t.me/' + (meta.channel || '') + '/' + (meta.id || '');
    if (canReact) it('😊', (myReacts[key] ? 'שנה / הסר תגובה' : 'הוסף תגובה שלך'), function () { openReactPick(node, key, node); });
    it('📋', 'העתק טקסט', function () { copyText(text, 'הטקסט הועתק'); });
    it('🔗', 'העתק קישור לפוסט', function () { copyText(link, 'הקישור הועתק'); });
    if (navigator.share) it('📤', 'שתף…', function () { navigator.share({ title: (ch && ch.title) || '', text: text, url: link }).catch(function () {}); });
    if (ch) {
      it(ch.pinned ? '📌' : '📍', ch.pinned ? 'בטל נעיצה של הערוץ' : 'נעץ את הערוץ למעלה', function () { setPin(ch, !ch.pinned); });
      it(ch.muted ? '🔔' : '🔕', ch.muted ? 'בטל השתקה לערוץ' : 'השתק התראות מהערוץ', function () { select(ch.name); document.getElementById('tglmute').click(); });
      if (current === 'all') it('👁', 'הצג רק את הערוץ הזה', function () { select(ch.name); });
    }
    it('🔠', 'גודל טקסט', cycleTxt);
    document.getElementById('sheetback').classList.add('open');
    sheet.classList.add('open');
  }
  function closeSheet() {
    document.getElementById('sheetback').classList.remove('open');
    document.getElementById('msgsheet').classList.remove('open');
    sheetFor = null;
  }
  document.getElementById('sheetback').onclick = closeSheet;
  function copyText(t, okMsg) {
    if (!t) { toast('אין מה להעתיק'); return; }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(t).then(function () { toast(okMsg); }, function () { fallbackCopy(t, okMsg); });
    } else fallbackCopy(t, okMsg);
  }
  function fallbackCopy(t, okMsg) {
    var ta = document.createElement('textarea'); ta.value = t; ta.style.position = 'fixed'; ta.style.opacity = '0';
    document.body.appendChild(ta); ta.select();
    try { document.execCommand('copy'); toast(okMsg); } catch (e) { toast('ההעתקה נכשלה'); }
    ta.remove();
  }
  function setPin(ch, target) {
    fetch('/api/pin', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ channel: ch.name, pinned: target }) })
      .then(function (r) { return r.json(); }).then(function (d) {
        if (!d.ok) { toast(d.error || 'שגיאה'); return; }
        ch.pinned = d.pinned; renderSidebar();
        toast(d.pinned ? 'הערוץ ננעץ לראש הרשימה' : 'הנעיצה בוטלה');
      }).catch(function () { toast('שגיאת תקשורת'); });
  }
  // Long press (touch) and right click (mouse) on a bubble.
  var lpTimer = null, lpStart = null;
  chat.addEventListener('touchstart', function (ev) {
    var node = ev.target.closest ? ev.target.closest('.msg') : null;
    if (!node || ev.target.closest('video, audio, a, button')) return;
    var t = ev.touches[0]; lpStart = { x: t.clientX, y: t.clientY };
    lpTimer = setTimeout(function () { lpTimer = null; if (navigator.vibrate) navigator.vibrate(12); openSheet(node); }, 520);
  }, { passive: true });
  chat.addEventListener('touchmove', function (ev) {
    if (!lpTimer || !lpStart) return;
    var t = ev.touches[0];
    if (Math.abs(t.clientX - lpStart.x) > 10 || Math.abs(t.clientY - lpStart.y) > 10) { clearTimeout(lpTimer); lpTimer = null; }
  }, { passive: true });
  chat.addEventListener('touchend', function () { if (lpTimer) { clearTimeout(lpTimer); lpTimer = null; } }, { passive: true });
  chat.addEventListener('contextmenu', function (ev) {
    var node = ev.target.closest ? ev.target.closest('.msg') : null;
    if (!node || ev.target.closest('video, audio, a')) return;
    ev.preventDefault(); openSheet(node);
  });
  // Sidebar rows: long press = pin / mute sheet on touch devices.
  var rowLp = null;
  document.getElementById('chanlist').addEventListener('touchstart', function (ev) {
    var row = ev.target.closest ? ev.target.closest('.row') : null;
    if (!row || !row.getAttribute('data-chan')) return;
    rowLp = setTimeout(function () {
      rowLp = null;
      var ch = chanByName(row.getAttribute('data-chan')); if (!ch) return;
      var sheet = document.getElementById('msgsheet'); sheet.innerHTML = '';
      function it(icon, label, fn) {
        var d = document.createElement('div'); d.className = 'sitem';
        var i = document.createElement('span'); i.className = 'mi'; i.textContent = icon;
        var t = document.createElement('span'); t.textContent = label;
        d.appendChild(i); d.appendChild(t); d.onclick = function () { closeSheet(); fn(); }; sheet.appendChild(d);
      }
      it(ch.pinned ? '📌' : '📍', ch.pinned ? 'בטל נעיצה' : 'נעץ למעלה', function () { setPin(ch, !ch.pinned); });
      it(ch.muted ? '🔔' : '🔕', ch.muted ? 'בטל השתקה' : 'השתק התראות', function () { select(ch.name); document.getElementById('tglmute').click(); });
      it('⤵', 'קפוץ להודעה האחרונה', function () { select(ch.name); scrollBottom(true); });
      if (navigator.vibrate) navigator.vibrate(12);
      document.getElementById('sheetback').classList.add('open'); sheet.classList.add('open');
    }, 520);
  }, { passive: true });
  document.getElementById('chanlist').addEventListener('touchend', function () { if (rowLp) { clearTimeout(rowLp); rowLp = null; } }, { passive: true });
  document.getElementById('chanlist').addEventListener('touchmove', function () { if (rowLp) { clearTimeout(rowLp); rowLp = null; } }, { passive: true });

  // ----- accessibility menu (IS 5568) ----------------------------------------
  var A11Y_Z = [1, 1.15, 1.3, 1.5];
  var a11y = {};
  try { a11y = JSON.parse(localStorage.getItem('tgpopup-a11y') || '{}') || {}; } catch (e) { a11y = {}; }
  function saveA11y() { try { localStorage.setItem('tgpopup-a11y', JSON.stringify(a11y)); } catch (e) {} }
  function applyA11y() {
    var H = document.documentElement;
    ['contrast', 'gray', 'links', 'font', 'still'].forEach(function (k) {
      H.classList.toggle('a11y-' + k, !!a11y[k]);
      var b = document.querySelector('.a11yopt[data-a11y="' + k + '"]');
      if (b) b.setAttribute('aria-pressed', a11y[k] ? 'true' : 'false');
    });
    var z = a11y.z || 1;
    H.style.setProperty('--z', z);
    document.getElementById('a11yzlabel').textContent = 'גודל תצוגה: ' + Math.round(z * 100) + '%';
    document.getElementById('a11ytheme').setAttribute('aria-pressed', theme === 'light' ? 'true' : 'false');
    if (a11y.still) document.querySelectorAll('video').forEach(function (v) { try { v.pause(); } catch (e) {} });
  }
  function stepZoom(dir) {
    var i = A11Y_Z.indexOf(a11y.z || 1); if (i < 0) i = 0;
    i = Math.max(0, Math.min(A11Y_Z.length - 1, i + dir));
    a11y.z = A11Y_Z[i]; saveA11y(); applyA11y();
    if (pinned) scrollBottom(true);
  }
  var a11yReturnFocus = null;
  function openA11y() {
    a11yReturnFocus = document.activeElement;
    applyA11y();
    document.getElementById('a11yback').classList.add('open');
    setTimeout(function () { document.getElementById('a11yplus').focus(); }, 30);
  }
  function closeA11y() {
    document.getElementById('a11yback').classList.remove('open');
    if (a11yReturnFocus && a11yReturnFocus.focus) a11yReturnFocus.focus();
  }
  document.getElementById('a11ybtn').onclick = openA11y;
  document.getElementById('a11yclose').onclick = closeA11y;
  document.getElementById('a11yback').onclick = function (ev) { if (ev.target.id === 'a11yback') closeA11y(); };
  document.getElementById('a11yplus').onclick = function () { stepZoom(1); };
  document.getElementById('a11yminus').onclick = function () { stepZoom(-1); };
  document.querySelectorAll('.a11yopt[data-a11y]').forEach(function (b) {
    b.onclick = function () { var k = b.getAttribute('data-a11y'); a11y[k] = !a11y[k]; saveA11y(); applyA11y(); };
  });
  document.getElementById('a11ytheme').onclick = function () { document.getElementById('themebtn').click(); applyA11y(); };
  document.getElementById('a11yreset').onclick = function () { a11y = {}; saveA11y(); applyA11y(); toast('הגדרות הנגישות אופסו'); };
  // Keep keyboard focus inside the open dialog (Tab wraps around).
  document.getElementById('a11yback').addEventListener('keydown', function (ev) {
    if (ev.key !== 'Tab') return;
    var f = Array.from(document.querySelectorAll('#a11yback button, #a11yback a'));
    if (!f.length) return;
    var first = f[0], last = f[f.length - 1];
    if (ev.shiftKey && document.activeElement === first) { ev.preventDefault(); last.focus(); }
    else if (!ev.shiftKey && document.activeElement === last) { ev.preventDefault(); first.focus(); }
  });
  applyA11y();

  // ----- stats panel --------------------------------------------------------
  var statsTimer = null;
  function renderStats(d) {
    if (!d || d.disabled) { toast('נתונים לא זמינים בגרסה הזו'); return false; }
    document.getElementById('st_active').textContent = d.activeNow;
    document.getElementById('st_today').textContent = d.today;
    document.getElementById('st_views').textContent = d.views;
    document.getElementById('st_devices').textContent = d.devices;
    if (d.since) {
      var sd = new Date(d.since * 1000);
      document.getElementById('ssince').textContent = 'נמדד מאז ' + sd.toLocaleDateString('he-IL');
    }
    var max = 1; (d.days || []).forEach(function (x) { if (x.v > max) max = x.v; });
    var bars = document.getElementById('st_bars'); bars.innerHTML = '';
    (d.days || []).forEach(function (x) {
      var day = new Date(x.d + 'T00:00:00');
      var b = document.createElement('div'); b.className = 'bar';
      var bv = document.createElement('div'); bv.className = 'bv'; bv.textContent = x.v || '';
      var fill = document.createElement('div'); fill.className = 'fill'; fill.style.height = Math.round((x.v / max) * 100) + '%';
      var dl = document.createElement('div'); dl.className = 'dl'; dl.textContent = ['א','ב','ג','ד','ה','ו','ש'][day.getDay()];
      b.appendChild(bv); b.appendChild(fill); b.appendChild(dl); bars.appendChild(b);
    });
    var ub = document.getElementById('st_unlock');
    if (d.unlockPath) {
      ub.style.display = 'block';
      ub.onclick = function () {
        var url = location.origin + d.unlockPath;
        copyText(url, 'הקישור הועתק — פתח אותו בטלפון כדי לראות שם את הנתונים');
      };
    } else { ub.style.display = 'none'; }
    document.getElementById('st_note').textContent = isCloud
      ? 'הנתונים נשמרים בשרת. בתוכנית החינמית של Render הם עשויים להתאפס בעת פריסת גרסה חדשה.'
      : 'הנתונים נספרים בתוכנה, ללא כל שירות מעקב חיצוני.';
    return true;
  }
  function openStats() {
    fetch('/api/stats', { cache: 'no-store' }).then(function (r) { return r.json(); }).then(function (d) {
      if (renderStats(d) === false) return;
      document.getElementById('statsback').classList.add('open');
      if (statsTimer) clearInterval(statsTimer);
      statsTimer = setInterval(function () {
        fetch('/api/stats', { cache: 'no-store' }).then(function (r) { return r.json(); }).then(function (d) {
          if (d && !d.disabled) document.getElementById('st_active').textContent = d.activeNow;
        }).catch(function () {});
      }, 5000);
    }).catch(function () { toast('שגיאת תקשורת'); });
  }
  function closeStats() {
    document.getElementById('statsback').classList.remove('open');
    if (statsTimer) { clearInterval(statsTimer); statsTimer = null; }
  }
  document.getElementById('st_close').onclick = closeStats;
  document.getElementById('statsback').onclick = function (ev) { if (ev.target.id === 'statsback') closeStats(); };
  document.getElementById('statsbtn').onclick = openStats;

  // ----- digest: "מה היה היום" ----------------------------------------------
  // A compact, text-only rundown of everything that came in, channel by
  // channel, built from what the page already holds. Half a minute of
  // skimming instead of scrolling through a day of bubbles.
  var digestMode = 'unread';
  function openDigest(mode) {
    if (mode) digestMode = mode;
    document.querySelectorAll('.dtab').forEach(function (t) { t.classList.toggle('on', t.getAttribute('data-mode') === digestMode); });
    var list = document.getElementById('dlist');
    list.innerHTML = '';
    var now = new Date();
    var dayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime() / 1000;
    var from = 0, to = 1e12;
    if (digestMode === 'today') from = dayStart;
    else if (digestMode === 'yesterday') { from = dayStart - 86400; to = dayStart; }
    var byChan = {};
    Object.keys(itemMeta).forEach(function (k) {
      var it = itemMeta[k];
      if (!it.ts) return;
      if (digestMode === 'unread') { if (it.ts <= (readUpTo[it.channel] || 0)) return; }
      else if (it.ts < from || it.ts >= to) return;
      (byChan[it.channel] = byChan[it.channel] || []).push(it);
    });
    var order = channels.map(function (c) { return c.name; });
    Object.keys(byChan).forEach(function (c) { if (order.indexOf(c) < 0) order.push(c); });
    var total = 0;
    order.forEach(function (name) {
      var arr = byChan[name]; if (!arr || !arr.length) return;
      arr.sort(function (a, b) { return (a.ts - b.ts) || (a.id - b.id); });
      total += arr.length;
      var ch = chanByName(name);
      var h = document.createElement('div'); h.className = 'dchan';
      var av = document.createElement('div'); av.className = 'ava'; avaStyle(av, ch || name);
      var nm = document.createElement('span'); nm.textContent = (ch && ch.title) || ('@' + name);
      var cnt = document.createElement('span'); cnt.className = 'cnt'; cnt.textContent = arr.length + ' הודעות';
      h.appendChild(av); h.appendChild(nm); h.appendChild(cnt);
      list.appendChild(h);
      arr.forEach(function (it) {
        var row = document.createElement('div'); row.className = 'drow';
        var dt = document.createElement('span'); dt.className = 'dt'; dt.textContent = fmtTime(it.ts);
        var dx = document.createElement('span'); dx.className = 'dx';
        var txt = (it.text || it.preview || '').replace(/\s+/g, ' ').trim();
        var icon = '';
        if (/^🎬|^📹|^🎥/.test(it.preview || '')) icon = '🎬';
        else if (/^📷|^🖼/.test(it.preview || '')) icon = '📷';
        else if (/^🎤/.test(it.preview || '')) icon = '🎤';
        if (!txt) txt = it.preview || '';
        dx.textContent = txt.length > 220 ? txt.slice(0, 220) + '…' : txt;
        row.appendChild(dt); row.appendChild(dx);
        if (icon) { var dm = document.createElement('span'); dm.className = 'dm'; dm.textContent = icon; row.appendChild(dm); }
        row.onclick = function () { closeDigest(); gotoMessage(it.channel, it.id); };
        list.appendChild(row);
      });
    });
    if (!total) {
      var e = document.createElement('div'); e.className = 'dempty';
      e.textContent = digestMode === 'unread' ? 'אין הודעות חדשות מאז שקראת 👌' : 'לא נמצאו הודעות בתקופה הזאת';
      list.appendChild(e);
    }
    document.getElementById('dtitle').textContent = digestMode === 'unread' ? 'מה פספסתי' : (digestMode === 'today' ? 'מה היה היום' : 'מה היה אתמול');
    document.getElementById('digestback').classList.add('open');
  }
  function closeDigest() { document.getElementById('digestback').classList.remove('open'); }
  document.getElementById('dclose').onclick = closeDigest;
  document.getElementById('digestbtn').onclick = function () { openDigest(); };
  document.querySelectorAll('.dtab').forEach(function (t) { t.onclick = function () { openDigest(t.getAttribute('data-mode')); }; });

  function buildMoreMenu() {
    var m = document.getElementById('moremenu');
    m.innerHTML = '';
    function item(icon, label, fn) {
      var d = document.createElement('div');
      d.className = 'mitem'; d.setAttribute('role', 'menuitem'); d.setAttribute('tabindex', '0');
      var i = document.createElement('span'); i.className = 'mi'; i.textContent = icon;
      var t = document.createElement('span'); t.textContent = label;
      d.appendChild(i); d.appendChild(t);
      d.onclick = function () { closeMore(); fn(); };
      m.appendChild(d);
    }
    item('📋', 'מה פספסתי / מה היה היום', function () { openDigest(); });
    if (isAdmin) item('📊', 'נתוני כניסות', function () { openStats(); });
    item('♿', 'נגישות', function () { openA11y(); });
    item('📄', 'תקנון ופרטיות', function () { window.open('/terms', '_blank', 'noopener'); });
    item('📄', 'הצהרת נגישות', function () { window.open('/accessibility', '_blank', 'noopener'); });
    item('🔠', 'גודל טקסט', cycleTxt);
    item('⚗️', 'סינון לפי סוג', function () { document.getElementById('filterbtn').click(); });
    item(theme === 'light' ? '🌙' : '🌓', 'מצב בהיר / כהה', function () { document.getElementById('themebtn').click(); });
    if (current !== 'all') {
      var ch = chanByName(current);
      item(ch && ch.muted ? '🔔' : '🔕', ch && ch.muted ? 'בטל השתקה לערוץ' : 'השתק התראות לערוץ', function () { document.getElementById('tglmute').click(); });
    }
    if (!isCloud) {
      item(setPopups ? '🖥️' : '🚫', setPopups ? 'כבה חלוניות במחשב' : 'הפעל חלוניות במחשב', function () { document.getElementById('tglpopups').click(); });
      item(setSound ? '🔊' : '🔇', setSound ? 'כבה צליל התראה' : 'הפעל צליל התראה', function () { document.getElementById('tglsound').click(); });
      item('⚙️', 'מילות מפתח להתראות', function () { document.getElementById('kwbtn').click(); });
    }
    if (deferredInstall) item('📲', 'התקן כאפליקציה', function () { document.getElementById('installbtn').click(); });
  }
  document.getElementById('morebtn').onclick = function (ev) {
    ev.stopPropagation();
    buildMoreMenu();
    document.getElementById('moremenu').classList.toggle('open');
  };
  document.addEventListener('click', function (ev) {
    if (!ev.target.closest || !ev.target.closest('#moremenu')) closeMore();
  });
  // Keyboard activation for the icon buttons (they are divs).
  document.querySelectorAll('.iconbtn[role="button"]').forEach(function (b) {
    b.addEventListener('keydown', function (ev) { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); b.click(); } });
  });

  // ----- PWA: service worker + install button --------------------------------
  var deferredInstall = null;
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', function () {
      navigator.serviceWorker.register('/sw.js').catch(function () {});
    });
  }
  var standalone = window.matchMedia && window.matchMedia('(display-mode: standalone)').matches || window.navigator.standalone === true;
  window.addEventListener('beforeinstallprompt', function (ev) {
    ev.preventDefault();
    deferredInstall = ev;
    if (!standalone) document.getElementById('installrow').style.display = 'block';
  });
  document.getElementById('installbtn').onclick = function () {
    if (!deferredInstall) return;
    deferredInstall.prompt();
    deferredInstall.userChoice.then(function (c) {
      if (c && c.outcome === 'accepted') { toast('האפליקציה הותקנה במסך הבית'); document.getElementById('installrow').style.display = 'none'; }
      deferredInstall = null;
    });
  };
  window.addEventListener('appinstalled', function () { document.getElementById('installrow').style.display = 'none'; });
  // iPhone/iPad never fire beforeinstallprompt — explain the manual way once.
  (function () {
    var ios = /iphone|ipad|ipod/i.test(navigator.userAgent) && !window.MSStream;
    if (!ios || standalone) return;
    var seen = false;
    try { seen = localStorage.getItem('tgpopup-ios-hint') === '1'; } catch (e) {}
    if (seen) return;
    var row = document.getElementById('installrow');
    var hint = document.getElementById('iosinstall');
    document.getElementById('installbtn').style.display = 'none';
    hint.style.display = 'block';
    hint.textContent = 'להתקנה במסך הבית: לחץ על כפתור השיתוף (⎋) בספארי ואז "הוסף למסך הבית".';
    row.style.display = 'block';
    try { localStorage.setItem('tgpopup-ios-hint', '1'); } catch (e) {}
  })();

  // ----- live stream, with self-healing resync ------------------------------
  // If the app restarted or the connection blipped, the tab quietly refetches
  // everything it missed instead of silently going stale.
  // After the first full load, every resync asks only for what is newer
  // than the last sequence number seen: a few hundred bytes, not the whole
  // timeline. A 401 means the login cookie is gone — go sign in again.
  var resyncing = null;
  function resync() {
    if (resyncing) return resyncing;
    var url = booted && lastSeq > 0 ? '/api/messages?since=' + lastSeq : '/api/messages';
    resyncing = fetch(url, { cache: 'no-store' }).then(function (r) {
      if (r.status === 401) { location.href = '/login'; return { items: [] }; }
      return r.json();
    }).then(function (data) {
      resyncing = null;
      touchFresh();
      if (data.seq > lastSeq) lastSeq = data.seq;
      var items = (data.items || []).slice();
      items.sort(function (a, b) { return (a.ts - b.ts) || (a.id - b.id); });
      var added = 0;
      var wasNear = nearBottom();
      items.forEach(function (it) {
        if (!document.getElementById('m' + it.key)) {
          insertNodeBottom(it);
          added++;
          // Unread state is recomputed from the read positions below.
        }
      });
      if (added) {
        trimDom();
        rebuildSeparators();
        recountUnread();
        renderEmptyState();
        renderSidebar();
        renderHead();
        if (wasNear) { scrollBottom(true); setTimeout(markReadVisible, 300); }
      }
      return added;
    }).catch(function () { resyncing = null; return 0; });
    return resyncing;
  }

  // ----- jump-to-message (popup clicks land HERE, in this tab) ---------------
  // A popup click never opens another page while this one lives: the server
  // broadcasts a goto and this tab selects the channel, scrolls to the exact
  // message, highlights it, and starts its video in place.
  function gotoMessage(channel, id) {
    var key = channel + '_' + id;
    if (chanByName(channel) && current !== channel) select(channel);
    var node = document.getElementById('m' + key);
    if (!node) { if (current !== 'all') select('all'); node = document.getElementById('m' + key); }
    if (!node) {
      resync().then(function () {
        if (document.getElementById('m' + key)) gotoMessage(channel, id);
        else toast('ההודעה אינה בציר הזמן הטעון');
      });
      return;
    }
    node.scrollIntoView({ block: 'center' });
    node.classList.add('fresh');
    setTimeout(function () { node.classList.remove('fresh'); }, 4000);

    // Play the video right here, inside the channel.
    var wrap = node.querySelector('.embedwrap');
    if (wrap) { wrap.click(); return; } // loads + plays through the app
    var v = node.querySelector('video[controls]');
    if (v) {
      var p = v.play();
      if (p && p.catch) p.catch(function () { toast('לחץ ▶ על הסרטון כדי לנגן'); });
    }
  }

  // Deep link: a freshly opened page (no tab existed) lands on the message.
  function handleHashTarget() {
    var m = (location.hash || '').match(/^#msg=([A-Za-z0-9_]+)_(\d+)$/);
    if (!m) return;
    try { history.replaceState(null, '', location.pathname); } catch (e) {}
    setTimeout(function () { gotoMessage(m[1], parseInt(m[2], 10)); }, 600);
  }

  // EventSource reconnects by itself after a network blip (and sends
  // Last-Event-ID, so the server replays what we missed). It gives up for
  // good on an HTTP error — the login cookie expiring, the cloud server
  // restarting mid-handshake — so we watch for CLOSED and rebuild it with
  // backoff, passing the last sequence number ourselves.
  var es = null, esBackoff = 2000, esTimer = null;
  function ensureStream() {
    if (es && es.readyState !== 2) return;
    openStream();
  }
  function openStream() {
    if (esTimer) { clearTimeout(esTimer); esTimer = null; }
    if (es) { try { es.close(); } catch (e) {} }
    es = new EventSource('/api/stream' + (lastSeq > 0 ? '?last=' + lastSeq : ''));
    es.onopen = function () {
      setStatus(true);
      esBackoff = 2000;
      // Pull current state on connect — the replay covers live posts, the
      // (cheap, since-based) resync covers anything else.
      resync();
    };
    es.onerror = function () {
      setStatus(false);
      if (es.readyState !== 2) return; // the browser is retrying on its own
      // Closed for good: check whether we were logged out, then rebuild.
      fetch('/api/settings', { cache: 'no-store' }).then(function (r) {
        if (r.status === 401) { location.href = '/login'; return; }
        scheduleReopen();
      }).catch(scheduleReopen);
    };
    es.onmessage = onStreamMessage;
  }
  function scheduleReopen() {
    if (esTimer) return;
    esTimer = setTimeout(function () { esTimer = null; openStream(); }, esBackoff);
    esBackoff = Math.min(esBackoff * 2, 30000);
  }
  window.addEventListener('online', function () { esBackoff = 2000; ensureStream(); resync(); });
  // Safety net: a slow first scan can finish after boot; poll a few times
  // early on (cheap since-based calls) so the page never sits empty.
  var bootPolls = 0;
  var bootTimer = setInterval(function () {
    bootPolls++;
    resync();
    if (bootPolls >= 6) clearInterval(bootTimer); // ~30s of coverage
  }, 5000);
  // Belt and braces for long sessions: a light catch-up every two minutes.
  setInterval(function () { if (!document.hidden) resync(); }, 120000);
  openStream();
  function onStreamMessage(ev) {
    touchFresh();
    try {
      if (ev.lastEventId) { var sq = parseInt(ev.lastEventId, 10); if (sq > lastSeq) lastSeq = sq; }
      var d = JSON.parse(ev.data);
      if (d.type === 'chaninfo') {
        // A channel's real name/avatar just arrived — refresh it in place.
        var ci = chanByName(d.channel);
        if (ci) {
          if (d.title) ci.title = d.title;
          if (d.photo) ci.photo = d.photo;
          scheduleUI();
        }
        return;
      }
      if (d.type === 'goto') {
        gotoMessage(d.channel, d.id);
        return;
      }
      if (d.type === 'videotoobig') {
        var w2 = document.querySelector('#m' + d.channel + '_' + d.id + ' .embedwrap');
        if (w2) markTooBig(w2, '');
        return;
      }
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
  }
</script>
</body>
</html>`
