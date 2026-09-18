package main

// lineupDragPreviewScript floats a preview chip under the pointer while a
// Team terminal grip is dragged. The GoSX transfer runtime captures the
// pointer on the grip and moves nothing on screen, so without this a drag
// had no visible object and no word on where a release would land (owner
// report 2026-09-18). The chip names the player; over a slot it names the
// slot a release assigns him to. It reads only attributes the page already
// renders (data-lineup-name, data-lineup-position on the source; the
// runtime's own gosx-transfer--active and gosx-transfer-target--over
// classes), acts only after the runtime has actually started a drag, and
// removes itself on release or cancel. It rides the navigation head with
// the page's CSP nonce, like the navigation runtime tag beside it.
const lineupDragPreviewScript = `(function(){
  var ghost = null, source = null, frame = 0;
  function clear(){ if (frame) { window.cancelAnimationFrame(frame); frame = 0; } if (ghost && ghost.parentNode) { ghost.parentNode.removeChild(ghost); } ghost = null; source = null; }
  function label(){
    var name = source.getAttribute("data-lineup-name") || "Player";
    var pos = source.getAttribute("data-lineup-position") || "";
    var over = document.querySelector(".gosx-transfer-target--over");
    var slot = over ? over.getAttribute("data-gosx-transfer-target") : "";
    ghost.setAttribute("data-over", slot ? "true" : "false");
    ghost.textContent = (pos ? pos + " · " : "") + name + (slot ? " → " + slot : "");
  }
  document.addEventListener("pointerdown", function(ev){
    var handle = ev.target && ev.target.closest && ev.target.closest("[data-gosx-transfer-handle]");
    if (!handle) { return; }
    var found = handle.closest("[data-gosx-transfer-source]");
    if (!found || found.getAttribute("data-gosx-transfer-disabled") === "true") { return; }
    source = found;
  }, true);
  var lastX = 0, lastY = 0;
  function place(){
    frame = 0;
    if (!ghost || !source) { return; }
    label();
    var w = ghost.offsetWidth, h = ghost.offsetHeight, pad = 8;
    var x = lastX + 14, y = lastY + 14;
    if (x + w > window.innerWidth - pad) { x = lastX - 14 - w; }
    if (y + h > window.innerHeight - pad) { y = lastY - 14 - h; }
    x = Math.max(pad, x); y = Math.max(pad, y);
    ghost.style.transform = "translate(" + x + "px," + y + "px)";
  }
  document.addEventListener("pointermove", function(ev){
    if (!source) { return; }
    var root = source.closest("[data-gosx-transfer]");
    if (!root || !root.classList.contains("gosx-transfer--active")) { return; }
    if (!ghost) {
      ghost = document.createElement("div");
      ghost.className = "lineup-drag-ghost";
      ghost.setAttribute("aria-hidden", "true");
      document.body.appendChild(ghost);
    }
    lastX = ev.clientX; lastY = ev.clientY;
    // The runtime updates the hovered slot in its own pointermove handler;
    // reading it on the next frame names the slot under the finger now,
    // not the one it just left.
    if (!frame) { frame = window.requestAnimationFrame(place); }
  }, true);
  document.addEventListener("pointerup", clear, true);
  document.addEventListener("pointercancel", clear, true);
  document.addEventListener("keydown", function(ev){ if (ev.key === "Escape") { clear(); } }, true);
})();`
