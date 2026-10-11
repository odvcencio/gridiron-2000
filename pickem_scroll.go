package main

// WebKit clamps the document's scroll offset while a live region replaces
// its subtree. GoSX preserves the region's own scroller, but Pick'em scrolls
// the document. Capture the viewport at the synchronous DOM swap, after the
// response arrives, so scrolling during the request is also respected.
const pickemScrollScript = `(function(){
  var positions = new WeakMap();
  document.addEventListener("gosx:runtime:content:before", function(event){
    var target = event.detail && event.detail.target;
    if (target && target.id === "pickem-live-region" && target.isConnected) {
      positions.set(target, {x: window.scrollX, y: window.scrollY});
    }
  });
  function restore(event){
    var target = event.detail && event.detail.target;
    if (!target) { return; }
    var position = positions.get(target);
    positions.delete(target);
    if (position && target.isConnected) {
      window.scrollTo({left: position.x, top: position.y, behavior: "instant"});
    }
  }
  document.addEventListener("gosx:runtime:content:after", restore);
  document.addEventListener("gosx:runtime:content:error", restore);
})();`
