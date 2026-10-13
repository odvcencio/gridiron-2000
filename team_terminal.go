package main

// Team's live fragment contains native editing controls. Hold a background
// replacement until the manager finishes with them; a DOM replacement cannot
// restore an open native select. Read-only disclosures and the document
// viewport survive ordinary refreshes, keyed to the same team and week.
const teamTerminalScript = `(function(){
  var pending = new Map(), snapshots = new WeakMap(), drafts = new WeakSet();
  var pointer = null;
  function isTeam(target){
    return target && target.classList && target.classList.contains("team-lineup-sync");
  }
  function context(target){
    var root = target.querySelector(".team-lineup-region");
    return root && root.getAttribute("data-gosx-transfer-context");
  }
  function editing(target){
    if (pointer && target.contains(pointer)) { return true; }
    var active = document.activeElement;
    if (active && target.contains(active) && active.matches("input, select, textarea, button")) { return true; }
    if (target.querySelector(".lineup-move-disclosure[open], .action-confirmation[open]")) { return true; }
    return Array.from(target.querySelectorAll("form")).some(function(form){ return drafts.has(form); });
  }
  function install(){
    var dom = window.__gosx && window.__gosx.host && window.__gosx.host.dom;
    if (!dom || typeof dom.replace !== "function" || dom.replace.gridironTeam) { return; }
    var replace = dom.replace;
    dom.replace = function(target, html){
      if (isTeam(target) && target.isConnected && editing(target)) {
        pending.set(target, {html: html, context: context(target)});
        return true;
      }
      pending.delete(target);
      return replace.apply(this, arguments);
    };
    dom.replace.gridironTeam = true;
  }
  function flush(){
    pending.forEach(function(update, target){
      if (!target.isConnected || context(target) !== update.context) { pending.delete(target); return; }
      if (editing(target)) { return; }
      pending.delete(target);
      window.__gosx.host.dom.replace(target, update.html);
    });
  }
  document.addEventListener("gosx:region:before", function(event){
    if (isTeam(event.detail && event.detail.element)) { install(); }
  });
  document.addEventListener("pointerdown", function(event){ pointer = event.target; }, true);
  document.addEventListener("pointerup", function(){ pointer = null; flush(); }, true);
  document.addEventListener("pointercancel", function(){ pointer = null; flush(); }, true);
  document.addEventListener("focusout", function(){ queueMicrotask(flush); });
  document.addEventListener("toggle", flush, true);
  document.addEventListener("change", function(event){
    var form = event.target && event.target.closest && event.target.closest("form");
    if (form && form.closest(".team-lineup-sync") && !form.closest("details")) { drafts.add(form); }
  });
  document.addEventListener("submit", function(event){
    var form = event.target;
    var region = form && form.closest && form.closest(".team-lineup-sync");
    if (region) { drafts.delete(form); pending.delete(region); }
  }, true);
  document.addEventListener("reset", function(event){
    drafts.delete(event.target);
    queueMicrotask(flush);
  });
  document.addEventListener("gosx:runtime:content:before", function(event){
    var target = event.detail && event.detail.target;
    if (target && !isTeam(target) && target.querySelector && target.querySelector(".team-lineup-sync")) { pending.clear(); }
    if (!isTeam(target) || !target.isConnected) { return; }
    var open = new Map();
    target.querySelectorAll("details[data-team-disclosure]").forEach(function(details){
      open.set(details.getAttribute("data-team-disclosure"), details.open);
    });
    var active = document.activeElement;
    var focus = active && target.contains(active) && active.matches("summary")
      ? active.parentElement.getAttribute("data-team-disclosure") : null;
    snapshots.set(target, {context: context(target), open: open, focus: focus, x: window.scrollX, y: window.scrollY});
  });
  function restore(event){
    var target = event.detail && event.detail.target;
    if (!target) { return; }
    var snapshot = snapshots.get(target);
    snapshots.delete(target);
    if (!snapshot || !target.isConnected || snapshot.context !== context(target)) { return; }
    target.querySelectorAll("details[data-team-disclosure]").forEach(function(details){
      var key = details.getAttribute("data-team-disclosure");
      if (snapshot.open.has(key)) { details.open = snapshot.open.get(key); }
      if (snapshot.focus && key === snapshot.focus) { details.querySelector("summary").focus({preventScroll: true}); }
    });
    window.scrollTo({left: snapshot.x, top: snapshot.y, behavior: "instant"});
  }
  document.addEventListener("gosx:runtime:content:after", restore);
  document.addEventListener("gosx:runtime:content:error", restore);
})();`
