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

  function startCountdown() {
    if (countdownTimer) clearInterval(countdownTimer);
    refreshCountdowns();
    if (document.querySelector("[data-draft-countdown]")) {
      countdownTimer = setInterval(refreshCountdowns, 1000);
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

  // Position-filter fallback: keeps the pool filters working even when the
  // island runtime is not hydrated (for example, a server-only build).
  document.addEventListener("click", function (event) {
    var button = event.target.closest(".position-filters .filter-button");
    if (!button) return;
    var pool = button.closest(".player-pool");
    if (!pool) return;
    var value = (button.textContent || "").trim().toUpperCase();
    pool.setAttribute("data-filter", value);
    all(".position-filters .filter-button", pool).forEach(function (candidate) {
      candidate.setAttribute("aria-pressed", candidate === button ? "true" : "false");
    });
  });

  var leagueFingerprint = null;
  var leagueSyncTimer = null;

  function checkLeagueVersion() {
    if (document.hidden) return;
    var active = document.activeElement;
    if (active && (active.tagName === "INPUT" || active.tagName === "TEXTAREA")) return;
    fetch("/api/league/version", { headers: { Accept: "application/json" } })
      .then(function (response) {
        return response.ok ? response.json() : null;
      })
      .then(function (payload) {
        if (!payload || !payload.fingerprint) return;
        if (leagueFingerprint === null) {
          leagueFingerprint = payload.fingerprint;
          return;
        }
        if (payload.fingerprint === leagueFingerprint) return;
        leagueFingerprint = payload.fingerprint;
        var nav = window.__gosx_page_nav;
        if (nav && typeof nav.revalidate === "function") {
          nav.revalidate();
        } else if (nav && typeof nav.refresh === "function") {
          nav.refresh();
        }
      })
      .catch(function () {});
  }

  function startLeagueSync() {
    if (leagueSyncTimer) clearInterval(leagueSyncTimer);
    leagueFingerprint = null;
    leagueSyncTimer = setInterval(checkLeagueVersion, 4000);
  }

  function bootPageEnhancers() {
    startCountdown();
    startScoreSync();
    startWireSync();
    startLeagueSync();
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
