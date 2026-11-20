# After the flood wait: do hakolhayehudi / nilchamim long videos download now?
import re, time, urllib.request, urllib.error
UA = {"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"}
BR = "https://tg.i-c-a.su"
def get(url, headers=None, timeout=90, limit=None):
    h = dict(UA); h.update(headers or {})
    t0 = time.time()
    try:
        r = urllib.request.urlopen(urllib.request.Request(url, headers=h), timeout=timeout)
        return r.status, dict(r.headers), (r.read(limit) if limit else r.read()), time.time() - t0
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers), e.read()[:300], time.time() - t0
    except Exception as e:
        return 0, {}, repr(e).encode(), time.time() - t0

print("time", time.strftime("%H:%M:%S UTC", time.gmtime()))
for ch in ["hakolhayehudi", "nilchamim"]:
    st, _, body, _ = get(BR + "/json/%s?limit=1" % ch)
    print(ch, "json", st, "" if st == 200 else " ".join(body.decode("utf-8", "replace").split())[:160])
    time.sleep(5)
    ids, before = [], ""
    for page in range(4):
        st, _, body, _ = get("https://t.me/s/%s%s" % (ch, before))
        html = body.decode("utf-8", "replace")
        seen = []
        for b in html.split('tgme_widget_message_wrap')[1:]:
            m = re.search(r'data-post="[^"/]+/(\d+)"', b)
            if not m: continue
            seen.append(int(m.group(1)))
            if "Media is too big" in b: ids.append(int(m.group(1)))
        if not seen: break
        before = "?before=%d" % min(seen); time.sleep(2)
    ids = sorted(set(ids), reverse=True)
    print(ch, "too-big posts:", ids)
    for mid in ids[:2]:
        st, hd, body, dt = get(BR + "/media/%s/%d" % (ch, mid), {"Range": "bytes=0-4194303"}, timeout=120, limit=4194304)
        print("   media", mid, "status", st, "got", len(body), "bytes in %.1fs" % dt, hd.get("Content-Range") or hd.get("content-range"), body[4:12] if st in (200, 206) else body[:160])
        time.sleep(6)
