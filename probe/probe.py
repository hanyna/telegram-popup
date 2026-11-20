# Real end-to-end check: the actual app, real Telegram, the real bridge.
import json, os, subprocess, tempfile, time, urllib.request, urllib.error

work = tempfile.mkdtemp()
env = dict(os.environ, PORT="18420", TGPOPUP_CHANNELS="ayeletlash,elisha_yered,realelchangr,SamariaUpdates")
log = open(os.path.join(work, "app.log"), "w")
app = subprocess.Popen(["/tmp/tgpopup"], cwd=work, env=env, stdout=log, stderr=log)
B = "http://127.0.0.1:18420"

def get(path, headers=None, limit=None, timeout=240):
    t0 = time.time()
    try:
        r = urllib.request.urlopen(urllib.request.Request(B + path, headers=headers or {}), timeout=timeout)
        body = r.read(limit) if limit else r.read()
        return r.status, dict(r.headers), body, time.time() - t0
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers), e.read()[:300], time.time() - t0
    except Exception as e:
        return 0, {}, repr(e).encode(), time.time() - t0

items = []
for i in range(40):
    time.sleep(5)
    st, _, body, _ = get("/api/messages")
    if st == 200:
        items = json.loads(body).get("items", [])
        big = [x for x in items if "toobig" in x.get("html", "")]
        if len(big) >= 3 or (i > 20 and big): break
big = [x for x in items if "toobig" in x.get("html", "")]
print("messages:", len(items), "too-big:", [x["key"] for x in big])
st, _, body, _ = get("/api/settings"); print("settings:", body.decode()[:300])

seen = {}
for x in sorted(big, key=lambda x: -x["ts"]):
    seen.setdefault(x["channel"], []).append(x)
todo = [v[0] for v in seen.values()][:3]
for x in todo:
    q = "channel=%s&id=%d" % (x["channel"], x["id"])
    st, _, body, dt = get("/api/bigvideo?" + q)
    print("\n", x["key"], "bigvideo ->", st, body.decode()[:200], "%.1fs" % dt)
    try: ok = json.loads(body).get("ok")
    except Exception: ok = False
    if not ok: continue
    st, hd, body, dt = get("/api/media?" + q, {"Range": "bytes=0-999999"})
    print("   first MB:", st, len(body), "bytes in %.1fs" % dt, hd.get("Content-Range"), body[4:12])
    st, hd, body, dt = get("/api/media?" + q)
    print("   whole file:", st, len(body), "bytes in %.1fs" % dt, "moov early:", b"moov" in body[:1500000])
    st, hd, body, dt = get("/api/media?" + q, {"Range": "bytes=5000000-5099999"})
    print("   cached range:", st, len(body), "bytes in %.2fs" % dt, hd.get("Content-Range"))

app.terminate(); time.sleep(1); log.close()
print("\n== app log (long video lines)")
for line in open(os.path.join(work, "app.log"), encoding="utf-8", errors="replace"):
    if "ארוך" in line or "גשר" in line: print(line.rstrip())
