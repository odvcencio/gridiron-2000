(function () {
  "use strict";

  var scoreTimer = null;
  var countdownTimer = null;
  var wireTimer = null;
  var wireCategory = "";
  var wireETag = "";
  var lastScoreSync = 0;
  var syncing = false;

  function all(selector, root) {
    return Array.prototype.slice.call((root || document).querySelectorAll(selector));
  }

  function countdownLabel(target) {
    var remaining = target.getTime() - Date.now();
    if (!Number.isFinite(remaining)) return "DATE OFFLINE";
    if (remaining <= 0) return "ROOM OPEN";
    var seconds = Math.floor(remaining / 1000);
    var days = Math.floor(seconds / 86400);
    var hours = Math.floor((seconds % 86400) / 3600);
    var minutes = Math.floor((seconds % 3600) / 60);
    var secs = seconds % 60;
    return [
      String(days).padStart(2, "0") + "D",
      String(hours).padStart(2, "0") + "H",
      String(minutes).padStart(2, "0") + "M",
      String(secs).padStart(2, "0") + "S"
    ].join(" : ");
  }

  function refreshCountdowns() {
    all("[data-draft-at]").forEach(function (root) {
      var value = root.getAttribute("data-draft-at");
      var target = new Date(value);
      all("[data-draft-countdown]", root).forEach(function (node) {
        node.textContent = countdownLabel(target);
      });
    });
  }

  // Pick-clock urgency thresholds (see internal/league draftclock.go).
  var PICK_CLOCK_WARN_AT = 30;
  var PICK_CLOCK_CRITICAL_AT = 15;

  var userInteracted = false;
  ["pointerdown", "keydown"].forEach(function (type) {
    document.addEventListener(type, function () { userInteracted = true; }, { once: true });
  });

  var pickClockBeeped = {};

  function playPickClockBeep() {
    try {
      var Ctx = window.AudioContext || window.webkitAudioContext;
      if (!Ctx) return;
      var ctx = new Ctx();
      var osc = ctx.createOscillator();
      var gain = ctx.createGain();
      osc.frequency.value = 880;
      gain.gain.setValueAtTime(0.0001, ctx.currentTime);
      gain.gain.exponentialRampToValueAtTime(0.2, ctx.currentTime + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, ctx.currentTime + 0.28);
      osc.connect(gain);
      gain.connect(ctx.destination);
      osc.start();
      osc.stop(ctx.currentTime + 0.3);
    } catch (err) {
      // WebAudio is best-effort; a blocked or unsupported context is silent, not fatal.
    }
  }

  function pickClockLabel(remainingSeconds) {
    var minutes = Math.floor(remainingSeconds / 60);
    var seconds = remainingSeconds % 60;
    return minutes + ":" + String(seconds).padStart(2, "0");
  }

  function setClockUrgency(elements, warn, critical) {
    elements.forEach(function (el) {
      if (!el) return;
      el.classList.toggle("is-warning", warn);
      el.classList.toggle("is-critical", critical);
    });
  }

  function refreshPickClocks() {
    var nodes = all("[data-pick-clock]");
    if (nodes.length === 0) return;
    var pageRoot = document.querySelector(".draft-page");
    var onClockCard = document.querySelector('[data-on-clock="true"]');
    nodes.forEach(function (node) {
      var paused = node.getAttribute("data-paused") === "true";
      var armed = node.getAttribute("data-armed") === "true";
      if (paused) {
        node.textContent = "PAUSED";
        setClockUrgency([node, pageRoot, onClockCard], false, false);
        return;
      }
      if (!armed) {
        node.textContent = "—:—";
        setClockUrgency([node, pageRoot, onClockCard], false, false);
        return;
      }
      var deadlineValue = node.getAttribute("data-deadline");
      var deadline = new Date(deadlineValue);
      var serverNow = new Date(node.getAttribute("data-server-now"));
      if (!Number.isFinite(deadline.getTime()) || !Number.isFinite(serverNow.getTime())) {
        node.textContent = "--:--";
        return;
      }
      // Offset corrects for client clock skew: the server's own "now" at
      // render time anchors the local Date.now() ticks that follow.
      var offset = serverNow.getTime() - Date.now();
      var remainingMs = deadline.getTime() - (Date.now() + offset);
      var remainingSeconds = Math.max(0, Math.ceil(remainingMs / 1000));
      node.textContent = pickClockLabel(remainingSeconds);
      var warn = remainingSeconds <= PICK_CLOCK_WARN_AT && remainingSeconds > PICK_CLOCK_CRITICAL_AT;
      var critical = remainingSeconds <= PICK_CLOCK_CRITICAL_AT;
      setClockUrgency([node, pageRoot, onClockCard], warn, critical);
      if (critical && userInteracted && !pickClockBeeped[deadlineValue]) {
        pickClockBeeped[deadlineValue] = true;
        playPickClockBeep();
      }
    });
  }

  function tickClocks() {
    refreshCountdowns();
    refreshPickClocks();
  }

  function startCountdown() {
    if (countdownTimer) clearInterval(countdownTimer);
    countdownTimer = null;
    tickClocks();
    if (document.querySelector("[data-draft-countdown]") || document.querySelector("[data-pick-clock]")) {
      countdownTimer = setInterval(tickClocks, 1000);
    }
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
    presenceHeartbeatTimer = setInterval(sendPresenceHeartbeat, 4000);
  }

  function bootPageEnhancers() {
    startCountdown();
    startScoreSync();
    startWireSync();
    startPresenceHeartbeat();
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
