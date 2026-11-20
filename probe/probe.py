# Where does the bridge stand on each followed channel right now?
import json, re, time, urllib.request, urllib.error
UA = {"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"}
BR = "https://tg.i-c-a.su"
def get(url, timeout=60, limit=None):
    try:
        r = urllib.request.urlopen(urllib.request.Request(url, headers=UA), timeout=timeout)
        return r.status, (r.read(limit) if limit else r.read())
    except urllib.error.HTTPError as e:
        return e.code, e.read()[:300]
    except Exception as e:
        return 0, repr(e).encode()

print("time", time.strftime("%H:%M:%S UTC", time.gmtime()))
target_id = None
for ch in ["hakolhayehudi", "SamariaUpdates", "nilchamim", "hatzhalhyosh", "elisha_yered", "realelchangr", "ayeletlash"]:
    st, body = get(BR + "/json/%s?limit=100" % ch)
    line = "%-16s %s" % (ch, st)
    if st == 200:
        d = json.loads(body)
        chats = d.get("chats") or []
        line += " messages=%d chats=%s" % (len(d.get("messages") or []), [(c.get("username"), c.get("id")) for c in chats][:12])
        for c in chats:
            names = [c.get("username")] + [u.get("username") for u in (c.get("usernames") or [])]
            if any((n or "").lower() == "hakolhayehudi" for n in names):
                target_id = c.get("id"); line += "  <-- hakolhayehudi seen here, id %s min=%s" % (target_id, c.get("min"))
    else:
        line += " " + " ".join(body.decode("utf-8", "replace").split())[:160]
    print(line)
    time.sleep(5)

print("\nnumeric id of hakolhayehudi from forwards:", target_id)
if target_id:
    for peer in ("-100%s" % target_id, str(target_id)):
        st, body = get(BR + "/json/%s?limit=3" % peer)
        print("json by id", peer, st, " ".join(body.decode("utf-8", "replace").split())[:200]); time.sleep(5)
    st, body = get(BR + "/json/hakolhayehudi?limit=3")
    print("json by name again", st, " ".join(body.decode("utf-8", "replace").split())[:200]); time.sleep(5)
    st, body = get(BR + "/media/-100%s/7296" % target_id, limit=200000)
    print("media by id", st, len(body), body[:12] if st != 200 else body[4:12])
