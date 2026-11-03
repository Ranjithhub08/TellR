(function () {
  "use strict";

  var $ = function (id) {
    return document.getElementById(id);
  };

  var tz = (function () {
    try {
      return Intl.DateTimeFormat().resolvedOptions().timeZone || "Asia/Kolkata";
    } catch (e) {
      return "Asia/Kolkata";
    }
  })();

  var Recognition = window.SpeechRecognition || window.webkitSpeechRecognition || null;
  var synth = "speechSynthesis" in window ? window.speechSynthesis : null;

  var state = {
    phase: "idle",
    turn: 0,
    pending: null,
    pendingTimer: null,
    period: "today",
    expenses: [],
    reminders: [],
    budgets: [],
    search: "",
    seenExpenses: null,
    seenReminders: null,
    categories: [],
    aiEnabled: false,
    ttsOn: false,
    ttsAvail: false,
    indicOn: false,
    indicFails: 0,
    sttOn: false,
    replyLang: "",
    lastLang: "",
    heardTag: "",
    ttsFails: 0,
    ttsWarned: false,
    serverUp: false,
    editId: 0,
    deleteTarget: null,
    alerted: {},
    seq: { summary: 0, expenses: 0, reminders: 0, budgets: 0, trend: 0 }
  };

  var rec = null;
  var recSession = 0;
  var speakSeq = 0;
  var speakCancel = null;
  var audioEl = null;
  var voice = null;

  var els = {
    orb: $("orb"),
    orbLabel: $("orb-label"),
    heard: $("heard"),
    log: $("log"),
    typeForm: $("form-type"),
    typeInput: $("type-input"),
    lang: $("lang"),
    keep: $("keep"),
    mute: $("mute"),
    voice: $("voice"),
    confirmBar: $("confirm-bar"),
    confirmText: $("confirm-text"),
    btnYes: $("btn-yes"),
    btnNo: $("btn-no"),
    banner: $("banner"),
    bannerText: $("banner-text"),
    pillApi: $("pill-api"),
    pillAi: $("pill-ai")
  };

  function h(tag, attrs) {
    var node = document.createElement(tag);
    if (attrs) {
      Object.keys(attrs).forEach(function (k) {
        var v = attrs[k];
        if (v === false || v === null || v === undefined) return;
        if (k === "class") node.className = v;
        else if (k === "text") node.textContent = v;
        else if (k.indexOf("on") === 0) node.addEventListener(k.slice(2), v);
        else node.setAttribute(k, v === true ? "" : String(v));
      });
    }
    for (var i = 2; i < arguments.length; i++) {
      var kid = arguments[i];
      if (kid === null || kid === undefined || kid === false) continue;
      if (Array.isArray(kid)) kid.forEach(function (x) { if (x) node.append(x); });
      else node.append(kid);
    }
    return node;
  }

  function store(key, value) {
    try {
      if (value === undefined) return window.localStorage.getItem("tellr." + key);
      window.localStorage.setItem("tellr." + key, value);
    } catch (e) {
      return null;
    }
    return null;
  }

  function pad(n) {
    return n < 10 ? "0" + n : String(n);
  }

  function nowParts() {
    var f = new Intl.DateTimeFormat("en-CA", {
      timeZone: tz,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23"
    });
    var p = {};
    f.formatToParts(new Date()).forEach(function (x) {
      p[x.type] = x.value;
    });
    return { date: p.year + "-" + p.month + "-" + p.day, time: p.hour + ":" + p.minute };
  }

  function addDays(dateStr, n) {
    var a = dateStr.split("-").map(Number);
    var t = new Date(Date.UTC(a[0], a[1] - 1, a[2] + n));
    return t.getUTCFullYear() + "-" + pad(t.getUTCMonth() + 1) + "-" + pad(t.getUTCDate());
  }

  function dateLabel(s) {
    if (!s) return "No date";
    var now = nowParts().date;
    if (s === now) return "Today";
    if (s === addDays(now, -1)) return "Yesterday";
    if (s === addDays(now, 1)) return "Tomorrow";
    var a = s.split("-").map(Number);
    var opts = { day: "numeric", month: "short", timeZone: "UTC" };
    if (String(a[0]) !== now.slice(0, 4)) opts.year = "numeric";
    return new Date(Date.UTC(a[0], a[1] - 1, a[2])).toLocaleDateString("en-IN", opts);
  }

  function timeLabel(s) {
    if (!s) return "";
    var a = s.split(":").map(Number);
    var suffix = a[0] >= 12 ? "pm" : "am";
    var hr = a[0] % 12 === 0 ? 12 : a[0] % 12;
    return hr + ":" + pad(a[1]) + " " + suffix;
  }

  function money(n) {
    var v = Number(n) || 0;
    var whole = Math.abs(v - Math.round(v)) < 0.005;
    return new Intl.NumberFormat("en-IN", {
      style: "currency",
      currency: "INR",
      minimumFractionDigits: whole ? 0 : 2,
      maximumFractionDigits: 2
    }).format(v);
  }

  function title(s) {
    s = String(s || "");
    return s.charAt(0).toUpperCase() + s.slice(1);
  }

  function ApiError(message, status) {
    this.name = "ApiError";
    this.message = message;
    this.status = status || 0;
  }
  ApiError.prototype = Object.create(Error.prototype);

  function api(path, opts) {
    opts = opts || {};
    var headers = { "X-Timezone": tz };
    var init = { method: opts.method || "GET", headers: headers };
    if (opts.body !== undefined) {
      headers["Content-Type"] = "application/json";
      init.body = JSON.stringify(opts.body);
    }
    var ctl = new AbortController();
    var limit = setTimeout(function () {
      ctl.abort();
    }, opts.timeout || 20000);
    init.signal = ctl.signal;
    return fetch(path, init)
      .catch(function (err) {
        if (err && err.name === "AbortError") throw new ApiError("That took too long. Please try again.", 0);
        throw new ApiError("Can't reach the server. Check your connection.", 0);
      })
      .then(function (res) {
        if (res.status === 204) return null;
        return res
          .json()
          .catch(function () {
            return null;
          })
          .then(function (data) {
            if (!res.ok) {
              var msg = data && data.error ? data.error : "Something went wrong (" + res.status + ").";
              throw new ApiError(msg, res.status);
            }
            return data;
          });
      })
      .finally(function () {
        clearTimeout(limit);
      });
  }

  function toast(message, kind) {
    var t = h("div", { class: "toast" + (kind ? " " + kind : ""), text: message, role: "status" });
    $("toasts").append(t);
    setTimeout(function () {
      t.remove();
    }, kind === "alarm" ? 9000 : 4000);
  }

  function showBanner(message, bad) {
    els.bannerText.textContent = message;
    els.banner.className = "banner" + (bad ? " bad" : "");
    els.banner.hidden = false;
  }

  function hideBanner() {
    els.banner.hidden = true;
  }

  var logItems = [];

  function saveLog() {
    try {
      store("history", JSON.stringify(logItems));
    } catch (e) {
      return;
    }
  }

  function addLog(kind, text, skipSave) {
    els.log.append(h("li", { class: kind, text: text }));
    while (els.log.children.length > 60) els.log.firstChild.remove();
    els.log.scrollTop = els.log.scrollHeight;
    if (skipSave) return;
    logItems.push({ kind: kind, text: String(text).slice(0, 400), t: Date.now() });
    if (logItems.length > 60) logItems.shift();
    saveLog();
  }

  function restoreLog() {
    var raw = store("history");
    if (!raw) return;
    try {
      var list = JSON.parse(raw);
      if (!Array.isArray(list)) return;
      list.slice(-60).forEach(function (it) {
        if (it && typeof it.text === "string" && ["you", "bot", "err"].indexOf(it.kind) >= 0) {
          logItems.push({ kind: it.kind, text: it.text, t: Number(it.t) || 0 });
          addLog(it.kind, it.text, true);
        }
      });
    } catch (e) {
      logItems = [];
    }
  }

  function openHistory() {
    var ul = $("hist-list");
    ul.textContent = "";
    logItems.forEach(function (it) {
      var when = it.t ? new Date(it.t).toLocaleString("en-IN", { day: "numeric", month: "short", hour: "numeric", minute: "2-digit" }) : "";
      ul.append(h("li", { class: it.kind }, h("span", { text: it.text }), when ? h("time", { text: when }) : null));
    });
    $("hist-empty").hidden = logItems.length > 0;
    $("dlg-history").showModal();
    ul.scrollTop = ul.scrollHeight;
  }

  function recentTurns() {
    var turns = [];
    logItems.forEach(function (it) {
      if (it.kind === "you") turns.push({ role: "user", text: it.text });
      else if (it.kind === "bot") turns.push({ role: "assistant", text: it.text });
    });
    return turns.slice(-6);
  }

  var labels = {
    idle: "Tap and speak",
    listening: "Listening… tap to stop",
    thinking: "Thinking…",
    speaking: "Speaking… tap to interrupt",
    off: "Voice input unavailable"
  };

  function setPhase(p) {
    state.phase = p;
    els.orb.setAttribute("data-phase", p);
    els.orbLabel.textContent = labels[p];
    els.orb.setAttribute("aria-label", labels[p]);
    var busy = p === "thinking";
    $("btn-send").disabled = busy;
    els.btnYes.disabled = busy;
    els.btnNo.disabled = busy;
    document.querySelectorAll(".chip").forEach(function (c) {
      c.disabled = busy;
    });
  }

  function setHeard(text, live) {
    els.heard.textContent = text;
    els.heard.className = "heard" + (live ? " live" : "");
  }

  function pickVoice() {
    if (!synth) return;
    var list = synth.getVoices();
    if (!list.length) return;
    var score = function (v) {
      var l = (v.lang || "").toLowerCase().replace("_", "-");
      var n = v.name || "";
      var pts = 0;
      if (l.indexOf("en") !== 0) return -1;
      if (l === "en-in") pts += 3;
      else if (l === "en-gb" || l === "en-us") pts += 2;
      else pts += 1;
      if (/natural|neural|online/i.test(n)) pts += 6;
      if (/google/i.test(n)) pts += 4;
      if (/samantha|serena|karen|moira|veena|aria|jenny|libby|sonia/i.test(n)) pts += 3;
      if (/compact|novelty|bad news|bahh|bells|boing|cellos|good news|jester|organ|superstar|trinoids|whisper|zarvox|albert|fred|junior|kathy|ralph|bubbles/i.test(n)) pts -= 8;
      return pts;
    };
    var best = null;
    var bestScore = -1;
    list.forEach(function (v) {
      var sc = score(v);
      if (sc > bestScore) {
        best = v;
        bestScore = sc;
      }
    });
    voice = best;
  }

  function cancelSpeech() {
    speakSeq++;
    var f = speakCancel;
    speakCancel = null;
    if (audioEl) {
      try {
        audioEl.pause();
      } catch (e) {
        audioEl = null;
      }
      audioEl = null;
    }
    if (synth) {
      try {
        synth.cancel();
      } catch (e) {
        f = f || null;
      }
    }
    if (f) f();
  }

  function chunkText(text, max) {
    var parts = text.match(/[\s\S]+?(?:[.!?]+(?=\s|$)|$)\s*/g) || [text];
    var out = [];
    var cur = "";
    parts.forEach(function (p) {
      p = p.trim();
      if (!p) return;
      while (p.length > max) {
        var cut = p.lastIndexOf(" ", max);
        if (cut < 20) cut = max;
        if (cur) {
          out.push(cur);
          cur = "";
        }
        out.push(p.slice(0, cut).trim());
        p = p.slice(cut).trim();
      }
      if (!p) return;
      if (cur && (cur + " " + p).length > max) {
        out.push(cur);
        cur = p;
      } else {
        cur = cur ? cur + " " + p : p;
      }
    });
    if (cur) out.push(cur);
    return out.slice(0, 4);
  }

  function fetchAudio(text, lang) {
    var ctl = new AbortController();
    var timer = setTimeout(function () {
      ctl.abort();
    }, 9000);
    return fetch("/api/speak", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Timezone": tz },
      body: JSON.stringify({ text: text, voice: els.voice.value, lang: lang || "" }),
      signal: ctl.signal
    })
      .then(function (res) {
        if (!res.ok) {
          return res
            .json()
            .catch(function () {
              return null;
            })
            .then(function (d) {
              throw new ApiError(d && d.error ? d.error : "The voice service failed.", res.status);
            });
        }
        return res.blob();
      })
      .then(function (blob) {
        return URL.createObjectURL(blob);
      })
      .finally(function () {
        clearTimeout(timer);
      });
  }

  function playUrl(url) {
    return new Promise(function (resolve, reject) {
      var a = new Audio(url);
      audioEl = a;
      a.onended = function () {
        URL.revokeObjectURL(url);
        if (audioEl === a) audioEl = null;
        resolve();
      };
      a.onerror = function () {
        URL.revokeObjectURL(url);
        reject(new Error("audio"));
      };
      var p = a.play();
      if (p && p.catch) p.catch(reject);
    });
  }

  function noteIndicFailure(err) {
    state.indicFails++;
    if (state.indicFails >= 2) state.indicOn = false;
    if (!state.ttsWarned) {
      state.ttsWarned = true;
      toast("Using this device's voice for Hindi and Telugu. " + (err && err.message ? err.message : ""), "bad");
    }
  }

  function noteTtsFailure(err) {
    state.ttsFails++;
    if (!state.ttsWarned) {
      state.ttsWarned = true;
      toast("Using this device's voice. " + (err && err.message ? err.message : ""), "bad");
    }
    if (state.ttsFails >= 2) state.ttsOn = false;
  }

  var warnedLang = {};

  function voiceForLang(lang) {
    if (!synth) return null;
    var prefix = lang.slice(0, 2).toLowerCase();
    var list = synth.getVoices().filter(function (v) {
      return (v.lang || "").toLowerCase().replace("_", "-").indexOf(prefix) === 0;
    });
    if (!list.length) return null;
    var best = list[0];
    list.forEach(function (v) {
      if (/google|enhanced|premium|natural/i.test(v.name) && !/google|enhanced|premium|natural/i.test(best.name)) best = v;
    });
    return best;
  }

  function speakDevice(text, my, lang, single) {
    if (lang && !single) {
      var parts = chunkText(text, 110);
      if (parts.length > 1) {
        return parts.reduce(function (p, part) {
          return p.then(function () {
            return my === speakSeq ? speakDevice(part, my, lang, true) : null;
          });
        }, Promise.resolve());
      }
    }
    return new Promise(function (resolve) {
      if (!synth || my !== speakSeq) {
        resolve();
        return;
      }
      var chosen = voice;
      var spokenLang = voice && voice.lang ? voice.lang : "en-IN";
      if (lang) {
        chosen = voiceForLang(lang);
        spokenLang = lang;
        if (!chosen && synth.getVoices().length && !warnedLang[lang]) {
          warnedLang[lang] = true;
          toast("No " + (lang.indexOf("te") === 0 ? "Telugu" : "Hindi") + " voice is installed on this device, so the reply is text only. Add one in System Settings, Accessibility, Spoken Content.", "bad");
        }
        if (!chosen && synth.getVoices().length) {
          resolve();
          return;
        }
      }
      var timer = null;
      var done = false;
      var finish = function () {
        if (done) return;
        done = true;
        clearTimeout(timer);
        resolve();
      };
      try {
        var u = new SpeechSynthesisUtterance(text);
        u.lang = spokenLang;
        if (chosen) u.voice = chosen;
        u.rate = lang ? 0.9 : 0.98;
        u.pitch = lang ? 1 : 1.04;
        u.onend = finish;
        u.onerror = finish;
        timer = setTimeout(finish, Math.min(30000, 4000 + text.length * 110));
        synth.speak(u);
      } catch (e) {
        finish();
      }
    });
  }

  function speakNatural(text, my, lang) {
    var chunks = chunkText(text, lang ? 160 : 190);
    var fetches = chunks.map(function (c) {
      return fetchAudio(c, lang);
    });
    var fail = lang ? noteIndicFailure : noteTtsFailure;
    var step = function (i) {
      if (my !== speakSeq || i >= chunks.length) return Promise.resolve();
      return fetches[i].then(
        function (url) {
          if (my !== speakSeq) {
            URL.revokeObjectURL(url);
            return;
          }
          if (lang) state.indicFails = 0;
          else state.ttsFails = 0;
          return playUrl(url).then(
            function () {
              return step(i + 1);
            },
            function (err) {
              fail(err);
              return speakDevice(chunks.slice(i).join(" "), my, lang || undefined);
            }
          );
        },
        function (err) {
          if (my !== speakSeq) return;
          fail(err);
          return speakDevice(chunks.slice(i).join(" "), my, lang || undefined);
        }
      );
    };
    fetches.forEach(function (f) {
      f.catch(function () {
        return null;
      });
    });
    return step(0);
  }

  function speak(text, lang) {
    return new Promise(function (resolve) {
      if (!text || els.mute.checked) {
        resolve();
        return;
      }
      cancelSpeech();
      var my = speakSeq;
      var finished = false;
      var finish = function () {
        if (finished) return;
        finished = true;
        if (speakCancel === finish) speakCancel = null;
        resolve();
      };
      speakCancel = finish;
      var foreign = lang && lang.toLowerCase().indexOf("en") !== 0 ? lang : "";
      var indic = foreign && state.indicOn && els.voice.value !== "device";
      var natural = !foreign && els.voice.value !== "device" && state.ttsOn;
      var run = indic ? speakNatural(text, my, foreign) : natural ? speakNatural(text, my) : speakDevice(text, my, foreign);
      run.then(finish, finish);
    });
  }

  var wrec = null;

  function canRecord() {
    return !!(window.MediaRecorder && navigator.mediaDevices && navigator.mediaDevices.getUserMedia);
  }

  function useWhisper() {
    return els.lang.value === "auto" && state.sttOn && canRecord();
  }

  function recogLang() {
    return els.lang.value === "auto" ? "en-IN" : els.lang.value;
  }

  function pickMime() {
    var list = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4", "audio/ogg;codecs=opus"];
    for (var i = 0; i < list.length; i++) {
      try {
        if (window.MediaRecorder.isTypeSupported(list[i])) return list[i];
      } catch (e) {
        return "";
      }
    }
    return "";
  }

  function uploadSpeech(blob, session) {
    setPhase("thinking");
    setHeard("Understanding…", true);
    var ctl = new AbortController();
    var limit = setTimeout(function () {
      ctl.abort();
    }, 20000);
    return fetch("/api/transcribe", {
      method: "POST",
      headers: { "Content-Type": blob.type || "audio/webm", "X-Timezone": tz, "X-Lang-Hint": state.lastLang || "" },
      body: blob,
      signal: ctl.signal
    })
      .then(function (res) {
        return res
          .json()
          .catch(function () {
            return null;
          })
          .then(function (data) {
            if (!res.ok) throw new ApiError(data && data.error ? data.error : "Speech recognition failed (" + res.status + ").", res.status);
            return data;
          });
      })
      .then(
        function (data) {
          if (session !== recSession) return;
          var text = data && data.text ? String(data.text).trim() : "";
          if (!text) {
            setPhase("idle");
            setHeard("I didn't catch that. Tap the mic and try again.", false);
            return;
          }
          var code = data && data.lang ? String(data.lang) : "";
          if (code === "hi" || code === "te" || code === "en") {
            state.lastLang = code;
            state.heardTag = { hi: "Hindi", te: "Telugu", en: "English" }[code];
          }
          return sendText(text, true);
        },
        function (err) {
          if (session !== recSession) return;
          setPhase("idle");
          var msg = err && err.name === "AbortError" ? "That took too long. Please try again." : err.message || "Speech recognition failed.";
          setHeard(msg, false);
          toast(msg, "bad");
        }
      )
      .finally(function () {
        clearTimeout(limit);
      });
  }

  function startRecording() {
    if (state.phase === "thinking") return;
    cancelSpeech();
    stopRecognition();
    var session = recSession;
    setHeard("Listening… speak in any language", true);
    setPhase("listening");
    navigator.mediaDevices
      .getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, channelCount: 1 } })
      .then(
        function (stream) {
          var AC = window.AudioContext || window.webkitAudioContext;
          var actx = null;
          var cleaned = false;
          var cleanup = function () {
            if (cleaned) return;
            cleaned = true;
            stream.getTracks().forEach(function (t) {
              t.stop();
            });
            if (actx) {
              try {
                actx.close();
              } catch (e) {
                return;
              }
            }
          };
          if (session !== recSession) {
            cleanup();
            return;
          }
          var mime = pickMime();
          var mr;
          try {
            mr = mime ? new MediaRecorder(stream, { mimeType: mime }) : new MediaRecorder(stream);
          } catch (e) {
            cleanup();
            setPhase("idle");
            setHeard("Couldn't start recording. Try again.", false);
            return;
          }
          var chunks = [];
          mr.ondataavailable = function (e) {
            if (e.data && e.data.size) chunks.push(e.data);
          };
          var analyser = null;
          var buf = null;
          try {
            actx = new AC();
            if (actx.state === "suspended") actx.resume();
            analyser = actx.createAnalyser();
            analyser.fftSize = 1024;
            actx.createMediaStreamSource(stream).connect(analyser);
            buf = new Float32Array(analyser.fftSize);
          } catch (e) {
            analyser = null;
          }
          var startedAt = Date.now();
          var heardAt = 0;
          var lastLoud = 0;
          var floor = 0;
          var finished = false;
          var cancelled = false;
          var wantSend = false;
          var timer = null;
          var finish = function (send) {
            if (finished) return;
            finished = true;
            wantSend = send;
            clearInterval(timer);
            wrec = null;
            try {
              if (mr.state !== "inactive") mr.stop();
              else mr.onstop();
            } catch (e) {
              cleanup();
            }
          };
          mr.onstop = function () {
            cleanup();
            if (cancelled || session !== recSession) return;
            if (!wantSend || !chunks.length) {
              setPhase("idle");
              setHeard("I didn't hear anything. Tap the mic and try again.", false);
              return;
            }
            uploadSpeech(new Blob(chunks, { type: mr.mimeType || mime || "audio/webm" }), session);
          };
          var tick = function () {
            var now = Date.now();
            var lv = 0;
            if (analyser) {
              analyser.getFloatTimeDomainData(buf);
              var sum = 0;
              for (var i = 0; i < buf.length; i++) sum += buf[i] * buf[i];
              lv = Math.sqrt(sum / buf.length);
            }
            if (now - startedAt < 350) {
              floor = Math.max(floor, lv);
              return;
            }
            if (lv > Math.max(0.012, floor * 2)) {
              if (!heardAt) heardAt = now;
              lastLoud = now;
            }
            if (heardAt && now - lastLoud > 2200) finish(true);
            else if (!heardAt && now - startedAt > 7000) finish(false);
            else if (now - startedAt > 25000) finish(!!heardAt);
          };
          wrec = {
            finish: function () {
              finish(true);
            },
            cancel: function () {
              cancelled = true;
              finish(false);
            }
          };
          mr.start(250);
          timer = setInterval(tick, 60);
        },
        function (err) {
          if (session !== recSession) return;
          setPhase("idle");
          var code = err && err.name;
          setHeard(code === "NotAllowedError" ? "Microphone is blocked. Allow it in the address bar, or type below." : "No microphone found. You can type below.", false);
          toast("Microphone permission is needed for voice.", "bad");
        }
      );
  }

  function stopRecognition() {
    if (wrec) {
      var w = wrec;
      wrec = null;
      w.cancel();
    }
    recSession++;
    if (rec) {
      var r = rec;
      rec = null;
      r.onresult = null;
      r.onerror = null;
      r.onend = null;
      try {
        r.abort();
      } catch (e) {
        return;
      }
    }
  }

  function startListening() {
    if (useWhisper()) {
      startRecording();
      return;
    }
    if (!Recognition) {
      setPhase("off");
      return;
    }
    if (state.phase === "thinking") return;
    cancelSpeech();
    stopRecognition();
    var session = recSession;
    var finalText = "";
    var interimText = "";
    var failed = false;
    var r = new Recognition();
    r.lang = recogLang();
    r.interimResults = true;
    r.continuous = true;
    r.maxAlternatives = 1;
    var quietTimer = null;
    var capTimer = setTimeout(function () {
      try {
        r.stop();
      } catch (e) {
        return;
      }
    }, 25000);

    r.onresult = function (e) {
      if (session !== recSession) return;
      var interim = "";
      for (var i = e.resultIndex; i < e.results.length; i++) {
        var piece = e.results[i][0].transcript;
        if (e.results[i].isFinal) finalText += piece;
        else interim += piece;
      }
      interimText = interim;
      setHeard((finalText + interim).trim() || "…", true);
      clearTimeout(quietTimer);
      quietTimer = setTimeout(function () {
        try {
          r.stop();
        } catch (e) {
          return;
        }
      }, 2000);
    };

    r.onerror = function (e) {
      if (session !== recSession) return;
      failed = true;
      var code = e && e.error;
      if (code === "aborted") return;
      if (code === "no-speech") {
        setHeard("I didn't hear anything. Tap the mic and try again.", false);
      } else if (code === "not-allowed" || code === "service-not-allowed") {
        setHeard("Microphone is blocked. Allow it in the address bar, or type below.", false);
        toast("Microphone permission is needed for voice.", "bad");
      } else if (code === "audio-capture") {
        setHeard("No microphone found. You can type below.", false);
      } else if (code === "network") {
        setHeard("Speech recognition needs an internet connection.", false);
      } else {
        setHeard("Couldn't hear that (" + code + "). Try again.", false);
      }
    };

    r.onend = function () {
      clearTimeout(quietTimer);
      clearTimeout(capTimer);
      if (session !== recSession) return;
      rec = null;
      var text = (finalText || interimText).trim();
      if (failed || !text) {
        if (state.phase === "listening") setPhase("idle");
        if (!failed && !text) setHeard("I didn't catch that. Tap the mic and try again.", false);
        return;
      }
      sendText(text);
    };

    rec = r;
    setHeard("Listening…", true);
    setPhase("listening");
    try {
      r.start();
    } catch (e) {
      rec = null;
      setPhase("idle");
      setHeard("Couldn't start the microphone. Try again.", false);
    }
  }

  function clearPending() {
    state.pending = null;
    clearTimeout(state.pendingTimer);
    state.pendingTimer = null;
    els.confirmBar.hidden = true;
  }

  function setPending(info) {
    clearPending();
    if (!info || !info.token) return;
    state.pending = info.token;
    els.confirmText.textContent = info.prompt || "Delete this? Say yes or no.";
    els.confirmBar.hidden = false;
    var ms = info.expires_at ? new Date(info.expires_at).getTime() - Date.now() : 120000;
    if (!isFinite(ms) || ms < 1000) ms = 120000;
    var token = info.token;
    state.pendingTimer = setTimeout(function () {
      if (state.pending === token) {
        clearPending();
        toast("That confirmation expired.");
      }
    }, ms);
  }

  function sendText(raw, force) {
    var text = String(raw || "").trim();
    if (!text || (state.phase === "thinking" && !force)) return;
    stopRecognition();
    cancelSpeech();
    var turn = ++state.turn;
    var history = recentTurns();
    addLog("you", text);
    setHeard(state.heardTag ? state.heardTag + ": " + text : text, false);
    state.heardTag = "";
    setPhase("thinking");
    var body = { text: text, lang: els.lang.value, tz: tz, history: history };
    if (state.pending) body.confirm_token = state.pending;
    return api("/api/voice", { method: "POST", body: body, timeout: 25000 }).then(
      function (data) {
        if (turn !== state.turn) return;
        return deliver(data, turn);
      },
      function (err) {
        if (turn !== state.turn) return;
        clearPending();
        addLog("err", err.message);
        setHeard(err.message, false);
        setPhase("idle");
        if (err.status === 0) probeServer();
      }
    );
  }

  function deliver(data, turn) {
    var reply = data && data.reply ? data.reply : "Done.";
    addLog(data && data.ok === false ? "err" : "bot", reply);
    setHeard(reply, false);
    if (data && data.confirm) setPending(data.confirm);
    else clearPending();
    state.replyLang = data && data.reply_lang ? data.reply_lang : "";
    if (data && data.view) renderResult(data.view);
    if (data && data.changed) refreshAll(true);
    setPhase("speaking");
    return speak(reply, data && data.reply_lang).then(function () {
      if (turn !== state.turn || state.phase !== "speaking") return;
      setPhase("idle");
      if ((Recognition || useWhisper()) && (state.pending || els.keep.checked) && document.visibilityState === "visible") {
        setTimeout(function () {
          if (turn === state.turn && state.phase === "idle") startListening();
        }, 450);
      }
    });
  }

  function renderResult(view) {
    var body = $("result-body");
    body.textContent = "";
    $("result-title").textContent = view.title || "Result";
    if (view.kind === "expenses") {
      var list = view.expenses || [];
      if (!list.length) body.append(h("p", { class: "muted", text: "Nothing to show." }));
      else body.append(h("ul", { class: "list" }, list.map(function (e) { return expenseRow(e, false); })));
    } else if (view.kind === "reminders") {
      var rl = view.reminders || [];
      if (!rl.length) body.append(h("p", { class: "muted", text: "Nothing to show." }));
      else body.append(h("ul", { class: "list" }, rl.map(function (r) { return reminderRow(r, false); })));
    } else if (view.kind === "budgets") {
      var bl = view.budgets || [];
      if (!bl.length) body.append(h("p", { class: "muted", text: "No budgets yet." }));
      else body.append(h("ul", { class: "budget-list" }, bl.map(function (b) { return budgetRow(b, false); })));
    } else if (view.kind === "summary" && view.summary) {
      var s = view.summary;
      body.append(
        h("p", { class: "result-total", text: money(s.total) }),
        h("p", { class: "sum-meta", text: s.count + " expenses, " + s.open_reminders + " reminders open" }),
        compareNode(s, s.period),
        barList(s)
      );
    } else {
      return;
    }
    $("result").hidden = false;
  }

  function compareText(s, period) {
    if (!s.has_previous || !(s.previous > 0) || !(s.count > 0)) return null;
    var diff = Math.round(((s.total - s.previous) / s.previous) * 100);
    var base = period === "week" ? "the 7 days before" : period === "month" ? "the same days last month" : period.indexOf("week") === 0 ? "the week before" : "yesterday";
    if (diff === 0) return { cls: "", text: "Same as " + base };
    return diff > 0
      ? { cls: "up", text: "Up " + diff + "% from " + base }
      : { cls: "down", text: "Down " + Math.abs(diff) + "% from " + base };
  }

  function compareNode(s, period) {
    var c = compareText(s, period);
    if (!c) return null;
    return h("p", { class: "compare " + c.cls, text: c.text });
  }

  function barList(s) {
    var rows = (s.by_category || []).slice().sort(function (a, b) {
      return b.total - a.total;
    });
    var total = s.total > 0 ? s.total : 1;
    return h("ul", { class: "bars" }, rows.map(function (c) {
      var pct = Math.max(2, Math.min(100, (c.total / total) * 100));
      var fill = h("span", { class: "fill" });
      fill.style.width = pct.toFixed(1) + "%";
      return h("li", { class: "cat-" + c.category },
        h("span", { class: "dot" }),
        h("span", { text: title(c.category) }),
        h("span", { class: "amt", text: money(c.total) }),
        h("span", { class: "track" }, fill)
      );
    }));
  }

  var SVGNS = "http://www.w3.org/2000/svg";

  function drawDonut(s) {
    var g = $("donut-segs");
    g.textContent = "";
    var rows = (s.by_category || []).filter(function (c) {
      return c.total > 0;
    });
    if (!rows.length || !(s.total > 0)) return;
    var C = 2 * Math.PI * 46;
    var gap = rows.length > 1 ? 2.5 : 0;
    var offset = 0;
    rows.forEach(function (c) {
      var len = (c.total / s.total) * C;
      var seg = document.createElementNS(SVGNS, "circle");
      seg.setAttribute("class", "seg cat-" + c.category);
      seg.setAttribute("cx", "60");
      seg.setAttribute("cy", "60");
      seg.setAttribute("r", "46");
      seg.setAttribute("stroke-dasharray", Math.max(0.5, len - gap).toFixed(2) + " " + (C - Math.max(0.5, len - gap)).toFixed(2));
      seg.setAttribute("stroke-dashoffset", (-offset).toFixed(2));
      g.append(seg);
      offset += len;
    });
  }

  function budgetRow(b, actions) {
    var pct = Math.max(0, Number(b.percent) || 0);
    var cls = pct >= 100 ? "over" : pct >= 80 ? "warn" : "";
    var fill = h("span", { class: "budget-fill" });
    fill.style.width = Math.min(100, pct) + "%";
    var left = b.limit - b.spent;
    var sub = left >= 0 ? money(left) + " left" : money(-left) + " over";
    return h("li", { class: "budget " + cls },
      h("div", { class: "budget-top" },
        h("span", { class: "budget-name", text: b.category === "all" ? "Everything" : title(b.category) }),
        h("span", { class: "budget-pct", text: pct + "%" })
      ),
      h("span", { class: "budget-track", role: "progressbar", "aria-valuemin": "0", "aria-valuemax": "100", "aria-valuenow": String(Math.min(100, pct)), "aria-label": (b.category === "all" ? "Everything" : title(b.category)) + " budget used" }, fill),
      h("div", { class: "budget-sub" },
        h("span", { text: money(b.spent) + " of " + money(b.limit) + " · " + sub }),
        actions ? h("button", { class: "btn small danger", type: "button", text: "Remove", "aria-label": "Remove " + (b.category === "all" ? "overall" : b.category) + " budget", onclick: function () { removeBudget(b); } }) : null
      )
    );
  }

  function removeBudget(b) {
    api("/api/budgets/" + encodeURIComponent(b.category), { method: "DELETE" }).then(
      function () {
        toast("Budget removed.");
        return refreshAll();
      },
      function (err) {
        toast(err.message, "bad");
        refreshAll();
      }
    );
  }

  function loadBudgets() {
    var my = ++state.seq.budgets;
    return api("/api/budgets").then(function (d) {
      if (my !== state.seq.budgets) return;
      var list = d.budgets || [];
      var ul = $("budget-list");
      ul.textContent = "";
      list.forEach(function (b) {
        ul.append(budgetRow(b, true));
      });
      $("bg-empty").hidden = list.length > 0;
      state.budgets = list;
    });
  }

  function shortDay(dateStr) {
    var a = dateStr.split("-").map(Number);
    return new Date(Date.UTC(a[0], a[1] - 1, a[2])).toLocaleDateString("en-IN", { weekday: "narrow", timeZone: "UTC" });
  }

  function svgEl(name, attrs) {
    var n = document.createElementNS(SVGNS, name);
    Object.keys(attrs).forEach(function (k) {
      n.setAttribute(k, String(attrs[k]));
    });
    return n;
  }

  function drawTrend(days) {
    var svg = $("trend");
    svg.textContent = "";
    if (!days.length) return;
    var max = 0;
    days.forEach(function (d) {
      if (d.total > max) max = d.total;
    });
    var W = 308, top = 16, base = 92, slot = W / days.length, bw = slot - 6;
    var peakIdx = -1;
    days.forEach(function (d, i) {
      if (d.total === max && max > 0 && peakIdx < 0) peakIdx = i;
    });
    days.forEach(function (d, i) {
      var hgt = max > 0 && d.total > 0 ? Math.max(3, (d.total / max) * (base - top)) : 3;
      var x = i * slot + 3;
      var bar = svgEl("rect", {
        "class": "bar" + (d.total > 0 ? " has" : "") + (i === days.length - 1 ? " today" : ""),
        x: x.toFixed(1),
        y: (base - hgt).toFixed(1),
        width: bw.toFixed(1),
        height: hgt.toFixed(1),
        rx: 3
      });
      var tip = svgEl("title", {});
      tip.textContent = dateLabel(d.date) + ": " + money(d.total);
      bar.append(tip);
      svg.append(bar);
      var tick = svgEl("text", { "class": "tick", x: (x + bw / 2).toFixed(1), y: 106 });
      tick.textContent = shortDay(d.date);
      svg.append(tick);
      if (i === peakIdx) {
        var pk = svgEl("text", { "class": "peak", x: (x + bw / 2).toFixed(1), y: (base - hgt - 4).toFixed(1) });
        pk.textContent = money(d.total);
        svg.append(pk);
      }
    });
  }

  function loadTrend() {
    var my = ++state.seq.trend;
    return api("/api/daily?days=14").then(function (d) {
      if (my !== state.seq.trend) return;
      drawTrend(d.days || []);
    });
  }

  function expenseRow(e, actions) {
    var flash = state.seenExpenses && !state.seenExpenses[e.id] && actions;
    if (actions) {
      return h("li", { class: "item trow cat-" + e.category + (flash ? " flash" : "") },
        h("span", { class: "tdate", text: dateLabel(e.spent_on) }),
        h("div", { class: "item-main" },
          h("div", { class: "item-title", text: e.note || title(e.category) }),
          h("div", { class: "item-sub" }, h("span", { class: "dot" }), h("span", { text: title(e.category) }))
        ),
        h("span", { class: "money", text: money(e.amount) }),
        h("div", { class: "item-side" },
          h("button", { class: "btn small", type: "button", text: "Edit", "aria-label": "Edit expense " + (e.note || e.category), onclick: function () { openEdit(e); } }),
          h("button", { class: "btn small danger", type: "button", text: "Delete", "aria-label": "Delete expense " + (e.note || e.category), onclick: function () { askDelete("expense", e); } })
        )
      );
    }
    return h("li", { class: "item cat-" + e.category },
      h("span", { class: "dot" }),
      h("div", { class: "item-main" },
        h("div", { class: "item-title", text: e.note || title(e.category) }),
        h("div", { class: "item-sub" }, h("span", { text: title(e.category) }), h("span", { text: dateLabel(e.spent_on) }))
      ),
      h("div", { class: "item-side" },
        h("span", { class: "money", text: money(e.amount) }),
        actions ? h("button", { class: "btn small", type: "button", text: "Edit", "aria-label": "Edit expense " + (e.note || e.category), onclick: function () { openEdit(e); } }) : null,
        actions ? h("button", { class: "btn small danger", type: "button", text: "Delete", "aria-label": "Delete expense " + (e.note || e.category), onclick: function () { askDelete("expense", e); } }) : null
      )
    );
  }

  function reminderRow(r, actions) {
    var np = nowParts();
    var late = !r.done && (r.due_date < np.date || (r.due_date === np.date && r.due_time && r.due_time < np.time));
    var soon = !r.done && !late && (r.due_date === np.date || r.due_date === addDays(np.date, 1));
    var when = dateLabel(r.due_date) + (r.due_time ? ", " + timeLabel(r.due_time) : "");
    var flash = state.seenReminders && !state.seenReminders[r.id] && actions;
    var tone = r.done ? "ok" : late ? "late" : soon ? "soon" : "";
    return h("li", { class: "item" + (r.done ? " done" : "") + (flash ? " flash" : "") },
      h("span", { class: "dot tone-" + (tone || "up") }),
      h("div", { class: "item-main" },
        h("div", { class: "item-title", text: r.title }),
        h("div", { class: "item-sub" },
          h("span", { class: "tag" + (tone ? " " + tone : ""), text: late ? "Overdue" : r.done ? "Done" : "Upcoming" }),
          r.repeat ? h("span", { class: "tag repeat", text: "Repeats " + r.repeat }) : null,
          h("span", { text: when })
        )
      ),
      actions
        ? h("div", { class: "item-side" },
            h("button", { class: "btn small", type: "button", text: r.done ? "Undo" : "Mark done", onclick: function () { toggleReminder(r); } }),
            h("button", { class: "btn small danger", type: "button", text: "Delete", "aria-label": "Delete reminder " + r.title, onclick: function () { askDelete("reminder", r); } })
          )
        : h("div", { class: "item-side" })
    );
  }

  function idMap(list) {
    var m = {};
    list.forEach(function (x) {
      m[x.id] = true;
    });
    return m;
  }

  function rangeLabel(s) {
    function fmt(d) {
      var p = String(d).split("-");
      if (p.length !== 3) return d;
      return parseInt(p[2], 10) + " " + ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"][parseInt(p[1], 10) - 1];
    }
    if (!s.from || !s.to) return "";
    return s.from === s.to ? fmt(s.from) : fmt(s.from) + " to " + fmt(s.to);
  }

  function loadSummary() {
    var my = ++state.seq.summary;
    return api("/api/summary?period=" + encodeURIComponent(state.period)).then(function (s) {
      if (my !== state.seq.summary) return;
      $("sum-total").textContent = money(s.total);
      $("sum-range").textContent = rangeLabel(s);
      var cmp = compareText(s, state.period);
      var cmpNode = $("sum-compare");
      cmpNode.hidden = !cmp;
      cmpNode.className = "compare" + (cmp && cmp.cls ? " " + cmp.cls : "");
      cmpNode.textContent = cmp ? cmp.text : "";
      $("sum-count").textContent = String(s.count);
      $("sum-rem").textContent = String(s.open_reminders);
      var bars = $("sum-bars");
      bars.replaceWith(Object.assign(barList(s), { id: "sum-bars" }));
      drawDonut(s);
      $("sum-empty").hidden = s.count > 0;
    });
  }

  function loadExpenses() {
    var my = ++state.seq.expenses;
    return api("/api/expenses?limit=100").then(function (d) {
      if (my !== state.seq.expenses) return;
      state.expenses = d.expenses || [];
      renderExpenses();
      state.seenExpenses = idMap(state.expenses);
    });
  }

  function renderExpenses() {
    var term = state.search.trim().toLowerCase();
    var list = state.expenses.filter(function (e) {
      if (!term) return true;
      return (e.note || "").toLowerCase().indexOf(term) >= 0 || e.category.indexOf(term) >= 0 || String(e.amount).indexOf(term) >= 0;
    });
    var ul = $("expense-list");
    ul.textContent = "";
    list.forEach(function (e) {
      ul.append(expenseRow(e, true));
    });
    var empty = $("ex-empty");
    empty.hidden = list.length > 0;
    empty.textContent = state.expenses.length
      ? "No expense matches “" + state.search.trim() + "”."
      : "No expenses yet. Tap the mic and say what you spent.";
  }

  function loadReminders() {
    var my = ++state.seq.reminders;
    var path = "/api/reminders?limit=100" + ($("rm-showdone").checked ? "" : "&open=1");
    return api(path).then(function (d) {
      if (my !== state.seq.reminders) return;
      var list = d.reminders || [];
      var ul = $("reminder-list");
      ul.textContent = "";
      list.forEach(function (r) {
        ul.append(reminderRow(r, true));
      });
      $("rm-empty").hidden = list.length > 0;
      state.reminders = list;
      state.seenReminders = idMap(list);
    });
  }

  function refreshAll() {
    return Promise.all([loadSummary(), loadExpenses(), loadReminders(), loadBudgets(), loadTrend()]).then(
      function () {
        state.serverUp = true;
        hideBanner();
      },
      function (err) {
        if (err.status === 0) {
          state.serverUp = false;
          showBanner(err.message, true);
        } else {
          toast(err.message, "bad");
        }
      }
    );
  }

  function setPill(node, text, kind) {
    node.textContent = text;
    node.className = "pill pill-" + kind;
  }

  function probeServer() {
    return api("/api/health").then(
      function (h2) {
        setPill(els.pillApi, h2.db ? "Server: online" : "Server: database problem", h2.db ? "ok" : "bad");
        state.aiEnabled = !!h2.ai;
        state.ttsOn = !!h2.tts;
        state.ttsAvail = !!h2.tts;
        state.indicOn = !!h2.indic;
        state.indicFails = 0;
        state.sttOn = !!h2.stt;
        if (state.phase === "off" && canRecord() && state.sttOn) setPhase("idle");
        setPill(els.pillAi, h2.ai ? "AI: ready" : "AI: key missing", h2.ai ? "ok" : "warn");
        if (!h2.ai) showBanner("The AI key isn't set on the server, so voice commands are off. The forms below still work.", false);
        else if (h2.db) hideBanner();
        return true;
      },
      function (err) {
        setPill(els.pillApi, "Server: offline", "bad");
        setPill(els.pillAi, "AI: unknown", "wait");
        showBanner(err.message, true);
        return false;
      }
    );
  }

  function fillBudgetCategories(list) {
    var sel = $("bg-category");
    sel.textContent = "";
    sel.append(h("option", { value: "all", text: "Everything" }));
    list.forEach(function (c) {
      sel.append(h("option", { value: c, text: c === "other" ? "Other (give it a name)" : title(c) }));
    });
  }

  function toggleBudgetName() {
    var wrap = $("bg-name-wrap");
    var show = $("bg-category").value === "other";
    wrap.hidden = !show;
    if (show) $("bg-name").focus();
  }

  function fillCategories(list) {
    fillBudgetCategories(list);
    ["ex-category", "ed-category"].forEach(function (id) {
      var sel = $(id);
      sel.textContent = "";
      list.forEach(function (c) {
        sel.append(h("option", { value: c, text: title(c) }));
      });
      sel.value = list.indexOf("other") >= 0 ? "other" : list[0];
    });
  }

  function loadCategories() {
    return api("/api/categories").then(function (d) {
      state.categories = d.categories || [];
      fillCategories(state.categories);
    });
  }

  function resetDates() {
    var today = nowParts().date;
    ["ex-date", "rm-date", "ed-date"].forEach(function (id) {
      $(id).max = id === "rm-date" ? "" : today;
    });
    $("rm-date").min = today;
    if (!$("ex-date").value) $("ex-date").value = today;
    if (!$("rm-date").value) $("rm-date").value = today;
  }

  function showFormError(id, message) {
    var node = $(id);
    node.textContent = message || "";
    node.hidden = !message;
  }

  function submitExpense(ev) {
    ev.preventDefault();
    showFormError("ex-error", "");
    var amount = parseFloat($("ex-amount").value);
    if (!isFinite(amount) || amount <= 0) {
      showFormError("ex-error", "Enter an amount greater than zero.");
      $("ex-amount").focus();
      return;
    }
    if (amount > 10000000) {
      showFormError("ex-error", "That amount is too large.");
      return;
    }
    var btn = $("btn-ex-add");
    btn.disabled = true;
    api("/api/expenses", {
      method: "POST",
      body: {
        amount: amount,
        category: $("ex-category").value,
        note: $("ex-note").value.trim(),
        spent_on: $("ex-date").value
      }
    })
      .then(function () {
        $("ex-amount").value = "";
        $("ex-note").value = "";
        $("dlg-expense").close();
        toast("Expense added.");
        return refreshAll();
      })
      .catch(function (err) {
        showFormError("ex-error", err.message);
      })
      .finally(function () {
        btn.disabled = false;
      });
  }

  function importFile() {
    var f = $("file-import").files && $("file-import").files[0];
    if (!f) return;
    if (f.size > 500 * 1024) {
      toast("That file is too big. Keep it under 500 KB.", "bad");
      return;
    }
    var btn = $("btn-import");
    btn.disabled = true;
    f.text()
      .then(function (text) {
        return fetch("/api/import", { method: "POST", headers: { "Content-Type": "text/csv", "X-Timezone": tz }, body: text });
      })
      .then(function (res) {
        return res
          .json()
          .catch(function () {
            return null;
          })
          .then(function (d) {
            if (!res.ok) throw new ApiError(d && d.error ? d.error : "Import failed (" + res.status + ").", res.status);
            return d;
          });
      })
      .then(function (d) {
        var msg = "Imported " + d.imported + (d.imported === 1 ? " expense" : " expenses") + ".";
        if (d.duplicates) msg += " " + d.duplicates + " already existed.";
        if (d.skipped) {
          var first = d.problems && d.problems[0];
          msg += " Skipped " + d.skipped + (first ? " (row " + first.row + ": " + first.reason + ")" : "") + ".";
        }
        toast(msg, d.skipped ? "bad" : undefined);
        return refreshAll();
      })
      .catch(function (err) {
        toast(err.message || "Import failed.", "bad");
      })
      .finally(function () {
        btn.disabled = false;
      });
  }

  function openBudgetDialog() {
    showFormError("bg-error", "");
    $("bg-limit").value = "";
    $("bg-name").value = "";
    $("bg-name-wrap").hidden = $("bg-category").value !== "other";
    $("dlg-budget").showModal();
    $("bg-limit").focus();
  }

  function submitBudget(ev) {
    ev.preventDefault();
    showFormError("bg-error", "");
    var amount = parseFloat($("bg-limit").value);
    if (!(amount > 0)) {
      showFormError("bg-error", "Enter a monthly limit above zero.");
      $("bg-limit").focus();
      return;
    }
    var cat = $("bg-category").value;
    if (cat === "other") {
      var typed = $("bg-name").value.trim().toLowerCase().replace(/\s+/g, " ");
      if (typed) cat = typed;
    }
    var btn = $("btn-bg-save");
    btn.disabled = true;
    api("/api/budgets/" + encodeURIComponent(cat), { method: "PUT", body: { limit: amount } })
      .then(function () {
        $("dlg-budget").close();
        toast("Budget saved.");
        return loadCategories().then(refreshAll);
      })
      .catch(function (err) {
        showFormError("bg-error", err.message);
      })
      .finally(function () {
        btn.disabled = false;
      });
  }

  function submitReminder(ev) {
    ev.preventDefault();
    showFormError("rm-error", "");
    var name = $("rm-title").value.trim();
    if (!name) {
      showFormError("rm-error", "Say what the reminder is for.");
      $("rm-title").focus();
      return;
    }
    if (!$("rm-date").value) {
      showFormError("rm-error", "Pick a date.");
      return;
    }
    var btn = $("btn-rm-add");
    btn.disabled = true;
    api("/api/reminders", {
      method: "POST",
      body: { title: name, due_date: $("rm-date").value, due_time: $("rm-time").value, repeat: $("rm-repeat").value }
    })
      .then(function () {
        $("rm-title").value = "";
        $("rm-time").value = "";
        $("rm-repeat").value = "";
        $("dlg-reminder").close();
        toast("Reminder added.");
        return refreshAll();
      })
      .catch(function (err) {
        showFormError("rm-error", err.message);
      })
      .finally(function () {
        btn.disabled = false;
      });
  }

  function toggleReminder(r) {
    api("/api/reminders/" + r.id + "/done", { method: "POST", body: { done: !r.done } }).then(
      function (res) {
        if (res && res.advanced) toast("Done. Next one: " + dateLabel(res.due_date) + (res.due_time ? ", " + timeLabel(res.due_time) : ""));
        return refreshAll();
      },
      function (err) {
        toast(err.message, "bad");
        refreshAll();
      }
    );
  }

  function openEdit(e) {
    state.editId = e.id;
    $("ed-amount").value = e.amount;
    $("ed-category").value = e.category;
    $("ed-note").value = e.note || "";
    $("ed-date").value = e.spent_on;
    showFormError("ed-error", "");
    $("dlg-edit").showModal();
    $("ed-amount").focus();
  }

  function submitEdit(ev) {
    ev.preventDefault();
    showFormError("ed-error", "");
    var amount = parseFloat($("ed-amount").value);
    if (!isFinite(amount) || amount <= 0) {
      showFormError("ed-error", "Enter an amount greater than zero.");
      return;
    }
    var btn = $("btn-ed-save");
    btn.disabled = true;
    api("/api/expenses/" + state.editId, {
      method: "PUT",
      body: {
        amount: amount,
        category: $("ed-category").value,
        note: $("ed-note").value.trim(),
        spent_on: $("ed-date").value
      }
    })
      .then(function () {
        $("dlg-edit").close();
        toast("Expense updated.");
        return refreshAll();
      })
      .catch(function (err) {
        showFormError("ed-error", err.message);
        if (err.status === 404) refreshAll();
      })
      .finally(function () {
        btn.disabled = false;
      });
  }

  function askDelete(kind, item) {
    state.deleteTarget = { kind: kind, id: item.id };
    $("del-h").textContent = kind === "expense" ? "Delete this expense?" : "Delete this reminder?";
    $("del-text").textContent =
      kind === "expense"
        ? money(item.amount) + " · " + (item.note || title(item.category)) + " · " + dateLabel(item.spent_on)
        : item.title + " · " + dateLabel(item.due_date);
    showFormError("del-error", "");
    $("dlg-del").showModal();
  }

  function confirmDelete() {
    var t = state.deleteTarget;
    if (!t) return;
    var btn = $("btn-del-go");
    btn.disabled = true;
    api("/api/" + (t.kind === "expense" ? "expenses/" : "reminders/") + t.id, { method: "DELETE" })
      .then(function () {
        $("dlg-del").close();
        toast("Deleted.");
        return refreshAll();
      })
      .catch(function (err) {
        if (err.status === 404) {
          $("dlg-del").close();
          toast(err.message);
          refreshAll();
        } else {
          showFormError("del-error", err.message);
        }
      })
      .finally(function () {
        btn.disabled = false;
      });
  }

  function answerConfirm(accept) {
    var token = state.pending;
    if (!token || state.phase === "thinking") return;
    stopRecognition();
    cancelSpeech();
    var turn = ++state.turn;
    setPhase("thinking");
    api("/api/confirm", { method: "POST", body: { token: token, accept: accept, lang: state.replyLang || (els.lang.value === "auto" ? "" : els.lang.value) } }).then(
      function (data) {
        if (turn !== state.turn) return;
        clearPending();
        return deliver(data, turn);
      },
      function (err) {
        if (turn !== state.turn) return;
        clearPending();
        addLog("err", err.message);
        setHeard(err.message, false);
        setPhase("idle");
      }
    );
  }

  function checkReminderAlerts() {
    if (!state.reminders.length) return;
    var np = nowParts();
    var nowMin = parseInt(np.time.slice(0, 2), 10) * 60 + parseInt(np.time.slice(3), 10);
    state.reminders.forEach(function (r) {
      var akey = r.id + "@" + r.due_date + " " + r.due_time;
      if (r.done || !r.due_time || r.due_date !== np.date || state.alerted[akey]) return;
      var dueMin = parseInt(r.due_time.slice(0, 2), 10) * 60 + parseInt(r.due_time.slice(3), 10);
      var diff = nowMin - dueMin;
      if (diff < 0 || diff > 10) return;
      state.alerted[akey] = true;
      toast("Reminder: " + r.title, "alarm");
      addLog("bot", "Reminder: " + r.title);
      if (state.phase === "idle") {
        setPhase("speaking");
        speak("Reminder. " + r.title).then(function () {
          if (state.phase === "speaking") setPhase("idle");
        });
      }
    });
  }

  var UI = {
    en: {
      settings: "Voice settings", auto: "Auto-detect (any language)", speak: "I speak", sounds: "TellR sounds like",
      keep: "Keep listening after replies", mute: "Mute spoken replies",
      note: "TellR will reply in the language you speak. Hindi and Telugu replies use a natural Indian voice when it is set up, otherwise a voice on this device.",
      v_hannah: "Hannah (warm, female)", v_daniel: "Daniel (calm, male)", v_device: "This device’s voice",
      hello: "Hi, I'm TellR. I'll keep track of your spending and reminders."
    },
    hi: {
      settings: "आवाज़ की सेटिंग", auto: "अपने आप पहचानो (कोई भी भाषा)", speak: "मैं बोलता हूँ", sounds: "टेलर की आवाज़",
      keep: "जवाब के बाद सुनते रहो", mute: "बोले गए जवाब बंद करो",
      note: "आप जिस भाषा में बोलेंगे, टेलर उसी में जवाब देगा। हिंदी जवाब के लिए इस डिवाइस की आवाज़ इस्तेमाल होती है।",
      v_hannah: "हन्ना (मधुर, महिला)", v_daniel: "डेनियल (शांत, पुरुष)", v_device: "इस डिवाइस की आवाज़",
      hello: "नमस्ते, मैं टेलर हूँ। आपके खर्चे और रिमाइंडर मैं संभालूँगा।"
    },
    te: {
      settings: "వాయిస్ సెట్టింగ్స్", auto: "ఆటోమేటిక్ (ఏ భాషైనా)", speak: "నేను మాట్లాడేది", sounds: "టెల్లర్ వాయిస్",
      keep: "జవాబు తర్వాత వింటూ ఉండు", mute: "మాట్లాడే జవాబులు ఆపు",
      note: "మీరు మాట్లాడే భాషలోనే టెల్లర్ జవాబిస్తుంది. తెలుగు జవాబులకు ఈ పరికరంలోని వాయిస్ వాడబడుతుంది.",
      v_hannah: "హన్నా (మృదువైన, మహిళ)", v_daniel: "డేనియల్ (ప్రశాంతమైన, పురుషుడు)", v_device: "ఈ పరికరం వాయిస్",
      hello: "నమస్కారం, నేను టెల్లర్. మీ ఖర్చులు, రిమైండర్లు నేను చూసుకుంటాను."
    }
  };

  function uiLang() {
    var c = (els.lang.value || "en-IN").slice(0, 2);
    return UI[c] ? c : "en";
  }

  function applyUiLang() {
    var t = UI[uiLang()];
    document.querySelectorAll("[data-i18n]").forEach(function (n) {
      var v = t[n.getAttribute("data-i18n")];
      if (v) n.textContent = v;
    });
    document.documentElement.lang = uiLang();
  }

  function sayHello() {
    var c = uiLang();
    speak(UI[c].hello, c === "en" ? "" : c + "-IN");
  }

  function applyLangNote() {
    $("lang-note").hidden = els.lang.value === "en-IN";
    applyUiLang();
  }

  function restorePrefs() {
    var lang = store("lang2");
    if (lang && Array.prototype.some.call(els.lang.options, function (o) { return o.value === lang; })) els.lang.value = lang;
    els.keep.checked = store("keep") !== "0";
    els.mute.checked = store("mute") === "1";
    var vc = store("voice");
    if (vc && Array.prototype.some.call(els.voice.options, function (o) { return o.value === vc; })) els.voice.value = vc;
    applyLangNote();
  }

  function bind() {
    els.orb.addEventListener("click", function () {
      if (state.phase === "off") {
        toast("Voice needs Chrome or Edge. You can type instead.");
        els.typeInput.focus();
      } else if (state.phase === "idle") {
        startListening();
      } else if (state.phase === "listening") {
        if (wrec) {
          wrec.finish();
        } else if (rec) {
          try {
            rec.stop();
          } catch (e) {
            setPhase("idle");
          }
        } else {
          setPhase("idle");
        }
      } else if (state.phase === "speaking") {
        state.turn++;
        cancelSpeech();
        setPhase("idle");
        startListening();
      }
    });

    els.typeForm.addEventListener("submit", function (ev) {
      ev.preventDefault();
      var v = els.typeInput.value;
      if (!v.trim()) {
        els.typeInput.focus();
        return;
      }
      els.typeInput.value = "";
      sendText(v);
    });

    document.querySelectorAll(".chip").forEach(function (c) {
      c.addEventListener("click", function () {
        sendText(c.getAttribute("data-say"));
      });
    });

    els.btnYes.addEventListener("click", function () {
      answerConfirm(true);
    });
    els.btnNo.addEventListener("click", function () {
      answerConfirm(false);
    });

    els.lang.addEventListener("change", function () {
      store("lang2", els.lang.value);
      applyLangNote();
      sayHello();
    });
    els.voice.addEventListener("change", function () {
      store("voice", els.voice.value);
      state.ttsFails = 0;
      if (els.voice.value !== "device" && state.ttsAvail) state.ttsOn = true;
      sayHello();
    });
    els.keep.addEventListener("change", function () {
      store("keep", els.keep.checked ? "1" : "0");
    });
    els.mute.addEventListener("change", function () {
      store("mute", els.mute.checked ? "1" : "0");
      if (els.mute.checked) cancelSpeech();
    });

    $("btn-history").addEventListener("click", openHistory);
    $("btn-hist-close").addEventListener("click", function () {
      $("dlg-history").close();
    });
    $("btn-hist-clear").addEventListener("click", function () {
      els.log.textContent = "";
      logItems = [];
      saveLog();
      openHistory();
    });
    $("btn-clear-log").addEventListener("click", function () {
      els.log.textContent = "";
      logItems = [];
      saveLog();
    });
    $("btn-close-result").addEventListener("click", function () {
      $("result").hidden = true;
    });
    $("btn-retry").addEventListener("click", function () {
      probeServer().then(function (ok) {
        if (ok) return Promise.all([loadCategories(), refreshAll()]);
      });
    });

    $("period-tabs").addEventListener("click", function (ev) {
      var tab = ev.target.closest(".tab");
      if (!tab) return;
      state.period = tab.getAttribute("data-period");
      document.querySelectorAll(".tab").forEach(function (t) {
        t.setAttribute("aria-pressed", t === tab ? "true" : "false");
      });
      loadSummary().catch(function (err) {
        toast(err.message, "bad");
      });
    });

    $("btn-add-expense").addEventListener("click", function () {
      resetDates();
      showFormError("ex-error", "");
      $("dlg-expense").showModal();
      $("ex-amount").focus();
    });
    $("btn-add-reminder").addEventListener("click", function () {
      resetDates();
      showFormError("rm-error", "");
      $("dlg-reminder").showModal();
      $("rm-title").focus();
    });
    $("btn-add-budget").addEventListener("click", openBudgetDialog);
    $("bg-category").addEventListener("change", toggleBudgetName);
    $("btn-import").addEventListener("click", function () {
      $("file-import").value = "";
      $("file-import").click();
    });
    $("file-import").addEventListener("change", importFile);
    $("btn-bg-cancel").addEventListener("click", function () {
      $("dlg-budget").close();
    });
    $("form-budget").addEventListener("submit", submitBudget);
    $("ex-search").addEventListener("input", function () {
      state.search = $("ex-search").value;
      renderExpenses();
    });
    $("btn-ex-cancel").addEventListener("click", function () {
      $("dlg-expense").close();
    });
    $("btn-rm-cancel").addEventListener("click", function () {
      $("dlg-reminder").close();
    });
    $("form-expense").addEventListener("submit", submitExpense);
    $("form-reminder").addEventListener("submit", submitReminder);
    $("form-edit").addEventListener("submit", submitEdit);
    $("btn-ed-cancel").addEventListener("click", function () {
      $("dlg-edit").close();
    });
    $("btn-del-keep").addEventListener("click", function () {
      $("dlg-del").close();
    });
    $("btn-del-go").addEventListener("click", confirmDelete);
    $("rm-showdone").addEventListener("change", function () {
      loadReminders().catch(function (err) {
        toast(err.message, "bad");
      });
    });

    document.addEventListener("keydown", function (ev) {
      if (ev.key !== "Escape") return;
      if (state.phase === "listening") {
        stopRecognition();
        setPhase("idle");
        setHeard("Stopped listening.", false);
      } else if (state.phase === "speaking") {
        state.turn++;
        cancelSpeech();
        setPhase("idle");
      }
    });

    document.addEventListener("visibilitychange", function () {
      if (document.visibilityState === "hidden") {
        if (state.phase === "listening") {
          stopRecognition();
          setPhase("idle");
        }
        cancelSpeech();
      } else {
        refreshAll();
      }
    });

    if (synth) {
      pickVoice();
      if (typeof synth.addEventListener === "function") synth.addEventListener("voiceschanged", pickVoice);
    }
  }

  function init() {
    restorePrefs();
    restoreLog();
    bind();
    resetDates();
    if (!Recognition) {
      setPhase("off");
      $("support-note").hidden = false;
      setHeard("Voice input isn't supported in this browser. Type a command instead.", false);
    } else {
      setPhase("idle");
    }
    probeServer().then(function (ok) {
      if (!ok) return;
      return loadCategories().then(refreshAll).catch(function (err) {
        toast(err.message, "bad");
      });
    });
    setInterval(checkReminderAlerts, 20000);
    setInterval(resetDates, 5 * 60000);
  }

  init();
})();
