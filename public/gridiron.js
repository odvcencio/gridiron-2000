(function () {
  "use strict";

  var scoreTimer = null;
  var wireTimer = null;
  var wireCategory = "";
  var wireETag = "";
  var lastScoreSync = 0;
  var syncing = false;

  function all(selector, root) {
    return Array.prototype.slice.call((root || document).querySelectorAll(selector));
  }

  function scoreText(value) {
    var score = Number(value);
    return Number.isFinite(score) ? score.toFixed(1) : "0.0";
  }

  function setText(selector, value, root) {
    all(selector, root).forEach(function (node) {
      node.textContent = value;
    });
  }

  function applySnapshot(snapshot, root) {
    (snapshot.matchups || []).forEach(function (matchup) {
      [matchup.away, matchup.home].forEach(function (team) {
        all('[data-score-team="' + team.id + '"]', root).forEach(function (node) {
          var next = scoreText(team.score);
          if (node.textContent.trim() !== next) {
            node.textContent = next;
            node.classList.remove("score-flash");
            void node.offsetWidth;
            node.classList.add("score-flash");
          }
        });
      });
      all('[data-live-matchup="' + matchup.id + '"]', root).forEach(function (card) {
        setText("[data-matchup-status]", matchup.status || "SYNCED", card);
        setText("[data-matchup-clock]", matchup.clock || "60 SEC", card);
      });
    });
    var updated = new Date(snapshot.lastUpdated || Date.now());
    var timestamp = updated.toLocaleTimeString([], { hour: "numeric", minute: "2-digit", second: "2-digit" });
    var label = (snapshot.sourceLabel || snapshot.source || "Live feed") + " · " + timestamp;
    if (snapshot.warning) label += " · FALLBACK";
    setText("[data-live-status]", label, root);
    setText("[data-live-updated]", timestamp, root);
  }

  function syncScores() {
    var root = document.querySelector("[data-live-root]");
    if (!root || syncing) return Promise.resolve();
    syncing = true;
    root.classList.add("is-syncing");
    setText("[data-live-status]", "Requesting fresh score packet…", root);
    return fetch("/api/live/week", {
      credentials: "same-origin",
      headers: { "Accept": "application/json" },
      cache: "no-store"
    })
      .then(function (response) {
        if (!response.ok) throw new Error("score endpoint returned " + response.status);
        return response.json();
      })
      .then(function (snapshot) {
        applySnapshot(snapshot, root);
        lastScoreSync = Date.now();
      })
      .catch(function () {
        setText("[data-live-status]", "Signal interrupted · retrying automatically", root);
      })
      .finally(function () {
        syncing = false;
        root.classList.remove("is-syncing");
      });
  }

  function startScoreSync() {
    if (scoreTimer) clearInterval(scoreTimer);
    scoreTimer = null;
    if (!document.querySelector("[data-live-root]")) return;
    syncScores();
    scoreTimer = setInterval(function () {
      if (!document.hidden) syncScores();
    }, 60000);
  }

  function wireTime(value) {
    var date = new Date(value);
    if (!Number.isFinite(date.getTime())) return "TIME UNKNOWN";
    return date.toLocaleString([], {
      month: "short",
      day: "2-digit",
      hour: "numeric",
      minute: "2-digit",
      timeZoneName: "short"
    }).toUpperCase();
  }

  function wireNode(tag, className, textValue) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (textValue !== undefined) node.textContent = textValue;
    return node;
  }

  function wireEventNode(event) {
    var category = String(event.category || "signal").replace(/[^a-z0-9_-]/g, "");
    var article = wireNode("article", "wire-event wire-event--" + category + " is-entering");
    article.setAttribute("data-wire-event", event.id || "");
    article.setAttribute("data-wire-category", category);

    var header = wireNode("header");
    var heading = wireNode("div", "wire-event__heading");
    heading.appendChild(wireNode("span", "wire-event__label", event.label || "SIGNAL"));
    heading.appendChild(wireNode("span", "wire-event__evidence", String(event.evidence_type || event.source || "source").replace(/_/g, " ").toUpperCase()));
    header.appendChild(heading);
    header.appendChild(wireNode("span", "wire-event__trust mono", (event.trust_tier || "PROVISIONAL") + " · " + Math.round(Number(event.confidence || 0) * 100) + "%"));
    article.appendChild(header);
    article.appendChild(wireNode("p", "", event.text || "Source post removed."));

    var footer = wireNode("footer");
    footer.appendChild(wireNode("span", "mono", event.source_name || event.source_handle || event.source_did || "League source"));
    if (event.reported_by) {
      footer.appendChild(wireNode("span", "mono", "VIA " + event.reported_by));
    }
    if (Number(event.corroborations || 0) > 1) {
      footer.appendChild(wireNode("span", "wire-event__corroboration mono", event.corroborations + " SOURCES"));
    }
    footer.appendChild(wireNode("time", "mono", wireTime(event.occurred_at)));
    if (event.source_url) {
      var link = wireNode("a", "", "Inspect source ↗");
      link.href = event.source_url;
      link.target = "_blank";
      link.rel = "noreferrer";
      footer.appendChild(link);
    }
    article.appendChild(footer);
    return article;
  }

  function renderWireEvents(payload, root) {
    var list = root.querySelector("[data-wire-list]");
    if (!list) return;
    var events = payload.events || [];
    list.replaceChildren();
    events.forEach(function (event) {
      list.appendChild(wireEventNode(event));
    });
    var empty = root.querySelector("[data-wire-empty]");
    if (empty) empty.hidden = events.length > 0;
    if (events.length === 0 && !empty) {
      empty = wireNode("div", "wire-empty");
      empty.setAttribute("data-wire-empty", "");
      empty.appendChild(wireNode("span", "mono", "NO SIGNALS IN THIS CHANNEL"));
      list.before(empty);
    }
    setText("[data-wire-count]", String((payload.status && payload.status.relevant_signals) || events.length), root);
    setText("[data-wire-mode]", String((payload.status && payload.status.mode) || "streaming").replace(/_/g, " ").toUpperCase(), root);
    var status = events.length + " relevant packet" + (events.length === 1 ? "" : "s") + " · " + wireTime(new Date().toISOString());
    setText("[data-wire-status]", status, root);
    root.setAttribute("data-wire-last", events[0] ? events[0].id : "");
  }

  function syncWire() {
    var root = document.querySelector("[data-wire-root]");
    if (!root) return Promise.resolve();
    var statusNode = root.querySelector("[data-wire-status]");
    if (statusNode) statusNode.textContent = "Checking the signal channel…";
    var headers = { "Accept": "application/json" };
    if (wireETag) headers["If-None-Match"] = wireETag;
    var query = wireCategory ? "&category=" + encodeURIComponent(wireCategory) : "";
    return fetch("/api/wire/events?limit=50" + query, {
      credentials: "same-origin",
      headers: headers,
      cache: "no-store"
    })
      .then(function (response) {
        if (response.status === 304) {
          if (statusNode) statusNode.textContent = "Channel current · no new packets";
          return null;
        }
        if (!response.ok) throw new Error("wire endpoint returned " + response.status);
        wireETag = response.headers.get("ETag") || "";
        return response.json();
      })
      .then(function (payload) {
        if (payload) renderWireEvents(payload, root);
      })
      .catch(function () {
        if (statusNode) statusNode.textContent = "Signal check interrupted · retrying";
      });
  }

  function startWireSync() {
    if (wireTimer) clearInterval(wireTimer);
    wireTimer = null;
    if (!document.querySelector("[data-wire-root]")) return;
    syncWire();
    wireTimer = setInterval(function () {
      if (!document.hidden) syncWire();
    }, 20000);
  }

  var poolSearchTimer = null;

  function applyPoolSearch(input) {
    var query = input.value.trim().toLowerCase();
    all(".pool-row").forEach(function (row) {
      var haystack = row.getAttribute("data-search") || "";
      if (query && haystack.indexOf(query) === -1) {
        row.setAttribute("data-search-miss", "");
      } else {
        row.removeAttribute("data-search-miss");
      }
    });
  }

  document.addEventListener("input", function (event) {
    var input = event.target.closest("[data-pool-search]");
    if (!input) return;
    if (poolSearchTimer) clearTimeout(poolSearchTimer);
    poolSearchTimer = setTimeout(function () {
      applyPoolSearch(input);
    }, 80);
  });

  var presenceHeartbeatTimer = null;

  // GoSX v0.42's declarative revalidation (data-gosx-revalidate-interval /
  // data-gosx-revalidate-src on each league page's <main>) now owns the
  // fingerprint poll and the page refresh; the old JS poller above is gone.
  // That same runtime poll hits /api/league/version with the session
  // cookie, so the server's RecordPresence call in the version handler
  // still fires for every tick — except the runtime skips its entire tick,
  // fetch included, while an input, textarea, or select has focus. A
  // manager typing in the pool search would then stop sending any request
  // at all and drift to AWAY. This loop exists only to close that one gap:
  // it fires a bare fetch, and only while a control is focused, so it never
  // duplicates the runtime's own poll.
  function focusedControlActive() {
    var active = document.activeElement;
    if (!active) return false;
    switch (String(active.tagName || "").toUpperCase()) {
      case "INPUT":
      case "TEXTAREA":
      case "SELECT":
        return true;
      default:
        return false;
    }
  }

  function sendPresenceHeartbeat() {
    if (document.hidden || !focusedControlActive()) return;
    fetch("/api/league/version", { credentials: "same-origin", headers: { Accept: "application/json" } }).catch(function () {});
  }

  function startPresenceHeartbeat() {
    if (presenceHeartbeatTimer) clearInterval(presenceHeartbeatTimer);
    presenceHeartbeatTimer = setInterval(function () {
      sendPresenceHeartbeat();
      evalDraftClockAlert();
    }, 4000);
  }

  // ---- Draft-room on-clock alert + pick-clock urgency ----------------
  //
  // Rides presenceHeartbeatTimer's existing 4-second tick above instead of
  // starting a new timer loop: evalDraftClockAlert() runs there, plus once
  // at boot/navigate below, so it re-evaluates on the same cadence this
  // file already uses for its sync polling.
  //
  // The draft page carries no dedicated "viewer team" or "on-clock team"
  // data attribute, so this reads the two hidden team_id inputs the page
  // already renders for other reasons: the "Toggle my ready state" form
  // (data.viewer.team_id — see app/draft/page.gsx) identifies the viewer's
  // own seat, and any pick-form row in the player pool (data.on_clock_id,
  // repeated once per row) identifies the team currently on the clock.

  var draftClockState = {
    initialized: false,
    onClock: false,
    originalTitle: null,
    titleFlashTimer: null,
    under10Beeped: false
  };

  var draftAudioCtx = null;

  function ensureDraftAudioContext() {
    if (draftAudioCtx) return draftAudioCtx;
    var Ctor = window.AudioContext || window.webkitAudioContext;
    if (!Ctor) return null;
    try {
      draftAudioCtx = new Ctor();
    } catch (err) {
      draftAudioCtx = null;
    }
    return draftAudioCtx;
  }

  function primeDraftAudioContext() {
    var ctx = ensureDraftAudioContext();
    if (ctx && ctx.state === "suspended") ctx.resume().catch(function () {});
  }

  document.addEventListener("pointerdown", primeDraftAudioContext, { once: true });
  document.addEventListener("keydown", primeDraftAudioContext, { once: true });

  // playDraftTone schedules one short sine-wave tone starting startOffset
  // seconds from now, ramping in and out (no click), lasting duration
  // seconds. No audio asset files: every tone is synthesized.
  function playDraftTone(ctx, freq, startOffset, duration) {
    var osc = ctx.createOscillator();
    var gain = ctx.createGain();
    osc.type = "sine";
    osc.frequency.value = freq;
    var startAt = ctx.currentTime + startOffset;
    gain.gain.setValueAtTime(0.0001, startAt);
    gain.gain.exponentialRampToValueAtTime(0.2, startAt + 0.02);
    gain.gain.exponentialRampToValueAtTime(0.0001, startAt + duration);
    osc.connect(gain);
    gain.connect(ctx.destination);
    osc.start(startAt);
    osc.stop(startAt + duration + 0.05);
  }

  // playOnClockChime plays two quick rising tones: the "it's your pick"
  // alert. A missing or blocked AudioContext is a silent no-op — the
  // title flash and the is-on-clock class still carry the alert.
  function playOnClockChime() {
    var ctx = ensureDraftAudioContext();
    if (!ctx) return;
    if (ctx.state === "suspended") ctx.resume().catch(function () {});
    playDraftTone(ctx, 660, 0, 0.12);
    playDraftTone(ctx, 880, 0.14, 0.16);
  }

  // playCountdownBeep plays the single short beep fired once when the
  // pick clock first crosses under 10 seconds.
  function playCountdownBeep() {
    var ctx = ensureDraftAudioContext();
    if (!ctx) return;
    if (ctx.state === "suspended") ctx.resume().catch(function () {});
    playDraftTone(ctx, 740, 0, 0.1);
  }

  function restoreDraftTitle() {
    if (draftClockState.titleFlashTimer) {
      clearInterval(draftClockState.titleFlashTimer);
      draftClockState.titleFlashTimer = null;
    }
    if (draftClockState.originalTitle !== null) {
      document.title = draftClockState.originalTitle;
    }
  }

  function flashDraftTitle(message) {
    if (draftClockState.originalTitle === null) draftClockState.originalTitle = document.title;
    if (draftClockState.titleFlashTimer) clearInterval(draftClockState.titleFlashTimer);
    var showingAlert = true;
    document.title = message;
    draftClockState.titleFlashTimer = setInterval(function () {
      showingAlert = !showingAlert;
      document.title = showingAlert ? message : draftClockState.originalTitle;
    }, 1000);
  }

  window.addEventListener("focus", restoreDraftTitle);

  function draftViewerTeamID() {
    var input = document.querySelector('form[action*="toggle-ready"] input[name="team_id"]');
    return input ? input.value : "";
  }

  function draftOnClockTeamID() {
    var input = document.querySelector('.pool-list form input[name="team_id"]');
    return input ? input.value : null;
  }

  // evalOnClockWatch fires once per turn-transition when the viewer's own
  // seat becomes on-clock: a chime, a flashing document title, and an
  // is-on-clock class on <body> the stylesheet uses for a restrained glow
  // on the pick-clock panel. Leaving the draft page (no viewer marker)
  // resets the baseline so returning to it never replays a stale alert.
  function evalOnClockWatch() {
    var viewerID = draftViewerTeamID();
    if (!viewerID) {
      if (draftClockState.initialized) {
        draftClockState.initialized = false;
        draftClockState.onClock = false;
        document.body.classList.remove("is-on-clock");
        restoreDraftTitle();
      }
      return;
    }
    var onClockID = draftOnClockTeamID();
    var isOnClock = onClockID !== null && onClockID === viewerID;
    var wasOnClock = draftClockState.onClock;
    var justInitialized = !draftClockState.initialized;
    draftClockState.initialized = true;
    draftClockState.onClock = isOnClock;
    document.body.classList.toggle("is-on-clock", isOnClock);
    if (justInitialized || isOnClock === wasOnClock) return;
    if (isOnClock) {
      playOnClockChime();
      flashDraftTitle("YOUR PICK IS ON THE CLOCK");
    } else {
      restoreDraftTitle();
    }
  }

  // parsePickClockSeconds reads the pick-clock countdown's rendered m:ss
  // text (data-gosx-countdown-format="mm:ss" — see app/draft/page.gsx)
  // back into a whole-second count.
  function parsePickClockSeconds(text) {
    var match = /^(\d+):(\d{2})$/.exec(String(text || "").trim());
    if (!match) return null;
    return Number(match[1]) * 60 + Number(match[2]);
  }

  // evalPickClockCountdown adds pick-clock--warn under 30 seconds and
  // fires one short beep the first time the clock crosses under 10.
  function evalPickClockCountdown() {
    var node = document.querySelector(".pick-clock[data-gosx-countdown]");
    if (!node) {
      draftClockState.under10Beeped = false;
      return;
    }
    var seconds = parsePickClockSeconds(node.textContent);
    if (seconds === null) return;
    node.classList.toggle("pick-clock--warn", seconds < 30);
    if (seconds < 10) {
      if (!draftClockState.under10Beeped) {
        draftClockState.under10Beeped = true;
        playCountdownBeep();
      }
    } else {
      draftClockState.under10Beeped = false;
    }
  }

  function evalDraftClockAlert() {
    evalOnClockWatch();
    evalPickClockCountdown();
  }

  function bootPageEnhancers() {
    startScoreSync();
    startWireSync();
    startPresenceHeartbeat();
    evalDraftClockAlert();
  }

  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-live-refresh]");
    if (!button) return;
    event.preventDefault();
    syncScores();
  });

  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-wire-filter]");
    if (!button) return;
    event.preventDefault();
    wireCategory = button.getAttribute("data-wire-filter") || "";
    wireETag = "";
    all("[data-wire-filter]").forEach(function (candidate) {
      candidate.classList.toggle("is-active", candidate === button);
    });
    syncWire();
  });

  document.addEventListener("visibilitychange", function () {
    if (!document.hidden && document.querySelector("[data-live-root]") && Date.now() - lastScoreSync > 60000) {
      syncScores();
    }
    if (!document.hidden && document.querySelector("[data-wire-root]")) {
      syncWire();
    }
  });

  document.addEventListener("gosx:navigate", bootPageEnhancers);
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", bootPageEnhancers, { once: true });
  } else {
    bootPageEnhancers();
  }
})();
